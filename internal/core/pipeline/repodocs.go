package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/manifest"
	"github.com/GokulMV/DocTheRepo/internal/core/repodocs"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// RepoDocsPayload is the repo_docs job payload: write a repository's Docs v2 documents.
type RepoDocsPayload struct {
	RepoID string `json:"repo_id"`
	// Full rewrites every document; Only rewrites these (type or type/key).
	Full bool     `json:"full,omitempty"`
	Only []string `json:"only,omitempty"`
	// RetryFailed writes documents that failed before (a manual run).
	RetryFailed bool   `json:"retry_failed,omitempty"`
	Reason      string `json:"reason,omitempty"`
	DryRun      bool   `json:"dry_run,omitempty"`
	// OverrideCeiling lets an admin's retry exceed the spend ceiling once.
	OverrideCeiling bool `json:"override_ceiling,omitempty"`
}

// RepoDocsFacts reads facts and removes the per-file docs Docs v2 replaces.
type RepoDocsFacts interface {
	Facts(ctx context.Context, repoID string) (*repodocs.Facts, error)
	DropLegacy(ctx context.Context, repoID string) ([]string, error)
}

// DocsBudget reports a repository's monthly docs cap and what this month's docs cost so far. estimate
// prices writing what is missing, for setting a first cap.
type DocsBudget func(ctx context.Context, repoID string, estimate func() (float64, error)) (capUSD, spentUSD float64, err error)

// ErrDocsBudget: the repository's monthly docs budget is used up.
var ErrDocsBudget = errors.New("this repository's monthly docs budget is used up")

// maxSpecialBytes caps each README, build or CI file read whole as context.
const maxSpecialBytes = 12000

// vendored directories never documented.
var skipDirs = []string{"node_modules/", "vendor/", "third_party/", "dist/", "build/", ".git/", "target/", ".venv/", "venv/", "__pycache__/"}

func skipPath(p string) bool {
	for _, d := range skipDirs {
		if strings.HasPrefix(p, d) || strings.Contains(p, "/"+d) {
			return true
		}
	}
	return false
}

// RepoDocs handles a repo_docs job.
func (p *Pipeline) RepoDocs(ctx context.Context, job ports.Job) (ports.Outcome, error) {
	var pl RepoDocsPayload
	if err := json.Unmarshal(job.Payload, &pl); err != nil {
		return ports.Outcome{}, ports.Permanent(fmt.Errorf("decode payload: %w", err))
	}
	if pl.RepoID == "" {
		pl.RepoID = job.RepoID
	}
	if p.RepoDocsGen == nil || p.RepoDocsFacts == nil {
		return ports.Outcome{Status: ports.JobAborted, Message: "Docs v2 is not set up on this Hub"}, nil
	}
	repo, err := p.Repos.Get(ctx, pl.RepoID)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return ports.Outcome{Status: ports.JobAborted, Message: "repository is no longer tracked"}, nil
		}
		return ports.Outcome{}, err
	}
	if !repo.Enabled {
		return ports.Outcome{Status: ports.JobAborted, Message: "repository is disabled"}, nil
	}
	facts, err := p.RepoDocsFacts.Facts(ctx, repo.ID)
	if err != nil {
		return ports.Outcome{}, err
	}
	if facts.Head == "" || len(facts.Files) == 0 {
		return ports.Outcome{Status: ports.JobAborted, Message: "the repository has not been read yet; docs follow its first sync"}, nil
	}
	host, err := p.Hosts(ctx, repo.ConnectorID)
	if err != nil {
		return ports.Outcome{}, err
	}
	tree, err := host.ListTree(ctx, repo.FullName, facts.Head)
	if err != nil {
		return ports.Outcome{}, fmt.Errorf("list files: %w", err)
	}
	for _, t := range tree {
		if !skipPath(t) {
			facts.AllPaths = append(facts.AllPaths, t)
		}
	}
	// Recent history, for Recent changes and Decision records (best effort: docs do not need it).
	if cs, err := host.CommitsForPath(ctx, repo.FullName, "", time.Now().AddDate(0, 0, -90)); err == nil {
		facts.Commits = cs
	}
	var mu sync.Mutex
	cache := map[string][]byte{}
	read := func(ctx context.Context, path string) ([]byte, error) {
		mu.Lock()
		if b, ok := cache[path]; ok {
			mu.Unlock()
			return b, nil
		}
		mu.Unlock()
		b, err := host.GetFile(ctx, repo.FullName, path, facts.Head)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		cache[path] = b
		mu.Unlock()
		return b, nil
	}
	n := 0
	for _, t := range facts.AllPaths {
		if !repodocs.SpecialFile(t) || n >= 30 {
			continue
		}
		if b, err := read(ctx, t); err == nil {
			s := string(b)
			if len(s) > maxSpecialBytes {
				s = s[:maxSpecialBytes] + "\n… (trimmed)"
			}
			facts.Special[t] = s
			n++
		}
	}
	meta := llmgateway.CallMeta{RepoID: repo.ID, JobID: job.ID, Override: pl.OverrideCeiling}
	opts := repodocs.RunOptions{Meta: meta, Read: read, Force: pl.Full, Only: pl.Only, DryRun: pl.DryRun, RetryFailed: pl.RetryFailed,
		Progress: func(done, total int, doing string) {
			if p.Progress != nil {
				p.Progress(ctx, job.ID, ports.JobProgress{Stage: doing, Done: done, Total: total})
			}
		}}
	if !pl.DryRun && p.DocsBudget != nil {
		estimate := func() (float64, error) {
			dry := opts
			dry.DryRun = true
			r, err := p.RepoDocsGen.Run(ctx, facts, dry)
			return r.EstimatedUSD, err
		}
		limit, spent, err := p.DocsBudget(ctx, repo.ID, estimate)
		if err != nil {
			return ports.Outcome{}, err
		}
		if limit > 0 && !pl.OverrideCeiling {
			if spent >= limit {
				return ports.Outcome{Status: ports.JobSpendBlocked, Message: fmt.Sprintf("%s ($%.2f of $%.2f); raise it on the Docs page or wait for next month", ErrDocsBudget, spent, limit)}, nil
			}
			opts.Guard = func(ctx context.Context) error {
				_, s, err := p.DocsBudget(ctx, repo.ID, nil)
				if err == nil && s >= limit {
					return ErrDocsBudget
				}
				return nil
			}
		}
	}
	res, err := p.RepoDocsGen.Run(ctx, facts, opts)
	if errors.Is(err, ErrDocsBudget) {
		_ = p.indexDocs(ctx, repo, meta, res) // keep what was written before the cap
		return ports.Outcome{Status: ports.JobSpendBlocked, Message: ErrDocsBudget.Error() + "; the rest is written next month or when an admin raises it", Result: res}, nil
	}
	var sb *ports.SpendBlockedError
	if errors.As(err, &sb) {
		_ = p.indexDocs(ctx, repo, meta, res)
		return ports.Outcome{Status: ports.JobSpendBlocked, Message: sb.Error(), Result: res}, nil
	}
	if err != nil {
		return ports.Outcome{}, err
	}
	if pl.DryRun {
		return ports.Outcome{Status: ports.JobDone, Result: res,
			Message: fmt.Sprintf("dry run: %d documents would be written, ~%d input tokens", len(res.WouldWrite), res.EstimatedTokens)}, nil
	}
	if err := p.indexDocs(ctx, repo, meta, res); err != nil {
		return ports.Outcome{}, err
	}
	if len(res.Changed) > 0 || len(res.Removed) > 0 {
		p.queueSystemDocs(ctx, "documents of "+repo.FullName+" changed")
	}
	msg := fmt.Sprintf("%d documents written, %d unchanged", len(res.Written), res.Unchanged)
	if len(res.Failed) > 0 {
		msg += fmt.Sprintf(", %d failed", len(res.Failed))
	}
	p.log().Info("repository docs written", "repo", repo.FullName, "written", len(res.Written), "unchanged", res.Unchanged, "failed", len(res.Failed), "cost_usd", res.CostUSD)
	return ports.Outcome{Status: ports.JobDone, Message: msg, Result: res}, nil
}

// indexDocs puts written documents in the search index (one piece per section) and takes out the
// per-file docs they replace.
func (p *Pipeline) indexDocs(ctx context.Context, repo ports.RepoConfig, meta llmgateway.CallMeta, res repodocs.Result) error {
	var w ports.ChunkWrite
	var embed []ports.Chunk
	paths := map[string][]ports.Chunk{}
	for _, d := range res.Changed {
		paths[repodocs.DocPath(d)] = repodocs.Chunks(repo.FullName, d)
	}
	for _, id := range res.Removed {
		paths["@docs/"+id] = nil
	}
	for path, fresh := range paths {
		stored, err := p.Chunks.ForPaths(ctx, repo.ID, ports.SourceGeneratedDoc, []string{path})
		if err != nil {
			return err
		}
		dd := manifest.Diff(stored, fresh, []string{path})
		w.Upserts = append(append(w.Upserts, dd.Added...), dd.Changed...)
		w.Revive = append(w.Revive, dd.Revived...)
		w.Remove = append(w.Remove, dd.Removed...)
		embed = append(embed, dd.NeedsEmbedding()...)
	}
	if _, err := p.Chunks.Apply(ctx, w); err != nil {
		return err
	}
	if dropped, err := p.RepoDocsFacts.DropLegacy(ctx, repo.ID); err != nil {
		return err
	} else if len(dropped) > 0 && p.Indexer != nil && p.Indexer.Index != nil {
		_ = p.Indexer.Index.Delete(ctx, dropped)
	}
	if p.Indexer == nil || len(embed) == 0 {
		return nil
	}
	_, err := p.Indexer.Embed(ctx, meta, embed)
	var sb *ports.SpendBlockedError
	if errors.As(err, &sb) || errors.Is(err, ports.ErrEmbeddingMismatch) {
		return nil // searchable by text until the reindex
	}
	return err
}

// SystemStore reads how repositories link and keeps the System architecture.
type SystemStore interface {
	SystemLinks(ctx context.Context) ([]repodocs.SystemLink, error)
	SystemDoc(ctx context.Context) (repodocs.Doc, error)
	DeleteSystemDoc(ctx context.Context) error
	PutDoc(ctx context.Context, d repodocs.Doc) (repodocs.Doc, error)
	Docs(ctx context.Context, repoID string) ([]repodocs.Doc, error)
	SystemChunks(ctx context.Context) ([]ports.Chunk, error)
}

// SystemDocsPayload is the system_docs job payload.
type SystemDocsPayload struct {
	Full   bool   `json:"full,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// queueSystemDocs asks for the System architecture to be brought up to date (after a repository's
// documents changed).
func (p *Pipeline) queueSystemDocs(ctx context.Context, reason string) {
	if p.Enqueue == nil || p.SystemStore == nil {
		return
	}
	_ = p.Enqueue(ctx, ports.NewJob{Type: ports.JobSystemDocs, SerialKey: "system", DedupeKey: "system-docs", Payload: SystemDocsPayload{Reason: reason}})
}

// SystemDocs handles a system_docs job: when tracked repositories talk to each other (APIs, events,
// shared packages), it writes the System architecture from their documents and those links; when they no
// longer do, it removes it.
func (p *Pipeline) SystemDocs(ctx context.Context, job ports.Job) (ports.Outcome, error) {
	var pl SystemDocsPayload
	_ = json.Unmarshal(job.Payload, &pl)
	if p.SystemStore == nil || p.RepoDocsGen == nil {
		return ports.Outcome{Status: ports.JobAborted, Message: "Docs v2 is not set up on this Hub"}, nil
	}
	links, err := p.SystemStore.SystemLinks(ctx)
	if err != nil {
		return ports.Outcome{}, err
	}
	if len(links) == 0 {
		if err := p.SystemStore.DeleteSystemDoc(ctx); err != nil {
			return ports.Outcome{}, err
		}
		if err := p.indexSystem(ctx, nil, nil); err != nil {
			return ports.Outcome{}, err
		}
		return ports.Outcome{Status: ports.JobAborted, Message: "the tracked repositories do not talk to each other"}, nil
	}
	ids := map[string]string{}
	for _, l := range links {
		ids[l.FromRepo], ids[l.ToRepo] = l.FromName, l.ToName
	}
	var in repodocs.SystemInput
	var heads []string
	for _, id := range sortedIDs(ids) {
		docs, err := p.SystemStore.Docs(ctx, id)
		if err != nil {
			return ports.Outcome{}, err
		}
		r := repodocs.SystemRepo{ID: id, Name: ids[id]}
		var hashes []string
		for _, d := range docs {
			if d.Status != "ok" {
				continue
			}
			switch d.Type {
			case "overview":
				r.Overview = d.AtAGlance
				if s := d.Section("what"); s != nil {
					r.Overview += "\n\n" + s.Markdown
				}
				hashes = append(hashes, d.InputsHash)
				heads = append(heads, d.SourceSHA)
			case "architecture":
				for _, k := range []string{"style", "components", "flow", "integrations"} {
					if s := d.Section(k); s != nil {
						r.Architecture += "**" + s.Title + ".** " + s.Markdown + "\n\n"
					}
				}
				hashes = append(hashes, d.InputsHash)
			}
		}
		r.Hash = repodocs.Hash(hashes...)
		in.Repos = append(in.Repos, r)
	}
	in.Links, in.Head = links, repodocs.Hash(heads...)
	prev, err := p.SystemStore.SystemDoc(ctx)
	var prevPtr *repodocs.Doc
	if err == nil {
		prevPtr = &prev
		if !pl.Full && prev.Status == "ok" && prev.InputsHash == repodocs.SystemHash(in) {
			if err := p.indexSystem(ctx, &prev, sortedIDs(ids)); err != nil {
				return ports.Outcome{}, err
			}
			return ports.Outcome{Status: ports.JobDone, Message: "the System architecture is current"}, nil
		}
	} else if !errors.Is(err, ports.ErrNotFound) {
		return ports.Outcome{}, err
	}
	d, werr := p.RepoDocsGen.WriteSystem(ctx, in, prevPtr, repodocs.RunOptions{Meta: llmgateway.CallMeta{JobID: job.ID}})
	var sb *ports.SpendBlockedError
	if errors.As(werr, &sb) {
		return ports.Outcome{Status: ports.JobSpendBlocked, Message: sb.Error()}, nil
	}
	if werr != nil && prevPtr != nil && prevPtr.Status == "ok" {
		return ports.Outcome{}, werr // keep the last good version; the job retries
	}
	saved, err := p.SystemStore.PutDoc(ctx, d)
	if err != nil {
		return ports.Outcome{}, err
	}
	if werr != nil {
		return ports.Outcome{}, werr
	}
	if err := p.indexSystem(ctx, &saved, sortedIDs(ids)); err != nil {
		return ports.Outcome{}, err
	}
	return ports.Outcome{Status: ports.JobDone, Message: fmt.Sprintf("System architecture written for %d repositories and %d links", len(in.Repos), len(links))}, nil
}

func sortedIDs(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return m[out[i]] < m[out[j]] })
	return out
}

// indexSystem puts the System architecture in Ask's index, one piece per section, each requiring every
// repository it covers (nil d removes it).
func (p *Pipeline) indexSystem(ctx context.Context, d *repodocs.Doc, repoIDs []string) error {
	var fresh []ports.Chunk
	if d != nil {
		fresh = repodocs.SystemChunks(*d, repoIDs)
	}
	stored, err := p.SystemStore.SystemChunks(ctx)
	if err != nil {
		return err
	}
	dd := manifest.Diff(stored, fresh, []string{repodocs.SystemPath})
	w := ports.ChunkWrite{Upserts: append(append([]ports.Chunk{}, dd.Added...), dd.Changed...), Revive: dd.Revived, Remove: dd.Removed}
	if len(w.Upserts)+len(w.Revive)+len(w.Remove) == 0 {
		return nil
	}
	if _, err := p.Chunks.Apply(ctx, w); err != nil {
		return err
	}
	if p.Indexer == nil {
		return nil
	}
	if len(dd.Removed) > 0 && p.Indexer.Index != nil {
		_ = p.Indexer.Index.Delete(ctx, dd.Removed)
	}
	_, err = p.Indexer.Embed(ctx, llmgateway.CallMeta{}, dd.NeedsEmbedding())
	var sb *ports.SpendBlockedError
	if errors.As(err, &sb) || errors.Is(err, ports.ErrEmbeddingMismatch) {
		return nil
	}
	return err
}
