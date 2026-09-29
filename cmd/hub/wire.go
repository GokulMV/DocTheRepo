package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/adapters/codehost"
	"github.com/GokulMV/DocTheRepo/internal/adapters/llm"
	"github.com/GokulMV/DocTheRepo/internal/adapters/push"
	"github.com/GokulMV/DocTheRepo/internal/adapters/push/lifecycle"
	"github.com/GokulMV/DocTheRepo/internal/adapters/vector/pgvector"
	"github.com/GokulMV/DocTheRepo/internal/adapters/vector/qdrant"
	"github.com/GokulMV/DocTheRepo/internal/config"
	"github.com/GokulMV/DocTheRepo/internal/core/docgen"
	"github.com/GokulMV/DocTheRepo/internal/core/grammars"
	"github.com/GokulMV/DocTheRepo/internal/core/library"
	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/manifest"
	"github.com/GokulMV/DocTheRepo/internal/core/pipeline"
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
}

func wire(ctx context.Context, cfg config.Config, st *store.Store, box *secrets.Box, q *queue.Queue, log *slog.Logger, m *observability.Metrics) (*app, error) {
	a := &app{cfg: cfg, log: log, st: st, q: q}
	conns := store.NewConnectors(st, box, secrets.ConnectorCredsAAD, secrets.ConnectorWebhookAAD)
	repos := store.NewRepos(st)
	prs := store.NewPRs(st)
	a.hosts = &codehost.Factory{Load: conns.Get}

	guard, err := store.LoadGuard(ctx, st, cfg.Spend.AllowUnlimited)
	if err != nil {
		return nil, fmt.Errorf("load spend limits: %w", err)
	}
	a.enforcer = spendguard.NewEnforcer(guard, store.NewLedger(st), nil)
	providers := llm.NewPool(&store.ProviderSource{Providers: store.NewProviders(st), Box: box, AAD: secrets.ProviderKeyAAD}, 5*time.Minute)
	gw := llmgateway.New(a.enforcer, store.NewRoutes(st), providers, cfg.Spend.AllowUnreportedUsage)

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
		{Name: "poll_git_repos", Every: 30 * time.Second, RunFirst: true, Fn: func(ctx context.Context) error {
			n, err := a.ingest.Poll(ctx)
			if n > 0 {
				a.log.Info("polling enqueued pushes", "count", n)
			}
			return err
		}},
		{Name: "pr_lifecycle_sweep", Every: 5 * time.Minute, Fn: a.sweeper.Sweep},
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
