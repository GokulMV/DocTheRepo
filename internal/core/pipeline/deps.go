// Package pipeline runs the Hub's job types by orchestrating ports and the pure core packages
// (plan § 4.3 Flow A). Handlers are idempotent: every write is keyed by chunk ID, entity key, doc path,
// or the job's own docs branch, so an at-least-once redelivery converges on the same state.
package pipeline

import (
	"context"
	"log/slog"

	"github.com/GokulMV/DocTheRepo/internal/core/docgen"
	"github.com/GokulMV/DocTheRepo/internal/core/grammars"
	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/palace"
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
}

// GraphStore is the Palace.
type GraphStore interface {
	ReplaceSource(ctx context.Context, repoID, sourcePath string, g palace.Graph) error
}

// DocsStore is the docs Tree.
type DocsStore interface {
	ReplaceFile(ctx context.Context, repoID, repoName, docPath, summary string, sections []ports.DocSection) error
	RemoveFile(ctx context.Context, repoID, docPath string) error
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
}

func (p *Pipeline) log() *slog.Logger {
	if p.Log != nil {
		return p.Log
	}
	return slog.Default()
}
