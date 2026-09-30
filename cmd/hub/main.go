// Command hub runs the DocTheRepo Hub server. One binary serves three roles, selected with --roles or
// DTH_ROLES: api (HTTP + UI), worker (job handlers), scheduler (leader-elected maintenance). Locally all
// three run in one process; in the cloud they run as separate services from the same image.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/adapters/secrets/awskms"
	"github.com/GokulMV/DocTheRepo/internal/adapters/secrets/gcpkms"
	"github.com/GokulMV/DocTheRepo/internal/adapters/secrets/localfile"
	"github.com/GokulMV/DocTheRepo/internal/api"
	"github.com/GokulMV/DocTheRepo/internal/config"
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/queue"
	"github.com/GokulMV/DocTheRepo/internal/scheduler"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/webui"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	// `dth-hub healthcheck` probes /readyz on the local listener (container healthchecks; the image has no curl).
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	cfgPath := flag.String("config", envOr("DTH_CONFIG", "dth.yaml"), "path to the bootstrap config file (optional)")
	roles := flag.String("roles", "", "comma-separated roles to run (api,worker,scheduler); overrides config")
	flag.Parse()
	if *roles != "" {
		os.Setenv("DTH_ROLES", *roles)
	}
	if err := run(*cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, "dth-hub:", err)
		os.Exit(1)
	}
}

func run(cfgPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	log := observability.NewLogger(os.Stderr, cfg.Logging.Format, cfg.Logging.Level)
	slog.SetDefault(log)
	log.Info("starting", "version", version, "roles", cfg.Roles)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, store.Options{
		URL: cfg.Database.URL(), MaxConns: cfg.Database.MaxConns,
		ConnectTimeout: cfg.Database.ConnectTimeout, StatementTimeout: cfg.Database.StatementTimeout,
	})
	if err != nil {
		return err
	}
	defer st.Close()
	if cfg.Database.MigrateOnStart {
		v, err := st.Migrate(ctx)
		if err != nil {
			return err
		}
		log.Info("database migrated", "version", v)
	}

	// Loaded eagerly so a missing or unreadable master key fails at startup, not on first use.
	box, err := openSecrets(ctx, cfg.Secrets)
	if err != nil {
		return err
	}

	metrics := observability.NewMetrics()
	q := queue.New(st, queue.Options{
		MaxAttempts: cfg.Queue.MaxAttempts, BackoffBase: cfg.Queue.BackoffBase, BackoffMax: cfg.Queue.BackoffMax,
	})
	a, err := wire(ctx, cfg, st, box, q, log, metrics)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	errc := make(chan error, 4)

	if cfg.HasRole(config.RoleWorker) {
		conc := map[ports.JobType]int{}
		for k, v := range cfg.Queue.Concurrency {
			conc[ports.JobType(k)] = v
		}
		pool := queue.NewPool(q, st.Pool, queue.PoolOptions{
			Concurrency: conc, LeaseTTL: cfg.Queue.LeaseTTL, PollInterval: cfg.Queue.PollInterval,
		}, log.With("component", "worker"), metrics)
		a.registerHandlers(pool)
		wg.Add(2)
		go func() { defer wg.Done(); pool.Run(ctx) }()
		go func() { defer wg.Done(); a.streams(log).Run(ctx, time.Minute) }()
		if !cfg.HasRole(config.RoleScheduler) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				t := time.NewTicker(time.Minute)
				defer t.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-t.C:
						if err := a.reloadGuard(ctx); err != nil {
							log.Warn("reload spend limits failed", "err", err)
						}
					}
				}
			}()
		}
	}

	if cfg.HasRole(config.RoleAPI) || cfg.HasRole(config.RoleWorker) {
		// Signal ingest runs wherever events arrive (webhooks on api, stream consumers and pollers on worker).
		if err := a.signals.Reload(ctx); err != nil {
			log.Warn("load known-issue rules failed; ingesting without them until the next reload", "err", err)
		}
		wg.Add(2)
		go func() { defer wg.Done(); a.agg.Run(ctx) }()
		go func() {
			defer wg.Done()
			t := time.NewTicker(30 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if err := a.signals.Reload(ctx); err != nil {
						log.Warn("reload known-issue rules failed", "err", err)
					}
					metrics.SignalPending.Set(float64(a.agg.Pending()))
				}
			}
		}()
	}

	if cfg.HasRole(config.RoleScheduler) {
		sch := scheduler.New(st.Pool, log, 10*time.Second)
		sch.Add(scheduler.Task{Name: "reclaim_expired_leases", Every: 30 * time.Second, RunFirst: true, Fn: func(ctx context.Context) error {
			rows, err := q.ReclaimExpired(ctx)
			for _, r := range rows {
				log.Warn("reclaimed job with expired lease", "job_id", r.ID, "job_type", r.Type, "new_status", r.Status)
			}
			return err
		}})
		sch.Add(scheduler.Task{Name: "ensure_partitions", Every: 24 * time.Hour, RunFirst: true, Fn: func(ctx context.Context) error {
			_, err := st.Pool.Exec(ctx, "SELECT ensure_month_partitions('usage_events', 3)")
			return err
		}})
		sch.Add(scheduler.Task{Name: "queue_depth_metric", Every: 15 * time.Second, RunFirst: true, Fn: func(ctx context.Context) error {
			d, err := q.Depth(ctx)
			if err != nil {
				return err
			}
			for _, t := range ports.AllJobTypes {
				metrics.QueueDepth.WithLabelValues(string(t)).Set(float64(d[t]))
			}
			return nil
		}})
		for _, t := range a.tasks() {
			sch.Add(t)
		}
		wg.Add(1)
		go func() { defer wg.Done(); sch.Run(ctx) }()
	}

	var servers []*http.Server
	if cfg.HasRole(config.RoleAPI) {
		h := api.NewRouter(api.Deps{Log: log, Metrics: metrics, Checks: a.readiness(), Git: a.ingest, Signals: a.signals, Auth: a.auth, OIDC: a.oidc, SecureCookies: strings.HasPrefix(cfg.Server.PublicURL, "https://"),
			V1: a.v1Routes(), UI: webui.Handler()})
		servers = append(servers, &http.Server{
			Addr: cfg.Server.Listen, Handler: h,
			ReadHeaderTimeout: 10 * time.Second, ReadTimeout: cfg.Server.ReadTimeout, WriteTimeout: cfg.Server.WriteTimeout,
		})
	}
	if cfg.Server.MetricsListen != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", metrics.Handler())
		// Liveness for roles without the API listener (Cloud Run and Kubernetes probe worker/scheduler here).
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
		servers = append(servers, &http.Server{Addr: cfg.Server.MetricsListen, Handler: mux, ReadHeaderTimeout: 10 * time.Second})
	}
	for _, s := range servers {
		wg.Add(1)
		go func(s *http.Server) {
			defer wg.Done()
			log.Info("listening", "addr", s.Addr)
			if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errc <- fmt.Errorf("listen %s: %w", s.Addr, err)
			}
		}(s)
	}

	var runErr error
	select {
	case <-ctx.Done():
		log.Info("shutting down")
	case runErr = <-errc:
		log.Error("fatal server error; shutting down", "err", runErr)
		stop()
	}
	sctx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownGrace)
	defer cancel()
	for _, s := range servers {
		_ = s.Shutdown(sctx)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-sctx.Done():
		log.Warn("shutdown grace period elapsed; exiting with work in flight (leases will be reclaimed)")
	}
	return runErr
}

func openSecrets(ctx context.Context, c config.SecretsConfig) (*secrets.Box, error) {
	var (
		kek ports.KeyEncrypter
		err error
	)
	switch c.Provider {
	case "localfile":
		if v := os.Getenv("DTH_LOCAL_KEY"); v != "" {
			kek, err = localfile.FromBase64(v)
		} else {
			kek, err = localfile.Open(expandHome(c.LocalKeyFile))
		}
	case "awskms":
		kek, err = awskms.New(ctx, c.KMSKeyID)
	case "gcpkms":
		kek, err = gcpkms.New(ctx, c.KMSKeyID)
	default:
		return nil, fmt.Errorf("secrets.provider %q: use localfile, awskms, or gcpkms", c.Provider)
	}
	if err != nil {
		return nil, err
	}
	box := secrets.NewBox(kek)
	// Prove the key works at startup (a missing KMS permission fails here, not on the first secret).
	probe, err := box.Seal(ctx, []byte("probe"), []byte("startup"))
	if err == nil {
		_, err = box.Open(ctx, probe, []byte("startup"))
	}
	if err != nil {
		return nil, fmt.Errorf("secrets provider %s is not usable: %w", c.Provider, err)
	}
	return box, nil
}

// healthcheck probes the local process: /readyz on the API listener, or /healthz on the metrics listener
// when this container runs only worker/scheduler roles (distroless images have no curl).
func healthcheck() int {
	addr, path := envOr("DTH_LISTEN", "127.0.0.1:8080"), "/readyz"
	if roles := os.Getenv("DTH_ROLES"); roles != "" && !strings.Contains(roles, "api") {
		addr, path = envOr("DTH_METRICS_LISTEN", "127.0.0.1:9090"), "/healthz"
	}
	if strings.HasPrefix(addr, "0.0.0.0:") || strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1:" + addr[strings.LastIndex(addr, ":")+1:]
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + addr + path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: status", resp.StatusCode)
		return 1
	}
	return 0
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return h + p[1:]
		}
	}
	return p
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
