// Package signals normalizes signal events (plan § 8.8–8.9): secret scrubbing (always on), PII redaction
// (per LLM provider), severity and service resolution, message normalization, and fingerprinting. It is
// pure: no I/O.
package signals

import (
	"regexp"
	"strings"
)

// secretRule replaces matches of re with repl (which may reference groups).
type secretRule struct {
	name string
	re   *regexp.Regexp
	repl string
}

// secretRules are applied in order. All are RE2 (linear time), so hostile input cannot cause catastrophic
// backtracking. Placeholders are typed so a reader (or model) still knows what was there.
var secretRules = []secretRule{
	{"private_key", regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`), "<PRIVATE_KEY>"},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.eyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]+`), "<JWT>"},
	{"aws_access_key", regexp.MustCompile(`\b(?:AKIA|ASIA|AGPA|AIDA|AROA|ANPA)[0-9A-Z]{16}\b`), "<AWS_KEY>"},
	{"aws_secret_key", regexp.MustCompile(`(?i)(aws_secret_access_key|secret_access_key|aws_secret)(["']?\s*[:=]\s*["']?)[A-Za-z0-9/+=]{40}`), "${1}${2}<AWS_SECRET>"},
	{"gcp_api_key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`), "<GCP_API_KEY>"},
	{"github_token", regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{40,})\b`), "<GITHUB_TOKEN>"},
	{"slack_token", regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9-]{10,}`), "<SLACK_TOKEN>"},
	{"stripe_key", regexp.MustCompile(`\b(?:sk|rk)_(?:live|test)_[A-Za-z0-9]{16,}\b`), "<STRIPE_KEY>"},
	// LLM provider keys (OpenAI sk-…/sk-proj-…, Anthropic sk-ant-…): the keys this Hub's users bring.
	{"llm_api_key", regexp.MustCompile(`\bsk-[A-Za-z0-9][A-Za-z0-9_-]{19,}`), "<API_KEY>"},
	{"bearer", regexp.MustCompile(`(?i)\b(bearer|token)\s+[A-Za-z0-9._~+/=-]{16,}`), "${1} <TOKEN>"},
	{"basic_auth", regexp.MustCompile(`(?i)\b(basic)\s+[A-Za-z0-9+/]{12,}={0,2}`), "${1} <CREDENTIALS>"},
	// scheme://user:password@host — keep the user and host, drop the password.
	{"url_password", regexp.MustCompile(`\b([a-zA-Z][a-zA-Z0-9+.-]*://[^:/\s@]+):([^@\s/]+)@`), "${1}:<PASSWORD>@"},
	// key=value connection strings and config dumps (Password=…; pwd=…).
	{"conn_password", regexp.MustCompile(`(?i)\b(password|passwd|pwd)(["']?\s*[=:]\s*)("[^"]*"|'[^']*'|[^;\s,&"'}]+)`), "${1}${2}<PASSWORD>"},
	// Generic secret-looking assignments: api_key=…, client_secret: "…", access_token=….
	{"assignment", regexp.MustCompile(`(?i)\b(api[_-]?key|apikey|secret|client[_-]?secret|access[_-]?token|refresh[_-]?token|auth[_-]?token|token|private[_-]?key)(["']?\s*[:=]\s*["']?)([A-Za-z0-9._~+/-]{8,})`), "${1}${2}<SECRET>"},
}

// Scrub removes credentials from s. It is always applied, to every stored sample and every model input,
// and cannot be switched off: a credential in a prompt, a stored event, or an answer is an incident
// whoever runs the model.
func Scrub(s string) string {
	if s == "" {
		return s
	}
	for _, r := range secretRules {
		if r.name == "private_key" && !strings.Contains(s, "PRIVATE KEY") {
			continue
		}
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}
