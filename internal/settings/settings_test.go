package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeHub is an in-memory /api/v1 for the endpoints Apply uses.
type fakeHub struct {
	providers  []map[string]any
	connectors []map[string]any
	routes     []map[string]any
	repos      []map[string]any
	limits     []any
	writes     []string
	n          int
}

func (f *fakeHub) id() string { f.n++; return fmt.Sprintf("id-%d", f.n) }

func roundTrip(in, out any) {
	b, _ := json.Marshal(in)
	_ = json.Unmarshal(b, out)
}

func (f *fakeHub) find(list []map[string]any, id string) map[string]any {
	for _, x := range list {
		if x["id"] == id {
			return x
		}
	}
	return nil
}

func (f *fakeHub) Do(_ context.Context, method, path string, in, out any) error {
	var body map[string]any
	roundTrip(in, &body)
	if method != "GET" {
		f.writes = append(f.writes, method+" "+path)
	}
	reply := func(v any) error {
		if out != nil {
			roundTrip(v, out)
		}
		return nil
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case method == "GET" && path == "/providers":
		return reply(map[string]any{"items": f.providers})
	case method == "GET" && path == "/connectors":
		return reply(map[string]any{"items": f.connectors})
	case method == "GET" && path == "/routes":
		return reply(map[string]any{"items": f.routes})
	case method == "GET" && path == "/repos":
		return reply(map[string]any{"items": f.repos})
	case method == "GET" && path == "/spend/limits":
		return reply(map[string]any{"items": f.limits})
	case method == "POST" && path == "/providers":
		body["id"], body["has_key"] = f.id(), body["api_key"] != nil
		if body["enabled"] == nil {
			body["enabled"] = true
		}
		delete(body, "api_key")
		f.providers = append(f.providers, body)
		return reply(map[string]any{"id": body["id"]})
	case method == "POST" && path == "/connectors":
		body["id"], body["enabled"] = f.id(), true
		delete(body, "credentials")
		delete(body, "webhook_secret")
		f.connectors = append(f.connectors, body)
		return reply(map[string]any{"id": body["id"], "webhook_path": "/hooks/x/" + body["id"].(string)})
	case method == "POST" && path == "/repos":
		r := map[string]any{"id": f.id(), "connector_id": body["connector_id"], "full_name": body["full_name"], "tracked_branch": body["tracked_branch"],
			"docs_path": body["docs_path"], "service_name": body["service_name"], "enabled": true,
			"push": map[string]any{"mode": body["push_mode"], "approver": body["approver"]}}
		f.repos = append(f.repos, r)
		return reply(map[string]any{"id": r["id"], "webhook": "registered"})
	case method == "PATCH" && len(parts) == 2:
		var list []map[string]any
		switch parts[0] {
		case "providers":
			list = f.providers
		case "connectors":
			list = f.connectors
		case "repos":
			list = f.repos
		}
		x := f.find(list, parts[1])
		if x == nil {
			return fmt.Errorf("%w: NOT_FOUND", ErrAPI)
		}
		for k, v := range body {
			if k == "api_key" || k == "credentials" || k == "webhook_secret" || k == "owners" {
				continue
			}
			if parts[0] == "repos" && (k == "push_mode" || k == "approver" || k == "on_reject" || k == "pr_conflict_strategy") {
				x["push"].(map[string]any)[strings.TrimPrefix(strings.Replace(k, "push_mode", "mode", 1), "pr_")] = v
				continue
			}
			x[k] = v
		}
		return nil
	case method == "PUT" && parts[0] == "routes":
		body["feature"] = parts[1]
		for i, r := range f.routes {
			if r["feature"] == parts[1] {
				f.routes[i] = body
				return nil
			}
		}
		f.routes = append(f.routes, body)
		return nil
	case method == "PUT" && path == "/spend/limits":
		f.limits = body["items"].([]any)
		return nil
	}
	return fmt.Errorf("unexpected %s %s", method, path)
}

const hubYAML = `
version: 1
providers:
  - name: anthropic
    kind: anthropic
    api_key: ${env:ANTHROPIC_API_KEY}
routes:
  docgen: { provider: anthropic, model: claude-opus-5-5, effort: high }
  qa:
    provider: anthropic
    model: claude-sonnet-5-5
connectors:
  - name: github
    type: github
    mode: webhook
    credentials: ${file:keys.json#github.token}
    webhook_secret: ${file:app.properties#github.webhook.secret}
  - name: wiz
    type: wiz
    config: { api_url: "https://api.us17.app.wiz.io/graphql", never_send_to_llm: "true" }
    credentials:
      client_id: wiz-client
      client_secret: ${gopass:infra/wiz#secret}
spend:
  limits:
    - { scope: global, window: day, max_tokens: 2000000 }
    - { scope: provider, key: anthropic, window: month, max_cost_usd: 300 }
`

const reposJSON = `{"repos": [
  {"full_name": "acme/payments", "connector": "github", "docs_path": "docs/", "push_mode": "pr", "owners": ["team-pay"]},
  {"full_name": "acme/orders", "connector": "github"}
]}`

func testResolver(t *testing.T, dir string) *Resolver {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "keys.json"), []byte(`{"github": {"token": "ghp_secret"}}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.properties"), []byte("# comment\ngithub.webhook.secret = whs3cret\nother: x\n"), 0o600))
	env := map[string]string{"ANTHROPIC_API_KEY": "sk-ant-test"}
	return &Resolver{
		FileRoot: dir,
		Getenv:   func(k string) (string, bool) { v, ok := env[k]; return v, ok },
		Exec: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "gopass" && strings.Join(args, " ") == "show infra/wiz secret" {
				return []byte("wiz-s3cret\n"), nil
			}
			return nil, fmt.Errorf("unexpected %s %v", name, args)
		},
	}
}

func load(t *testing.T, r *Resolver, files ...Source) Document {
	t.Helper()
	doc, err := Load(context.Background(), files, r)
	require.NoError(t, err)
	return doc
}

func TestLoadMergesFilesAndResolvesReferences(t *testing.T) {
	dir := t.TempDir()
	r := testResolver(t, dir)
	doc := load(t, r, Source{"hub.yaml", []byte(hubYAML)}, Source{"repos.json", []byte(reposJSON)})

	require.Len(t, doc.Providers, 1)
	assert.Equal(t, "sk-ant-test", doc.Providers[0].APIKey.Value)
	assert.Equal(t, "ghp_secret", doc.Connectors[0].Credentials.Value, "JSON file, dotted key")
	assert.Equal(t, "whs3cret", doc.Connectors[0].WebhookSecret.Value, ".properties file")
	assert.JSONEq(t, `{"client_id":"wiz-client","client_secret":"wiz-s3cret"}`, doc.Connectors[1].Credentials.Value, "a mapping becomes JSON")
	assert.Len(t, doc.Repos, 2)
	assert.Equal(t, "claude-opus-5-5", doc.Routes["docgen"].Model)
	assert.Equal(t, []string{"env", "file", "gopass"}, r.Used())
}

func TestLoadRejectsTyposWithLineNumbers(t *testing.T) {
	_, err := Load(context.Background(), []Source{{"hub.yaml", []byte("providers:\n  - name: a\n    kind: openai\n    api_kee: x\n")}}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "line 4")
	assert.Contains(t, err.Error(), "api_kee")
}

func TestLoadValidatesReferences(t *testing.T) {
	_, err := Load(context.Background(), []Source{{"x.yaml", []byte("routes:\n  qa: { model: m }\nrepos:\n  - full_name: a/b\n")}}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "routes.qa: provider and model are required")
	assert.Contains(t, err.Error(), "repos[0]: full_name and connector are required")
}

func TestResolverPolicy(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	r := &Resolver{Allowed: map[string]bool{"env": true, "file": true}, EnvPrefix: "DTH_SECRET_", FileRoot: dir,
		Getenv: func(k string) (string, bool) { return "v-" + k, true }}

	v, err := r.Expand(ctx, "${env:DTH_SECRET_A}")
	require.NoError(t, err)
	assert.Equal(t, "v-DTH_SECRET_A", v)

	_, err = r.Expand(ctx, "${env:DTH_DATABASE_URL}")
	assert.ErrorContains(t, err, "only variables starting with DTH_SECRET_")

	_, err = r.Expand(ctx, "${file:../../etc/passwd}")
	assert.ErrorContains(t, err, "files must be inside")
	_, err = r.Expand(ctx, "${file:/etc/passwd}")
	assert.ErrorContains(t, err, "files must be inside")

	_, err = r.Expand(ctx, "${vault:secret/data/x#k}")
	assert.ErrorContains(t, err, "not enabled on this Hub")

	_, err = r.Expand(ctx, "${nope:x}")
	assert.ErrorContains(t, err, "unknown secret source")

	v, err = r.Expand(ctx, "literal $${env:X} stays")
	require.NoError(t, err)
	assert.Equal(t, "literal ${env:X} stays", v)
}

func TestResolverErrorsNeverContainValues(t *testing.T) {
	r := &Resolver{Getenv: func(string) (string, bool) { return `{"a":"TOPSECRET"}`, true }}
	_, err := r.Expand(context.Background(), "${env:X#missing}")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "TOPSECRET")
}

func TestVaultKV2AndV1(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "tok" {
			w.WriteHeader(403)
			return
		}
		switch r.URL.Path {
		case "/v1/secret/data/dth":
			fmt.Fprint(w, `{"data":{"data":{"anthropic":"sk-v2","github":"ghp"},"metadata":{"version":3}}}`)
		case "/v1/kv/single":
			fmt.Fprint(w, `{"data":{"value":"only-one"}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	env := map[string]string{"VAULT_ADDR": srv.URL, "VAULT_TOKEN": "tok"}
	r := &Resolver{Getenv: func(k string) (string, bool) { v, ok := env[k]; return v, ok }}
	ctx := context.Background()

	v, err := r.Expand(ctx, "${vault:secret/data/dth#anthropic}")
	require.NoError(t, err)
	assert.Equal(t, "sk-v2", v)
	v, err = r.Expand(ctx, "${vault:kv/single}")
	require.NoError(t, err)
	assert.Equal(t, "only-one", v)
	_, err = r.Expand(ctx, "${vault:secret/data/dth}")
	assert.ErrorContains(t, err, "name one with #key")
	_, err = r.Expand(ctx, "${vault:secret/data/missing#x}")
	assert.ErrorContains(t, err, "vault answered 404")
}

func TestCloudSecretsAndShortForms(t *testing.T) {
	var gotGCP string
	r := &Resolver{
		AWSSecret: func(_ context.Context, id string) (string, error) { return `{"token":"aws-` + id + `"}`, nil },
		GCPSecret: func(_ context.Context, name string) (string, error) { gotGCP = name; return "gcp-value", nil },
	}
	ctx := context.Background()
	v, err := r.Expand(ctx, "${awssm:prod/dth#token}")
	require.NoError(t, err)
	assert.Equal(t, "aws-prod/dth", v)
	v, err = r.Expand(ctx, "${gcpsm:my-proj/dth-key}")
	require.NoError(t, err)
	assert.Equal(t, "gcp-value", v)
	assert.Equal(t, "projects/my-proj/secrets/dth-key/versions/latest", gotGCP)
	assert.Equal(t, "eu-west-1", arnRegion("arn:aws:secretsmanager:eu-west-1:123:secret:dth"))
	assert.Equal(t, "", arnRegion("prod/dth"))
}

func TestDotenvAndPlainFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("export TOKEN=\"abc\"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "key.txt"), []byte("raw-key\n"), 0o600))
	r := &Resolver{}
	v, err := r.Expand(context.Background(), "${file:"+filepath.Join(dir, ".env")+"#TOKEN}")
	require.NoError(t, err)
	assert.Equal(t, "abc", v)
	v, err = r.Expand(context.Background(), "${file:"+filepath.Join(dir, "key.txt")+"}")
	require.NoError(t, err)
	assert.Equal(t, "raw-key", v)
}

func TestApplyCreatesInOrderThenIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	doc := load(t, testResolver(t, dir), Source{"hub.yaml", []byte(hubYAML)}, Source{"repos.json", []byte(reposJSON)})
	hub := &fakeHub{}
	ctx := context.Background()

	plan, err := Apply(ctx, hub, doc, true)
	require.NoError(t, err)
	assert.Empty(t, hub.writes, "a dry run writes nothing")
	c, u, _ := plan.Counts()
	assert.Equal(t, 1+2+2+2, c, "provider, 2 connectors, 2 routes, 2 repos")
	assert.Equal(t, 1, u, "spend limits")

	res, err := Apply(ctx, hub, doc, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"POST /providers", "POST /connectors", "POST /connectors", "PUT /routes/docgen", "PUT /routes/qa",
		"POST /repos", "PATCH /repos/id-4", "POST /repos", "PUT /spend/limits"}, hub.writes)
	assert.Equal(t, "id-1", hub.routes[0]["provider_id"], "routes name the created provider")
	assert.Equal(t, "id-2", hub.repos[0]["connector_id"], "repos name the created connector")
	assert.Equal(t, "id-1", hub.limits[1].(map[string]any)["scope_key"], "a provider limit uses the provider's id")
	assert.Contains(t, res.Changes[1].Detail, "webhook")

	hub.writes = nil
	again, err := Apply(ctx, hub, doc, false)
	require.NoError(t, err)
	for _, ch := range again.Changes {
		switch ch.Kind {
		case "provider", "connector":
			assert.Equal(t, "update", ch.Action, "write-only secrets are re-applied: %+v", ch)
			for _, f := range ch.Fields {
				assert.Contains(t, []string{"api_key", "credentials", "webhook_secret"}, f)
			}
		case "repo":
			if ch.Name == "acme/payments" {
				assert.Equal(t, []string{"owners"}, ch.Fields)
			} else {
				assert.Equal(t, "unchanged", ch.Action)
			}
		default:
			assert.Equal(t, "unchanged", ch.Action, "%+v", ch)
		}
	}
}

func TestApplyStopsOnUnknownReferenceAndKindChange(t *testing.T) {
	hub := &fakeHub{providers: []map[string]any{{"id": "p1", "name": "main", "kind": "openai", "enabled": true}}}
	doc := Document{Routes: map[string]Route{"qa": {Provider: "missing", Model: "m"}}}
	res, err := Apply(context.Background(), hub, doc, false)
	require.Error(t, err)
	assert.Contains(t, res.Error, `no provider named "missing"`)

	doc = Document{Providers: []Provider{{Name: "main", Kind: "anthropic"}}}
	_, err = Apply(context.Background(), hub, doc, false)
	assert.ErrorContains(t, err, "it is a openai provider on the Hub")
}

func TestExportWritesReferencesNotSecrets(t *testing.T) {
	hub := &fakeHub{
		providers:  []map[string]any{{"id": "p1", "name": "Anthropic Prod", "kind": "anthropic", "enabled": true, "has_key": true}},
		connectors: []map[string]any{{"id": "c1", "name": "github", "type": "github", "mode": "webhook", "enabled": true, "has_credentials": true}},
		routes:     []map[string]any{{"feature": "qa", "provider_id": "p1", "model": "m"}},
		repos:      []map[string]any{{"id": "r1", "connector_id": "c1", "full_name": "acme/a", "enabled": true, "push": map[string]any{"mode": "pr"}}},
		limits:     []any{map[string]any{"scope": "repo", "scope_key": "r1", "window": "day", "max_tokens": 5, "on_breach": "block"}},
	}
	doc, err := Export(context.Background(), hub)
	require.NoError(t, err)
	b, err := Marshal(doc)
	require.NoError(t, err)
	s := string(b)
	assert.Contains(t, s, "${env:ANTHROPIC_PROD_API_KEY}")
	assert.Contains(t, s, "${env:GITHUB_CREDENTIALS}")
	assert.Contains(t, s, "provider: Anthropic Prod")
	assert.Contains(t, s, "key: acme/a")
	assert.NotContains(t, s, "webhook_secret", "unset secrets are omitted")

	// The export loads back without a resolver (references kept verbatim).
	back, err := Load(context.Background(), []Source{{"export.yaml", b}}, nil)
	require.NoError(t, err)
	assert.Equal(t, doc.Routes, back.Routes)
}

func TestExampleFileIsValid(t *testing.T) {
	srcs, err := ReadPaths([]string{"../../deploy/settings"}, nil)
	require.NoError(t, err)
	doc, err := Load(context.Background(), srcs, nil) // references stay unresolved
	require.NoError(t, err)
	assert.Len(t, doc.Providers, 2)
	assert.Len(t, doc.Routes, 4)
	assert.NotEmpty(t, doc.Spend.Limits)
}

func TestAuthAndUsersSections(t *testing.T) {
	ctx := context.Background()
	_, err := Load(ctx, []Source{{Name: "a.yaml", Data: []byte("auth:\n  sso:\n    issuer: https://x\nusers:\n  - email: a@b.c\n    role: boss\n")}}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth.sso: issuer and client_id are required")
	assert.Contains(t, err.Error(), "users[0]: role must be")

	doc, err := Load(ctx, []Source{
		{Name: "1.yaml", Data: []byte("auth:\n  password: true\nusers:\n  - email: Ann@acme.com\n    role: viewer\n")},
		{Name: "2.yaml", Data: []byte("auth:\n  sso: {issuer: https://acme.okta.com, client_id: x, client_secret: s}\nusers:\n  - email: ann@acme.com\n    role: owner\n")},
	}, nil)
	require.NoError(t, err)
	require.NotNil(t, doc.Auth.Password, "later files add to auth, they do not drop earlier fields")
	assert.Equal(t, "https://acme.okta.com", doc.Auth.SSO.Issuer)
	require.Len(t, doc.Users, 1, "users merge by email, case-insensitively")
	assert.Equal(t, "owner", doc.Users[0].Role)
}
