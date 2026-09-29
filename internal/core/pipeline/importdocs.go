package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/core/chunker"
	"github.com/GokulMV/DocTheRepo/internal/core/docassembly"
	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/manifest"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// ImportPayload is the import_docs job payload: a one-time (repeatable) import of a repository's
// existing Markdown so Q&A and the Library have it before the first generated docs land (plan § 2).
type ImportPayload struct {
	RepoID string `json:"repo_id"`
	// Prefixes limits the import to paths under these prefixes (empty = every Markdown file).
	Prefixes []string `json:"prefixes,omitempty"`
	// Ref is the commit or branch to import (empty = the tracked branch).
	Ref string `json:"ref,omitempty"`
}

// ImportResult is stored on the job.
type ImportResult struct {
	Files    int      `json:"files"`
	Added    int      `json:"added"`
	Changed  int      `json:"changed"`
	Removed  int      `json:"removed"`
	Skipped  int      `json:"skipped"`
	Embedded int      `json:"embedded"`
	Notes    []string `json:"notes,omitempty"`
}

// ImportDocs chunks every Markdown file outside the generated-docs path as imported_doc. Generated docs
// always win over imported ones for the same chunk ID (manifest precedence), and re-running the import
// only writes what changed.
func (p *Pipeline) ImportDocs(ctx context.Context, job ports.Job) (ports.Outcome, error) {
	var pl ImportPayload
	if err := json.Unmarshal(job.Payload, &pl); err != nil {
		return ports.Outcome{}, ports.Permanent(fmt.Errorf("decode payload: %w", err))
	}
	if pl.RepoID == "" {
		pl.RepoID = job.RepoID
	}
	repo, err := p.Repos.Get(ctx, pl.RepoID)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return ports.Outcome{Status: ports.JobAborted, Message: "repository is no longer tracked"}, nil
		}
		return ports.Outcome{}, err
	}
	host, err := p.Hosts(ctx, repo.ConnectorID)
	if err != nil {
		return ports.Outcome{}, err
	}
	ref := pl.Ref
	if ref == "" {
		if ref, err = host.BranchHead(ctx, repo.FullName, repo.Branch()); err != nil {
			return ports.Outcome{}, err
		}
	}
	files, err := host.ListTree(ctx, repo.FullName, ref)
	if err != nil {
		return ports.Outcome{}, err
	}
	var res ImportResult
	var embed []ports.Chunk
	var w ports.ChunkWrite
	type treeDoc struct {
		path, summary, content string
		sections               []ports.DocSection
	}
	var tree []treeDoc
	for _, f := range files {
		if !chunker.IsMarkdown(f) || strings.HasPrefix(f, repo.DocsPath) || !underAny(f, pl.Prefixes) {
			continue
		}
		b, err := host.GetFile(ctx, repo.FullName, f, ref)
		if errors.Is(err, ports.ErrNotFound) {
			continue
		}
		if err != nil {
			return ports.Outcome{}, err
		}
		if len(b) > chunker.MaxFileBytes {
			continue
		}
		fresh := chunker.Docs(repo.FullName, repo.ID, f, b, chunker.DocOptions{Source: ports.SourceImportedDoc})
		for i := range fresh {
			fresh[i].CommitSHA = ref
		}
		stored, err := p.Chunks.ForPaths(ctx, repo.ID, ports.SourceImportedDoc, []string{f})
		if err != nil {
			return ports.Outcome{}, err
		}
		d := manifest.Diff(stored, fresh, []string{f})
		a, c, r := d.Counts()
		res.Files, res.Added, res.Changed, res.Removed, res.Skipped = res.Files+1, res.Added+a, res.Changed+c, res.Removed+r, res.Skipped+len(d.Skipped)
		w.Upserts = append(append(w.Upserts, d.Added...), d.Changed...)
		w.Revive = append(w.Revive, d.Revived...)
		w.Remove = append(w.Remove, d.Removed...)
		embed = append(embed, d.NeedsEmbedding()...)
		td := treeDoc{path: f, summary: firstParagraph(b), content: string(b)}
		for _, ch := range fresh {
			td.sections = append(td.sections, ports.DocSection{ChunkID: ch.ID, Title: ch.Symbol})
		}
		tree = append(tree, td)
	}
	if _, err := p.Chunks.Apply(ctx, w); err != nil {
		return ports.Outcome{}, err
	}
	n, err := p.Indexer.Embed(ctx, llmgateway.CallMeta{RepoID: repo.ID, JobID: job.ID}, embed)
	var sb *ports.SpendBlockedError
	switch {
	case errors.As(err, &sb):
		res.Notes = append(res.Notes, "embedding blocked by the spend guard: "+sb.Error())
	case err != nil:
		return ports.Outcome{}, err
	}
	res.Embedded = n
	for _, td := range tree {
		if err := p.Docs.ReplaceFile(ctx, repo.ID, repo.FullName, ports.DocFile{Path: td.path, Summary: td.summary, Content: td.content,
			CommitSHA: ref, Sections: td.sections}); err != nil {
			return ports.Outcome{}, err
		}
	}
	return ports.Outcome{Status: ports.JobDone, Result: res}, nil
}

func underAny(p string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, pre := range prefixes {
		if p == pre || strings.HasPrefix(p, strings.TrimSuffix(pre, "/")+"/") {
			return true
		}
	}
	return false
}

// firstParagraph is a doc's summary for the Tree: the generated-doc summary when present, else the first
// non-heading paragraph.
func firstParagraph(b []byte) string {
	if s := docassembly.Summary(b); s != "" {
		return s
	}
	for _, para := range strings.Split(string(b), "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" || strings.HasPrefix(para, "#") || strings.HasPrefix(para, "<!--") {
			continue
		}
		if len(para) > 300 {
			para = para[:297] + "..."
		}
		return strings.Join(strings.Fields(para), " ")
	}
	return ""
}
