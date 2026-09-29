package palace

import (
	"bufio"
	"path"
	"regexp"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/core/triage"
)

// ExtractManifest derives depends_on edges from a dependency manifest (go.mod, package.json, pom.xml,
// Cargo.toml, requirements.txt, pyproject.toml, build.gradle).
func ExtractManifest(repo, filePath, commit string, content []byte) Graph {
	var g Graph
	deps, ok := triage.ParseDeps(filePath, content)
	if !ok {
		return g
	}
	r := g.AddEntity(Entity{Ref: RepoRef(repo), Name: repo, Repo: repo})
	eco := ecosystem(filePath)
	for key, ver := range deps {
		name := key
		if i := strings.LastIndex(key, ":"); i >= 0 && eco != "maven" {
			name = key[i+1:] // drop the section prefix ("dependencies:react" → "react")
		} else if eco == "maven" {
			name = strings.TrimPrefix(key, "parent:")
		}
		if name == "" || name == "python" {
			continue
		}
		d := g.AddEntity(Entity{Ref: Ref{KindDependency, eco + ":" + name}, Name: name, Attrs: map[string]string{"ecosystem": eco}})
		g.AddEdge(r, EdgeDependsOn, d, Evidence{Path: filePath, Commit: commit})
		_ = ver
	}
	return g
}

func ecosystem(p string) string {
	switch path.Base(p) {
	case "go.mod":
		return "go"
	case "package.json":
		return "npm"
	case "pom.xml", "build.gradle", "build.gradle.kts":
		return "maven"
	case "Cargo.toml":
		return "cargo"
	default:
		return "pypi"
	}
}

// IsCodeowners reports whether a path is a CODEOWNERS file.
func IsCodeowners(p string) bool {
	switch p {
	case "CODEOWNERS", ".github/CODEOWNERS", "docs/CODEOWNERS", ".gitlab/CODEOWNERS":
		return true
	}
	return false
}

// ExtractCodeowners maps CODEOWNERS rules to owned_by edges: "*" rules own the repo, directory rules own
// that module. Owners starting with "@org/team" become teams, other "@user"s and emails become people.
func ExtractCodeowners(repo, filePath, commit string, content []byte) Graph {
	var g Graph
	g.AddEntity(Entity{Ref: RepoRef(repo), Name: repo, Repo: repo})
	sc := bufio.NewScanner(strings.NewReader(string(content)))
	line := 0
	for sc.Scan() {
		line++
		t := strings.TrimSpace(sc.Text())
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "[") {
			continue
		}
		fields := strings.Fields(t)
		if len(fields) < 2 {
			continue
		}
		pattern, owners := fields[0], fields[1:]
		var subject Ref
		switch {
		case pattern == "*" || pattern == "/*" || pattern == "/":
			subject = RepoRef(repo)
		default:
			dir := strings.Trim(strings.TrimSuffix(strings.TrimSuffix(pattern, "**"), "*"), "/")
			if dir == "" || strings.ContainsAny(dir, "*?") {
				continue // file-glob rules do not map onto modules
			}
			subject = g.AddEntity(Entity{Ref: ModuleRef(repo, dir), Name: dir, Repo: repo})
		}
		for _, o := range owners {
			if strings.HasPrefix(o, "#") {
				break
			}
			var owner Ref
			switch {
			case strings.HasPrefix(o, "@") && strings.Contains(o, "/"):
				owner = g.AddEntity(Entity{Ref: Ref{KindTeam, o}, Name: o})
			default:
				owner = g.AddEntity(Entity{Ref: Ref{KindPerson, o}, Name: o})
			}
			g.AddEdge(subject, EdgeOwnedBy, owner, Evidence{Path: filePath, Line: line, Commit: commit})
		}
	}
	return g
}

var (
	k8sKind      = regexp.MustCompile(`(?m)^kind:\s*(Deployment|StatefulSet|DaemonSet|CronJob|Job|Service|Rollout)\s*$`)
	yamlMetaName = regexp.MustCompile(`(?m)^metadata:\s*\n(?:[ \t]+.*\n)*?[ \t]+name:\s*["']?([A-Za-z0-9.\-_{}]+)["']?`)
	chartName    = regexp.MustCompile(`(?m)^name:\s*["']?([A-Za-z0-9.\-_]+)["']?\s*$`)
	slsService   = regexp.MustCompile(`(?m)^service:\s*["']?([A-Za-z0-9.\-_]+)["']?\s*$`)
	composeSvc   = regexp.MustCompile(`(?m)^  ([A-Za-z0-9.\-_]+):\s*$`)
	dockerLabel  = regexp.MustCompile(`(?mi)^LABEL\s+.*(?:service|app|org\.opencontainers\.image\.title)=["']?([A-Za-z0-9.\-_]+)`)
)

// IsDeploymentFile reports files that name deployed services.
func IsDeploymentFile(p string) bool {
	b := path.Base(p)
	switch {
	case b == "Chart.yaml", b == "serverless.yml", b == "serverless.yaml", b == "docker-compose.yml",
		b == "docker-compose.yaml", b == "compose.yaml", b == "compose.yml", b == "Dockerfile", strings.HasPrefix(b, "Dockerfile."):
		return true
	case strings.HasSuffix(b, ".yaml") || strings.HasSuffix(b, ".yml"):
		d := "/" + path.Dir(p) + "/"
		return strings.Contains(d, "/k8s/") || strings.Contains(d, "/kubernetes/") || strings.Contains(d, "/manifests/") ||
			strings.Contains(d, "/deploy/") || strings.Contains(d, "/helm/")
	}
	return false
}

// ExtractDeployments finds service names in deployment descriptors and links repo deployed_as service.
// Repos with an explicit service name configured (repos.service_name) are linked by the pipeline too.
func ExtractDeployments(repo, filePath, commit string, content []byte) Graph {
	var g Graph
	r := g.AddEntity(Entity{Ref: RepoRef(repo), Name: repo, Repo: repo})
	s := string(content)
	var names []string
	b := path.Base(filePath)
	switch {
	case b == "Chart.yaml":
		if m := chartName.FindStringSubmatch(s); m != nil {
			names = append(names, m[1])
		}
	case strings.HasPrefix(b, "serverless."):
		if m := slsService.FindStringSubmatch(s); m != nil {
			names = append(names, m[1])
		}
	case strings.HasPrefix(b, "docker-compose") || strings.HasPrefix(b, "compose."):
		if i := strings.Index(s, "\nservices:"); i >= 0 || strings.HasPrefix(s, "services:") {
			body := s[max(i, 0):]
			if j := regexp.MustCompile(`\n[A-Za-z]`).FindStringIndex(body[1:]); j != nil {
				body = body[:j[0]+1]
			}
			for _, m := range composeSvc.FindAllStringSubmatch(body, -1) {
				names = append(names, m[1])
			}
		}
	case b == "Dockerfile" || strings.HasPrefix(b, "Dockerfile."):
		if m := dockerLabel.FindStringSubmatch(s); m != nil {
			names = append(names, m[1])
		}
	default:
		for _, doc := range strings.Split(s, "\n---") {
			if k8sKind.MatchString(doc) && !strings.Contains(doc, "kind: Service\n") {
				if m := yamlMetaName.FindStringSubmatch(doc); m != nil && !strings.Contains(m[1], "{{") {
					names = append(names, m[1])
				}
			}
		}
	}
	for _, n := range names {
		sr := g.AddEntity(Entity{Ref: ServiceRef(n), Name: n})
		g.AddEdge(r, EdgeDeployedAs, sr, Evidence{Path: filePath, Commit: commit})
	}
	return g
}
