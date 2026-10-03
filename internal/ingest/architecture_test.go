package ingest

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

type scanHost struct {
	ports.CodeHost
	files   map[string]string
	fail    map[string]error
	headErr error
}

func (h *scanHost) BranchHead(context.Context, string, string) (string, error) {
	return "abc", h.headErr
}
func (h *scanHost) ListTree(context.Context, string, string) ([]string, error) {
	out := []string{}
	for p := range h.files {
		out = append(out, p)
	}
	for p := range h.fail {
		out = append(out, p)
	}
	return out, nil
}
func (h *scanHost) GetFile(_ context.Context, _, p, _ string) ([]byte, error) {
	if err := h.fail[p]; err != nil {
		return nil, err
	}
	return []byte(h.files[p]), nil
}

type scanStore struct {
	upserted []string
	kept     []string
}

func (s *scanStore) UpsertDiagram(_ context.Context, _, p, _, _, _, _ string) error {
	s.upserted = append(s.upserted, p)
	return nil
}
func (s *scanStore) DeleteDiagrams(context.Context, string, []string) error { return nil }
func (s *scanStore) KeepDiagrams(_ context.Context, _ string, paths []string) (int64, error) {
	s.kept = paths
	return 0, nil
}

type scanRepos struct{}

func (scanRepos) Get(context.Context, string) (ports.RepoConfig, error) {
	return ports.RepoConfig{ID: "r", FullName: "acme/shop", DefaultBranch: "main"}, nil
}

const archifyDoc = `<html><head><meta name="generator" content="archify 3"><title>Flow A Diagram</title></head></html>`

// One unreadable diagram does not stop the scan or drop its stored copy; a git host failure says why.
func TestScanSurvivesAFailedFile(t *testing.T) {
	host := &scanHost{files: map[string]string{"docs/architecture/a.html": archifyDoc},
		fail: map[string]error{"docs/architecture/b.html": ports.Transient(errors.New("github: connection reset"))}}
	st := &scanStore{}
	s := &ArchitectureSync{Repos: scanRepos{}, Store: st, Hosts: func(context.Context, string) (ports.CodeHost, error) { return host, nil }}
	res, err := s.Scan(context.Background(), "r")
	require.NoError(t, err)
	assert.Equal(t, 1, res.Found)
	assert.Equal(t, 2, res.Checked)
	require.Len(t, res.Failed, 1)
	assert.Equal(t, "docs/architecture/b.html", res.Failed[0].Path)
	assert.Contains(t, res.Failed[0].Error, "connection reset")
	assert.ElementsMatch(t, []string{"docs/architecture/a.html", "docs/architecture/b.html"}, st.kept, "the failed file's copy is kept")

	host.headErr = ports.Permanent(errors.New("github 401: Bad credentials"))
	_, err = s.Scan(context.Background(), "r")
	var he *HostError
	require.ErrorAs(t, err, &he)
	assert.Equal(t, "read the head of main: github 401: Bad credentials", he.Error())
}
