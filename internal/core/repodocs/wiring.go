package repodocs

import (
	"path"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Wire is one fact about how a repository is wired to the outside, read from its configuration and CI
// files (no model): what it serves as, what it calls, which images it publishes or runs, and which
// other repositories its pipelines reference. The System links match them across repositories.
type Wire struct {
	// Kind: host (a name it is reached by: a Kubernetes Service, an ingress host), calls (a host it
	// calls), image_pub (an image its pipeline publishes), image_use (an image it runs or builds on),
	// ci_ref (an owner/name its CI refers to).
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Note  string `json:"note,omitempty"` // what to show: the URL, image or CI line
	Path  string `json:"path"`
	Line  int    `json:"line"`
}

// WiringFile reports configuration, deployment and CI files worth reading for wiring.
func WiringFile(p string) bool {
	l := strings.ToLower(p)
	base, ext := path.Base(l), path.Ext(l)
	if CIFile(l) || base == "dockerfile" || strings.HasSuffix(base, ".dockerfile") || strings.HasPrefix(base, "docker-compose") ||
		base == "compose.yaml" || base == "compose.yml" || base == "fly.toml" || base == "makefile" || base == "skaffold.yaml" {
		return true
	}
	if base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".env") {
		return true
	}
	switch ext {
	case ".yaml", ".yml", ".json", ".toml", ".properties", ".env", ".ini", ".conf", ".tf", ".tfvars", ".hcl":
	default:
		return false
	}
	if base == "package.json" || base == "package-lock.json" || base == "tsconfig.json" || strings.HasSuffix(base, ".lock") {
		return false
	}
	if strings.HasPrefix(base, "application") || strings.HasPrefix(base, "values") || strings.HasPrefix(base, "config") || strings.HasPrefix(base, "settings") {
		return true
	}
	for _, seg := range strings.Split(path.Dir(l), "/") {
		switch seg {
		case "config", "configs", "conf", "deploy", "deployment", "deployments", "k8s", "kube", "kubernetes", "manifests", "helm",
			"charts", "templates", "infra", "infrastructure", "terraform", "ops", "env", "environments", "overlays", "base":
			return true
		}
	}
	return false
}

// CIFile reports CI pipeline definitions.
func CIFile(p string) bool {
	l := strings.ToLower(p)
	base := path.Base(l)
	return strings.HasPrefix(l, ".github/workflows/") || strings.HasPrefix(l, ".github/actions/") || base == ".gitlab-ci.yml" ||
		strings.HasPrefix(l, ".gitlab/") || base == "jenkinsfile" || strings.HasPrefix(l, ".circleci/") || base == "azure-pipelines.yml" ||
		base == "bitbucket-pipelines.yml" || base == "cloudbuild.yaml" || base == "buildspec.yml" || strings.HasPrefix(l, ".buildkite/") ||
		strings.HasPrefix(l, ".tekton/") || base == ".drone.yml"
}

var (
	urlRE       = regexp.MustCompile(`(?i)\b(?:https?|grpcs?|wss?|amqps?|nats|redis|kafka)://(?:[^\s/@"'<>]+@)?([a-z0-9][a-z0-9.-]*[a-z0-9])(?::\d+)?(?:/[^\s"'<>\x60]*)?`)
	hostVarRE   = regexp.MustCompile(`(?i)\b[A-Z0-9_]*(?:HOST|ADDR|ADDRESS|ENDPOINT|SERVICE|TARGET|SERVER|URL)\b\s*[:=]\s*["']?([a-z0-9][a-z0-9.-]*[a-z0-9])(?::(\d+))?["']?\s*$`)
	ciRefRE     = regexp.MustCompile(`\b([A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*)`)
	fromRE      = regexp.MustCompile(`(?i)^\s*FROM\s+(?:--platform=\S+\s+)?(\S+)`)
	buildTagRE  = regexp.MustCompile(`docker\s+(?:buildx\s+)?build\b.*?(?:-t|--tag)[ =](\S+)`)
	pushRE      = regexp.MustCompile(`docker\s+push\s+(\S+)`)
	ghRepoVarRE = regexp.MustCompile(`\$\{\{\s*github\.repository\s*\}\}`)
)

// skipHosts are hosts that never name another tracked service.
var skipHosts = map[string]bool{"localhost": true, "127.0.0.1": true, "0.0.0.0": true, "example.com": true, "true": true, "false": true}

// ExtractWiring reads one file's wiring facts. repo is the repository's own owner/name.
func ExtractWiring(repo, p, content string) []Wire {
	var out []Wire
	add := func(kind, value, note string, line int) {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 200 {
			return
		}
		out = append(out, Wire{Kind: kind, Value: value, Note: clipNote(note), Path: p, Line: line})
	}
	lines := strings.Split(content, "\n")
	ci := CIFile(p)
	l := strings.ToLower(p)
	base := path.Base(l)
	isYAML := strings.HasSuffix(l, ".yaml") || strings.HasSuffix(l, ".yml")
	for i, ln := range lines {
		n := i + 1
		trim := strings.TrimSpace(ln)
		if trim == "" || strings.HasPrefix(trim, "//") || (strings.HasPrefix(trim, "#") && !strings.HasPrefix(trim, "#!")) {
			continue
		}
		for _, m := range urlRE.FindAllStringSubmatch(ln, -1) {
			if h := strings.ToLower(m[1]); !skipHosts[h] && strings.ContainsAny(h, "abcdefghijklmnopqrstuvwxyz") {
				add("calls", h, m[0], n)
			}
		}
		if !strings.Contains(ln, "://") {
			if m := hostVarRE.FindStringSubmatch(ln); m != nil {
				if h := strings.ToLower(m[1]); !skipHosts[h] && (strings.Contains(h, ".") || m[2] != "") && !isNumberish(h) {
					add("calls", h, trim, n)
				}
			}
		}
		if base == "dockerfile" || strings.HasSuffix(base, ".dockerfile") {
			if m := fromRE.FindStringSubmatch(ln); m != nil {
				if img := normImage(repo, m[1]); img != "" {
					add("image_use", img, "FROM "+m[1], n)
				}
			}
		}
		for _, re := range []*regexp.Regexp{buildTagRE, pushRE} {
			if m := re.FindStringSubmatch(ln); m != nil {
				if img := normImage(repo, m[1]); img != "" {
					add("image_pub", img, trim, n)
				}
			}
		}
		if ci {
			for _, m := range ciRefRE.FindAllStringSubmatch(ln, -1) {
				ref := strings.ToLower(m[1])
				if ref != strings.ToLower(repo) && !strings.HasPrefix(ref, "actions/") && !strings.HasPrefix(ref, "github/") {
					add("ci_ref", ref, trim, n)
				}
			}
		}
	}
	if isYAML || base == "fly.toml" {
		out = append(out, yamlWiring(repo, p, content, ci)...)
	}
	return dedupeWires(out)
}

// yamlWiring reads Kubernetes, Compose, Helm and workflow YAML: Services and ingress hosts it serves,
// images it runs or publishes.
func yamlWiring(repo, p, content string, ci bool) []Wire {
	var out []Wire
	if strings.HasSuffix(strings.ToLower(p), "fly.toml") {
		for i, ln := range strings.Split(content, "\n") {
			if m := regexp.MustCompile(`^\s*app\s*=\s*["']([a-z0-9-]+)["']`).FindStringSubmatch(ln); m != nil {
				out = append(out, Wire{Kind: "host", Value: m[1] + ".fly.dev", Note: "fly.io app " + m[1], Path: p, Line: i + 1})
			}
		}
		return out
	}
	dec := yaml.NewDecoder(strings.NewReader(content))
	for range 50 {
		var doc yaml.Node
		if err := dec.Decode(&doc); err != nil {
			break
		}
		if len(doc.Content) == 0 {
			continue
		}
		root := doc.Content[0]
		kind := scalar(get(root, "kind"))
		switch kind {
		case "Service":
			if name := get(get(root, "metadata"), "name"); name != nil && name.Value != "" {
				out = append(out, Wire{Kind: "host", Value: strings.ToLower(name.Value), Note: "Kubernetes Service " + name.Value, Path: p, Line: name.Line})
			}
		case "Ingress":
			for _, r := range seq(get(get(root, "spec"), "rules")) {
				if h := get(r, "host"); h != nil && h.Value != "" {
					out = append(out, Wire{Kind: "host", Value: strings.ToLower(h.Value), Note: "ingress host " + h.Value, Path: p, Line: h.Line})
				}
			}
		case "HTTPRoute", "GRPCRoute", "VirtualService":
			for _, h := range seq(get(get(root, "spec"), "hostnames")) {
				out = append(out, Wire{Kind: "host", Value: strings.ToLower(h.Value), Note: kind + " host " + h.Value, Path: p, Line: h.Line})
			}
			for _, h := range seq(get(get(root, "spec"), "hosts")) {
				out = append(out, Wire{Kind: "host", Value: strings.ToLower(h.Value), Note: kind + " host " + h.Value, Path: p, Line: h.Line})
			}
		}
		walkYAML(root, func(key string, v *yaml.Node, parent *yaml.Node) {
			switch {
			case key == "image" && v.Kind == yaml.ScalarNode:
				if img := normImage(repo, v.Value); img != "" {
					out = append(out, Wire{Kind: "image_use", Value: img, Note: "image " + v.Value, Path: p, Line: v.Line})
				}
			case key == "repository" && v.Kind == yaml.ScalarNode && get(parent, "tag") != nil:
				// Helm values: image: {repository: ghcr.io/acme/web, tag: ...}
				if img := normImage(repo, v.Value); img != "" {
					out = append(out, Wire{Kind: "image_use", Value: img, Note: "image " + v.Value, Path: p, Line: v.Line})
				}
			case ci && key == "tags":
				// docker/build-push-action and similar: the images the pipeline publishes.
				for _, t := range splitTags(v) {
					if img := normImage(repo, t.Value); img != "" {
						out = append(out, Wire{Kind: "image_pub", Value: img, Note: "publishes " + t.Value, Path: p, Line: t.Line})
					}
				}
			case ci && key == "repository" && v.Kind == yaml.ScalarNode && strings.Count(v.Value, "/") == 1:
				out = append(out, Wire{Kind: "ci_ref", Value: strings.ToLower(v.Value), Note: "repository: " + v.Value, Path: p, Line: v.Line})
			case ci && key == "project" && v.Kind == yaml.ScalarNode && strings.Contains(v.Value, "/"):
				out = append(out, Wire{Kind: "ci_ref", Value: strings.ToLower(strings.Trim(v.Value, "'\"")), Note: "project: " + v.Value, Path: p, Line: v.Line})
			}
		})
	}
	return out
}

func get(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func scalar(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	return n.Value
}

func seq(n *yaml.Node) []*yaml.Node {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	return n.Content
}

// walkYAML visits every mapping entry (key, value, the mapping it sits in).
func walkYAML(n *yaml.Node, f func(key string, v, parent *yaml.Node)) {
	if n == nil {
		return
	}
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			f(n.Content[i].Value, n.Content[i+1], n)
			walkYAML(n.Content[i+1], f)
		}
	case yaml.SequenceNode, yaml.DocumentNode:
		for _, c := range n.Content {
			walkYAML(c, f)
		}
	}
}

// splitTags reads a tags value: a list, or a block or comma-separated string.
func splitTags(v *yaml.Node) []*yaml.Node {
	if v.Kind == yaml.SequenceNode {
		return v.Content
	}
	if v.Kind != yaml.ScalarNode {
		return nil
	}
	var out []*yaml.Node
	for i, part := range strings.FieldsFunc(v.Value, func(r rune) bool { return r == '\n' || r == ',' }) {
		line := v.Line
		if v.Style == yaml.LiteralStyle || v.Style == yaml.FoldedStyle {
			line += i + 1
		}
		out = append(out, &yaml.Node{Kind: yaml.ScalarNode, Value: strings.TrimSpace(part), Line: line})
	}
	return out
}

// normImage reduces an image reference to registry/path without tag or digest; references built from
// variables other than the repository's own name are dropped.
func normImage(repo, ref string) string {
	ref = strings.Trim(strings.TrimSpace(ref), `"'`)
	ref = ghRepoVarRE.ReplaceAllString(ref, strings.ToLower(repo))
	if ref == "" || strings.ContainsAny(ref, "${}") || strings.EqualFold(ref, "scratch") {
		return ""
	}
	if i := strings.Index(ref, "@"); i >= 0 {
		ref = ref[:i]
	}
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		ref = ref[:i]
	}
	ref = strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(ref, "docker.io/"), "index.docker.io/"))
	if strings.HasPrefix(ref, "library/") {
		ref = strings.TrimPrefix(ref, "library/")
	}
	return ref
}

func isNumberish(h string) bool {
	return strings.Trim(h, "0123456789.") == ""
}

func clipNote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 160 {
		return s[:159] + "…"
	}
	return s
}

func dedupeWires(ws []Wire) []Wire {
	seen := map[[3]string]bool{}
	out := ws[:0]
	for _, w := range ws {
		k := [3]string{w.Kind, w.Value, w.Path}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, w)
	}
	return out
}

// InternalHost reports hosts that only resolve inside a deployment: a bare service name (Compose,
// Kubernetes in the same namespace) or a cluster or private DNS name.
func InternalHost(h string) bool {
	if !strings.Contains(h, ".") {
		return true
	}
	for _, s := range []string{".svc", ".svc.cluster.local", ".cluster.local", ".internal", ".local", ".consul", ".lan", ".intranet", ".private"} {
		if strings.HasSuffix(h, s) {
			return true
		}
	}
	return strings.Contains(h, ".svc.")
}

// WiringRepo is one tracked repository with its wiring facts.
type WiringRepo struct {
	ID, Name, Service string
	Wires             []Wire
}

// MatchWiring links repositories through their configuration and pipelines:
//   - api: one calls a host another serves (its Kubernetes Service or ingress host), or an internal
//     host named after it (http://billing:8080, billing.default.svc.cluster.local);
//   - image: one runs or builds on an image another publishes (or one named after it);
//   - pipeline: one's CI uses another's reusable workflow or action, checks it out, triggers it, or
//     includes its configuration.
func MatchWiring(repos []WiringRepo) []SystemLink {
	type ident struct{ hosts, names, images map[string]bool }
	ids := make([]ident, len(repos))
	byFull := map[string]int{}
	for i, r := range repos {
		id := ident{hosts: map[string]bool{}, names: map[string]bool{}, images: map[string]bool{}}
		full := strings.ToLower(r.Name)
		byFull[full] = i
		id.names[path.Base(full)] = true
		if r.Service != "" {
			id.names[strings.ToLower(r.Service)] = true
		}
		id.images[full] = true
		for _, w := range r.Wires {
			switch w.Kind {
			case "host":
				id.hosts[w.Value] = true
				if !strings.Contains(w.Value, ".") {
					id.names[w.Value] = true
				}
			case "image_pub":
				id.images[w.Value] = true
			}
		}
		ids[i] = id
	}
	var out []SystemLink
	link := func(from, to int, kind, via string, w Wire) {
		if from == to {
			return
		}
		out = append(out, SystemLink{FromRepo: repos[from].ID, FromName: repos[from].Name, ToRepo: repos[to].ID, ToName: repos[to].Name,
			Kind: kind, Via: via, Path: w.Path, Line: w.Line, N: 1})
	}
	for i, r := range repos {
		for _, w := range r.Wires {
			switch w.Kind {
			case "calls":
				first := w.Value
				if j := strings.IndexByte(first, '.'); j > 0 {
					first = first[:j]
				}
				for j := range repos {
					if ids[j].hosts[w.Value] || (InternalHost(w.Value) && ids[j].names[first]) {
						link(i, j, "api", w.Value, w)
					}
				}
			case "image_use":
				for j := range repos {
					if ids[j].images[w.Value] || strings.HasSuffix(w.Value, "/"+strings.ToLower(repos[j].Name)) {
						link(i, j, "image", w.Value, w)
					}
				}
			case "ci_ref":
				if j, ok := byFull[w.Value]; ok {
					link(i, j, "pipeline", w.Note, w)
				}
			}
		}
	}
	return out
}
