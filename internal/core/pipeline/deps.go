// Package pipeline runs the Hub's job types by orchestrating ports and the pure core packages
// (plan § 4.3 Flow A). Handlers are idempotent: every write is keyed by chunk ID, entity key, doc path,
// or the job's own docs branch, so an at-least-once redelivery converges on the same state.
package pipeline

import (
	"context"
	"log/slog"

	"github.com/GokulMV/DocTheRepo/internal/core/docgen"
	"github.com/GokulMV/DocTheRepo/internal/core/docrouter"
	"github.com/GokulMV/DocTheRepo/internal/core/grammars"
	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/palace"
	"github.com/GokulMV/DocTheRepo/internal/core/repodocs"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// RepoStore reads repository config and records progress.
type RepoStore interface {
	Get(ctx context.Context, id string) (ports.RepoConfig, error)
	SetLastProcessed(ctx context.Context, id, sha string) error
}

// ChunkStore is the chunk manifest.
type ChunkStore interface {
	ForPaths(ctx context.Context, repoID string, source ports.ChunkSource, paths []string) ([]ports.Chunk, error)
	Apply(ctx context.Context, w ports.ChunkWrite) (int64, error)
	RecordRename(ctx context.Context, repoID, oldPath, newPath, sha string) error
	// LiveAfter walks live chunks in ID order (reindex).
	LiveAfter(ctx context.Context, after string, limit int) ([]ports.Chunk, error)
}

// GraphStore is the Palace.
type GraphStore interface {
	ReplaceSource(ctx context.Context, repoID, sourcePath string, g palace.Graph) error
}

// DocsStore is the docs Tree.
type DocsStore interface {
	ReplaceFile(ctx context.Context, repoID, repoName string, f ports.DocFile) error
	RemoveFile(ctx context.Context, repoID, docPath string) error
	// DocumentedChunks lists the chunks that have a generated doc section.
	DocumentedChunks(ctx context.Context, repoID string) (map[string]bool, error)
}

// DocCache holds generated docs by the code they describe (docrouter).
type DocCache interface {
	Get(ctx context.Context, keys []docrouter.Key) (map[docrouter.Key]string, error)
	Put(ctx context.Context, model string, bodies map[docrouter.Key]string) error
}

// SavingsRecorder records usage the Hub avoided.
type SavingsRecorder interface {
	Record(ctx context.Context, kind string, tokens int64, costUSD float64, ref string) error
}

// HostFactory returns the CodeHost for a git connector.
type HostFactory func(ctx context.Context, connectorID string) (ports.CodeHost, error)

// Pipeline holds everything the job handlers need.
type Pipeline struct {
	Repos    RepoStore
	Chunks   ChunkStore
	Graph    GraphStore
	Docs     DocsStore
	Savings  SavingsRecorder
	Hosts    HostFactory
	Lander   ports.Lander
	GW       *llmgateway.Gateway
	DocGen   *docgen.Generator
	Indexer  *Indexer
	Grammars *grammars.Registry
	Log      *slog.Logger
	// OnChanges, if set, sees every push's changed files (the Architecture tab syncs authored diagrams).
	// It must not fail the push.
	OnChanges func(ctx context.Context, repo ports.RepoConfig, host ports.CodeHost, head string, changed []ports.ChangedFile)
	// Progress, if set, receives a running job's progress (files documented so far).
	Progress func(ctx context.Context, jobID string, p ports.JobProgress)
	// DocGenParallel is how many files one job documents at once (default 4).
	DocGenParallel int
	// DocCache reuses docs of code documented before; DocMode trades thoroughness for cost (docrouter).
	DocCache DocCache
	DocMode  docrouter.Mode
	// ModeSetting, if set, returns the mode chosen in the UI ("" keeps DocMode).
	ModeSetting func(ctx context.Context) string
	// DocsV2 writes readable documents per repository (repodocs) instead of one doc per source file: a
	// push indexes the code and queues a repo_docs job.
	DocsV2        bool
	RepoDocsGen   *repodocs.Generator
	RepoDocsFacts RepoDocsFacts
	// SystemStore keeps the System architecture (documents across repositories); nil turns it off.
	SystemStore SystemStore
	// DocsBudget caps what one repository's documents may cost per month (nil: no cap).
	DocsBudget DocsBudget
	// Enqueue queues a follow-up job (the repo_docs job after a push).
	Enqueue func(ctx context.Context, j ports.NewJob) error
}

func (p *Pipeline) log() *slog.Logger {
	if p.Log != nil {
		return p.Log
	}
	return slog.Default()
}
