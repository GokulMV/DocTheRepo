package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeDocker struct {
	mu    sync.Mutex
	calls []string
	fail  bool
}

func (f *fakeDocker) run(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if f.fail {
		return nil, errors.New("docker: command not found")
	}
	return []byte("ok"), nil
}

// fakeHub answers readyz after a couple of polls, and the login + token calls `dth up` makes.
func fakeHub(t *testing.T) (*httptest.Server, *string) {
	polls := 0
	var gotPassword string
	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		polls++
		if polls < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"status":"ready"}`))
	})
	mux.HandleFunc("/api/v1/auth/local/login", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		gotPassword = in["password"]
		http.SetCookie(w, &http.Cookie{Name: "dth_session", Value: "s", Path: "/"})
		w.Write([]byte(`{"csrf_token":"c1"}`))
	})
	mux.HandleFunc("/api/v1/tokens", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-CSRF-Token") != "c1" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if c, err := r.Cookie("dth_session"); err != nil || c.Value != "s" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"token":"dth_pat_x"}`))
	})
	bearer := func(r *http.Request) bool { return r.Header.Get("Authorization") == "Bearer dth_pat_x" }
	mux.HandleFunc("/api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"id":"u1"}`))
	})
	mux.HandleFunc("/api/v1/users/u1/invite", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(r) || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"path":"/invite/tok1","expires_at":"2026-10-10T00:00:00Z"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &gotPassword
}

func portOf(u string) int {
	p, _ := strconv.Atoi(u[strings.LastIndex(u, ":")+1:])
	return p
}

func TestUpFirstRunThenIdempotent(t *testing.T) {
	dir := t.TempDir()
	d := &fakeDocker{}
	srv, pw := fakeHub(t)
	o := Options{Dir: dir, Port: portOf(srv.URL), OwnerEmail: "me@acme.com", Runner: d.run, WaitTimeout: 10 * time.Second, Image: "dth:dev", Ollama: true}
	res, err := Up(context.Background(), o)
	require.NoError(t, err)
	assert.NotEmpty(t, *pw, "a generated bootstrap password signed in")
	assert.Equal(t, res.URL+"/invite/tok1", res.SetupLink, "the owner gets a one-time link, not a password")
	assert.Equal(t, "dth_pat_x", res.Token)
	assert.Equal(t, "me@acme.com", res.OwnerEmail)

	env, _ := readEnv(filepath.Join(dir, ".env"))
	assert.NotEmpty(t, env["DTH_DB_PASSWORD"])
	assert.Empty(t, env["DTH_OWNER_PASSWORD"], "the owner password is never stored")
	assert.Equal(t, "dth:dev", env["DTH_IMAGE"])
	st, _ := os.Stat(filepath.Join(dir, ".env"))
	assert.Equal(t, os.FileMode(0o600), st.Mode().Perm())
	compose, _ := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	assert.Equal(t, ComposeFile(), compose)
	assert.Contains(t, strings.Join(d.calls, "\n"), "--profile ollama")
	assert.Contains(t, strings.Join(d.calls, "\n"), "up -d --remove-orphans")

	dbPass := env["DTH_DB_PASSWORD"]
	res, err = Up(context.Background(), Options{Dir: dir, Port: portOf(srv.URL), Runner: d.run, Pull: true})
	require.NoError(t, err)
	assert.Empty(t, res.SetupLink, "re-running does not reset credentials")
	assert.Empty(t, res.Token)
	assert.Equal(t, "me@acme.com", res.OwnerEmail)
	env, _ = readEnv(filepath.Join(dir, ".env"))
	assert.Equal(t, dbPass, env["DTH_DB_PASSWORD"])
	assert.Contains(t, strings.Join(d.calls, "\n"), " pull")

	require.NoError(t, Down(context.Background(), Options{Dir: dir, Runner: d.run}, true))
	assert.Contains(t, d.calls[len(d.calls)-1], "down --volumes")
	_, err = os.Stat(filepath.Join(dir, ".env"))
	assert.True(t, os.IsNotExist(err), "wiping forgets the credentials")
}

func TestUpWithoutDocker(t *testing.T) {
	_, err := Up(context.Background(), Options{Dir: t.TempDir(), Runner: (&fakeDocker{fail: true}).run})
	assert.ErrorContains(t, err, "Docker with the Compose plugin is required")
}

func TestUpTimesOut(t *testing.T) {
	d := &fakeDocker{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	_, err := Up(context.Background(), Options{Dir: t.TempDir(), Port: portOf(srv.URL), Runner: d.run, WaitTimeout: 1500 * time.Millisecond})
	assert.ErrorContains(t, err, "did not become ready")
}

func TestComposeFileMatchesDeployCopy(t *testing.T) {
	b, err := os.ReadFile("../../deploy/compose/docker-compose.yml")
	require.NoError(t, err)
	assert.Equal(t, string(ComposeFile()), string(b), "deploy/compose/docker-compose.yml must equal internal/bootstrap/compose.yaml")
}
