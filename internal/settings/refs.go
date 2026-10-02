package settings

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Schemes lists the secret sources a reference can name.
var Schemes = []string{"env", "file", "vault", "gopass", "awssm", "gcpsm"}

var refRE = regexp.MustCompile(`\$?\$\{([a-z0-9]+):([^}]*)\}`)

// Resolver turns ${scheme:ref#key} references into values. The zero value resolves every scheme with the
// process environment, local files, Vault (VAULT_ADDR/VAULT_TOKEN), gopass on PATH, and the default AWS
// and Google credentials — right for the CLI on your machine or in CI. A server sets Allowed, EnvPrefix
// and FileRoot so that pasted settings cannot read arbitrary environment variables or files.
type Resolver struct {
	// Allowed limits the schemes; nil allows all.
	Allowed map[string]bool
	// EnvPrefix, when set, is required at the start of every ${env:NAME}.
	EnvPrefix string
	// FileRoot, when set, is the directory every ${file:…} must be inside.
	FileRoot string

	Getenv func(string) (string, bool)
	HTTP   *http.Client
	Exec   func(ctx context.Context, name string, args ...string) ([]byte, error)
	// AWSSecret and GCPSecret fetch a secret's value by id; nil uses the SDK default credential chains.
	AWSSecret func(ctx context.Context, id string) (string, error)
	GCPSecret func(ctx context.Context, name string) (string, error)

	cache map[string]string
	used  map[string]bool
}

// Used reports which schemes the last Load resolved, sorted (for reports; never values).
func (r *Resolver) Used() []string {
	out := make([]string, 0, len(r.used))
	for s := range r.used {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func (r *Resolver) interpolate(ctx context.Context, n *yaml.Node) error {
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range n.Content {
			if err := r.interpolate(ctx, c); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		for i := 1; i < len(n.Content); i += 2 { // values only; keys stay literal
			if err := r.interpolate(ctx, n.Content[i]); err != nil {
				return err
			}
		}
	case yaml.ScalarNode:
		if !strings.Contains(n.Value, "${") {
			return nil
		}
		v, err := r.Expand(ctx, n.Value)
		if err != nil {
			return fmt.Errorf("line %d: %w", n.Line, err)
		}
		n.Value, n.Tag, n.Style = v, "!!str", yaml.DoubleQuotedStyle
	}
	return nil
}

// Expand replaces every reference in s; "$${" is a literal "${".
func (r *Resolver) Expand(ctx context.Context, s string) (string, error) {
	var firstErr error
	out := refRE.ReplaceAllStringFunc(s, func(m string) string {
		if strings.HasPrefix(m, "$$") {
			return m[1:]
		}
		sub := refRE.FindStringSubmatch(m)
		v, err := r.resolve(ctx, sub[1], sub[2])
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return v
	})
	return out, firstErr
}

func (r *Resolver) resolve(ctx context.Context, scheme, ref string) (string, error) {
	label := "${" + scheme + ":" + ref + "}" // errors name the reference, never a value
	if !contains(Schemes, scheme) {
		return "", fmt.Errorf("%s: unknown secret source %q (use %s)", label, scheme, strings.Join(Schemes, ", "))
	}
	if r.Allowed != nil && !r.Allowed[scheme] {
		return "", fmt.Errorf("%s: the %s source is not enabled on this Hub (resolve it with `dth apply` instead, or see DTH_SETTINGS_SECRET_SOURCES)", label, scheme)
	}
	key := scheme + ":" + ref
	if v, ok := r.cache[key]; ok {
		return v, nil
	}
	path, field := splitField(ref)
	if path == "" {
		return "", fmt.Errorf("%s: empty reference", label)
	}
	var v string
	var err error
	switch scheme {
	case "env":
		v, err = r.env(path)
	case "file":
		v, err = r.file(path, field)
		field = "" // the file reader handles the field per format
	case "vault":
		v, err = r.vault(ctx, path, field)
		field = ""
	case "gopass":
		v, err = r.gopass(ctx, path, field)
		field = ""
	case "awssm":
		v, err = r.aws(ctx, path)
	case "gcpsm":
		v, err = r.gcp(ctx, path)
	}
	if err == nil && field != "" {
		v, err = pickJSON(v, field)
	}
	if err != nil {
		return "", fmt.Errorf("%s: %w", label, err)
	}
	if r.cache == nil {
		r.cache, r.used = map[string]string{}, map[string]bool{}
	}
	r.cache[key], r.used[scheme] = v, true
	return v, nil
}

// splitField splits "path#field" at the last '#'.
func splitField(ref string) (string, string) {
	if i := strings.LastIndexByte(ref, '#'); i >= 0 {
		return strings.TrimSpace(ref[:i]), strings.TrimSpace(ref[i+1:])
	}
	return strings.TrimSpace(ref), ""
}

func (r *Resolver) env(name string) (string, error) {
	if r.EnvPrefix != "" && !strings.HasPrefix(name, r.EnvPrefix) {
		return "", fmt.Errorf("only variables starting with %s can be read here", r.EnvPrefix)
	}
	get := r.Getenv
	if get == nil {
		get = os.LookupEnv
	}
	v, ok := get(name)
	if !ok {
		return "", fmt.Errorf("environment variable %s is not set", name)
	}
	return v, nil
}

func (r *Resolver) file(path, field string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	if r.FileRoot != "" {
		root, err := filepath.Abs(r.FileRoot)
		if err != nil {
			return "", err
		}
		abs, err := filepath.Abs(filepath.Join(root, path))
		if filepath.IsAbs(path) {
			abs, err = filepath.Abs(path)
		}
		if err != nil {
			return "", err
		}
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
		if realRoot, err := filepath.EvalSymlinks(root); err == nil {
			root = realRoot
		}
		if rel, err := filepath.Rel(root, abs); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("files must be inside %s", r.FileRoot)
		}
		path = abs
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file: %w", unwrapPathErr(err))
	}
	if field == "" {
		return strings.TrimRight(string(b), "\r\n"), nil
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json", ".yaml", ".yml":
		var m any
		if err := yaml.Unmarshal(b, &m); err != nil {
			return "", fmt.Errorf("parse %s: %w", filepath.Base(path), err)
		}
		return pick(m, field)
	default: // .properties, .env, .conf, or no extension: key=value lines
		kv := parseKeyValues(string(b))
		v, ok := kv[field]
		if !ok {
			return "", fmt.Errorf("no key %q in %s", field, filepath.Base(path))
		}
		return v, nil
	}
}

func unwrapPathErr(err error) error {
	if pe, ok := err.(*os.PathError); ok {
		return fmt.Errorf("%s: %w", pe.Path, pe.Err)
	}
	return err
}

// parseKeyValues reads Java .properties and dotenv files: key=value or key: value, # and ! comments,
// optional "export ", and surrounding quotes.
func parseKeyValues(s string) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == '!' {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		i := strings.IndexAny(line, "=:")
		if i <= 0 {
			continue
		}
		k, v := strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:])
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		out[k] = v
	}
	return out
}

// pick walks a decoded JSON/YAML value by a dotted path (a.b.0.c).
func pick(v any, path string) (string, error) {
	cur := v
	for _, part := range strings.Split(path, ".") {
		switch t := cur.(type) {
		case map[string]any:
			next, ok := t[part]
			if !ok {
				return "", fmt.Errorf("no key %q", path)
			}
			cur = next
		case []any:
			var i int
			if _, err := fmt.Sscanf(part, "%d", &i); err != nil || i < 0 || i >= len(t) {
				return "", fmt.Errorf("no key %q", path)
			}
			cur = t[i]
		default:
			return "", fmt.Errorf("no key %q", path)
		}
	}
	switch t := cur.(type) {
	case string:
		return t, nil
	case nil:
		return "", fmt.Errorf("key %q is empty", path)
	case map[string]any, []any:
		b, err := json.Marshal(t)
		return string(b), err
	default:
		return fmt.Sprint(t), nil
	}
}

func pickJSON(s, field string) (string, error) {
	var m any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return "", fmt.Errorf("#%s needs a JSON secret", field)
	}
	return pick(m, field)
}

func (r *Resolver) httpClient() *http.Client {
	if r.HTTP != nil {
		return r.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// vault reads HashiCorp Vault over its HTTP API: KV v2 (secret/data/app) or v1 paths. VAULT_ADDR,
// VAULT_TOKEN (or ~/.vault-token) and VAULT_NAMESPACE as in the vault CLI.
func (r *Resolver) vault(ctx context.Context, path, field string) (string, error) {
	addr, _ := r.lookup("VAULT_ADDR")
	if addr == "" {
		return "", fmt.Errorf("VAULT_ADDR is not set")
	}
	token, _ := r.lookup("VAULT_TOKEN")
	if token == "" {
		if home, err := os.UserHomeDir(); err == nil {
			if b, err := os.ReadFile(filepath.Join(home, ".vault-token")); err == nil {
				token = strings.TrimSpace(string(b))
			}
		}
	}
	if token == "" {
		return "", fmt.Errorf("VAULT_TOKEN is not set (and no ~/.vault-token)")
	}
	u := strings.TrimRight(addr, "/") + "/v1/" + strings.TrimLeft(escapePath(path), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Vault-Token", token)
	if ns, _ := r.lookup("VAULT_NAMESPACE"); ns != "" {
		req.Header.Set("X-Vault-Namespace", ns)
	}
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vault answered %d", resp.StatusCode)
	}
	var out struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("vault: %w", err)
	}
	data := out.Data
	if inner, ok := data["data"].(map[string]any); ok { // KV v2 wraps the values with metadata
		if _, hasMeta := data["metadata"]; hasMeta {
			data = inner
		}
	}
	if field == "" {
		if len(data) == 1 {
			for _, v := range data {
				return pick(map[string]any{"v": v}, "v")
			}
		}
		return "", fmt.Errorf("the secret has %d keys: name one with #key", len(data))
	}
	return pick(data, field)
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// gopass runs `gopass show -o <path>` (the password line) or `gopass show <path> <key>`.
func (r *Resolver) gopass(ctx context.Context, path, field string) (string, error) {
	run := r.Exec
	if run == nil {
		run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Stdin = nil
			return cmd.Output()
		}
	}
	args := []string{"show", "-o", path}
	if field != "" {
		args = []string{"show", path, field}
	}
	out, err := run(ctx, "gopass", args...)
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("gopass: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("gopass: %w", err)
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

func (r *Resolver) aws(ctx context.Context, id string) (string, error) {
	if r.AWSSecret != nil {
		return r.AWSSecret(ctx, id)
	}
	return awsSecret(ctx, id)
}

func (r *Resolver) gcp(ctx context.Context, name string) (string, error) {
	fetch := r.GCPSecret
	if fetch == nil {
		fetch = r.gcpSecret
	}
	// Short forms: project/secret (latest) and project/secret/version.
	if !strings.HasPrefix(name, "projects/") {
		p := strings.Split(name, "/")
		switch len(p) {
		case 2:
			name = "projects/" + p[0] + "/secrets/" + p[1] + "/versions/latest"
		case 3:
			name = "projects/" + p[0] + "/secrets/" + p[1] + "/versions/" + p[2]
		default:
			return "", fmt.Errorf("use projects/P/secrets/S/versions/V or P/S[/V]")
		}
	} else if !strings.Contains(name, "/versions/") {
		name += "/versions/latest"
	}
	return fetch(ctx, name)
}

func (r *Resolver) lookup(name string) (string, bool) {
	if r.Getenv != nil {
		return r.Getenv(name)
	}
	return os.LookupEnv(name)
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
