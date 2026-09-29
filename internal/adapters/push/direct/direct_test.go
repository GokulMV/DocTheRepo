package direct

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/test/mocks/hostfixture"
)

// racingHost pushes a human commit right before the Hub's first commit attempt.
type racingHost struct {
	ports.CodeHost
	race func()
}

func (r *racingHost) CommitFiles(ctx context.Context, repo string, req ports.CommitRequest) (string, error) {
	if r.race != nil {
		r.race()
		r.race = nil
	}
	return r.CodeHost.CommitFiles(ctx, repo, req)
}

func landing() ports.DocsLanding {
	return ports.DocsLanding{Repo: hostfixture.Repo, Branch: "main", Message: "docs",
		Files: []ports.FileChange{{Path: "docs/generated/main.go.md", Content: []byte("# m\n")}}}
}

func TestRetriesWhenUnrelatedCommitRaces(t *testing.T) {
	for _, f := range hostfixture.All(t, map[string]string{"main.go": "package main\n"}) {
		t.Run(f.Name, func(t *testing.T) {
			h := &racingHost{CodeHost: f.Host, race: func() { f.Push("main", map[string]*string{"x.go": hostfixture.S("package x\n")}, "carol") }}
			res, err := Lander{}.Land(context.Background(), h, ports.PushConfig{}, landing())
			require.NoError(t, err)
			assert.Equal(t, f.Head("main"), res.CommitSHA)
			_, ok := f.File("main", "x.go")
			assert.True(t, ok, "the human commit is kept")
		})
	}
}

func TestRegeneratesWhenRaceTouchesDocs(t *testing.T) {
	for _, f := range hostfixture.All(t, map[string]string{"main.go": "package main\n"}) {
		t.Run(f.Name, func(t *testing.T) {
			h := &racingHost{CodeHost: f.Host, race: func() {
				f.Push("main", map[string]*string{"docs/generated/main.go.md": hostfixture.S("<!-- dth:human -->edit<!-- dth:end -->\n")}, "carol")
			}}
			_, err := Lander{}.Land(context.Background(), h, ports.PushConfig{}, landing())
			_, transient := ports.AsTransient(err)
			assert.True(t, transient)
			assert.ErrorIs(t, err, ports.ErrConflict)
			got, _ := f.File("main", "docs/generated/main.go.md")
			assert.Contains(t, got, "dth:human", "a concurrent human edit is never clobbered")
		})
	}
}

func TestTouches(t *testing.T) {
	files := []ports.FileChange{{Path: "a.md"}}
	assert.True(t, touches([]ports.ChangedFile{{Path: "b.md", PreviousPath: "a.md"}}, files))
	assert.False(t, touches([]ports.ChangedFile{{Path: "b.md"}}, files))
}
