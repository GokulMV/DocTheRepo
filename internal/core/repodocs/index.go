package repodocs

import (
	"sort"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/core/chunker"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Chunks turns a document into search pieces: its summary and one piece per section, each starting with
// where it sits (repository › document › section) so a piece read alone still makes sense.
func Chunks(repo string, d Doc) []ports.Chunk {
	if d.Status != "ok" {
		return nil
	}
	p := DocPath(d)
	crumb := repo + " › " + d.Title
	if d.Type == "module" {
		crumb = repo + " › Module " + d.Title
	}
	mk := func(key, title, body string) ports.Chunk {
		// The confidence travels with the text, so the answering model knows when to prefer the code.
		content := crumb + " › " + title + " (written from commit " + short(d.SourceSHA) + ", confidence " + Label(d.Confidence) + ")\n\n" + strings.TrimSpace(body)
		return ports.Chunk{ID: chunker.ID(repo, p, key), RepoID: d.RepoID, Scope: repo, Source: ports.SourceGeneratedDoc, Path: p,
			Symbol: title, Language: "markdown", Content: content, ContentHash: chunker.Hash(content), CommitSHA: d.SourceSHA,
			URL: Link(d) + "#" + key}
	}
	out := []ports.Chunk{mk("at-a-glance", "At a glance", d.AtAGlance)}
	for _, s := range d.Sections {
		out = append(out, mk(s.Key, s.Title, s.Markdown))
	}
	return out
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// SystemPath is where the System architecture's sections live in the search index.
const SystemPath = "@docs/system/system"

// SystemChunks turns the System architecture into search pieces. Each piece carries every repository it
// was written from (RequiresRepos), so only someone who can read all of them finds it; its own repository
// is the first of them, so the usual repository check applies too.
func SystemChunks(d Doc, repoIDs []string) []ports.Chunk {
	if d.Status != "ok" || len(repoIDs) == 0 {
		return nil
	}
	ids := append([]string(nil), repoIDs...)
	sort.Strings(ids)
	mk := func(key, title, body string) ports.Chunk {
		content := "System architecture › " + title + " (confidence " + Label(d.Confidence) + ")\n\n" + strings.TrimSpace(body)
		return ports.Chunk{ID: chunker.ID("system", SystemPath, key), RepoID: ids[0], Scope: "System", Source: ports.SourceGeneratedDoc,
			Path: SystemPath, Symbol: title, Language: "markdown", Content: content, ContentHash: chunker.Hash(content + "\x00" + strings.Join(ids, ",")),
			URL: "/system#" + key, RequiresRepos: ids}
	}
	out := []ports.Chunk{mk("at-a-glance", "At a glance", d.AtAGlance)}
	for _, s := range d.Sections {
		out = append(out, mk(s.Key, s.Title, s.Markdown))
	}
	return out
}
