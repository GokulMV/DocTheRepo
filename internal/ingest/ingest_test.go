package ingest_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/api"
	"github.com/GokulMV/DocTheRepo/internal/core/pipeline"
	"github.com/GokulMV/DocTheRepo/internal/ingest"
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/test/mocks/hostfixture"
)

type memQueue struct {
	mu    sync.Mutex
	jobs  []ports.Job
	byKey map[string]ports.Job
}

func (q *memQueue) Enqueue(_ context.Context, nj ports.NewJob) (ports.Job, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.byKey == nil {
		q.byKey = map[string]ports.Job{}
	}
	if j, ok := q.byKey[nj.DedupeKey]; ok && nj.DedupeKey != "" {
		return j, false, nil
	}
	b, _ := json.Marshal(nj.Payload)
	j := ports.Job{ID: ports.NewID(), Type: nj.Type, RepoID: nj.RepoID, SerialKey: nj.SerialKey, DedupeKey: nj.DedupeKey, Payload: b, ReplayedFrom: nj.ReplayedFrom}
	q.jobs = append(q.jobs, j)
	q.byKey[nj.DedupeKey] = j
	return j, true, nil
}

type memRepos struct{ repos []ports.RepoConfig }

func (m *memRepos) Get(_ context.Context, id string) (ports.RepoConfig, error) {
	for _, r := range m.repos {
		if r.ID == id {
			return r, nil
		}
	}
	return ports.RepoConfig{}, ports.ErrNotFound
}
func (m *memRepos) ByName(_ context.Context, cid, name string) (ports.RepoConfig, error) {
	for _, r := range m.repos {
		if r.ConnectorID == cid && r.FullName == name {
			return r, nil
		}
	}
	return ports.RepoConfig{}, ports.ErrNotFound
}
func (m *memRepos) ListEnabled(context.Context) ([]ports.RepoConfig, error) { return m.repos, nil }

type fixedHosts struct {
	host ports.CodeHost
	cc   ports.ConnectorConfig
}

func (h *fixedHosts) HostAndConfig(_ context.Context, id string) (ports.CodeHost, ports.ConnectorConfig, error) {
	if id != h.cc.ID {
		return nil, ports.ConnectorConfig{}, ports.ErrNotFound
	}
	return h.host, h.cc, nil
}

type env struct {
	f   *hostfixture.Fixture
	q   *memQueue
	svc *ingest.Service
	cc  ports.ConnectorConfig
}

func newEnv(t *testing.T, f *hostfixture.Fixture, mode string) *env {
	cc := ports.ConnectorConfig{ID: "conn-1", Type: f.Name, Mode: mode, PollSeconds: 60, WebhookSecret: hostfixture.WebhookSecret,
		Config: map[string]string{"bot_login": f.BotLogin}}
	q := &memQueue{}
	repos := &memRepos{repos: []ports.RepoConfig{{ID: "repo-1", ConnectorID: cc.ID, FullName: hostfixture.Repo, DefaultBranch: "main",
		DocsPath: "docs/generated/", Enabled: true}}}
	return &env{f: f, q: q, cc: cc, svc: &ingest.Service{Queue: q, Repos: repos, Hosts: &fixedHosts{host: f.Host, cc: cc}}}
}

// signed builds a push delivery for the fixture's host.
func signed(kind, event string, body string) http.Header {
	if kind == "github" {
		mac := hmac.New(sha256.New, []byte(hostfixture.WebhookSecret))
		mac.Write([]byte(body))
		ev := map[string]string{"push": "push", "review": "pull_request_review"}[event]
		return http.Header{"X-Hub-Signature-256": {"sha256=" + hex.EncodeToString(mac.Sum(nil))}, "X-Github-Event": {ev}, "X-Github-Delivery": {"d-1"}}
	}
	ev := map[string]string{"push": "Push Hook", "review": "Merge Request Hook"}[event]
	return http.Header{"X-Gitlab-Token": {hostfixture.WebhookSecret}, "X-Gitlab-Event": {ev}, "X-Gitlab-Event-Uuid": {"d-1"}}
}

func pushBody(kind, branch, pusher string, paths ...string) string {
	pl, _ := json.Marshal(paths)
	if kind == "github" {
		return fmt.Sprintf(`{"ref":"refs/heads/%s","before":"aaa","after":"bbb","repository":{"full_name":"acme/shop"},"pusher":{"name":%q},
			"commits":[{"id":"bbb","author":{"email":"%s@x"},"modified":%s}]}`, branch, pusher, pusher, pl)
	}
	return fmt.Sprintf(`{"object_kind":"push","ref":"refs/heads/%s","before":"aaa","after":"bbb","user_username":%q,
		"project":{"path_with_namespace":"acme/shop"},"commits":[{"id":"bbb","author":{"email":"%s@x"},"modified":%s}]}`, branch, pusher, pusher, pl)
}

func reviewBody(kind string) string {
	if kind == "github" {
		return `{"action":"submitted","review":{"state":"approved","user":{"login":"bob"}},"pull_request":{"number":7},"repository":{"full_name":"acme/shop"}}`
	}
	return `{"object_kind":"merge_request","user":{"username":"bob"},"project":{"path_with_namespace":"acme/shop"},"object_attributes":{"iid":7,"action":"approved"}}`
}

func each(t *testing.T, fn func(t *testing.T, f *hostfixture.Fixture)) {
	for _, mk := range []func(testing.TB, map[string]string) *hostfixture.Fixture{hostfixture.GitHub, hostfixture.GitLab} {
		f := mk(t, map[string]string{"main.go": "package main\n"})
		t.Run(f.Name, func(t *testing.T) { fn(t, f) })
	}
}

func TestGitWebhook_PushEnqueuesAndGuards(t *testing.T) {
	each(t, func(t *testing.T, f *hostfixture.Fixture) {
		ctx := context.Background()
		e := newEnv(t, f, "webhook")
		k := f.Name
		call := func(body string) (ingest.Result, error) {
			return e.svc.GitWebhook(ctx, k, "conn-1", signed(k, "push", body), []byte(body))
		}
		res, err := call(pushBody(k, "main", "alice", "main.go"))
		require.NoError(t, err)
		assert.Equal(t, http.StatusAccepted, res.Status)
		assert.True(t, res.Accepted)
		require.Len(t, e.q.jobs, 1)
		j := e.q.jobs[0]
		assert.Equal(t, ports.JobCodePush, j.Type)
		assert.Equal(t, "repo:repo-1", j.SerialKey, "pushes to one repo run in order")
		var pl pipeline.CodePushPayload
		require.NoError(t, json.Unmarshal(j.Payload, &pl))
		assert.Equal(t, pipeline.CodePushPayload{RepoID: "repo-1", Before: "aaa", After: "bbb", Pusher: "alice", Delivery: "d-1"}, pl)

		again, err := call(pushBody(k, "main", "alice", "main.go"))
		require.NoError(t, err)
		assert.Equal(t, res.JobID, again.JobID, "a redelivery collapses onto the same job")

		for body, reason := range map[string]string{
			pushBody(k, "feature", "alice", "main.go"):                                          ingest.ReasonUntrackedBranch,
			pushBody(k, "main", f.BotLogin, "main.go"):                                          ingest.ReasonBotLoop,
			pushBody(k, "main", "alice", "docs/generated/main.go.md"):                           ingest.ReasonIgnoredPath,
			strings.Replace(pushBody(k, "main", "alice", "a.go"), "acme/shop", "acme/other", 1): ingest.ReasonUntrackedRepo,
		} {
			res, err := call(body)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, res.Status)
			assert.False(t, res.Accepted)
			assert.Equal(t, reason, res.Reason)
		}
		assert.Len(t, e.q.jobs, 1)

		body := pushBody(k, "main", "alice", "main.go")
		bad := signed(k, "push", body)
		bad.Set("X-Hub-Signature-256", "sha256=00")
		bad.Set("X-Gitlab-Token", "wrong")
		_, err = e.svc.GitWebhook(ctx, k, "conn-1", bad, []byte(body))
		assert.ErrorIs(t, err, ports.ErrInvalidSignature)
		other := map[string]string{"github": "gitlab", "gitlab": "github"}[k]
		_, err = e.svc.GitWebhook(ctx, other, "conn-1", signed(k, "push", body), []byte(body))
		assert.ErrorIs(t, err, ports.ErrNotFound, "a connector only accepts its own kind")
		_, err = e.svc.GitWebhook(ctx, k, "nope", signed(k, "push", body), []byte(body))
		assert.ErrorIs(t, err, ports.ErrNotFound)
	})
}

func TestGitWebhook_ReviewEnqueuesPRReview(t *testing.T) {
	each(t, func(t *testing.T, f *hostfixture.Fixture) {
		e := newEnv(t, f, "webhook")
		body := reviewBody(f.Name)
		res, err := e.svc.GitWebhook(context.Background(), f.Name, "conn-1", signed(f.Name, "review", body), []byte(body))
		require.NoError(t, err)
		assert.True(t, res.Accepted)
		require.Len(t, e.q.jobs, 1)
		assert.Equal(t, ports.JobPRReview, e.q.jobs[0].Type)
		assert.JSONEq(t, `{"repo_id":"repo-1","number":7}`, string(e.q.jobs[0].Payload))
	})
}

func TestPoll(t *testing.T) {
	each(t, func(t *testing.T, f *hostfixture.Fixture) {
		ctx := context.Background()
		e := newEnv(t, f, "poll")
		now := time.Now()
		e.svc.Now = func() time.Time { return now }
		n, err := e.svc.Poll(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, n, "never-processed repo is enqueued")
		f.Push("main", map[string]*string{"x.go": hostfixture.S("package x\n")}, "alice")
		n, _ = e.svc.Poll(ctx)
		assert.Zero(t, n, "not due before the connector interval")
		now = now.Add(time.Minute)
		n, _ = e.svc.Poll(ctx)
		assert.Equal(t, 1, n)
		var pl pipeline.CodePushPayload
		require.NoError(t, json.Unmarshal(e.q.jobs[1].Payload, &pl))
		assert.Equal(t, f.Head("main"), pl.After)

		webhookOnly := newEnv(t, f, "webhook")
		n, _ = webhookOnly.svc.Poll(ctx)
		assert.Zero(t, n, "webhook-mode connectors are not polled")
	})
}

func TestRequeueDocs(t *testing.T) {
	f := hostfixture.GitHub(t, map[string]string{"a.go": "package a\n"})
	e := newEnv(t, f, "webhook")
	require.NoError(t, e.svc.RequeueDocs(context.Background(), ports.PRRecord{RepoID: "repo-1", Number: 3, JobID: "j0", SourcePaths: []string{"a.go"}}, "stale"))
	require.NoError(t, e.svc.RequeueDocs(context.Background(), ports.PRRecord{RepoID: "repo-1", Number: 4}, "stale"), "nothing to regenerate")
	require.Len(t, e.q.jobs, 1)
	assert.Equal(t, "j0", e.q.jobs[0].ReplayedFrom)
	assert.JSONEq(t, `{"repo_id":"repo-1","force_paths":["a.go"],"reason":"stale"}`, string(e.q.jobs[0].Payload))
}

func TestHTTPIngress(t *testing.T) {
	f := hostfixture.GitHub(t, map[string]string{"a.go": "package a\n"})
	e := newEnv(t, f, "webhook")
	srv := httptest.NewServer(api.NewRouter(api.Deps{Log: slog.Default(), Metrics: observability.NewMetrics(), Git: e.svc, WebhookPerMinute: 60}))
	defer srv.Close()
	post := func(path string, hdr http.Header, body []byte) (int, map[string]any) {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(body))
		for k, v := range hdr {
			req.Header[k] = v
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	body := pushBody("github", "main", "alice", "a.go")
	code, out := post("/hooks/github/conn-1", signed("github", "push", body), []byte(body))
	assert.Equal(t, http.StatusAccepted, code)
	assert.Equal(t, true, out["accepted"])
	assert.NotEmpty(t, out["job_id"])

	code, out = post("/hooks/github/conn-1", http.Header{"X-Github-Event": {"push"}}, []byte(body))
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Equal(t, "INVALID_SIGNATURE", out["error"].(map[string]any)["code"])
	code, _ = post("/hooks/github/missing", signed("github", "push", body), []byte(body))
	assert.Equal(t, http.StatusNotFound, code)
	code, _ = post("/hooks/sentry/conn-1", nil, []byte(body))
	assert.Equal(t, http.StatusNotFound, code, "non-git kinds are not routed here")
	bad := "{"
	code, out = post("/hooks/github/conn-1", signed("github", "push", bad), []byte(bad))
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "MALFORMED_PAYLOAD", out["error"].(map[string]any)["code"])
	big := bytes.Repeat([]byte("x"), api.MaxWebhookBytes+1)
	code, _ = post("/hooks/github/conn-1", nil, big)
	assert.Equal(t, http.StatusRequestEntityTooLarge, code)

	limited := 0
	for range 20 {
		if c, _ := post("/hooks/github/conn-2", nil, []byte("{}")); c == http.StatusTooManyRequests {
			limited++
		}
	}
	assert.Positive(t, limited, "per-connector rate limit")
}
