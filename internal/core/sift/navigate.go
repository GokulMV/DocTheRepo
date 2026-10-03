package sift

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Navigation budgets: they bound cost on large monorepos; reaching one marks the report incomplete.
const (
	MaxPaths     = 5000 // indexed files considered
	DirectFiles  = 40   // with this few files, skip directories and judge files directly
	MaxDirs      = 120  // directories judged
	MaxPreviews  = 60   // files previewed and judged
	MaxReads     = 8    // files read in full and judged piece by piece
	previewChars = 700
	listEntries  = 40
)

// Tree is the indexed content a question may read (already restricted by the repository ACL).
type Tree interface {
	Paths(ctx context.Context, limit int) ([]File, error)
	Read(ctx context.Context, f File, limit int) ([]ports.Chunk, error)
}

// File is one indexed file.
type File struct {
	Repo, Path string
}

type dir struct {
	key      string // repo + ":" + directory path ("" for the repository root)
	label    string
	children map[string]*dir
	files    []File
	total    int
}

// Navigate looks for evidence the way a person explores an unfamiliar repository: judge directories from
// the names inside them, descend only into promising ones, judge files from a short preview, then read
// the best files and keep the pieces that pass. It returns kept pieces, most relevant first.
func (s *Sifter) Navigate(ctx context.Context, meta llmgateway.CallMeta, question string, t Tree, answer llmgateway.Route) ([]ports.Chunk, Report) {
	var rep Report
	judge, why := s.Worthwhile(ctx, answer)
	if why != "" {
		rep.Skipped = why
		return nil, rep
	}
	files, err := t.Paths(ctx, MaxPaths)
	if err != nil || len(files) == 0 {
		rep.Skipped = "nothing indexed to explore"
		return nil, rep
	}
	rep.Incomplete = len(files) >= MaxPaths
	root := buildTree(files)

	candidates := files
	if len(files) > DirectFiles {
		candidates = s.walk(ctx, meta, judge, question, root, &rep)
	}
	if len(candidates) > MaxPreviews {
		candidates, rep.Incomplete = candidates[:MaxPreviews], true
	}
	if len(candidates) == 0 {
		return nil, rep
	}

	// Files: judge a short preview of each.
	previews := make([]item, 0, len(candidates))
	kept := make([]File, 0, len(candidates))
	for _, f := range candidates {
		cs, err := t.Read(ctx, f, 2)
		if err != nil || len(cs) == 0 {
			continue
		}
		previews = append(previews, filePreview(f, cs))
		kept = append(kept, f)
	}
	got, err := s.judgeAll(ctx, meta, judge, question, previews, fileQuestions, &rep)
	if err != nil {
		rep.Skipped = "judge failed: " + err.Error()
		return nil, rep
	}
	type scored struct {
		f File
		p float64
	}
	var good []scored
	for i, f := range kept {
		if got[i][0] >= s.keepAt() {
			good = append(good, scored{f, got[i][0]})
		}
	}
	sort.SliceStable(good, func(a, b int) bool { return good[a].p > good[b].p })
	if len(good) > MaxReads {
		good, rep.Incomplete = good[:MaxReads], true
	}

	// The best files, piece by piece.
	var pieces []ports.Chunk
	for _, g := range good {
		cs, err := t.Read(ctx, g.f, 12)
		if err == nil {
			pieces = append(pieces, cs...)
			rep.Navigated++
		}
	}
	if len(pieces) == 0 {
		return nil, rep
	}
	scores, err := s.score(ctx, meta, judge, question, pieces, &rep)
	if err != nil {
		rep.Skipped = "judge failed: " + err.Error()
		return nil, rep
	}
	out := s.keep(pieces, scores, &rep)
	if rep.Weak {
		return nil, rep // nothing in the explored files answers it: better "not found" than padding
	}
	rep.Kept = len(out)
	return out, rep
}

// walk judges directories level by level and returns the files inside promising ones, best first.
func (s *Sifter) walk(ctx context.Context, meta llmgateway.CallMeta, judge llmgateway.Route, question string, root *dir, rep *Report) []File {
	files := append([]File(nil), root.files...)
	frontier := sortedChildren(root)
	for len(frontier) > 0 {
		if rep.Directories+len(frontier) > MaxDirs {
			frontier, rep.Incomplete = frontier[:max(0, MaxDirs-rep.Directories)], true
			if len(frontier) == 0 {
				break
			}
		}
		items := make([]item, len(frontier))
		for i, d := range frontier {
			items[i] = item{Kind: "directory", Path: d.label, Entries: entries(d), Files: d.total}
		}
		got, err := s.judgeAll(ctx, meta, judge, question, items, dirQuestions, rep)
		if err != nil {
			break
		}
		rep.Directories += len(frontier)
		type scored struct {
			d *dir
			p float64
		}
		var open []scored
		for i, d := range frontier {
			if got[i][0] >= s.keepAt() {
				open = append(open, scored{d, got[i][0]})
			}
		}
		sort.SliceStable(open, func(a, b int) bool { return open[a].p > open[b].p })
		var next []*dir
		for _, o := range open {
			files = append(files, o.d.files...)
			next = append(next, sortedChildren(o.d)...)
		}
		frontier = next
	}
	return files
}

func dirQuestions(i int, it item) []ports.JudgeQuestion {
	return []ports.JudgeQuestion{{ID: fmt.Sprintf("dir%d", i), Instructions: fmt.Sprintf(
		"Is directory state.items[%d] (%q) worth exploring to answer the question? Judge from the names of its entries and how many files it holds. A partial listing does not prove useful files are absent; folder names are clues, not rules.", i, it.Path)}}
}

func fileQuestions(i int, it item) []ports.JudgeQuestion {
	return []ports.JudgeQuestion{{ID: fmt.Sprintf("file%d", i), Instructions: fmt.Sprintf(
		"Judging from its path and preview, is file state.items[%d] (%q) likely to contain material that answers the question: the implementation, configuration, documentation or tests of what is asked? Files that only mention the topic do not count.", i, it.Path)}}
}

func filePreview(f File, cs []ports.Chunk) item {
	var syms []string
	for _, c := range cs {
		if s := symbolOf(c); s != "" {
			syms = append(syms, s)
		}
	}
	text := clip(cs[0].Content, previewChars)
	if len(syms) > 0 {
		text = "Declarations: " + strings.Join(syms, ", ") + "\n" + text
	}
	return item{Kind: kindOf(cs[0].Source) + " file", Repo: f.Repo, Path: f.Path, Text: text}
}

func buildTree(files []File) *dir {
	root := &dir{children: map[string]*dir{}}
	repos := map[string]bool{}
	for _, f := range files {
		repos[f.Repo] = true
	}
	multi := len(repos) > 1
	for _, f := range files {
		node := root
		node.total++
		var parts []string
		if multi {
			parts = append(parts, f.Repo+":")
		}
		if d := path.Dir(f.Path); d != "." && d != "/" {
			parts = append(parts, strings.Split(strings.Trim(d, "/"), "/")...)
		}
		key := f.Repo + ":"
		for _, p := range parts {
			if !strings.HasSuffix(p, ":") {
				key += p + "/"
			}
			child, ok := node.children[p]
			if !ok {
				label := strings.TrimSuffix(strings.TrimPrefix(key, f.Repo+":"), "/")
				if multi {
					label = f.Repo + ":" + label
				}
				child = &dir{key: key + "\x00" + p, label: label, children: map[string]*dir{}}
				node.children[p] = child
			}
			node = child
			node.total++
		}
		node.files = append(node.files, f)
	}
	return root
}

func sortedChildren(d *dir) []*dir {
	out := make([]*dir, 0, len(d.children))
	for _, c := range d.children {
		out = append(out, c)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].label < out[b].label })
	return out
}

func entries(d *dir) []string {
	var out []string
	for _, c := range sortedChildren(d) {
		out = append(out, path.Base(c.label)+"/")
	}
	for _, f := range d.files {
		out = append(out, path.Base(f.Path))
	}
	sort.Strings(out)
	if len(out) > listEntries {
		out = append(out[:listEntries], fmt.Sprintf("… %d more", len(out)-listEntries))
	}
	return out
}
