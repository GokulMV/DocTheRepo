package repodocs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Names written in the material (environment variables, config keys, table names) are grounded even when
// they are not declarations; a citation of a whole file ([path:0]) is valid when the file exists.
func TestCheckGroundsOnTheMaterial(t *testing.T) {
	f := &Facts{
		Files:    []File{{Path: "internal/config/config.go", Lines: 40, Symbols: []Symbol{{Name: "Load"}}}},
		AllPaths: []string{"internal/config/config.go", "deploy/compose/docker-compose.yml"},
	}
	spec := Spec{Sections: []Section{{Key: "config", Title: "Configuration", Required: true}}}
	material := "### deploy/compose/docker-compose.yml\nDTH_LISTEN: 127.0.0.1:8080\nDTH_PUBLIC_URL: http://x\n" +
		"CREATE TABLE usage_events (id uuid);\ndatabase:\n  max_conns: 20\n"
	md := "`Load` reads `DTH_LISTEN`, `DTH_PUBLIC_URL` and `DTH_*` [internal/config/config.go:12]. " +
		"Rows go to `usage_events` [deploy/compose/docker-compose.yml:0]. `VAULT_ADDR`, `VAULT_TOKEN`, `VAULT_NAMESPACE` and `RoleWorker` are invented."
	w := &written{AtAGlance: "It reads settings."}
	w.Sections = append(w.Sections, struct {
		Key      string `json:"key"`
		Markdown string `json:"markdown"`
	}{"config", md})

	r := check(spec, w, f, f.Known(), material)
	assert.ElementsMatch(t, []string{"VAULT_ADDR", "VAULT_TOKEN", "VAULT_NAMESPACE", "RoleWorker"}, r.unknown["config"], "only the names in neither the code nor the material")
	assert.Equal(t, 2, r.valid["config"], "a line citation and a whole-file citation")
	assert.Empty(t, r.bad["config"])
	assert.NotEmpty(t, r.hard, "four invented names still need a repair")

	r = check(spec, w, f, f.Known(), "")
	assert.Contains(t, r.unknown["config"], "DTH_LISTEN", "without the material, config names are not grounded")
}

func TestDropDeadRefs(t *testing.T) {
	f := &Facts{AllPaths: []string{"internal/core/x.go", "README.md"}}
	md := "Simplified the app [web:0]. Docs moved [Repository root:0] [internal/core:0], see [internal/core/x.go:12] and [README.md:3-5].\n" +
		"A [link](https://example.com:8080) and [web:0](x) stay. | Area [docs:0] |"
	assert.Equal(t, "Simplified the app. Docs moved, see [internal/core/x.go:12] and [README.md:3-5].\n"+
		"A [link](https://example.com:8080) and [web:0](x) stay. | Area |", DropDeadRefs(md, f))
}
