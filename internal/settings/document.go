// Package settings applies a declarative Hub configuration — providers, routing, connectors, repositories
// and spend limits — from YAML or JSON files. Secret values are references (${env:…}, ${file:…},
// ${vault:…}, ${gopass:…}, ${awssm:…}, ${gcpsm:…}) resolved at apply time, so the files can live in git.
// It changes the Hub only through its REST API, so validation, audit and webhook registration are the
// same as in the UI. It never deletes anything.
package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Version is the document format version.
const Version = 1

// Document is one Hub configuration. Every section is optional.
type Document struct {
	Version    int              `yaml:"version,omitempty" json:"version,omitempty"`
	Auth       *Auth            `yaml:"auth,omitempty" json:"auth,omitempty"`
	Users      []User           `yaml:"users,omitempty" json:"users,omitempty"`
	Providers  []Provider       `yaml:"providers,omitempty" json:"providers,omitempty"`
	Routes     map[string]Route `yaml:"routes,omitempty" json:"routes,omitempty"`
	Connectors []Connector      `yaml:"connectors,omitempty" json:"connectors,omitempty"`
	Repos      []Repo           `yaml:"repos,omitempty" json:"repos,omitempty"`
	Spend      *Spend           `yaml:"spend,omitempty" json:"spend,omitempty"`
}

// Provider is an LLM or embedding account, matched by name.
type Provider struct {
	Name      string            `yaml:"name" json:"name"`
	Kind      string            `yaml:"kind" json:"kind"`
	BaseURL   string            `yaml:"base_url,omitempty" json:"base_url,omitempty"`
	APIKey    Secret            `yaml:"api_key,omitempty" json:"api_key,omitempty"`
	Extra     map[string]string `yaml:"extra,omitempty" json:"extra,omitempty"`
	RedactPII *bool             `yaml:"redact_pii,omitempty" json:"redact_pii,omitempty"`
	Enabled   *bool             `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

// Route sends one feature (docgen, qa, decode, …) to a provider by name.
type Route struct {
	Provider           string    `yaml:"provider" json:"provider"`
	Model              string    `yaml:"model" json:"model"`
	Effort             string    `yaml:"effort,omitempty" json:"effort,omitempty"`
	MaxOutputTokens    int       `yaml:"max_output_tokens,omitempty" json:"max_output_tokens,omitempty"`
	ContextTokenBudget int       `yaml:"context_token_budget,omitempty" json:"context_token_budget,omitempty"`
	Temperature        *float64  `yaml:"temperature,omitempty" json:"temperature,omitempty"`
	Fallback           *Fallback `yaml:"fallback,omitempty" json:"fallback,omitempty"`
}

// Fallback is the provider and model tried when the primary fails.
type Fallback struct {
	Provider string `yaml:"provider" json:"provider"`
	Model    string `yaml:"model" json:"model"`
}

// Connector is a git host, signal source or knowledge source, matched by name.
type Connector struct {
	Name          string            `yaml:"name" json:"name"`
	Type          string            `yaml:"type" json:"type"`
	Mode          string            `yaml:"mode,omitempty" json:"mode,omitempty"`
	PollSeconds   int64             `yaml:"poll_seconds,omitempty" json:"poll_seconds,omitempty"`
	Config        map[string]string `yaml:"config,omitempty" json:"config,omitempty"`
	Credentials   Secret            `yaml:"credentials,omitempty" json:"credentials,omitempty"`
	WebhookSecret Secret            `yaml:"webhook_secret,omitempty" json:"webhook_secret,omitempty"`
	Enabled       *bool             `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

// Repo is a tracked repository, matched by full name; Connector names its git host connector.
type Repo struct {
	FullName           string    `yaml:"full_name" json:"full_name"`
	Connector          string    `yaml:"connector" json:"connector"`
	TrackedBranch      string    `yaml:"tracked_branch,omitempty" json:"tracked_branch,omitempty"`
	DocsPath           string    `yaml:"docs_path,omitempty" json:"docs_path,omitempty"`
	PushMode           string    `yaml:"push_mode,omitempty" json:"push_mode,omitempty"`
	OnReject           string    `yaml:"on_reject,omitempty" json:"on_reject,omitempty"`
	Approver           string    `yaml:"approver,omitempty" json:"approver,omitempty"`
	PRConflictStrategy string    `yaml:"pr_conflict_strategy,omitempty" json:"pr_conflict_strategy,omitempty"`
	ServiceName        string    `yaml:"service_name,omitempty" json:"service_name,omitempty"`
	Owners             *[]string `yaml:"owners,omitempty" json:"owners,omitempty"`
	Enabled            *bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

// Auth is how people sign in (applying it needs the owner role).
type Auth struct {
	// Password turns email-and-password sign-in on or off (unset: the deployment's default).
	Password *bool `yaml:"password,omitempty" json:"password,omitempty"`
	SSO      *SSO  `yaml:"sso,omitempty" json:"sso,omitempty"`
}

// SSO is an OpenID Connect identity provider (Google Workspace, Microsoft Entra ID, Okta, Keycloak, …).
type SSO struct {
	Provider       string   `yaml:"provider,omitempty" json:"provider,omitempty"`
	Issuer         string   `yaml:"issuer" json:"issuer"`
	ClientID       string   `yaml:"client_id" json:"client_id"`
	ClientSecret   Secret   `yaml:"client_secret,omitempty" json:"client_secret,omitempty"`
	AllowedDomains []string `yaml:"allowed_domains,omitempty" json:"allowed_domains,omitempty"`
	GroupsClaim    string   `yaml:"groups_claim,omitempty" json:"groups_claim,omitempty"`
}

// User is a person, matched by email. They sign in with single sign-on (same email) or a password link
// an admin creates; a user listed with role owner owns the Hub from their first sign-in.
type User struct {
	Email    string `yaml:"email" json:"email"`
	Name     string `yaml:"name,omitempty" json:"name,omitempty"`
	Role     string `yaml:"role,omitempty" json:"role,omitempty"` // viewer (default), editor, admin, owner
	Disabled *bool  `yaml:"disabled,omitempty" json:"disabled,omitempty"`
}

// Spend replaces the spend limits when present.
type Spend struct {
	Limits []SpendLimit `yaml:"limits" json:"limits"`
}

// SpendLimit is one ceiling. Key is a provider name, repository full name or feature, per Scope.
type SpendLimit struct {
	Scope      string   `yaml:"scope" json:"scope"`
	Key        string   `yaml:"key,omitempty" json:"key,omitempty"`
	Window     string   `yaml:"window" json:"window"`
	MaxTokens  *int64   `yaml:"max_tokens,omitempty" json:"max_tokens,omitempty"`
	MaxCostUSD *float64 `yaml:"max_cost_usd,omitempty" json:"max_cost_usd,omitempty"`
	OnBreach   string   `yaml:"on_breach,omitempty" json:"on_breach,omitempty"`
	AlertURL   string   `yaml:"alert_url,omitempty" json:"alert_url,omitempty"`
}

// Secret is a write-only value. In a file it is a string, or a mapping that is sent as compact JSON
// (for connectors whose credentials are JSON, e.g. {"client_id": …, "client_secret": …}).
type Secret struct {
	Value string
	Set   bool
}

// UnmarshalYAML accepts a scalar or a mapping.
func (s *Secret) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		s.Value, s.Set = n.Value, n.Value != ""
		return nil
	case yaml.MappingNode:
		var m map[string]any
		if err := n.Decode(&m); err != nil {
			return err
		}
		b, err := json.Marshal(m)
		if err != nil {
			return err
		}
		s.Value, s.Set = string(b), true
		return nil
	}
	return fmt.Errorf("line %d: a secret must be a string or a mapping", n.Line)
}

// MarshalYAML writes the value (export writes references, never resolved secrets).
func (s Secret) MarshalYAML() (any, error) { return s.Value, nil }

// IsZero lets omitempty drop unset secrets.
func (s Secret) IsZero() bool { return !s.Set }

// MarshalJSON writes the value as a string.
func (s Secret) MarshalJSON() ([]byte, error) { return json.Marshal(s.Value) }

// Source is one input file's name and bytes.
type Source struct {
	Name string
	Data []byte
}

// ReadPaths reads files and directories (every *.yaml, *.yml and *.json inside, sorted); "-" is stdin.
func ReadPaths(paths []string, stdin io.Reader) ([]Source, error) {
	var out []Source
	for _, p := range paths {
		if p == "-" {
			b, err := io.ReadAll(stdin)
			if err != nil {
				return nil, err
			}
			out = append(out, Source{Name: "stdin", Data: b})
			continue
		}
		st, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		files := []string{p}
		if st.IsDir() {
			files = nil
			ents, err := os.ReadDir(p)
			if err != nil {
				return nil, err
			}
			for _, e := range ents {
				ext := strings.ToLower(filepath.Ext(e.Name()))
				if !e.IsDir() && (ext == ".yaml" || ext == ".yml" || ext == ".json") {
					files = append(files, filepath.Join(p, e.Name()))
				}
			}
			sort.Strings(files)
			if len(files) == 0 {
				return nil, fmt.Errorf("%s: no .yaml, .yml or .json files", p)
			}
		}
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			out = append(out, Source{Name: f, Data: b})
		}
	}
	return out, nil
}

// Load parses the sources, resolves secret references in every string value, and merges them into one
// document: lists are concatenated (a later item with the same name replaces an earlier one), routes are
// merged by feature, and the last spend section wins.
func Load(ctx context.Context, srcs []Source, r *Resolver) (Document, error) {
	var doc Document
	for _, s := range srcs {
		var root yaml.Node
		if err := yaml.Unmarshal(s.Data, &root); err != nil {
			return Document{}, fmt.Errorf("%s: %w", s.Name, err)
		}
		if root.Kind == 0 { // empty file
			continue
		}
		// Check keys against the original text, so a typo is an error with its real line number.
		strict := yaml.NewDecoder(bytes.NewReader(s.Data))
		strict.KnownFields(true)
		if err := strict.Decode(new(Document)); err != nil {
			return Document{}, fmt.Errorf("%s: %w", s.Name, err)
		}
		if r != nil {
			if err := r.interpolate(ctx, &root); err != nil {
				return Document{}, fmt.Errorf("%s: %w", s.Name, err)
			}
		}
		var part Document
		if err := root.Decode(&part); err != nil {
			return Document{}, fmt.Errorf("%s: %w", s.Name, err)
		}
		if part.Version > Version {
			return Document{}, fmt.Errorf("%s: version %d is newer than this Hub understands (%d)", s.Name, part.Version, Version)
		}
		doc.merge(part)
	}
	return doc, doc.Validate()
}

func (d *Document) merge(p Document) {
	if p.Auth != nil {
		if d.Auth == nil {
			d.Auth = &Auth{}
		}
		if p.Auth.Password != nil {
			d.Auth.Password = p.Auth.Password
		}
		if p.Auth.SSO != nil {
			d.Auth.SSO = p.Auth.SSO
		}
	}
	d.Users = mergeBy(d.Users, p.Users, func(x User) string { return strings.ToLower(strings.TrimSpace(x.Email)) })
	d.Providers = mergeBy(d.Providers, p.Providers, func(x Provider) string { return x.Name })
	d.Connectors = mergeBy(d.Connectors, p.Connectors, func(x Connector) string { return x.Name })
	d.Repos = mergeBy(d.Repos, p.Repos, func(x Repo) string { return x.FullName })
	for f, rt := range p.Routes {
		if d.Routes == nil {
			d.Routes = map[string]Route{}
		}
		d.Routes[f] = rt
	}
	if p.Spend != nil {
		d.Spend = p.Spend
	}
}

func mergeBy[T any](base, add []T, key func(T) string) []T {
	for _, x := range add {
		if i := slices.IndexFunc(base, func(y T) bool { return key(y) == key(x) }); i >= 0 {
			base[i] = x
		} else {
			base = append(base, x)
		}
	}
	return base
}

// Validate checks what the file alone can: required fields and references between sections.
func (d *Document) Validate() error {
	var errs []error
	bad := func(f string, a ...any) { errs = append(errs, fmt.Errorf(f, a...)) }
	if d.Auth != nil && d.Auth.SSO != nil && (d.Auth.SSO.Issuer == "" || d.Auth.SSO.ClientID == "") {
		bad("auth.sso: issuer and client_id are required")
	}
	for i, u := range d.Users {
		if strings.TrimSpace(u.Email) == "" {
			bad("users[%d]: email is required", i)
		}
		switch u.Role {
		case "", "viewer", "editor", "admin", "owner":
		default:
			bad("users[%d]: role must be viewer, editor, admin or owner", i)
		}
	}
	for i, p := range d.Providers {
		if p.Name == "" || p.Kind == "" {
			bad("providers[%d]: name and kind are required", i)
		}
	}
	for i, c := range d.Connectors {
		if c.Name == "" || c.Type == "" {
			bad("connectors[%d]: name and type are required", i)
		}
	}
	for i, r := range d.Repos {
		if r.FullName == "" || r.Connector == "" {
			bad("repos[%d]: full_name and connector are required", i)
		}
	}
	for f, rt := range d.Routes {
		if rt.Provider == "" || rt.Model == "" {
			bad("routes.%s: provider and model are required", f)
		}
		if rt.Fallback != nil && (rt.Fallback.Provider == "" || rt.Fallback.Model == "") {
			bad("routes.%s.fallback: provider and model are required", f)
		}
	}
	if d.Spend != nil {
		for i, l := range d.Spend.Limits {
			if l.Scope == "" || l.Window == "" {
				bad("spend.limits[%d]: scope and window are required", i)
			}
		}
	}
	return errors.Join(errs...)
}
