package settings

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"sort"

	"github.com/GokulMV/DocTheRepo/internal/secrets"
)

// API calls the Hub's /api/v1 (path without that prefix). in is sent as JSON; out, if non-nil, receives
// the JSON response.
type API interface {
	Do(ctx context.Context, method, path string, in, out any) error
}

// Change is one line of a plan or an apply report. It never carries secret values.
type Change struct {
	Kind   string   `json:"kind"` // provider, connector, route, repo, spend
	Name   string   `json:"name"`
	Action string   `json:"action"` // create, update, unchanged, failed
	Fields []string `json:"fields,omitempty"`
	Detail string   `json:"detail,omitempty"`
}

// Result is what Apply did (or, with DryRun, would do).
type Result struct {
	DryRun  bool     `json:"dry_run"`
	Changes []Change `json:"changes"`
	// Sources lists the secret sources used (env, vault, …), never values.
	Sources []string `json:"sources,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// Counts summarises the result.
func (r Result) Counts() (create, update, unchanged int) {
	for _, c := range r.Changes {
		switch c.Action {
		case "create":
			create++
		case "update":
			update++
		case "unchanged":
			unchanged++
		}
	}
	return
}

// Remote views (the subset of each list response that Apply compares).
type remoteProvider struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Name      string            `json:"name"`
	BaseURL   string            `json:"base_url"`
	Extra     map[string]string `json:"extra"`
	RedactPII bool              `json:"redact_pii"`
	Enabled   bool              `json:"enabled"`
}

type remoteConnector struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Name        string            `json:"name"`
	Config      map[string]string `json:"config"`
	Mode        string            `json:"mode"`
	PollSeconds int64             `json:"poll_seconds"`
	Enabled     bool              `json:"enabled"`
}

type remoteRoute struct {
	Feature            string   `json:"feature"`
	ProviderID         string   `json:"provider_id"`
	Model              string   `json:"model"`
	MaxOutputTokens    int      `json:"max_output_tokens"`
	ContextTokenBudget int      `json:"context_token_budget"`
	Temperature        *float64 `json:"temperature"`
	Effort             string   `json:"effort"`
	FallbackProviderID *string  `json:"fallback_provider_id"`
	FallbackModel      string   `json:"fallback_model"`
}

type remoteRepo struct {
	ID            string `json:"id"`
	ConnectorID   string `json:"connector_id"`
	FullName      string `json:"full_name"`
	TrackedBranch string `json:"tracked_branch"`
	DocsPath      string `json:"docs_path"`
	ServiceName   string `json:"service_name"`
	Enabled       bool   `json:"enabled"`
	Push          struct {
		Mode             string `json:"mode"`
		OnReject         string `json:"on_reject"`
		Approver         string `json:"approver"`
		ConflictStrategy string `json:"conflict_strategy"`
	} `json:"push"`
}

type remoteLimit struct {
	Scope      string   `json:"scope"`
	ScopeKey   string   `json:"scope_key"`
	Window     string   `json:"window"`
	MaxTokens  *int64   `json:"max_tokens"`
	MaxCostUSD *float64 `json:"max_cost_usd"`
	OnBreach   string   `json:"on_breach"`
	AlertURL   string   `json:"alert_url,omitempty"`
}

type state struct {
	providers  []remoteProvider
	connectors []remoteConnector
	routes     []remoteRoute
	repos      []remoteRepo
	limits     []remoteLimit
}

func fetchState(ctx context.Context, api API) (*state, error) {
	var s state
	var p struct{ Items []remoteProvider }
	var c struct{ Items []remoteConnector }
	var rt struct{ Items []remoteRoute }
	var rp struct{ Items []remoteRepo }
	var l struct{ Items []remoteLimit }
	for _, q := range []struct {
		path string
		out  any
	}{{"/providers", &p}, {"/connectors", &c}, {"/routes", &rt}, {"/repos", &rp}, {"/spend/limits", &l}} {
		if err := api.Do(ctx, "GET", q.path, nil, q.out); err != nil {
			return nil, fmt.Errorf("read %s: %w", q.path, err)
		}
	}
	s.providers, s.connectors, s.routes, s.repos, s.limits = p.Items, c.Items, rt.Items, rp.Items, l.Items
	return &s, nil
}

// pendingID stands for an object a dry run would create.
const pendingID = "(new)"

// Apply makes the Hub match doc: it creates what is missing and updates what differs, in dependency order
// (providers, connectors, routes, repositories, spend). It never deletes. It stops at the first error;
// everything before it is applied, and running it again is safe.
func Apply(ctx context.Context, api API, doc Document, dryRun bool) (Result, error) {
	res := Result{DryRun: dryRun, Changes: []Change{}}
	st, err := fetchState(ctx, api)
	if err != nil {
		res.Error = err.Error()
		return res, err
	}
	a := &applier{api: api, st: st, dry: dryRun, res: &res, providerIDs: map[string]string{}, connectorIDs: map[string]string{}, repoIDs: map[string]string{}}
	for _, p := range st.providers {
		a.providerIDs[p.Name] = p.ID
	}
	for _, c := range st.connectors {
		a.connectorIDs[c.Name] = c.ID
	}
	for _, r := range st.repos {
		a.repoIDs[r.FullName] = r.ID
	}
	steps := []func(context.Context, Document) error{a.providers, a.connectors, a.routes, a.repos, a.spend}
	for _, step := range steps {
		if err := step(ctx, doc); err != nil {
			res.Error = err.Error()
			return res, err
		}
	}
	return res, nil
}

type applier struct {
	api          API
	sealKey      *secrets.PublicSealKey
	sealTried    bool
	st           *state
	dry          bool
	res          *Result
	providerIDs  map[string]string
	connectorIDs map[string]string
	repoIDs      map[string]string
}

func (a *applier) record(c Change) { a.res.Changes = append(a.res.Changes, c) }

// seal encrypts a secret to the Hub's sealing key before it is sent, when the Hub has one (older Hubs
// take the value as it is, over TLS).
func (a *applier) seal(ctx context.Context, v, purpose string) (string, error) {
	if v == "" || secrets.IsSealed(v) {
		return v, nil
	}
	if !a.sealTried {
		a.sealTried = true
		var k struct {
			KID, Alg         string
			X25519, MLKEM768 string
		}
		if err := a.api.Do(ctx, "GET", "/seal/key", nil, &k); err == nil && k.KID != "" {
			x, err1 := base64.StdEncoding.DecodeString(k.X25519)
			m, err2 := base64.StdEncoding.DecodeString(k.MLKEM768)
			if err1 == nil && err2 == nil {
				a.sealKey = &secrets.PublicSealKey{ID: k.KID, Alg: k.Alg, X25519: x, MLKEM768: m}
			}
		}
	}
	if a.sealKey == nil {
		return v, nil
	}
	return secrets.Seal(*a.sealKey, []byte(v), purpose)
}

func (a *applier) fail(kind, name string, err error) error {
	a.record(Change{Kind: kind, Name: name, Action: "failed", Detail: err.Error()})
	return fmt.Errorf("%s %q: %w", kind, name, err)
}

func (a *applier) providers(ctx context.Context, doc Document) error {
	for _, p := range doc.Providers {
		i := slices.IndexFunc(a.st.providers, func(r remoteProvider) bool { return r.Name == p.Name })
		if i < 0 {
			body := map[string]any{"kind": p.Kind, "name": p.Name, "base_url": p.BaseURL, "extra": p.Extra}
			fields := []string{"kind"}
			if p.APIKey.Set {
				v, err := a.seal(ctx, p.APIKey.Value, secrets.PurposeProviderKey)
				if err != nil {
					return a.fail("provider", p.Name, err)
				}
				body["api_key"], fields = v, append(fields, "api_key")
			}
			if p.RedactPII != nil {
				body["redact_pii"] = *p.RedactPII
			}
			if p.Enabled != nil {
				body["enabled"] = *p.Enabled
			}
			id := pendingID
			if !a.dry {
				var out struct{ ID string }
				if err := a.api.Do(ctx, "POST", "/providers", body, &out); err != nil {
					return a.fail("provider", p.Name, err)
				}
				id = out.ID
			}
			a.providerIDs[p.Name] = id
			a.record(Change{Kind: "provider", Name: p.Name, Action: "create", Fields: fields})
			continue
		}
		cur := a.st.providers[i]
		if cur.Kind != p.Kind {
			return a.fail("provider", p.Name, fmt.Errorf("it is a %s provider on the Hub, not %s; use a new name", cur.Kind, p.Kind))
		}
		patch, fields := map[string]any{}, []string{}
		if p.BaseURL != cur.BaseURL {
			patch["base_url"], fields = p.BaseURL, append(fields, "base_url")
		}
		if p.Extra != nil && !maps.Equal(p.Extra, cur.Extra) {
			patch["extra"], fields = p.Extra, append(fields, "extra")
		}
		if p.RedactPII != nil && *p.RedactPII != cur.RedactPII {
			patch["redact_pii"], fields = *p.RedactPII, append(fields, "redact_pii")
		}
		if p.Enabled != nil && *p.Enabled != cur.Enabled {
			patch["enabled"], fields = *p.Enabled, append(fields, "enabled")
		}
		if p.APIKey.Set { // write-only: always re-applied
			v, err := a.seal(ctx, p.APIKey.Value, secrets.PurposeProviderKey)
			if err != nil {
				return a.fail("provider", p.Name, err)
			}
			patch["api_key"], fields = v, append(fields, "api_key")
		}
		if err := a.update(ctx, "provider", p.Name, "/providers/"+cur.ID, patch, fields); err != nil {
			return err
		}
	}
	return nil
}

func (a *applier) update(ctx context.Context, kind, name, path string, patch map[string]any, fields []string) error {
	if len(fields) == 0 {
		a.record(Change{Kind: kind, Name: name, Action: "unchanged"})
		return nil
	}
	if !a.dry {
		if err := a.api.Do(ctx, "PATCH", path, patch, nil); err != nil {
			return a.fail(kind, name, err)
		}
	}
	a.record(Change{Kind: kind, Name: name, Action: "update", Fields: fields})
	return nil
}

func (a *applier) connectors(ctx context.Context, doc Document) error {
	for _, c := range doc.Connectors {
		i := slices.IndexFunc(a.st.connectors, func(r remoteConnector) bool { return r.Name == c.Name })
		if i < 0 {
			body := map[string]any{"type": c.Type, "name": c.Name, "mode": c.Mode, "poll_seconds": c.PollSeconds, "config": c.Config}
			fields := []string{"type"}
			if err := a.sealInto(ctx, body, &fields, c); err != nil {
				return a.fail("connector", c.Name, err)
			}
			id, detail := pendingID, ""
			if !a.dry {
				var out struct {
					ID          string `json:"id"`
					WebhookPath string `json:"webhook_path"`
				}
				if err := a.api.Do(ctx, "POST", "/connectors", body, &out); err != nil {
					return a.fail("connector", c.Name, err)
				}
				id = out.ID
				if out.WebhookPath != "" {
					detail = "webhook: " + out.WebhookPath
				}
				if c.Enabled != nil && !*c.Enabled {
					if err := a.api.Do(ctx, "PATCH", "/connectors/"+id, map[string]any{"enabled": false}, nil); err != nil {
						return a.fail("connector", c.Name, err)
					}
				}
			}
			a.connectorIDs[c.Name] = id
			a.record(Change{Kind: "connector", Name: c.Name, Action: "create", Fields: fields, Detail: detail})
			continue
		}
		cur := a.st.connectors[i]
		if cur.Type != c.Type {
			return a.fail("connector", c.Name, fmt.Errorf("it is a %s connector on the Hub, not %s; use a new name", cur.Type, c.Type))
		}
		patch, fields := map[string]any{}, []string{}
		if c.Mode != "" && c.Mode != cur.Mode {
			patch["mode"], fields = c.Mode, append(fields, "mode")
		}
		if c.PollSeconds != 0 && c.PollSeconds != cur.PollSeconds {
			patch["poll_seconds"], fields = c.PollSeconds, append(fields, "poll_seconds")
		}
		if c.Config != nil && !maps.Equal(c.Config, cur.Config) {
			patch["config"], fields = c.Config, append(fields, "config")
		}
		if c.Enabled != nil && *c.Enabled != cur.Enabled {
			patch["enabled"], fields = *c.Enabled, append(fields, "enabled")
		}
		if err := a.sealInto(ctx, patch, &fields, c); err != nil {
			return a.fail("connector", c.Name, err)
		}
		if err := a.update(ctx, "connector", c.Name, "/connectors/"+cur.ID, patch, fields); err != nil {
			return err
		}
	}
	return nil
}

// sealInto puts a connector's secrets into body, sealed.
func (a *applier) sealInto(ctx context.Context, body map[string]any, fields *[]string, c Connector) error {
	if c.Credentials.Set {
		v, err := a.seal(ctx, c.Credentials.Value, secrets.PurposeConnectorCreds)
		if err != nil {
			return err
		}
		body["credentials"], *fields = v, append(*fields, "credentials")
	}
	if c.WebhookSecret.Set {
		v, err := a.seal(ctx, c.WebhookSecret.Value, secrets.PurposeConnectorWebhook)
		if err != nil {
			return err
		}
		body["webhook_secret"], *fields = v, append(*fields, "webhook_secret")
	}
	return nil
}

func (a *applier) providerID(name string) (string, error) {
	if id, ok := a.providerIDs[name]; ok {
		return id, nil
	}
	return "", fmt.Errorf("no provider named %q (define it under providers)", name)
}

func (a *applier) routes(ctx context.Context, doc Document) error {
	features := make([]string, 0, len(doc.Routes))
	for f := range doc.Routes {
		features = append(features, f)
	}
	sort.Strings(features)
	for _, f := range features {
		rt := doc.Routes[f]
		pid, err := a.providerID(rt.Provider)
		if err != nil {
			return a.fail("route", f, err)
		}
		body := map[string]any{"provider_id": pid, "model": rt.Model, "effort": rt.Effort,
			"max_output_tokens": rt.MaxOutputTokens, "context_token_budget": rt.ContextTokenBudget, "temperature": rt.Temperature}
		fbID, fbModel := "", ""
		if rt.Fallback != nil {
			if fbID, err = a.providerID(rt.Fallback.Provider); err != nil {
				return a.fail("route", f, err)
			}
			fbModel = rt.Fallback.Model
			body["fallback_provider_id"], body["fallback_model"] = fbID, fbModel
		}
		action, fields := "create", []string{"provider", "model"}
		if i := slices.IndexFunc(a.st.routes, func(r remoteRoute) bool { return r.Feature == f }); i >= 0 {
			cur := a.st.routes[i]
			fields = fields[:0]
			if cur.ProviderID != pid {
				fields = append(fields, "provider")
			}
			if cur.Model != rt.Model {
				fields = append(fields, "model")
			}
			if cur.Effort != rt.Effort {
				fields = append(fields, "effort")
			}
			if rt.MaxOutputTokens != 0 && cur.MaxOutputTokens != rt.MaxOutputTokens {
				fields = append(fields, "max_output_tokens")
			}
			if rt.ContextTokenBudget != 0 && cur.ContextTokenBudget != rt.ContextTokenBudget {
				fields = append(fields, "context_token_budget")
			}
			if !eqFloat(cur.Temperature, rt.Temperature) {
				fields = append(fields, "temperature")
			}
			curFB := ""
			if cur.FallbackProviderID != nil {
				curFB = *cur.FallbackProviderID
			}
			if curFB != fbID || (fbID != "" && cur.FallbackModel != fbModel) {
				fields = append(fields, "fallback")
			}
			if len(fields) == 0 {
				a.record(Change{Kind: "route", Name: f, Action: "unchanged"})
				continue
			}
			action = "update"
		}
		if !a.dry {
			if err := a.api.Do(ctx, "PUT", "/routes/"+url.PathEscape(f), body, nil); err != nil {
				return a.fail("route", f, err)
			}
		}
		a.record(Change{Kind: "route", Name: f, Action: action, Fields: fields})
	}
	return nil
}

func eqFloat(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func (a *applier) repos(ctx context.Context, doc Document) error {
	for _, r := range doc.Repos {
		cid, ok := a.connectorIDs[r.Connector]
		if !ok {
			return a.fail("repo", r.FullName, fmt.Errorf("no connector named %q (define it under connectors)", r.Connector))
		}
		i := slices.IndexFunc(a.st.repos, func(x remoteRepo) bool { return x.FullName == r.FullName && x.ConnectorID == cid })
		// Fields only PATCH can set, applied after a create too.
		extra := map[string]any{}
		if r.OnReject != "" {
			extra["on_reject"] = r.OnReject
		}
		if r.PRConflictStrategy != "" {
			extra["pr_conflict_strategy"] = r.PRConflictStrategy
		}
		if r.Owners != nil {
			extra["owners"] = *r.Owners
		}
		if r.Enabled != nil {
			extra["enabled"] = *r.Enabled
		}
		if i < 0 {
			detail := ""
			if !a.dry {
				var out struct {
					ID      string `json:"id"`
					Webhook string `json:"webhook"`
				}
				body := map[string]any{"connector_id": cid, "full_name": r.FullName, "tracked_branch": r.TrackedBranch, "docs_path": r.DocsPath,
					"push_mode": r.PushMode, "approver": r.Approver, "service_name": r.ServiceName}
				if err := a.api.Do(ctx, "POST", "/repos", body, &out); err != nil {
					return a.fail("repo", r.FullName, err)
				}
				if len(extra) > 0 {
					if err := a.api.Do(ctx, "PATCH", "/repos/"+out.ID, extra, nil); err != nil {
						return a.fail("repo", r.FullName, err)
					}
				}
				a.repoIDs[r.FullName] = out.ID
				if out.Webhook != "" {
					detail = "webhook " + out.Webhook
				}
			} else {
				a.repoIDs[r.FullName] = pendingID
			}
			a.record(Change{Kind: "repo", Name: r.FullName, Action: "create", Detail: detail})
			continue
		}
		cur := a.st.repos[i]
		patch, fields := map[string]any{}, []string{}
		set := func(key string, want, have string) {
			if want != "" && want != have {
				patch[key], fields = want, append(fields, key)
			}
		}
		set("tracked_branch", r.TrackedBranch, cur.TrackedBranch)
		set("docs_path", r.DocsPath, cur.DocsPath)
		set("push_mode", r.PushMode, cur.Push.Mode)
		set("on_reject", r.OnReject, cur.Push.OnReject)
		set("approver", r.Approver, cur.Push.Approver)
		set("pr_conflict_strategy", r.PRConflictStrategy, cur.Push.ConflictStrategy)
		set("service_name", r.ServiceName, cur.ServiceName)
		if r.Enabled != nil && *r.Enabled != cur.Enabled {
			patch["enabled"], fields = *r.Enabled, append(fields, "enabled")
		}
		if r.Owners != nil { // not in the list view: always re-applied
			patch["owners"], fields = *r.Owners, append(fields, "owners")
		}
		if err := a.update(ctx, "repo", r.FullName, "/repos/"+cur.ID, patch, fields); err != nil {
			return err
		}
	}
	return nil
}

func (a *applier) spend(ctx context.Context, doc Document) error {
	if doc.Spend == nil {
		return nil
	}
	want := make([]remoteLimit, 0, len(doc.Spend.Limits))
	for _, l := range doc.Spend.Limits {
		key := l.Key
		switch l.Scope {
		case "provider":
			if id, ok := a.providerIDs[key]; ok {
				key = id
			}
		case "repo":
			if id, ok := a.repoIDs[key]; ok {
				key = id
			}
		case "global", "feature":
		default:
			return a.fail("spend", "limits", fmt.Errorf("scope must be global, feature, provider or repo, not %q", l.Scope))
		}
		onBreach := l.OnBreach
		if onBreach == "" {
			onBreach = "block"
		}
		want = append(want, remoteLimit{Scope: l.Scope, ScopeKey: key, Window: l.Window, MaxTokens: l.MaxTokens, MaxCostUSD: l.MaxCostUSD,
			OnBreach: onBreach, AlertURL: l.AlertURL})
	}
	if limitsEqual(want, a.st.limits) {
		a.record(Change{Kind: "spend", Name: "limits", Action: "unchanged"})
		return nil
	}
	if !a.dry {
		if err := a.api.Do(ctx, "PUT", "/spend/limits", map[string]any{"items": want}, nil); err != nil {
			return a.fail("spend", "limits", err)
		}
	}
	a.record(Change{Kind: "spend", Name: "limits", Action: "update", Detail: fmt.Sprintf("%d limit(s) replace %d", len(want), len(a.st.limits))})
	return nil
}

func limitsEqual(a, b []remoteLimit) bool {
	if len(a) != len(b) {
		return false
	}
	key := func(l remoteLimit) string {
		mt, mc := "-", "-"
		if l.MaxTokens != nil {
			mt = fmt.Sprint(*l.MaxTokens)
		}
		if l.MaxCostUSD != nil {
			mc = fmt.Sprint(*l.MaxCostUSD)
		}
		return fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s", l.Scope, l.ScopeKey, l.Window, mt, mc, l.OnBreach, l.AlertURL)
	}
	ka, kb := make([]string, len(a)), make([]string, len(b))
	for i := range a {
		ka[i], kb[i] = key(a[i]), key(b[i])
	}
	sort.Strings(ka)
	sort.Strings(kb)
	return slices.Equal(ka, kb)
}

// ErrAPI is wrapped by API implementations for non-2xx answers.
var ErrAPI = errors.New("hub api error")
