package repodocs

import (
	"path"
	"sort"
	"strings"
)

// Module is a part of the repository documented by one guide: a directory (with what is under it), or
// the files directly in a directory whose subdirectories are modules of their own.
type Module struct {
	Key   string   `json:"key"`   // slug, stable across runs
	Dir   string   `json:"dir"`   // "." for the repository root
	Title string   `json:"title"` // the directory path, or "Repository root"
	Files []string `json:"files"`
	Tests []string `json:"tests,omitempty"`
	Lines int      `json:"lines"`
	// Only: the module is the files directly in Dir; its subdirectories are separate modules.
	Only bool `json:"only,omitempty"`
}

// SplitOptions bounds module sizes (lines of indexed code).
type SplitOptions struct {
	MinLines, MaxLines int
	// Target is the number of modules to aim for at most.
	Target int
}

// DefaultSplit: a module is a few hundred to fifteen thousand lines, and a repository has at most about 25.
var DefaultSplit = SplitOptions{MinLines: 300, MaxLines: 15000, Target: 25}

type dirNode struct {
	name     string
	files    []string // files directly here
	lines    int      // directly here
	total    int      // here and below
	children map[string]*dirNode
}

func (d *dirNode) child(name string) *dirNode {
	if d.children == nil {
		d.children = map[string]*dirNode{}
	}
	c, ok := d.children[name]
	if !ok {
		c = &dirNode{name: name}
		d.children[name] = c
	}
	return c
}

func (d *dirNode) sum() int {
	d.total = d.lines
	for _, c := range d.children {
		d.total += c.sum()
	}
	return d.total
}

// collapse skips directories that only hold one subdirectory (src/main/java/com/acme → one level).
func (d *dirNode) path(prefix string) string {
	if prefix == "" || prefix == "." {
		return d.name
	}
	return prefix + "/" + d.name
}

// Split groups the indexed files into modules. Test files are attached to the module of the code they
// sit next to (or test, by name) rather than forming modules.
func Split(f *Facts, o SplitOptions) []Module {
	if o.MaxLines <= 0 {
		o = DefaultSplit
	}
	root := &dirNode{name: "."}
	for _, fl := range f.Files {
		if IsTest(fl.Path) {
			continue
		}
		n := root
		dir := path.Dir(fl.Path)
		if dir != "." {
			for _, seg := range strings.Split(dir, "/") {
				n = n.child(seg)
			}
		}
		n.files = append(n.files, fl.Path)
		n.lines += max(fl.Lines, 1)
	}
	root.sum()
	var mods []Module
	for attempt := 0; attempt < 4; attempt++ {
		mods = nil
		walk(root, ".", o, &mods)
		mods = mergeSmall(mods, o.MinLines)
		if o.Target <= 0 || len(mods) <= o.Target {
			break
		}
		// Too many: make modules coarser.
		o.MaxLines *= 2
		o.MinLines = o.MinLines * 3 / 2
	}
	attachTests(mods, f.AllPaths)
	sort.Slice(mods, func(i, j int) bool { return mods[i].Dir < mods[j].Dir })
	seen := map[string]int{}
	for i := range mods {
		k := slug(mods[i].Dir)
		if mods[i].Only && mods[i].Dir != "." {
			k += "-root"
		}
		if n := seen[k]; n > 0 {
			seen[k] = n + 1
			k = k + "-" + string(rune('a'+n))
		} else {
			seen[k] = 1
		}
		mods[i].Key = k
		sort.Strings(mods[i].Files)
	}
	return mods
}

func walk(n *dirNode, dir string, o SplitOptions, out *[]Module) {
	if n.total == 0 {
		return
	}
	title := dir
	if dir == "." {
		title = "Repository root"
	}
	if n.total <= o.MaxLines || len(n.children) == 0 {
		*out = append(*out, Module{Dir: dir, Title: title, Files: allFiles(n), Lines: n.total})
		return
	}
	// Too big: subdirectories of a useful size become modules; the files here and the small
	// subdirectories stay together as this directory's module.
	rest := Module{Dir: dir, Title: title, Files: append([]string{}, n.files...), Lines: n.lines, Only: true}
	for _, name := range sortedKeys(n.children) {
		c := n.children[name]
		if c.total < o.MinLines {
			rest.Files = append(rest.Files, allFiles(c)...)
			rest.Lines += c.total
			rest.Only = false
			continue
		}
		walk(c, joinDir(dir, name), o, out)
	}
	if len(rest.Files) > 0 {
		if !rest.Only {
			if dir == "." {
				rest.Title = "Repository root and small parts"
			} else {
				rest.Title = dir + " (other files)"
			}
		}
		*out = append(*out, rest)
	}
}

func joinDir(dir, name string) string {
	if dir == "." {
		return name
	}
	return dir + "/" + name
}

func allFiles(n *dirNode) []string {
	out := append([]string{}, n.files...)
	for _, c := range n.children {
		out = append(out, allFiles(c)...)
	}
	return out
}

// mergeSmall folds modules under minLines into the closest module whose directory contains theirs (a
// small leftover with no such module stays as it is).
func mergeSmall(mods []Module, minLines int) []Module {
	for {
		sort.SliceStable(mods, func(i, j int) bool { return mods[i].Lines < mods[j].Lines })
		changed := false
		for i := range mods {
			if mods[i].Lines >= minLines || len(mods) <= 1 {
				continue
			}
			best, depth := -1, -1
			for j := range mods {
				if j != i && mods[j].Dir != mods[i].Dir && isAncestor(mods[j].Dir, mods[i].Dir) {
					if d := commonDepth(mods[j].Dir, mods[i].Dir); mods[j].Dir == "." && best < 0 || d > depth {
						best, depth = j, d
					}
				}
			}
			if best < 0 {
				continue
			}
			mods[best].Files = append(mods[best].Files, mods[i].Files...)
			mods[best].Lines += mods[i].Lines
			if mods[best].Only {
				mods[best].Only = false
				if mods[best].Dir == "." {
					mods[best].Title = "Repository root and small parts"
				} else {
					mods[best].Title = mods[best].Dir + " (other files)"
				}
			}
			mods = append(mods[:i], mods[i+1:]...)
			changed = true
			break
		}
		if !changed {
			return mods
		}
	}
}

func isAncestor(a, b string) bool { return a == "." || b == a || strings.HasPrefix(b, a+"/") }

func commonDepth(a, b string) int {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	n := 0
	for n < len(as) && n < len(bs) && as[n] == bs[n] {
		n++
	}
	return n
}

// attachTests gives each module the test files next to its code, or under a test directory mirroring it
// (tests/foo → foo).
func attachTests(mods []Module, all []string) {
	for _, p := range all {
		if !IsTest(p) {
			continue
		}
		dir := path.Dir(p)
		mirror := strings.Trim(strings.NewReplacer("/__tests__", "", "/tests", "", "/test", "", "/spec", "").Replace("/"+dir), "/")
		best, depth := -1, -1
		for i, m := range mods {
			for _, d := range []string{dir, mirror} {
				if d == "" {
					d = "."
				}
				if !isAncestor(m.Dir, d) || (m.Only && m.Dir != d) {
					continue
				}
				if dd := commonDepth(m.Dir, d); m.Dir != "." && dd > depth || best < 0 {
					best, depth = i, dd
				}
			}
		}
		if best >= 0 {
			mods[best].Tests = append(mods[best].Tests, p)
		}
	}
}

// ModuleOf maps every file to its module key.
func ModuleOf(mods []Module) map[string]string {
	m := map[string]string{}
	for _, mod := range mods {
		for _, f := range mod.Files {
			m[f] = mod.Key
		}
	}
	return m
}

func slug(s string) string {
	if s == "." || s == "" {
		return "root"
	}
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}
