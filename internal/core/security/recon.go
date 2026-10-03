package security

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// Recon limits: files read per scan, files and bytes given to one module, bytes kept per file.
const (
	MaxFilesRead      = 80
	MaxFilesPerModule = 20
	MaxModuleBytes    = 120 << 10
	MaxFileBytes      = 40 << 10
)

var (
	codeExt = map[string]bool{".go": true, ".py": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true, ".java": true, ".kt": true,
		".rb": true, ".php": true, ".cs": true, ".rs": true, ".scala": true, ".swift": true, ".mjs": true, ".cjs": true, ".sql": true}
	skipRE = regexp.MustCompile(`(^|/)(node_modules|vendor|third_party|dist|build|out|target|\.git|coverage|testdata|__tests__|__mocks__|fixtures?|docs/generated)/|` +
		`(_test\.go|\.test\.[jt]sx?|\.spec\.[jt]sx?|_test\.py|test_[^/]*\.py|\.min\.js|\.lock|-lock\.json|\.pb\.go|_gen\.go|\.generated\.)$`)
	manifests = map[string]bool{"go.mod": true, "package.json": true, "requirements.txt": true, "pyproject.toml": true, "Pipfile": true,
		"pom.xml": true, "build.gradle": true, "build.gradle.kts": true, "Gemfile": true, "Cargo.toml": true, "composer.json": true,
		"requirements-dev.txt": true, "setup.py": true, "setup.cfg": true}
)

// moduleFocus says, per module, which paths matter and which words in a file make it relevant.
type moduleFocus struct {
	paths    *regexp.Regexp // a path match alone selects the file
	keywords *regexp.Regexp // content words that rank code files
	code     bool           // code files with keyword hits qualify
}

var focus = map[string]moduleFocus{
	"security-pentest": {
		paths:    regexp.MustCompile(`(?i)(auth|login|session|token|jwt|oauth|oidc|password|admin|middleware|handler|route|controller|api|upload|webhook|proxy)`),
		keywords: regexp.MustCompile(`(?i)\b(exec|system|popen|subprocess|eval|query|sql|innerhtml|dangerouslysetinnerhtml|jwt|bcrypt|password|token|authorization|cookie|redirect|http\.get|fetch\(|requests\.get|urlopen|deserializ|pickle|yaml\.load|md5|sha1)\b`),
		code:     true},
	"api-abuse-and-limits": {
		paths:    regexp.MustCompile(`(?i)(route|router|handler|controller|middleware|api|graphql|server|endpoint|rate)`),
		keywords: regexp.MustCompile(`(?i)\b(limit|offset|page|pagesize|per_page|ratelimit|rate_limit|throttle|timeout|maxbytes|readall|body|upload|batch|graphql)\b`),
		code:     true},
	"config-and-secrets": {
		paths:    regexp.MustCompile(`(?i)(^|/)(\.env[^/]*|dockerfile[^/]*|docker-compose[^/]*\.ya?ml|compose\.ya?ml|[^/]*\.tf|[^/]*\.tfvars|[^/]*\.properties|settings\.py|config[^/]*\.(ya?ml|json|toml|js|ts|go|py)|application[^/]*\.ya?ml|values[^/]*\.ya?ml|[^/]*\.ini)$`),
		keywords: regexp.MustCompile(`(?i)(getenv|process\.env|os\.environ|secret|password|passwd|api[_-]?key|private[_-]?key|access[_-]?key|token|debug\s*[:=]\s*true|cors|allow_origin|tls|insecure)`),
		code:     true},
	"supply-chain-deps": {
		paths: regexp.MustCompile(`(?i)(^|/)(\.github/workflows/[^/]+\.ya?ml|\.gitlab-ci\.yml|dockerfile[^/]*|go\.mod|package\.json|requirements[^/]*\.txt|pyproject\.toml|pipfile|pom\.xml|build\.gradle(\.kts)?|gemfile|cargo\.toml|composer\.json|setup\.py)$`)},
	"data-logic-integrity": {
		paths:    regexp.MustCompile(`(?i)(order|payment|billing|invoice|refund|ledger|account|wallet|inventory|cart|checkout|transaction|balance|price)`),
		keywords: regexp.MustCompile(`(?i)\b(transaction|tx\.|begin|commit|rollback|amount|price|balance|total|quantity|refund|float64|parsefloat|decimal|idempoten|unique|for update|lock|race|concurren)\b`),
		code:     true},
	"privacy-compliance": {
		paths:    regexp.MustCompile(`(?i)(user|account|profile|customer|log|audit|analytics|tracking|consent|gdpr|export|retention)`),
		keywords: regexp.MustCompile(`(?i)\b(email|phone|ssn|address|birth|dob|ip_?address|log\.|logger|console\.log|print\(|analytics|track|retention|delete|anonymi|consent|gdpr|pii)\b`),
		code:     true},
	"observability-operability": {
		paths:    regexp.MustCompile(`(?i)(log|metric|trace|telemetry|health|monitor|main\.|server\.)`),
		keywords: regexp.MustCompile(`(?i)\b(log|logger|metric|prometheus|otel|trace|span|health|readiness|liveness|panic|recover|error)\b`),
		code:     true},
	"failure-ux-degradation": {
		paths:    regexp.MustCompile(`(?i)(client|http|retry|timeout|fallback|error|errors|boundary|api)`),
		keywords: regexp.MustCompile(`(?i)\b(timeout|retry|backoff|circuit|fallback|catch|recover|errorboundary|deadline|context\.with)\b`),
		code:     true},
	"cost-efficiency": {
		paths:    regexp.MustCompile(`(?i)(query|repo|store|dao|cache|worker|job|cron|batch|llm|openai|anthropic)`),
		keywords: regexp.MustCompile(`(?i)\b(select \*|for .* range|n\+1|cache|ttl|batch|loop|query|llm|max_tokens|completion|embedding)\b`),
		code:     true},
	"accessibility-i18n": {
		paths:    regexp.MustCompile(`(?i)\.(tsx|jsx|vue|svelte|html)$`),
		keywords: regexp.MustCompile(`(?i)(<img|<button|onclick|aria-|alt=|<input|<label|tabindex|i18n|t\(|locale)`),
		code:     true},
}

// Candidate is a file a module may read.
type Candidate struct {
	Path  string
	Score int
}

// Relevant reports whether a path can matter to any module (code, config, manifests), skipping vendored,
// generated and test files.
func Relevant(p string) bool {
	if skipRE.MatchString(p) {
		return false
	}
	if codeExt[strings.ToLower(path.Ext(p))] || manifests[path.Base(p)] {
		return true
	}
	for _, f := range focus {
		if f.paths != nil && f.paths.MatchString(p) && !codeExt[strings.ToLower(path.Ext(p))] {
			return true
		}
	}
	return false
}

// PathCandidates ranks a repository's files by how likely each module needs them, before any file is read.
// The result is at most MaxFilesRead paths.
func PathCandidates(tree []string, modules []string) []string {
	score := map[string]int{}
	for _, p := range tree {
		if !Relevant(p) {
			continue
		}
		for _, m := range modules {
			f, ok := focus[m]
			if !ok {
				continue
			}
			isCode := codeExt[strings.ToLower(path.Ext(p))]
			switch {
			case f.paths != nil && f.paths.MatchString(p):
				score[p] += 3
			case f.code && isCode:
				score[p]++ // may still qualify on content
			}
		}
	}
	out := make([]string, 0, len(score))
	for p, s := range score {
		if s > 0 {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if score[out[i]] != score[out[j]] {
			return score[out[i]] > score[out[j]]
		}
		return out[i] < out[j]
	})
	if len(out) > MaxFilesRead {
		out = out[:MaxFilesRead]
	}
	return out
}

// ModuleFiles picks, from files already read, the ones one module attacks: path matches first, then code
// files by keyword hits, within MaxFilesPerModule and MaxModuleBytes.
func ModuleFiles(module string, files map[string]string) []string {
	f, ok := focus[module]
	if !ok {
		return nil
	}
	var cands []Candidate
	for p, body := range files {
		s := 0
		if f.paths != nil && f.paths.MatchString(p) {
			s += 10
		}
		if f.keywords != nil && f.code && codeExt[strings.ToLower(path.Ext(p))] {
			s += min(len(f.keywords.FindAllStringIndex(body, 50)), 20)
		}
		if s > 0 {
			cands = append(cands, Candidate{Path: p, Score: s})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].Score != cands[j].Score {
			return cands[i].Score > cands[j].Score
		}
		return cands[i].Path < cands[j].Path
	})
	var out []string
	total := 0
	for _, c := range cands {
		n := min(len(files[c.Path]), MaxFileBytes)
		if len(out) == MaxFilesPerModule || total+n > MaxModuleBytes {
			break
		}
		out = append(out, c.Path)
		total += n
	}
	sort.Strings(out)
	return out
}
