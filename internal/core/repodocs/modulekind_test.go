package repodocs

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func moduleSpecFixture() (*env, map[string]*Module) {
	storeSQL := "func (s *Store) Claim(ctx context.Context) error {\n\t_, err := s.db.Exec(ctx, `UPDATE jobs SET state = 'running'\n\t\tFROM unnest($1::text[]) AS k WHERE jobs.key = k`)\n" +
		"\t_, err = s.db.Exec(ctx, `INSERT INTO jobs (key) VALUES ($1)`)\n\trows, err := s.db.Query(ctx, `SELECT r.name FROM repos r JOIN jobs j ON j.repo = r.id`)\n\treturn err\n}"
	f := &Facts{Files: []File{
		{Path: "internal/store/jobs.go", Lines: 400, Symbols: []Symbol{{Name: "Store.Claim", Path: "internal/store/jobs.go", Line: 10, Body: storeSQL}}},
		{Path: "internal/config/config.go", Lines: 400, Symbols: []Symbol{{Name: "Config", Path: "internal/config/config.go", Line: 5,
			Body: "type Config struct {\n\tAddr string `yaml:\"addr\"` // read from the file\n}\n// Load reads the config from the file given."}}},
		{Path: "internal/ports/ports.go", Lines: 400, Symbols: []Symbol{{Name: "Job", Path: "internal/ports/ports.go", Line: 3,
			Body: "type Job struct {\n\tID string `db:\"id\"`\n}"}}},
		{Path: "internal/schema/schema.go", Lines: 400},
	}, AllPaths: []string{"internal/schema/migrations/0001_init.up.sql", "internal/schema/migrations/0001_init.down.sql", "internal/store/jobs_test.go"}}
	mods := Split(f, SplitOptions{MinLines: 100, MaxLines: 1000, Target: 25})
	e := newEnv(f, mods, nil, nil)
	byDir := map[string]*Module{}
	for i := range e.mods {
		byDir[e.mods[i].Dir] = &e.mods[i]
	}
	return e, byDir
}

func sectionOf(s Spec, key string) Section {
	for _, sec := range s.Sections {
		if sec.Key == key {
			return sec
		}
	}
	return Section{}
}

func TestModuleSpec_DataTargetFollowsWhatTheModuleStores(t *testing.T) {
	e, byDir := moduleSpecFixture()
	base, _ := SpecByType("module")
	for _, dir := range []string{"internal/store", "internal/config", "internal/ports", "internal/schema"} {
		require.Contains(t, byDir, dir)
	}

	// SQL tables, a persisted type, or migrations under it: a full section.
	for _, dir := range []string{"internal/store", "internal/ports", "internal/schema"} {
		assert.Equal(t, 150, sectionOf(e.moduleSpec(base, byDir[dir]), "data").Words, dir)
	}
	// Config structs only: a brief section, and the catalogue itself is left alone.
	cfg := sectionOf(e.moduleSpec(base, byDir["internal/config"]), "data")
	assert.Equal(t, briefDataWords, cfg.Words)
	assert.Contains(t, cfg.Guide, "one or two sentences")
	assert.Equal(t, 150, sectionOf(base, "data").Words)

	store := e.moduleData(byDir["internal/store"]).text()
	assert.Contains(t, store, "- jobs (3 statements) [internal/store/jobs.go:11]", "the first statement's own line is cited")
	assert.Contains(t, store, "- repos (1 statement) [internal/store/jobs.go:14]")
	assert.NotContains(t, store, "unnest", "a table function is not a table")
	assert.Contains(t, e.moduleData(byDir["internal/schema"]).text(), "- internal/schema/migrations/0001_init.up.sql")
	assert.Contains(t, e.moduleData(byDir["internal/ports"]).text(), "- Job [internal/ports/ports.go:3]")
	assert.Empty(t, e.moduleData(byDir["internal/config"]).tables, `"from the file" in prose is not SQL`)

	in := e.inputs(context.Background(), base, byDir["internal/config"], 100000)
	assert.Contains(t, in, "## Data this module owns\nNone found")
	in = e.inputs(context.Background(), base, byDir["internal/store"], 100000)
	assert.Contains(t, in, "Tables its SQL names (most used first):\n- jobs")
}

func TestDocsModuleGetsTheDocSetGuide(t *testing.T) {
	f := &Facts{Files: []File{
		{Path: "docs/install.md", Lines: 300}, {Path: "docs/guide/usage.md", Lines: 300},
		{Path: "docs/generated/internal/app/app.go.md", Lines: 300, Symbols: []Symbol{{Name: "app", Body: "# app.go\nSECRET-GENERATED-TEXT"}}},
		{Path: "internal/app/app.go", Lines: 900}, {Path: "internal/app/notes.md", Lines: 50},
	}, AllPaths: []string{"docs/images/flow.png", "docs/generated/internal/app/other.go.md"}}
	mods := Split(f, SplitOptions{MinLines: 100, MaxLines: 1000, Target: 25})
	read := func(_ context.Context, p string) ([]byte, error) {
		if p == "docs/install.md" {
			return []byte("# Installing\n\nRun the installer on a fresh machine.\n\n```sh\n# not a heading\n```\n\n## Upgrading\nStop first.\n"), nil
		}
		return nil, errors.New("not read")
	}
	e := newEnv(f, mods, nil, read)
	var docs, app *Module
	for i := range e.mods {
		switch e.mods[i].Dir {
		case "docs":
			docs = &e.mods[i]
		case "internal":
			app = &e.mods[i]
		}
	}
	require.NotNil(t, docs)
	require.NotNil(t, app)
	assert.True(t, IsDocsModule(docs))
	assert.False(t, IsDocsModule(app), "code with a stray Markdown note is still code")
	assert.True(t, IsGeneratedDoc("docs/generated/internal/app/app.go.md"))
	assert.False(t, IsGeneratedDoc("internal/generated/types.go"), "generated code is not generated documentation")

	base, _ := SpecByType("module")
	spec := e.moduleSpec(base, docs)
	assert.True(t, spec.Prose)
	assert.Equal(t, "module", spec.Type, "it is still the module's guide, at the same address")
	assert.NotEmpty(t, sectionOf(spec, "contents").Key)
	assert.Empty(t, sectionOf(spec, "interface").Key, "no public interface for prose")
	assert.Equal(t, "interface", sectionOf(e.moduleSpec(base, app), "interface").Key)
	assert.Equal(t, []string{"docs/guide/usage.md", "docs/install.md"}, moduleSources(spec, docs), "generated docs are not its sources")

	in := e.inputs(context.Background(), spec, docs, 100000)
	assert.Contains(t, in, "docs: 2 hand-written documents. Directory: docs; 1 images")
	assert.Contains(t, in, "2 files under docs/generated/ are generated", "generated docs are one line")
	assert.NotContains(t, in, "SECRET-GENERATED-TEXT")
	assert.NotContains(t, in, "### docs/generated/")
	assert.Contains(t, in, "### docs/install.md (300 lines)\n    1| # Installing\n    9| ## Upgrading\nOpens: Run the installer on a fresh machine.\n")
	assert.NotContains(t, in, "# not a heading")
	assert.Contains(t, in, "## Modules of the code")
}

func TestReport_TargetsFollowTheWrittenGuide(t *testing.T) {
	brief := "It owns no persistent data. The only state is the `Config` struct, loaded once at start-up from the YAML file and the environment [internal/config/config.go:5], then passed by value to the modules that need it; nothing is cached or written."
	docs := []Doc{
		{Type: "module", Key: "internal-config", Title: "internal/config", Status: "ok", Confidence: 0.8,
			Sections: []DocSection{{Key: "data", Title: "Data it owns", Markdown: brief, Target: briefDataWords, Score: 0.9}}},
		{Type: "module", Key: "old", Title: "old", Status: "ok", Confidence: 0.8,
			Sections: []DocSection{{Key: "data", Title: "Data it owns", Markdown: brief, Score: 0.9}}},
		{Type: "module", Key: "docs", Title: "docs", Status: "ok", Confidence: 0.8,
			Sections: []DocSection{{Key: "purpose", Title: "Purpose", Markdown: "Guides for operators.", Target: 60},
				{Key: "contents", Title: "Documents", Markdown: "| Document | What | For |\n|---|---|---|\n| [docs/install.md:0] | Installing | operators |"}}},
	}
	r := BuildReport("acme/shop", docs, false)
	cfg := r.Items[0]
	require.Len(t, cfg.Sections, 1)
	assert.Equal(t, briefDataWords, cfg.Sections[0].Target)
	assert.Equal(t, "ok", cfg.Sections[0].Length, "a brief data section for a module with no data is not short")
	assert.Equal(t, "short", r.Items[1].Sections[0].Length, "a guide written before targets were recorded keeps the catalogue's")
	ds := r.Items[2]
	assert.Contains(t, ds.Missing, "layout")
	assert.Contains(t, ds.Missing, "updating")
	assert.NotContains(t, ds.Missing, "how", "a documentation set is held to its own sections")
	md := r.Markdown()
	assert.False(t, strings.Contains(md, "| internal/config | Data it owns |"), "not listed off length")
	assert.True(t, strings.Contains(md, "| old | Data it owns |"))
}
