package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/GokulMV/DocTheRepo/internal/core/decode"
	"github.com/GokulMV/DocTheRepo/internal/core/docassembly"
	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store/gen"
)

// Decodes implements decode.Store.
type Decodes struct {
	s      *Store
	chunks *Chunks
}

// NewDecodes returns the decode store.
func NewDecodes(s *Store, chunks *Chunks) *Decodes { return &Decodes{s: s, chunks: chunks} }

// Issue implements decode.Store.
func (d *Decodes) Issue(ctx context.Context, id string) (decode.Issue, error) {
	var is decode.Issue
	var repo, dec *string
	err := d.s.Pool.QueryRow(ctx, `SELECT id, fingerprint, kind::text, title, service, environment, status::text, repo_id, decode_id,
		occurrences, sources FROM issues WHERE id = $1`, id).Scan(&is.ID, &is.Fingerprint, &is.Kind, &is.Title, &is.Service,
		&is.Environment, &is.Status, &repo, &dec, &is.Occurrences, &is.Sources)
	if errors.Is(err, pgx.ErrNoRows) {
		return is, ports.ErrNotFound
	}
	if err != nil {
		return is, fmt.Errorf("load issue: %w", err)
	}
	if repo != nil {
		is.RepoID = *repo
	}
	if dec != nil {
		is.DecodeID = *dec
	}
	return is, nil
}

// Samples implements decode.Store.
func (d *Decodes) Samples(ctx context.Context, issueID string, n int) ([]ports.SignalEvent, error) {
	rows, err := d.s.Q.IssueSamples(ctx, gen.IssueSamplesParams{IssueID: issueID, Lim: int32(n)})
	if err != nil {
		return nil, fmt.Errorf("load samples: %w", err)
	}
	out := make([]ports.SignalEvent, 0, len(rows))
	for _, r := range rows {
		ev := ports.SignalEvent{Source: r.Source, ExternalID: r.ExternalID, OccurredAt: r.OccurredAt, ReceivedAt: r.ReceivedAt,
			Severity: ports.Severity(r.Severity), Kind: ports.SignalKind(r.Kind), Service: r.Service, Environment: r.Environment,
			Title: r.Title, Message: r.MessageScrubbed, ExceptionType: r.ExceptionType, Fingerprint: r.Fingerprint}
		_ = json.Unmarshal(r.Stack, &ev.Stack)
		_ = json.Unmarshal(r.Attrs, &ev.Attrs)
		out = append(out, ev)
	}
	return out, nil
}

// ServiceRepos implements decode.Store.
func (d *Decodes) ServiceRepos(ctx context.Context, service string) ([]string, error) {
	if service == "" || service == signals.Unknown {
		return nil, nil
	}
	rows, err := d.s.Pool.Query(ctx, `SELECT DISTINCT repo_id::text FROM service_map WHERE lower(service_name) = lower($1) ORDER BY 1`, service)
	if err != nil {
		return nil, fmt.Errorf("service repos: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// FrameChunks implements decode.Store: for each frame, the chunk for its symbol in its file, else the
// file's top-level chunk.
func (d *Decodes) FrameChunks(ctx context.Context, repoIDs []string, frames []ports.StackFrame) ([]ports.Chunk, error) {
	if repoIDs == nil {
		repoIDs = []string{}
	}
	seen := map[string]bool{}
	var out []ports.Chunk
	for _, f := range frames {
		file, fn, qualified := frameTarget(f)
		if file == "" {
			continue
		}
		for _, name := range []string{fn, ""} { // symbol match, then the file itself
			rows, err := d.s.Q.FrameChunk(ctx, gen.FrameChunkParams{RepoIds: repoIDs, File: file, Fn: name, Qualified: qualified})
			if err != nil {
				return nil, fmt.Errorf("frame chunks: %w", err)
			}
			if len(rows) == 0 {
				continue
			}
			if c := toChunks(rows)[0]; !seen[c.ID] {
				seen[c.ID] = true
				out = append(out, c)
			}
			break
		}
	}
	return out, nil
}

// frameTarget turns a frame into (file, function, qualified symbol). JVM frames carry a bare file name and
// a dotted class: com.acme.orders.Repo + Repo.java → com/acme/orders/Repo.java, symbol Repo.save.
func frameTarget(f ports.StackFrame) (file, fn, qualified string) {
	file = strings.TrimPrefix(strings.TrimPrefix(f.File, "./"), "/")
	fn = f.Function
	if i := strings.LastIndex(fn, "."); i >= 0 && i < len(fn)-1 {
		qualified, fn = fn, fn[i+1:]
	}
	if file == "" {
		file = strings.TrimPrefix(f.Module, "/")
	}
	if ext := path.Ext(file); !strings.Contains(file, "/") && strings.Count(f.Module, ".") >= 1 && (ext == ".java" || ext == ".kt" || ext == ".scala") {
		mod := f.Module
		if i := strings.IndexAny(mod, "$"); i >= 0 {
			mod = mod[:i] // inner classes live in the outer class's file
		}
		file = strings.ReplaceAll(mod, ".", "/") + ext
		if qualified == "" {
			qualified = mod[strings.LastIndex(mod, ".")+1:] + "." + fn
		}
	}
	if qualified == "" {
		qualified = fn
	}
	return file, fn, qualified
}

// Docs implements decode.Store.
func (d *Decodes) Docs(ctx context.Context, repoID string, paths []string) ([]ports.Chunk, error) {
	if repoID == "" || len(paths) == 0 {
		return nil, nil
	}
	var docsPath string
	if err := d.s.Pool.QueryRow(ctx, `SELECT docs_path FROM repos WHERE id = $1`, repoID).Scan(&docsPath); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("repo docs path: %w", err)
	}
	targets := make([]string, len(paths))
	for i, p := range paths {
		targets[i] = docassembly.DocPath(docsPath, p)
	}
	all, err := d.chunks.ForPaths(ctx, repoID, ports.SourceGeneratedDoc, targets)
	if err != nil {
		return nil, err
	}
	var out []ports.Chunk
	for _, c := range all {
		if c.Live() {
			out = append(out, c)
		}
	}
	return out, nil
}

// Chunks implements decode.Store.
func (d *Decodes) Chunks(ctx context.Context, ids []string) ([]ports.Chunk, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return d.chunks.ByIDs(ctx, ids)
}

// LatestDecode implements decode.Store.
func (d *Decodes) LatestDecode(ctx context.Context, issueID string) (decode.Stored, bool, error) {
	var st decode.Stored
	var affected []byte
	err := d.s.Pool.QueryRow(ctx, `SELECT id, fingerprint_version, affected_code, tokens, cost_usd::float8 FROM decodes
		WHERE issue_id = $1 ORDER BY created_at DESC LIMIT 1`, issueID).Scan(&st.ID, &st.FingerprintVersion, &affected, &st.Tokens, &st.CostUSD)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, false, nil
	}
	if err != nil {
		return st, false, fmt.Errorf("latest decode: %w", err)
	}
	_ = json.Unmarshal(affected, &st.Affected)
	return st, true, nil
}

// SaveDecode implements decode.Store.
func (d *Decodes) SaveDecode(ctx context.Context, r decode.Record) (string, error) {
	id := ports.NewID()
	affected, _ := json.Marshal(orEmptySlice(r.Affected))
	commits, _ := json.Marshal(orEmptySlice(r.RelatedCommits))
	docs, _ := json.Marshal(orEmptySlice(r.RelatedDocs))
	similar := r.SimilarIssues
	if similar == nil {
		similar = []string{}
	}
	steps := r.Result.NextSteps
	if steps == nil {
		steps = []string{}
	}
	err := d.s.InTx(ctx, func(_ *gen.Queries, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO decodes (id, issue_id, summary, probable_cause, impact, affected_code, related_commits,
			related_docs, similar_issue_ids, next_steps, confidence, is_actionable, suggest_known_issue, provider, model, tokens,
			cost_usd, fingerprint_version) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::uuid[],$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
			id, r.IssueID, r.Result.Summary, r.Result.ProbableCause, r.Result.Impact, affected, commits, docs, similar, steps,
			r.Result.Confidence, r.Result.IsActionable, r.Result.SuggestKnownIssue, r.Provider, r.Model, r.Tokens, r.CostUSD,
			signals.FingerprintVersion); err != nil {
			return fmt.Errorf("insert decode: %w", err)
		}
		_, err := tx.Exec(ctx, `UPDATE issues SET decode_id = $2, updated_at = now(),
			status = CASE WHEN status = 'new' THEN 'decoded'::issue_status ELSE status END WHERE id = $1`, r.IssueID, id)
		return err
	})
	return id, err
}

// PutChunk implements decode.Store.
func (d *Decodes) PutChunk(ctx context.Context, c ports.Chunk) error {
	_, err := d.chunks.Apply(ctx, ChunkWrite{Upserts: []ports.Chunk{c}})
	return err
}

// IssueForChunks implements decode.Store (decode chunks live at issues/<issue id>).
func (d *Decodes) IssueForChunks(ctx context.Context, chunkIDs []string) (map[string]string, error) {
	cs, err := d.Chunks(ctx, chunkIDs)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, c := range cs {
		if c.Source == ports.SourceIssueDecode && strings.HasPrefix(c.Path, "issues/") && c.Live() {
			out[c.ID] = strings.TrimPrefix(c.Path, "issues/")
		}
	}
	return out, nil
}

func orEmptySlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
