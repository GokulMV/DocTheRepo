// Package security runs kryptonite-style security scans over a repository's code (github.com/levitasOrg/
// kryptonite, MIT, vendored in ./kryptonite). The Hub never attacks a running system: kryptonite's rule is
// to fall back to static analysis when no local test instance exists, so every phase here reads code.
//
//	kryptonite phase              In the Hub
//	1 Recon & baseline            files per module from the tracked branch (no model call)
//	2 Scenario design (gate)      the person picks modules and sees the estimate before starting
//	3 Attack loop                 one attacker call per module over its files (the security route)
//	4 Verify findings             one verifier call per module: confirmed / plausible / rejected, re-ranked
//	5 Design fixes                only for findings someone selected, when they click Fix
//	6 Approval gate               that click; nothing is written before it
//	7 Apply & re-verify           a pull request (never merged by the Hub); the repository's CI runs tests
//	8 Readiness report            GO / NO-GO per scan from open P0/P1 findings
package security

import (
	"embed"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed kryptonite/modules/*.md kryptonite/references/*.md
var vendored embed.FS

// Module is one kryptonite attack dimension.
type Module struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Static reports whether the module can run on code alone; Reason says why not otherwise.
	Static bool   `json:"static"`
	Reason string `json:"reason,omitempty"`
	// Default modules are selected when a person starts a scan without choosing.
	Default  bool   `json:"default"`
	Playbook string `json:"-"`
}

// needsLiveApp are modules whose probes only mean something against a running instance.
var needsLiveApp = map[string]string{
	"load-chaos-resilience": "it measures behaviour under load and failure injection, which needs a running test instance",
}

// defaults are the modules a scan uses unless the person picks others: the ones code review finds well.
var defaults = map[string]bool{"security-pentest": true, "config-and-secrets": true, "supply-chain-deps": true,
	"api-abuse-and-limits": true, "data-logic-integrity": true, "privacy-compliance": true}

// Modules returns every vendored module, defaults first.
func Modules() []Module {
	entries, _ := fs.ReadDir(vendored, "kryptonite/modules")
	var out []Module
	for _, e := range entries {
		b, err := vendored.ReadFile(path.Join("kryptonite/modules", e.Name()))
		if err != nil {
			continue
		}
		m := parseModule(string(b))
		if m.Name == "" {
			m.Name = strings.TrimSuffix(e.Name(), ".md")
		}
		m.Reason = needsLiveApp[m.Name]
		m.Static = m.Reason == ""
		m.Default = defaults[m.Name]
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Default != out[j].Default {
			return out[i].Default
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// ModuleByName finds a module.
func ModuleByName(name string) (Module, bool) {
	for _, m := range Modules() {
		if m.Name == name {
			return m, true
		}
	}
	return Module{}, false
}

// parseModule reads a module's front matter (name, description) and keeps the whole text as the playbook.
func parseModule(s string) Module {
	m := Module{Playbook: s}
	if !strings.HasPrefix(s, "---\n") {
		return m
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return m
	}
	for _, line := range strings.Split(s[4:4+end], "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"`)
		switch strings.TrimSpace(k) {
		case "name":
			m.Name = v
		case "description":
			m.Description = v
		}
	}
	return m
}

// Reference returns a vendored reference document (severity, finding-schema, agent-prompts).
func Reference(name string) string {
	b, _ := vendored.ReadFile("kryptonite/references/" + name + ".md")
	return string(b)
}

// Severities and exploitabilities, in kryptonite's order.
var (
	Severities       = []string{"critical", "high", "medium", "low", "info"}
	Exploitabilities = []string{"trivial", "easy", "moderate", "hard"}
)

// matrix is kryptonite's severity × exploitability → priority table (references/severity.md).
var matrix = map[string][4]string{
	"critical": {"P0", "P0", "P0", "P1"},
	"high":     {"P0", "P0", "P1", "P1"},
	"medium":   {"P0", "P1", "P1", "P2"},
	"low":      {"P1", "P1", "P2", "P2"},
	"info":     {"P1", "P2", "P2", "P3"},
}

// Priority maps severity and exploitability to P0..P3; unknown values rank as the least urgent.
func Priority(severity, exploitability string) string {
	row, ok := matrix[severity]
	if !ok {
		row = matrix["info"]
	}
	for i, e := range Exploitabilities {
		if e == exploitability {
			return row[i]
		}
	}
	return row[3]
}

// Verdict is kryptonite's readiness call: NO-GO while a confirmed or plausible P0/P1 finding is open.
func Verdict(findings []Finding) string {
	for _, f := range findings {
		if (f.Status == StatusConfirmed || f.Status == StatusPlausible) && (f.Priority == "P0" || f.Priority == "P1") {
			return "NO-GO"
		}
	}
	return "GO"
}
