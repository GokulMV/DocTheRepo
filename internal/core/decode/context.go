package decode

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// System is the decode instruction.
const System = `You explain production issues for the engineers who own the code. You are given one issue, its
most recent redacted events, the code and docs it most likely involves, recent commits touching that code,
similar issues that were explained before, and runbooks. Reply with a single JSON object:
{"summary": one or two sentences on what is failing, "probable_cause": the most likely cause grounded in
the supplied code/commits/events, "impact": who or what is affected, "affected_code": [{"chunk_id": an ID
from the supplied code sections only, "reason": why}], "next_steps": concrete actions,
"confidence": "high"|"medium"|"low", "is_actionable": false when nothing in the owners' control can fix
it (expected noise, third-party outage), "suggest_known_issue": true when it looks like accepted,
recurring noise}. Never invent chunk IDs, file paths, or commits. Say so when the context is insufficient
and use low confidence.`

// gathered is the raw context before packing.
type gathered struct {
	issue    Issue
	samples  []ports.SignalEvent
	code     []ports.Chunk // frame matches first, then vector matches
	docs     []ports.Chunk
	commits  []ports.Commit
	similar  []string // issue IDs
	simText  []ports.Chunk
	runbooks []ports.Chunk
}

func (d *Decoder) gather(ctx context.Context, meta llmgateway.CallMeta, is Issue) (*gathered, error) {
	g := &gathered{issue: is}
	var err error
	if g.samples, err = d.Store.Samples(ctx, is.ID, samplesUsed); err != nil {
		return nil, err
	}
	repos, err := d.Store.ServiceRepos(ctx, is.Service)
	if err != nil {
		return nil, err
	}
	if is.RepoID != "" {
		repos = uniq(append([]string{is.RepoID}, repos...))
	}

	// 1. Exact frame → chunk lookups (path suffix + symbol), most specific first.
	var frames []ports.StackFrame
	for _, s := range g.samples {
		frames = append(frames, s.Stack...)
	}
	if len(frames) > 0 {
		if g.code, err = d.Store.FrameChunks(ctx, repos, dedupeFrames(frames)); err != nil {
			return nil, err
		}
	}

	// 2. One embedding of the issue text drives code search (when frames did not fill the budget), similar
	// decoded issues, and runbooks.
	if d.Index != nil {
		if vs, _, err := d.GW.Embed(ctx, meta, []string{queryText(is, g.samples)}); err == nil {
			vec := vs[0]
			if len(g.code) < maxCodeChunks {
				hits, err := d.Index.Search(ctx, vec, maxCodeChunks, ports.VectorFilter{RepoIDs: repos, Sources: []ports.ChunkSource{ports.SourceCode}})
				if err != nil && !errors.Is(err, ports.ErrNotFound) {
					return nil, err
				}
				g.code = append(g.code, d.load(ctx, hits, 0, maxCodeChunks-len(g.code), g.code)...)
			}
			hits, err := d.Index.Search(ctx, vec, 6, ports.VectorFilter{Sources: []ports.ChunkSource{ports.SourceIssueDecode}})
			if err != nil && !errors.Is(err, ports.ErrNotFound) {
				return nil, err
			}
			var simIDs []string
			for _, h := range hits {
				if h.Score >= SimilarThreshold && h.ChunkID != DecodeChunkID(is.ID) {
					simIDs = append(simIDs, h.ChunkID)
				}
			}
			if len(simIDs) > 3 {
				simIDs = simIDs[:3]
			}
			if len(simIDs) > 0 {
				owners, err := d.Store.IssueForChunks(ctx, simIDs)
				if err != nil {
					return nil, err
				}
				for _, id := range simIDs {
					if owner, ok := owners[id]; ok {
						g.similar = append(g.similar, owner)
					}
				}
				g.simText, _ = d.Store.Chunks(ctx, simIDs)
			}
			hits, err = d.Index.Search(ctx, vec, 2, ports.VectorFilter{Sources: []ports.ChunkSource{ports.SourceConfluence}})
			if err != nil && !errors.Is(err, ports.ErrNotFound) {
				return nil, err
			}
			g.runbooks = d.load(ctx, hits, RunbookThreshold, 2, nil)
		} else if !errors.Is(err, llmgateway.ErrNoRoute) {
			return nil, err
		}
	}

	// 3. Docs and recent commits for the code's files.
	byRepo := map[string][]string{}
	for _, c := range g.code {
		byRepo[c.RepoID] = append(byRepo[c.RepoID], c.Path)
	}
	for _, repo := range sortedRepoKeys(byRepo) {
		paths := uniq(byRepo[repo])
		docs, err := d.Store.Docs(ctx, repo, paths)
		if err != nil {
			return nil, err
		}
		g.docs = append(g.docs, docs...)
		if d.Commits == nil {
			continue
		}
		seen := map[string]bool{}
		for i, p := range paths {
			if i == maxCommitPaths {
				break
			}
			cs, err := d.Commits(ctx, repo, p, d.now().Add(-commitWindow))
			if err != nil {
				continue // the code host being unreachable should not block a decode
			}
			for _, c := range cs {
				if !seen[c.SHA] {
					seen[c.SHA] = true
					g.commits = append(g.commits, c)
				}
			}
		}
	}
	sort.SliceStable(g.commits, func(i, j int) bool { return g.commits[i].At.After(g.commits[j].At) })
	return g, nil
}

// load resolves vector hits to live chunks above a score, skipping ones already chosen.
func (d *Decoder) load(ctx context.Context, hits []ports.VectorHit, minScore float64, limit int, have []ports.Chunk) []ports.Chunk {
	if limit <= 0 {
		return nil
	}
	skip := map[string]bool{}
	for _, c := range have {
		skip[c.ID] = true
	}
	var ids []string
	for _, h := range hits {
		if h.Score >= minScore && !skip[h.ChunkID] {
			ids = append(ids, h.ChunkID)
		}
	}
	chunks, err := d.Store.Chunks(ctx, ids)
	if err != nil {
		return nil
	}
	byID := map[string]ports.Chunk{}
	for _, c := range chunks {
		byID[c.ID] = c
	}
	var out []ports.Chunk
	for _, id := range ids {
		if c, ok := byID[id]; ok && c.Live() && len(out) < limit {
			out = append(out, c)
		}
	}
	return out
}

// prompt packs the context by priority into the token budget (chars/4). It returns the chunks shown, by ID.
func (g *gathered) prompt(budgetTokens int) (string, map[string]ports.Chunk) {
	limit := budgetTokens * 4
	var b strings.Builder
	supplied := map[string]ports.Chunk{}
	add := func(s string) bool {
		if b.Len()+len(s) > limit {
			return false
		}
		b.WriteString(s)
		return true
	}
	is := g.issue
	add(fmt.Sprintf("## Issue\nKind: %s\nTitle: %s\nService: %s\nEnvironment: %s\nOccurrences: %d\nSources: %s\n\n",
		is.Kind, is.Title, is.Service, is.Environment, is.Occurrences, strings.Join(is.Sources, ", ")))
	if len(g.samples) > 0 {
		add("## Recent events (redacted, newest first)\n")
		for i, s := range g.samples {
			add(sampleText(i+1, s))
		}
		add("\n")
	}
	if len(g.code) > 0 {
		add("## Code (cite these chunk IDs in affected_code)\n")
		for _, c := range g.code {
			if add(chunkText(c, 6000)) {
				supplied[c.ID] = c
			}
		}
		add("\n")
	}
	if len(g.commits) > 0 {
		add("## Commits in the last 7 days touching that code\n")
		for i, c := range g.commits {
			if i == 10 {
				break
			}
			add(fmt.Sprintf("- %s %s %s: %s\n", c.At.UTC().Format("2006-01-02"), short(c.SHA), c.Author, firstLine(c.Message)))
		}
		add("\n")
	}
	if len(g.docs) > 0 {
		add("## Docs for that code\n")
		for _, c := range g.docs {
			if add(chunkText(c, 3000)) {
				supplied[c.ID] = c
			}
		}
		add("\n")
	}
	if len(g.simText) > 0 {
		add("## Similar issues explained before\n")
		for _, c := range g.simText {
			add("---\n" + truncate(c.Content, 2000) + "\n")
		}
		add("\n")
	}
	if len(g.runbooks) > 0 {
		add("## Runbooks\n")
		for _, c := range g.runbooks {
			add(chunkText(c, 4000))
		}
	}
	return b.String(), supplied
}

func sampleText(n int, s ports.SignalEvent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d. %s [%s] %s\n", n, s.OccurredAt.UTC().Format("2006-01-02T15:04:05Z"), s.Severity, s.Title)
	if s.ExceptionType != "" {
		fmt.Fprintf(&b, "   exception: %s\n", s.ExceptionType)
	}
	if s.Message != "" && s.Message != s.Title {
		fmt.Fprintf(&b, "   message: %s\n", truncate(s.Message, 1500))
	}
	for i, f := range s.Stack {
		if i == 8 {
			break
		}
		fmt.Fprintf(&b, "   at %s %s (%s:%d)\n", f.Module, f.Function, f.File, f.Line)
	}
	keys := make([]string, 0, len(s.Attrs))
	for k := range s.Attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		if i == 12 {
			break
		}
		fmt.Fprintf(&b, "   %s=%s\n", k, truncate(s.Attrs[k], 200))
	}
	return b.String()
}

func chunkText(c ports.Chunk, max int) string {
	loc := c.Path
	if c.Symbol != "" {
		loc += " · " + c.Symbol
	}
	if c.StartLine > 0 {
		loc += fmt.Sprintf(" (lines %d-%d)", c.StartLine, c.EndLine)
	}
	return fmt.Sprintf("[%s] %s\n```\n%s\n```\n", c.ID, loc, truncate(c.Content, max))
}

// queryText is what the issue is searched by.
func queryText(is Issue, samples []ports.SignalEvent) string {
	parts := []string{is.Title, is.Service}
	if len(samples) > 0 {
		s := samples[0]
		parts = append(parts, s.ExceptionType, truncate(s.Message, 1000))
		for i, f := range s.Stack {
			if i == 5 {
				break
			}
			parts = append(parts, f.Module+" "+f.Function)
		}
	}
	return strings.Join(uniq(parts), "\n")
}

// dedupeFrames keeps frames with a file or function, in-app first, without repeats, at most 10.
func dedupeFrames(frames []ports.StackFrame) []ports.StackFrame {
	sort.SliceStable(frames, func(i, j int) bool { return frames[i].InApp && !frames[j].InApp })
	seen := map[string]bool{}
	var out []ports.StackFrame
	for _, f := range frames {
		key := f.File + "|" + f.Module + "|" + f.Function
		if (f.File == "" && f.Module == "") || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
		if len(out) == 10 {
			break
		}
	}
	return out
}

func sortedRepoKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func short(sha string) string { return truncate(sha, 8) }

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
