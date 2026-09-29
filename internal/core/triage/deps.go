package triage

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"path"
	"regexp"
	"strings"
)

// IsDependencyManifest reports whether a path is a dependency manifest handled by the semver rule.
func IsDependencyManifest(p string) bool { return dependencyManifest(p) }

func dependencyManifest(p string) bool {
	switch path.Base(p) {
	case "go.mod", "package.json", "pom.xml", "Cargo.toml", "requirements.txt", "pyproject.toml", "build.gradle", "build.gradle.kts":
		return true
	}
	return false
}

// ParseDeps returns dependency name → version for a manifest (keys may carry a section prefix such as
// "dependencies:react"). ok is false when the content cannot be parsed.
func ParseDeps(p string, content []byte) (map[string]string, bool) { return parseDeps(p, content) }

// parseDeps returns dependency name → version for a manifest. Unparseable content yields ok=false so
// the caller can fall back to PROCEED (a false ABORT is the expensive mistake).
func parseDeps(p string, content []byte) (map[string]string, bool) {
	switch path.Base(p) {
	case "go.mod":
		return parseGoMod(content), true
	case "package.json":
		return parsePackageJSON(content)
	case "pom.xml":
		return parsePom(content)
	case "Cargo.toml", "pyproject.toml":
		return parseTOMLDeps(content), true
	case "requirements.txt":
		return parseRequirements(content), true
	case "build.gradle", "build.gradle.kts":
		return parseGradle(content), true
	}
	return nil, false
}

var goModReq = regexp.MustCompile(`^\s*(?:require\s+)?([^\s()]+)\s+(v[^\s]+)`)
var goMajorSuffix = regexp.MustCompile(`/v(\d+)$`)

// parseGoMod keys modules without their /vN suffix and folds the suffix into the version, so
// "example.com/x/v2 v2.0.0" and "example.com/x v1.9.0" compare as a major bump of one dependency.
func parseGoMod(content []byte) map[string]string {
	out := map[string]string{}
	inBlock := false
	sc := bufio.NewScanner(strings.NewReader(string(content)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		switch {
		case strings.HasPrefix(line, "require ("):
			inBlock = true
			continue
		case inBlock && line == ")":
			inBlock = false
			continue
		case !inBlock && !strings.HasPrefix(line, "require "):
			continue
		}
		m := goModReq.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		mod, ver := m[1], m[2]
		if s := goMajorSuffix.FindStringSubmatch(mod); s != nil {
			mod = strings.TrimSuffix(mod, "/v"+s[1])
		}
		out[mod] = ver
	}
	return out
}

func parsePackageJSON(content []byte) (map[string]string, bool) {
	var pkg map[string]json.RawMessage
	if err := json.Unmarshal(content, &pkg); err != nil {
		return nil, false
	}
	out := map[string]string{}
	for _, section := range []string{"dependencies", "devDependencies", "peerDependencies", "optionalDependencies"} {
		var deps map[string]string
		if raw, ok := pkg[section]; ok && json.Unmarshal(raw, &deps) == nil {
			for k, v := range deps {
				out[section+":"+k] = v
			}
		}
	}
	return out, true
}

func parsePom(content []byte) (map[string]string, bool) {
	var pom struct {
		Properties struct {
			Items []struct {
				XMLName xml.Name
				Value   string `xml:",chardata"`
			} `xml:",any"`
		} `xml:"properties"`
		Deps []struct {
			GroupID    string `xml:"groupId"`
			ArtifactID string `xml:"artifactId"`
			Version    string `xml:"version"`
		} `xml:"dependencies>dependency"`
		Managed []struct {
			GroupID    string `xml:"groupId"`
			ArtifactID string `xml:"artifactId"`
			Version    string `xml:"version"`
		} `xml:"dependencyManagement>dependencies>dependency"`
		Parent struct {
			GroupID    string `xml:"groupId"`
			ArtifactID string `xml:"artifactId"`
			Version    string `xml:"version"`
		} `xml:"parent"`
	}
	if err := xml.Unmarshal(content, &pom); err != nil {
		return nil, false
	}
	props := map[string]string{}
	for _, it := range pom.Properties.Items {
		props[it.XMLName.Local] = strings.TrimSpace(it.Value)
	}
	resolve := func(v string) string {
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}") {
			if r, ok := props[v[2:len(v)-1]]; ok {
				return r
			}
		}
		return v
	}
	out := map[string]string{}
	for _, d := range append(pom.Deps, pom.Managed...) {
		out[d.GroupID+":"+d.ArtifactID] = resolve(d.Version)
	}
	if pom.Parent.ArtifactID != "" {
		out["parent:"+pom.Parent.GroupID+":"+pom.Parent.ArtifactID] = resolve(pom.Parent.Version)
	}
	return out, true
}

var (
	tomlSection = regexp.MustCompile(`^\[([^\]]+)\]$`)
	tomlKV      = regexp.MustCompile(`^([A-Za-z0-9_.\-"]+)\s*=\s*(.+)$`)
	tomlVersion = regexp.MustCompile(`version\s*=\s*"([^"]+)"`)
	quotedDep   = regexp.MustCompile(`"([A-Za-z0-9_.\-\[\]]+)\s*([<>=!~^]=?\s*[^",;]+)?`)
)

// parseTOMLDeps reads Cargo.toml [*dependencies] tables, Poetry [tool.poetry.*dependencies] tables, and
// PEP 621 `dependencies = [...]` arrays — a narrow reader, not a TOML parser (no TOML library in § 3).
func parseTOMLDeps(content []byte) map[string]string {
	out := map[string]string{}
	section := ""
	inArray := false
	sc := bufio.NewScanner(strings.NewReader(string(content)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if i := strings.Index(line, " #"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := tomlSection.FindStringSubmatch(line); m != nil {
			section, inArray = m[1], false
			continue
		}
		if inArray || strings.HasPrefix(line, "dependencies = [") || strings.HasPrefix(line, "dependencies=[") {
			inArray = !strings.Contains(line, "]") || strings.HasSuffix(line, "[")
			for _, m := range quotedDep.FindAllStringSubmatch(line, -1) {
				if m[1] != "dependencies" {
					out["pep621:"+m[1]] = strings.TrimSpace(strings.TrimLeft(m[2], "<>=!~^ "))
				}
			}
			if strings.Contains(line, "]") && !strings.HasSuffix(line, "[") {
				inArray = false
			}
			continue
		}
		if !strings.HasSuffix(section, "dependencies") {
			continue
		}
		m := tomlKV.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name, val := strings.Trim(m[1], `"`), strings.TrimSpace(m[2])
		if strings.HasPrefix(val, `"`) {
			val = strings.Trim(val, `"`)
		} else if v := tomlVersion.FindStringSubmatch(val); v != nil {
			val = v[1]
		}
		out[section+":"+name] = strings.TrimLeft(val, "^~=<> ")
	}
	return out
}

var reqLine = regexp.MustCompile(`^([A-Za-z0-9_.\-\[\]]+)\s*(?:==|>=|~=|<=|>|<|===)\s*([^\s;#,]+)`)

func parseRequirements(content []byte) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(string(content)))
	for sc.Scan() {
		if m := reqLine.FindStringSubmatch(strings.TrimSpace(sc.Text())); m != nil {
			out[strings.ToLower(m[1])] = m[2]
		}
	}
	return out
}

var gradleDep = regexp.MustCompile(`["']([\w.\-]+):([\w.\-]+):([\w.\-]+)["']`)

func parseGradle(content []byte) map[string]string {
	out := map[string]string{}
	for _, m := range gradleDep.FindAllStringSubmatch(string(content), -1) {
		out[m[1]+":"+m[2]] = m[3]
	}
	return out
}

var leadingNumber = regexp.MustCompile(`^\D*?(\d+)`)

// major returns the leading semver component of a version string ("^2.3.1" → "2", "v1.9.0" → "1").
func major(v string) string {
	m := leadingNumber.FindStringSubmatch(v)
	if m == nil {
		return ""
	}
	return m[1]
}

// depsDecision applies the plan § 8.1 rule: PROCEED only when an existing dependency's leading semver
// component changes; additions, removals, and minor/patch bumps are ABORT.
func depsDecision(p string, old, new []byte) (Decision, string) {
	o, ok1 := parseDeps(p, old)
	n, ok2 := parseDeps(p, new)
	if !ok1 || !ok2 {
		return Proceed, "dependency manifest could not be parsed; treating as structural"
	}
	var bumps []string
	for name, nv := range n {
		ov, existed := o[name]
		if !existed {
			continue
		}
		if om, nm := major(ov), major(nv); om != "" && nm != "" && om != nm {
			bumps = append(bumps, strings.TrimPrefix(name[strings.LastIndex(name, ":")+1:], "")+" "+ov+" → "+nv)
		}
	}
	if len(bumps) > 0 {
		return Proceed, "major dependency upgrade: " + joinCapped(bumps, 5)
	}
	return Abort, "dependency manifest changed without a major version bump"
}
