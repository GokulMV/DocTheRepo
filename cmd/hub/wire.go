package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/GokulMV/DocTheRepo/internal/adapters/codehost"
	"github.com/GokulMV/DocTheRepo/internal/adapters/llm"
	"github.com/GokulMV/DocTheRepo/internal/adapters/push"
	"github.com/GokulMV/DocTheRepo/internal/adapters/push/lifecycle"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/awsbus"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/cloudwatch"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/gcplogging"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/kafka"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/pubsub"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/pubsubbus"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/rabbitmq"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/registry"
	"github.com/GokulMV/DocTheRepo/internal/adapters/vector/pgvector"
	"github.com/GokulMV/DocTheRepo/internal/adapters/vector/qdrant"
	"github.com/GokulMV/DocTheRepo/internal/api"
	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/config"
	"github.com/GokulMV/DocTheRepo/internal/core/aggregate"
	"github.com/GokulMV/DocTheRepo/internal/core/docgen"
	"github.com/GokulMV/DocTheRepo/internal/core/grammars"
	"github.com/GokulMV/DocTheRepo/internal/core/library"
	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/manifest"
	"github.com/GokulMV/DocTheRepo/internal/core/pipeline"
	"github.com/GokulMV/DocTheRepo/internal/core/rag"
	"github.com/GokulMV/DocTheRepo/internal/core/spendguard"
	"github.com/GokulMV/DocTheRepo/internal/ingest"
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/queue"
	"github.com/GokulMV/DocTheRepo/internal/scheduler"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// app is the composition root: every adapter and core service, built once and shared by the roles.
type app struct {
	cfg      config.Config
	log      *slog.Logger
	st       *store.Store
	q        *queue.Queue
	enforcer *spendguard.Enforcer
	hosts    *codehost.Factory
	index    ports.VectorIndex
	chunks   *store.Chunks
	docs     *store.Docs
	pipe     *pipeline.Pipeline
	sweeper  *lifecycle.Sweeper
	ingest   *ingest.Service
	auth     *auth.Service
	oidc     *auth.OIDC
	qa       *store.QA
	rag      *rag.Engine
	box      *secrets.Box
	llmPool  *llm.Pool
	provSrc  *store.ProviderSource
	routes   *store.Routes
	repos    *store.Repos
	conns    *store.Connectors
	browse   *store.Browse
	// Signals: storage, the aggregation hot path, and the ingest entry point.
	signalStore *store.Signals
	knownIssues *store.KnownIssues
	agg         *aggregate.Aggregator
	signals     *ingest.SignalIngest
	polls       *ingest.SignalPolls
}

func wire(ctx context.Context, cfg config.Config, st *store.Store, box *secrets.Box, q *queue.Queue, log *slog.Logger, m *observability.Metrics) (*app, error) {
	a := &app{cfg: cfg, log: log, st: st, q: q, box: box}
	conns := store.NewConnectors(st, box, secrets.ConnectorCredsAAD, secrets.ConnectorWebhookAAD)
	repos := store.NewRepos(st)
	a.conns, a.repos, a.browse = conns, repos, store.NewBrowse(st)
	prs := store.NewPRs(st)
	a.hosts = &codehost.Factory{Load: conns.Get}

	guard, err := store.LoadGuard(ctx, st, cfg.Spend.AllowUnlimited)
	if err != nil {
		return nil, fmt.Errorf("load spend limits: %w", err)
	}
	a.enforcer = spendguard.NewEnforcer(guard, store.NewLedger(st), nil)
	a.provSrc = &store.ProviderSource{Providers: store.NewProviders(st), Box: box, AAD: secrets.ProviderKeyAAD}
	a.llmPool = llm.NewPool(a.provSrc, 5*time.Minute)
	a.routes = store.NewRoutes(st)
	gw := llmgateway.New(a.enforcer, a.routes, a.llmPool, cfg.Spend.AllowUnreportedUsage)

	switch cfg.Vector.Backend {
	case "qdrant":
		a.index = qdrant.New(st.Pool, cfg.Vector.QdrantURL, cfg.Vector.QdrantKey(), nil)
	default:
		a.index = pgvector.New(st.Pool)
	}
	reg := grammars.NewBuiltin()
	if cfg.Grammars.LoadDir != "" {
		if err := reg.LoadDir(expandHome(cfg.Grammars.LoadDir), log); err != nil {
			return nil, err
		}
	}
	shelves, err := library.Compile(library.DefaultShelves())
	if err != nil {
		return nil, err
	}
	a.chunks = store.NewChunks(st)
	a.docs = store.NewDocs(st, shelves)

	a.signalStore = store.NewSignals(st)
	a.signalStore.OnIssues = func(_ context.Context, refs []store.IssueRef) {
		for _, r := range refs {
			m.IssuesNew.WithLabelValues(r.Status).Inc()
			log.Info("issue opened", "component", "signals", "issue_id", r.ID, "status", r.Status, "service", r.Service, "kind", r.Kind)
		}
	}
	a.knownIssues = store.NewKnownIssues(st)
	a.agg = aggregate.New(a.signalStore, aggregate.Options{OnFlush: func(b aggregate.Batch, d time.Duration, err error) {
		if err != nil {
			log.Warn("signal flush failed; retrying", "component", "signals", "err", err, "groups", len(b.Groups))
			return
		}
		m.SignalFlush.Observe(d.Seconds())
		m.EventsFlushed.Add(float64(b.Events()))
	}})
	a.signals = &ingest.SignalIngest{Agg: a.agg, Rules: a.signalStore, Log: log.With("component", "signals"),
		OnEvent:  func(source, outcome string) { m.SignalEvents.WithLabelValues(source, outcome).Inc() },
		Webhooks: registry.Webhooks(), Accepts: registry.Accepts, LoadConnector: conns.Get}
	a.polls = &ingest.SignalPolls{Sink: a.signals, Store: conns, Queue: q,
		Pollers: map[string]ports.SignalPoller{"cloudwatch": cloudwatch.New(), "gcp": gcplogging.New()}}
	resolve := func(ctx context.Context, fps []string) error {
		n, err := a.signalStore.Resolve(ctx, fps)
		if n > 0 {
			log.Info("issues auto-resolved", "component", "signals", "count", n)
		}
		return err
	}
	for _, in := range []ports.BusInspector{kafka.New(), awsbus.New("sqs"), awsbus.New("sns"), awsbus.New("eventbridge"),
		awsbus.New("kinesis"), pubsubbus.New(), rabbitmq.New()} {
		a.polls.Pollers[in.Type()] = &ingest.BusPoller{Inspector: in, Resolve: resolve}
	}
	a.ingest = &ingest.Service{Queue: q, Repos: repos, Hosts: a.hosts, Log: log.With("component", "ingest")}
	a.sweeper = &lifecycle.Sweeper{PRs: prs, Requeue: a.ingest.RequeueDocs, Log: log.With("component", "pr_lifecycle"),
		Hosts: func(ctx context.Context, repoID string) (ports.CodeHost, ports.RepoConfig, error) {
			repo, err := repos.Get(ctx, repoID)
			if err != nil {
				return nil, repo, err
			}
			h, err := a.hosts.Host(ctx, repo.ConnectorID)
			return h, repo, err
		}}
	a.auth = auth.New(st, cfg.Auth)
	if cfg.Auth.Mode == "oidc" {
		secret := os.Getenv(cfg.Auth.OIDC.ClientSecretEnv)
		if a.oidc, err = auth.NewOIDC(ctx, cfg.Auth.OIDC, secret); err != nil {
			return nil, err
		}
	}
	if err := bootstrapOwner(ctx, cfg, st, a.auth, log); err != nil {
		return nil, err
	}
	a.qa = store.NewQA(st)
	a.rag = &rag.Engine{Store: a.qa, Index: a.index, GW: gw, Savings: store.NewSavings(st),
		Cost: func(kind, model, feature string, in, out int64) (float64, bool) {
			return a.enforcer.Guard().Cost(kind, model, feature, in, out)
		},
		Observe: func(stage string, d time.Duration) { m.Retrieval.WithLabelValues(stage).Observe(d.Seconds()) }}
	a.pipe = &pipeline.Pipeline{Repos: repos, Chunks: a.chunks, Graph: store.NewGraph(st), Docs: a.docs, Savings: store.NewSavings(st),
		Hosts: a.hosts.Host, Lander: &push.Dispatcher{PRs: prs, Lifecycle: a.sweeper}, GW: gw, DocGen: &docgen.Generator{GW: gw},
		Indexer: &pipeline.Indexer{GW: gw, Index: a.index}, Grammars: reg, Log: log.With("component", "pipeline")}
	return a, nil
}

// registerHandlers binds job types to their handlers.
func (a *app) registerHandlers(pool *queue.Pool) {
	pool.Register(ports.JobCodePush, a.pipe.CodePush)
	pool.Register(ports.JobImportDocs, a.pipe.ImportDocs)
	pool.Register(ports.JobReindex, a.pipe.Reindex)
	pool.Register(ports.JobSignalBatch, a.polls.Handle)
	pool.Register(ports.JobPRReview, func(ctx context.Context, job ports.Job) (ports.Outcome, error) {
		var pl ingest.ReviewPayload
		if err := json.Unmarshal(job.Payload, &pl); err != nil {
			return ports.Outcome{}, ports.Permanent(err)
		}
		state, err := a.sweeper.OnReview(ctx, pl.RepoID, pl.Number)
		if err != nil {
			return ports.Outcome{}, err
		}
		return ports.Outcome{Result: map[string]string{"pr_state": state}}, nil
	})
}

// tasks are the Phase 4 maintenance tasks for the leader-elected scheduler.
func (a *app) tasks() []scheduler.Task {
	return []scheduler.Task{
		{Name: "seed_library_shelves", Every: 24 * time.Hour, RunFirst: true, Fn: a.docs.SeedShelves},
		{Name: "signal_maintenance", Every: 6 * time.Hour, RunFirst: true, Fn: func(ctx context.Context) error {
			return a.signalStore.Maintain(ctx, time.Now())
		}},
		{Name: "poll_git_repos", Every: 30 * time.Second, RunFirst: true, Fn: func(ctx context.Context) error {
			n, err := a.ingest.Poll(ctx)
			if n > 0 {
				a.log.Info("polling enqueued pushes", "count", n)
			}
			return err
		}},
		{Name: "poll_signal_connectors", Every: 15 * time.Second, RunFirst: true, Fn: func(ctx context.Context) error {
			_, err := a.polls.Enqueue(ctx)
			return err
		}},
		{Name: "pr_lifecycle_sweep", Every: max(a.cfg.Docs.PRSweepInterval, time.Second), Fn: a.sweeper.Sweep},
		{Name: "session_gc", Every: time.Hour, Fn: a.auth.GCSessions},
		{Name: "answer_cache_gc", Every: time.Hour, Fn: a.qa.GCCache},
		{Name: "reload_spend_guard", Every: time.Minute, Fn: a.reloadGuard},
		{Name: "chunk_gc", Every: 24 * time.Hour, Fn: func(ctx context.Context) error {
			ids, err := a.chunks.GC(ctx, manifest.GCCutoff(time.Now(), a.cfg.Retention.ChunkGCDays))
			if err != nil || len(ids) == 0 {
				return err
			}
			a.log.Info("garbage-collected soft-deleted chunks", "count", len(ids))
			return a.index.Delete(ctx, ids)
		}},
	}
}

// reloadGuard picks up edited spend limits and prices. Workers run it too so every replica enforces the
// current limits within a minute of a change.
func (a *app) reloadGuard(ctx context.Context) error {
	g, err := store.LoadGuard(ctx, a.st, a.cfg.Spend.AllowUnlimited)
	if err != nil {
		return err
	}
	a.enforcer.SetGuard(g)
	return nil
}

// bootstrapOwner creates the local-mode owner on first start from DTH_OWNER_EMAIL / DTH_OWNER_PASSWORD
// (`dth up` sets them); it never touches an existing user base.
func bootstrapOwner(ctx context.Context, cfg config.Config, st *store.Store, svc *auth.Service, log *slog.Logger) error {
	email, pw := os.Getenv("DTH_OWNER_EMAIL"), os.Getenv("DTH_OWNER_PASSWORD")
	if cfg.Auth.Mode != "local" || email == "" || pw == "" {
		return nil
	}
	n, err := st.Q.CountUsers(ctx)
	if err != nil || n > 0 {
		return err
	}
	if _, err := svc.BootstrapOwner(ctx, email, pw); err != nil {
		return fmt.Errorf("create owner account: %w", err)
	}
	log.Info("created the owner account", "email", email)
	return nil
}

// v1Routes mounts the authenticated resource APIs (added as each area is built).
func (a *app) v1Routes() []func(chi.Router) {
	admin := api.AdminDeps{Auth: a.auth, Repos: a.repos, Connectors: a.conns, Providers: a.provSrc.Providers, Routes: a.routes,
		Browse: a.browse, Queue: a.q, Seal: a.box.Seal, ProviderAAD: secrets.ProviderKeyAAD, ProviderKinds: llm.Kinds(),
		InvalidateProvider: a.llmPool.Invalidate, Host: a.hosts.Host, InvalidateHost: func(id string) { a.hosts.Invalidate(id); a.signals.InvalidateConnector(id) }, DryRun: a.pipe.CodePush,
		RegisterWebhook: a.registerWebhook,
		ReloadSpend:     a.reloadGuard,
		TestProvider: func(ctx context.Context, id, model string) (time.Duration, error) {
			cfg, _, err := a.provSrc.ProviderConfig(ctx, id)
			if err != nil {
				return 0, err
			}
			return llm.Test(ctx, cfg, model)
		}}
	return []func(chi.Router){
		api.AskRoutes(a.rag, a.qa, a.auth),
		api.BrowseRoutes(a.browse, a.auth),
		api.AdminRoutes(admin),
		api.OpsRoutes(a.q, a.browse, a.auth),
	}
}

// registerWebhook points the repo's push/review webhook at <public_url>/hooks/<kind>/<connector>, unless the
// connector only polls or has no webhook secret (unsigned deliveries are always rejected).
func (a *app) registerWebhook(ctx context.Context, connectorID, repo string) (string, error) {
	host, cc, err := a.hosts.HostAndConfig(ctx, connectorID)
	if err != nil {
		return "", err
	}
	switch {
	case cc.Mode == "poll":
		return "skipped: the connector polls", nil
	case cc.WebhookSecret == "":
		return "skipped: the connector has no webhook secret", nil
	case a.cfg.Server.PublicURL == "":
		return "skipped: server.public_url is not set", nil
	}
	url := strings.TrimSuffix(a.cfg.Server.PublicURL, "/") + "/hooks/" + cc.Type + "/" + connectorID
	if err := host.RegisterWebhook(ctx, repo, url, cc.WebhookSecret); err != nil {
		return "", err
	}
	return "registered", nil
}

// readiness are the /readyz checks beyond the database.
func (a *app) readiness() map[string]api.ReadinessCheck {
	return map[string]api.ReadinessCheck{
		"db": a.st.Ping,
		// A fresh install has no routes yet and must still be ready (the setup UI configures them); only
		// failures to read routing are fatal.
		"llm_routes": func(ctx context.Context) error {
			_, err := a.routes.Route(ctx, llmgateway.FeatureQA)
			if errors.Is(err, llmgateway.ErrNoRoute) {
				return nil
			}
			return err
		},
		"vector": func(ctx context.Context) error {
			_, err := a.index.State(ctx)
			return err
		},
	}
}

// streams runs a pull consumer per enabled Pub/Sub connector (worker role).
func (a *app) streams(log *slog.Logger) *ingest.StreamRunner {
	log = log.With("component", "streams")
	health := func(id string, err error) {
		if herr := a.conns.SetHealth(context.Background(), id, err); herr != nil {
			log.Warn("record connector health failed", "connector_id", id, "err", herr)
		}
	}
	return &ingest.StreamRunner{
		Log:  log,
		List: func(ctx context.Context) ([]ports.ConnectorConfig, error) { return a.conns.ListByType(ctx, "pubsub") },
		Start: func(ctx context.Context, cc ports.ConnectorConfig) (func(context.Context), error) {
			client, err := pubsub.NewClient(ctx, cc)
			if err != nil {
				return nil, err
			}
			c := &pubsub.Consumer{Client: client, CC: cc, Sink: a.signals, Log: log,
				OnHealth: func(err error) { health(cc.ID, err) }}
			return c.Run, nil
		},
		OnError: func(cc ports.ConnectorConfig, err error) { health(cc.ID, err) },
	}
}
