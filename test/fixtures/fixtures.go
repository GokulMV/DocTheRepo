// Package fixtures loads the fixture repositories in test/testdata/repos (one directory per owner/repo)
// and seeds them into the GitHub mock. The E2E stack, the retrieval eval, and the perf runs share them.
package fixtures

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/GokulMV/DocTheRepo/test/mocks/githubmock"
)

// Dir is the absolute path of test/testdata/repos.
func Dir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "testdata", "repos")
}

// Load reads every <owner>/<repo> directory under dir into full name → path → content.
func Load(dir string) (map[string]map[string]string, error) {
	out := map[string]map[string]string{}
	owners, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, o := range owners {
		if !o.IsDir() {
			continue
		}
		repos, err := os.ReadDir(filepath.Join(dir, o.Name()))
		if err != nil {
			return nil, err
		}
		for _, r := range repos {
			if !r.IsDir() {
				continue
			}
			root := filepath.Join(dir, o.Name(), r.Name())
			files := map[string]string{}
			err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				b, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				rel, _ := filepath.Rel(root, p)
				files[filepath.ToSlash(rel)] = string(b)
				return nil
			})
			if err != nil {
				return nil, err
			}
			out[o.Name()+"/"+r.Name()] = files
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no fixture repositories under %s", dir)
	}
	return out, nil
}

// Seed creates every repository on the mock with a main branch and returns their names, sorted.
func Seed(gh *githubmock.Server, repos map[string]map[string]string) []string {
	var names []string
	for name, files := range repos {
		gh.CreateRepo(name, "main", files)
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// SourceFiles counts non-Markdown files (what the pipeline chunks as code).
func SourceFiles(files map[string]string) int {
	n := 0
	for p := range files {
		if !strings.HasSuffix(p, ".md") {
			n++
		}
	}
	return n
}
