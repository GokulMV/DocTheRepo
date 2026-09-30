// Command stack runs the real hub binary against everything it talks to, for Playwright E2E and k6 runs:
// PostgreSQL + pgvector (testcontainers, or -database-url), the GitHub mock seeded with the fixture
// repositories, an OIDC provider, and the behavioural stub LLM. A control API lets tests push commits,
// choose who signs in next, and inspect the mocks. It prints one JSON line with every URL when ready and
// stops everything on SIGINT/SIGTERM.
//
//	go run ./test/e2e/stack -hub ./bin/dth-hub
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/GokulMV/DocTheRepo/test/fixtures"
	"github.com/GokulMV/DocTheRepo/test/mocks/githubmock"
	"github.com/GokulMV/DocTheRepo/test/mocks/oidcmock"
	"github.com/GokulMV/DocTheRepo/test/mocks/stubllm"
)

// ClientID is the OIDC client the hub is configured with.
const ClientID = "dth-e2e"

// Domain is the only email domain allowed to sign in.
const Domain = "acme.test"

// State is what the stack prints and serves at GET /state.
type State struct {
	HubURL       string   `json:"hub_url"`
	ControlURL   string   `json:"control_url"`
	GitHubAPIURL string   `json:"github_api_url"`
	GitHubBot    string   `json:"github_bot"`
	LLMURL       string   `json:"llm_url"` // OpenAI-compatible base URL (…/v1); control at …/_stub
	OIDCIssuer   string   `json:"oidc_issuer"`
	Repos        []string `json:"repos"`
	AuthMode     string   `json:"auth_mode"`
	HubLog       string   `json:"hub_log"`
	DatabaseURL  string   `json:"database_url"` // the throwaway test database (perf seeding)
	MetricsURL   string   `json:"metrics_url,omitempty"`
}

func main() {
	var (
		hubBin      = flag.String("hub", "./bin/dth-hub", "hub binary (build with `make release` for the UI)")
		listen      = flag.String("listen", "127.0.0.1:18090", "hub listen address")
		control     = flag.String("control", "127.0.0.1:18099", "control API listen address")
		llmListen   = flag.String("llm", "127.0.0.1:18098", "stub LLM listen address")
		dbURL       = flag.String("database-url", os.Getenv("DTH_E2E_DATABASE_URL"), "PostgreSQL URL (default: start a pgvector container)")
		authMode    = flag.String("auth", "oidc", "hub auth mode: oidc (first sign-in becomes owner) or local")
		owner       = flag.String("owner", "owner@"+Domain, "local mode: owner email (password: -owner-password)")
		ownerPW     = flag.String("owner-password", "correct horse battery staple", "local mode: owner password")
		docgenDelay = flag.Duration("docgen-delay", 0, "stub LLM latency per docgen call")
		qaDelay     = flag.Duration("qa-delay", 0, "stub LLM latency per Q&A call")
		sweep       = flag.Duration("pr-sweep", 2*time.Second, "hub PR lifecycle sweep interval")
		stateFile   = flag.String("state-file", "", "also write the ready JSON here")
		workDir     = flag.String("work-dir", "", "directory for the hub log and key (default: a temp dir)")
		extraEnv    = flag.String("hub-env", "", "extra KEY=VALUE pairs for the hub, comma-separated")
		metrics     = flag.String("metrics", "", "hub metrics listen address (e.g. 127.0.0.1:18091); off by default")
		synthetic   = flag.Int("synthetic-repos", 0, "also create acme/svc-01..N (small Go services) for perf runs")
	)
	flag.Parse()
	log.SetFlags(log.Ltime)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, runOpts{hubBin: *hubBin, listen: *listen, control: *control, llmListen: *llmListen, dbURL: *dbURL,
		authMode: *authMode, owner: *owner, ownerPW: *ownerPW, docgenDelay: *docgenDelay, qaDelay: *qaDelay, sweep: *sweep,
		stateFile: *stateFile, workDir: *workDir, extraEnv: *extraEnv, metrics: *metrics, synthetic: *synthetic}); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

type runOpts struct {
	hubBin, listen, control, llmListen, dbURL, authMode, owner, ownerPW, stateFile, workDir, extraEnv, metrics string
	docgenDelay, qaDelay, sweep                                                                                time.Duration
	synthetic                                                                                                  int
}

func run(ctx context.Context, o runOpts) error {
	hubBin, err := filepath.Abs(o.hubBin)
	if err != nil {
		return err
	}
	if _, err := os.Stat(hubBin); err != nil {
		return fmt.Errorf("hub binary %s: %w (run `make release`)", hubBin, err)
	}
	if o.workDir == "" {
		if o.workDir, err = os.MkdirTemp("", "dth-e2e-"); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(o.workDir, 0o755); err != nil {
		return err
	}

	dbURL := o.dbURL
	if dbURL == "" {
		log.Print("starting PostgreSQL (pgvector) …")
		c, err := tcpostgres.Run(ctx, "pgvector/pgvector:pg16", tcpostgres.WithDatabase("dth"), tcpostgres.WithUsername("dth"), tcpostgres.WithPassword("dth"),
			testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(2*time.Minute)))
		if err != nil {
			return fmt.Errorf("start postgres: %w", err)
		}
		defer func() { _ = c.Terminate(context.Background()) }()
		if dbURL, err = c.ConnectionString(ctx, "sslmode=disable"); err != nil {
			return err
		}
	}

	repos, err := fixtures.Load(fixtures.Dir())
	if err != nil {
		return err
	}
	for i := 1; i <= o.synthetic; i++ {
		repos[fmt.Sprintf("acme/svc-%02d", i)] = syntheticService(i)
	}
	gh := githubmock.New()
	defer gh.Close()
	names := fixtures.Seed(gh, repos)

	idp := oidcmock.New(ClientID)
	defer idp.Close()
	idp.SetUser(user("owner@" + Domain))

	llm := stubllm.NewHandler()
	llm.SetDelay(stubllm.DocGen, o.docgenDelay)
	llm.SetDelay(stubllm.QA, o.qaDelay)
	llmSrv, llmURL, err := serve(o.llmListen, llm.Handler())
	if err != nil {
		return fmt.Errorf("stub LLM: %w", err)
	}
	defer llmSrv.Close()

	hubURL := "http://" + o.listen
	st := State{HubURL: hubURL, GitHubAPIURL: gh.APIURL(), GitHubBot: gh.BotLogin, LLMURL: llmURL + "/v1", OIDCIssuer: idp.URL,
		Repos: names, AuthMode: o.authMode, HubLog: filepath.Join(o.workDir, "hub.log"), DatabaseURL: dbURL}
	if o.metrics != "" {
		st.MetricsURL = "http://" + o.metrics + "/metrics"
	}

	env := append(os.Environ(),
		"DTH_DATABASE_URL="+dbURL, "DTH_LISTEN="+o.listen, "DTH_METRICS_LISTEN="+o.metrics, "DTH_PUBLIC_URL="+hubURL,
		"DTH_LOCAL_KEY_FILE="+filepath.Join(o.workDir, "master.key"), "DTH_LOG_FORMAT=json", "DTH_LOG_LEVEL=info",
		"DTH_PR_SWEEP_INTERVAL="+o.sweep.String(), "DTH_ALL_USERS_READ_ALL_REPOS=false", "DTH_AUTH_MODE="+o.authMode,
		"DTH_GRAMMARS_DIR="+filepath.Join(o.workDir, "grammars"))
	switch o.authMode {
	case "oidc":
		env = append(env, "DTH_OIDC_ISSUER="+idp.URL, "DTH_OIDC_CLIENT_ID="+ClientID, "DTH_OIDC_CLIENT_SECRET=e2e-secret",
			"DTH_OIDC_REDIRECT_URL="+hubURL+"/api/v1/auth/callback", "DTH_OIDC_ALLOWED_DOMAINS="+Domain)
	case "local":
		env = append(env, "DTH_OWNER_EMAIL="+o.owner, "DTH_OWNER_PASSWORD="+o.ownerPW)
	default:
		return fmt.Errorf("-auth must be oidc or local")
	}
	for _, kv := range strings.Split(o.extraEnv, ",") {
		if kv = strings.TrimSpace(kv); kv != "" {
			env = append(env, kv)
		}
	}
	logf, err := os.Create(st.HubLog)
	if err != nil {
		return err
	}
	defer logf.Close()
	hub := exec.Command(hubBin)
	hub.Env, hub.Stdout, hub.Stderr = env, logf, logf
	if err := hub.Start(); err != nil {
		return fmt.Errorf("start hub: %w", err)
	}
	hubDone := make(chan error, 1)
	go func() { hubDone <- hub.Wait() }()
	defer func() {
		_ = hub.Process.Signal(syscall.SIGTERM)
		select {
		case <-hubDone:
		case <-time.After(15 * time.Second):
			_ = hub.Process.Kill()
		}
	}()
	if err := waitReady(ctx, hubURL+"/readyz", hubDone); err != nil {
		return fmt.Errorf("%w (hub log: %s)", err, st.HubLog)
	}

	ctlSrv, ctlURL, err := serve(o.control, controlAPI(&st, gh, idp))
	if err != nil {
		return fmt.Errorf("control API: %w", err)
	}
	defer ctlSrv.Close()
	st.ControlURL = ctlURL
	ready, _ := json.Marshal(st)
	if o.stateFile != "" {
		if err := os.WriteFile(o.stateFile, ready, 0o644); err != nil {
			return err
		}
	}
	fmt.Println(string(ready))
	log.Printf("stack ready: hub %s, control %s (Ctrl-C to stop)", hubURL, ctlURL)

	select {
	case <-ctx.Done():
		return nil
	case err := <-hubDone:
		return fmt.Errorf("hub exited: %v (log: %s)", err, st.HubLog)
	}
}

// syntheticService is a small Go service for perf runs: a handler, a store, and a client.
func syntheticService(n int) map[string]string {
	name := fmt.Sprintf("svc%02d", n)
	return map[string]string{
		"README.md": fmt.Sprintf("# svc-%02d\n\nSynthetic service %d for load tests.\n", n, n),
		"go.mod":    fmt.Sprintf("module example.com/%s\n\ngo 1.25\n", name),
		"handler/handler.go": fmt.Sprintf(`// Package handler serves the %[1]s HTTP API.
package handler

import "net/http"

// Health reports liveness for %[1]s.
func Health(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

// Orders lists orders for the caller.
func Orders(w http.ResponseWriter, r *http.Request) { _ = r.URL.Query().Get("customer") }
`, name),
		"store/store.go": fmt.Sprintf(`// Package store persists %[1]s orders.
package store

// Order is one order.
type Order struct {
	ID    string
	Total int64
}

// Save writes an order.
func Save(o Order) error { return nil }
`, name),
	}
}

func user(email string) oidcmock.User {
	name := email[:strings.Index(email, "@")]
	return oidcmock.User{Subject: "sub-" + name, Email: email, EmailVerified: true, Name: strings.ToUpper(name[:1]) + name[1:]}
}

func serve(addr string, h http.Handler) (*http.Server, string, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, "", err
	}
	s := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = s.Serve(l) }()
	return s, "http://" + l.Addr().String(), nil
}

func waitReady(ctx context.Context, url string, hubDone <-chan error) error {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if resp, err := http.Get(url); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case err := <-hubDone:
			return fmt.Errorf("hub exited during startup: %v", err)
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return errors.New("hub not ready after 2 minutes")
}

// controlAPI is how tests drive the mocks.
//
//	GET  /state                                   URLs and fixture repos
//	POST /oidc/user      {"email","groups"}        who the next SSO sign-in is
//	POST /github/push    {"repo","branch","author","files":{path: content|null}} → {"sha"}
//	GET  /github/prs?repo=                         docs PRs on the mock
//	POST /github/checks  {"repo","number","state"}  CI result on a PR's head (success|failure|pending)
//	GET  /github/file?repo=&path=[&branch=]        {"exists","content"}
//	GET  /github/deliveries                        webhook delivery log
func controlAPI(st *State, gh *githubmock.Server, idp *oidcmock.Server) http.Handler {
	mux := http.NewServeMux()
	var pushMu sync.Mutex
	mux.HandleFunc("GET /state", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, st) })
	mux.HandleFunc("POST /oidc/user", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Email  string   `json:"email"`
			Groups []string `json:"groups"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || !strings.Contains(in.Email, "@") {
			http.Error(w, "email required", http.StatusBadRequest)
			return
		}
		u := user(in.Email)
		u.Groups = in.Groups
		idp.SetUser(u)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /github/push", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Repo   string             `json:"repo"`
			Branch string             `json:"branch"`
			Author string             `json:"author"`
			Files  map[string]*string `json:"files"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Repo == "" || len(in.Files) == 0 {
			http.Error(w, "repo and files required", http.StatusBadRequest)
			return
		}
		if in.Branch == "" {
			in.Branch = "main"
		}
		if in.Author == "" {
			in.Author = "dev"
		}
		if gh.Head(in.Repo, in.Branch) == "" {
			http.Error(w, "unknown repo or branch", http.StatusNotFound)
			return
		}
		pushMu.Lock()
		sha := gh.Push(in.Repo, in.Branch, in.Files, in.Author)
		pushMu.Unlock()
		writeJSON(w, map[string]string{"sha": sha})
	})
	mux.HandleFunc("GET /github/prs", func(w http.ResponseWriter, r *http.Request) {
		prs := gh.PRs(r.URL.Query().Get("repo"))
		out := make([]map[string]any, 0, len(prs))
		for _, p := range prs {
			out = append(out, map[string]any{"number": p.Number, "head": p.Head, "base": p.Base, "title": p.Title, "state": p.State, "merged": p.Merged})
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("POST /github/checks", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Repo   string `json:"repo"`
			Number int    `json:"number"`
			State  string `json:"state"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for _, p := range gh.PRs(in.Repo) {
			if p.Number == in.Number {
				gh.SetChecks(gh.Head(in.Repo, p.Head), in.State)
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		http.Error(w, "no such PR", http.StatusNotFound)
	})
	mux.HandleFunc("GET /github/file", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		branch := q.Get("branch")
		if branch == "" {
			branch = "main"
		}
		content, ok := gh.File(q.Get("repo"), branch, q.Get("path"))
		writeJSON(w, map[string]any{"exists": ok, "content": content})
	})
	mux.HandleFunc("GET /github/deliveries", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, gh.Deliveries()) })
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
