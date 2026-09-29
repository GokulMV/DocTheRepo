package scopedcontext

import (
	"path"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/core/chunker"
)

// MaxCrossFiles caps how many import-resolved files one job fetches for context.
const MaxCrossFiles = 20

// ImportCandidates resolves a file's imports to repository files, one hop, without a language server:
// following the import statements is enough to find the defining file for most references, and teams that
// need deeper resolution point a ContextProvider at a tool that does it (plan § 8.6 step 5).
//
// repoFiles is the repository's file list; goModule is the module path from go.mod (Go only).
func ImportCandidates(language, importer string, imports []chunker.Import, repoFiles []string, goModule string) []string {
	files := make(map[string]bool, len(repoFiles))
	for _, f := range repoFiles {
		files[f] = true
	}
	dir := path.Dir(importer)
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p != importer && files[p] && !seen[p] && len(out) < MaxCrossFiles {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, im := range imports {
		switch language {
		case "go":
			if goModule == "" || !strings.HasPrefix(im.Path, goModule+"/") {
				continue
			}
			pkgDir := strings.TrimPrefix(im.Path, goModule+"/")
			for _, f := range repoFiles {
				if path.Dir(f) == pkgDir && strings.HasSuffix(f, ".go") && !strings.HasSuffix(f, "_test.go") {
					add(f)
				}
			}
		case "python":
			base := im.Path
			rel := strings.HasPrefix(base, ".")
			dots := len(base) - len(strings.TrimLeft(base, "."))
			base = strings.ReplaceAll(strings.TrimLeft(base, "."), ".", "/")
			root := ""
			if rel {
				root = dir
				for i := 1; i < dots; i++ {
					root = path.Dir(root)
				}
			}
			mod := path.Join(root, base)
			add(mod + ".py")
			add(path.Join(mod, "__init__.py"))
			for _, n := range im.Names { // `from pkg import submodule`
				add(path.Join(mod, n+".py"))
			}
			if !rel { // also try under common source roots
				for _, r := range []string{"src", "app", "lib"} {
					add(path.Join(r, base) + ".py")
					add(path.Join(r, base, "__init__.py"))
				}
			}
		case "typescript", "tsx", "javascript":
			if !strings.HasPrefix(im.Path, ".") {
				continue // package imports are outside the repo
			}
			base := path.Join(dir, im.Path)
			for _, ext := range []string{"", ".ts", ".tsx", ".js", ".jsx", ".mjs", "/index.ts", "/index.tsx", "/index.js"} {
				add(base + ext)
			}
		case "java":
			suffix := "/" + strings.ReplaceAll(strings.TrimSuffix(im.Path, ".*"), ".", "/") + ".java"
			for _, f := range repoFiles {
				if strings.HasSuffix("/"+f, suffix) {
					add(f)
				}
			}
		case "rust":
			p := strings.TrimSpace(im.Path)
			if i := strings.IndexAny(p, "{ "); i >= 0 {
				p = p[:i]
			}
			p = strings.TrimSuffix(p, "::")
			segs := strings.Split(p, "::")
			if len(segs) == 0 || (segs[0] != "crate" && segs[0] != "super" && segs[0] != "self") {
				continue
			}
			root := "src"
			switch segs[0] {
			case "super":
				root = path.Dir(dir)
			case "self":
				root = dir
			}
			segs = segs[1:]
			// The last segments may name items inside a module file; try progressively shorter paths.
			for n := len(segs); n >= 1; n-- {
				mod := path.Join(append([]string{root}, segs[:n]...)...)
				add(mod + ".rs")
				add(path.Join(mod, "mod.rs"))
			}
		}
	}
	return out
}
