package api

import (
	"net/http"
	"strings"
	"sync"
)

// op describes one endpoint for the OpenAPI document. Keeping the table next to the handlers (and a test
// that every mounted route appears here) keeps the spec honest.
type op struct {
	Method, Path, Tag, Summary, Role string
	Body                             bool
	Stream                           bool
}

// Operations lists every /api/v1 and ingress endpoint (plan § 7).
var Operations = []op{
	{"POST", "/hooks/{kind}/{connector_id}", "Ingress", "Git host webhook (GitHub HMAC or GitLab token verified)", "", true, false},
	{"POST", "/hooks/firehose/{connector_id}", "Ingress", "Amazon Data Firehose HTTP endpoint (X-Amz-Firehose-Access-Key); answers after events are persisted", "", true, false},
	{"POST", "/hooks/{source}/{connector_id}", "Ingress", "Signal webhook: sentry, pagerduty (signed), opsgenie, datadog, grafana, alertmanager, aws, gcp, generic (connector secret)", "", true, false},
	{"GET", "/api/v1/auth/config", "Auth", "Available sign-in methods", "", false, false},
	{"GET", "/api/v1/auth/login", "Auth", "Start single sign-on (redirects to the identity provider)", "", false, false},
	{"GET", "/api/v1/auth/callback", "Auth", "Single sign-on callback", "", false, false},
	{"POST", "/api/v1/auth/local/login", "Auth", "Password login (when password sign-in is on)", "", true, false},
	{"GET", "/api/v1/auth/invite/{token}", "Auth", "Check an invite or password-reset link", "", false, false},
	{"POST", "/api/v1/auth/invite/{token}", "Auth", "Set a password from an invite link and sign in", "", true, false},
	{"POST", "/api/v1/auth/logout", "Auth", "End the session", "", false, false},
	{"GET", "/api/v1/auth/settings", "Auth", "Sign-in settings: passwords, single sign-on, callback URL", "owner", false, false},
	{"PUT", "/api/v1/auth/settings", "Auth", "Change sign-in settings (SSO is tested before it is saved)", "owner", true, false},
	{"POST", "/api/v1/auth/email/test", "Auth", "Send a test email to yourself (checks the SMTP settings)", "owner", false, false},
	{"GET", "/api/v1/me", "Auth", "Current user, role, repo access, CSRF token", "viewer", false, false},
	{"GET", "/api/v1/tokens", "Auth", "List your personal access tokens", "viewer", false, false},
	{"POST", "/api/v1/tokens", "Auth", "Create a personal access token (returned once)", "viewer", true, false},
	{"DELETE", "/api/v1/tokens/{id}", "Auth", "Revoke a token", "viewer", false, false},
	{"GET", "/api/v1/docs/mode", "Admin", "Docs generation mode (thorough, balanced, economy)", "admin", false, false},
	{"PUT", "/api/v1/docs/mode", "Admin", "Change the docs generation mode", "admin", true, false},
	{"GET", "/api/v1/users", "Users", "List users", "admin", false, false},
	{"POST", "/api/v1/users", "Users", "Add a user (optionally with an invite link, emailed when email is set up)", "admin", true, false},
	{"PATCH", "/api/v1/users/{id}", "Users", "Change name or role, or disable", "admin", true, false},
	{"DELETE", "/api/v1/users/{id}", "Users", "Remove a user", "admin", false, false},
	{"POST", "/api/v1/users/{id}/invite", "Users", "New invite or password-reset link (returned once; emailed when email is set up)", "admin", false, false},
	{"GET", "/api/v1/users/{id}/repo-access", "Users", "A user's direct repository grants", "admin", false, false},
	{"PUT", "/api/v1/users/{id}/repo-access", "Users", "Replace a user's repository grants", "admin", true, false},
	{"GET", "/api/v1/audit", "Users", "Audit log", "admin", false, false},
	{"POST", "/api/v1/ask", "Ask", "Ask a question (JSON, or text/event-stream with Accept)", "viewer", true, true},
	{"GET", "/api/v1/threads", "Ask", "Your Q&A threads", "viewer", false, false},
	{"GET", "/api/v1/threads/{id}", "Ask", "A thread with its messages", "viewer", false, false},
	{"DELETE", "/api/v1/threads/{id}", "Ask", "Delete a thread", "viewer", false, false},
	{"POST", "/api/v1/messages/{id}/feedback", "Ask", "Rate an answer", "viewer", true, false},
	{"GET", "/api/v1/docs/tree", "Docs", "Docs Tree roots or children", "viewer", false, false},
	{"GET", "/api/v1/docs/node/{id}", "Docs", "A doc file or section with Markdown", "viewer", false, false},
	{"GET", "/api/v1/palace/entities", "Palace", "Search knowledge-graph entities", "viewer", false, false},
	{"GET", "/api/v1/palace/entities/{id}/graph", "Palace", "Entity neighbourhood (depth 1-3)", "viewer", false, false},
	{"GET", "/api/v1/palace/overview", "Palace", "High-level map: the most connected repos, services, topics, datastores, endpoints (kinds=, limit=), edges lifted to repositories, counts per kind", "viewer", false, false},
	{"GET", "/api/v1/library/shelves", "Library", "Library shelves", "viewer", false, false},
	{"GET", "/api/v1/library/shelves/{slug}", "Library", "A shelf with its items", "viewer", false, false},
	{"POST", "/api/v1/library/shelves", "Library", "Create a shelf", "editor", true, false},
	{"GET", "/api/v1/library/uploads", "Library", "Uploaded documents", "viewer", false, false},
	{"GET", "/api/v1/security/modules", "Security", "kryptonite attack modules (which run on code alone, which are defaults)", "editor", false, false},
	{"GET", "/api/v1/security", "Security", "Latest scan of each readable repository", "editor", false, false},
	{"GET", "/api/v1/security/repos/{id}/scans", "Security", "A repository's scans", "editor", false, false},
	{"POST", "/api/v1/security/repos/{id}/plan", "Security", "What a scan would read and its estimate (no model call)", "editor", true, false},
	{"POST", "/api/v1/security/repos/{id}/scans", "Security", "Start a scan with the chosen modules", "editor", true, false},
	{"GET", "/api/v1/security/scans/{id}", "Security", "A scan with its findings", "editor", false, false},
	{"POST", "/api/v1/security/repos/{id}/fix", "Security", "Fix the selected findings in one pull request (never merged by the Hub)", "editor", true, false},
	{"GET", "/api/v1/library/uploads/{id}", "Library", "An uploaded document with its Markdown", "viewer", false, false},
	{"POST", "/api/v1/library/uploads", "Library", "Upload documents (multipart field files, optional collection): Markdown, text or HTML", "editor", false, false},
	{"DELETE", "/api/v1/library/uploads/{id}", "Library", "Remove an uploaded document", "editor", false, false},
	{"PATCH", "/api/v1/library/shelves/{id}", "Library", "Edit a shelf", "editor", true, false},
	{"DELETE", "/api/v1/library/shelves/{id}", "Library", "Delete a shelf", "editor", false, false},
	{"POST", "/api/v1/library/shelves/{id}/items", "Library", "Pin an item to a shelf", "editor", true, false},
	{"GET", "/api/v1/repos", "Repos", "Tracked repositories", "viewer", false, false},
	{"POST", "/api/v1/repos", "Repos", "Track a repository", "admin", true, false},
	{"PATCH", "/api/v1/repos/{id}", "Repos", "Edit repository docs settings", "admin", true, false},
	{"POST", "/api/v1/repos/{id}/dry-run", "Repos", "What the next push would document, at what cost (no side effects)", "editor", true, false},
	{"POST", "/api/v1/repos/{id}/import", "Repos", "Import existing Markdown", "admin", true, false},
	{"POST", "/api/v1/repos/{id}/generate-docs", "Repositories", "Document every file in the repository now (e.g. code synced before docgen had a route); 400 NO_DOCGEN_ROUTE without one", "admin", true, false},
	{"GET", "/api/v1/connectors", "Connectors", "Connectors (credentials never returned)", "admin", false, false},
	{"POST", "/api/v1/connectors", "Connectors", "Add a connector", "admin", true, false},
	{"PATCH", "/api/v1/connectors/{id}", "Connectors", "Edit a connector (secrets write-only); disabling or enabling a GitHub App connector suspends or resumes its installation", "admin", true, false},
	{"DELETE", "/api/v1/connectors/{id}", "Connectors", "Remove a connector; a GitHub App connector is also uninstalled from GitHub", "admin", false, false},
	{"POST", "/api/v1/connectors/{id}/test", "Connectors", "Read-only permission probe", "admin", false, false},
	{"POST", "/api/v1/connectors/{id}/sync", "Connectors", "Poll now", "admin", false, false},
	{"GET", "/api/v1/mcp/servers", "MCP connections", "MCP connections (secrets never returned) and the OAuth callback address", "admin", false, false},
	{"POST", "/api/v1/mcp/servers", "MCP connections", "Add an MCP connection; connects and lists its tools unless it needs sign-in", "admin", true, false},
	{"PATCH", "/api/v1/mcp/servers/{id}", "MCP connections", "Edit a connection: address, key (write-only), who may use it, which tools are on", "admin", true, false},
	{"DELETE", "/api/v1/mcp/servers/{id}", "MCP connections", "Remove a connection", "admin", false, false},
	{"POST", "/api/v1/mcp/servers/{id}/check", "MCP connections", "Connect again and refresh the tool list", "admin", false, false},
	{"POST", "/api/v1/mcp/servers/{id}/sign-in", "MCP connections", "Start the OAuth sign-in; returns the address to open", "admin", false, false},
	{"GET", "/api/v1/mcp/oauth/callback", "MCP connections", "Where the sign-in provider returns the browser", "admin", false, false},
	{"GET", "/api/v1/providers", "Providers", "LLM providers (keys never returned)", "admin", false, false},
	{"POST", "/api/v1/providers", "Providers", "Add an LLM provider with your key", "admin", true, false},
	{"PATCH", "/api/v1/providers/{id}", "Providers", "Edit a provider", "admin", true, false},
	{"DELETE", "/api/v1/providers/{id}", "Providers", "Remove a provider", "admin", false, false},
	{"POST", "/api/v1/providers/{id}/test", "Providers", "Test a provider with a model", "admin", true, false},
	{"GET", "/api/v1/routes", "Providers", "Feature routing", "admin", false, false},
	{"PUT", "/api/v1/routes/{feature}", "Providers", "Route a feature to a provider and model", "admin", true, false},
	{"GET", "/api/v1/spend/limits", "Spend", "Spend ceilings", "admin", false, false},
	{"PUT", "/api/v1/spend/limits", "Spend", "Replace spend ceilings", "admin", true, false},
	{"GET", "/api/v1/seal/key", "Security", "The Hub's public sealing key (X25519 + ML-KEM-768); clients seal secrets to it before sending", "viewer", false, false},
	{"POST", "/api/v1/seal/rotate", "Security", "Retire the sealing key and create a new one (values sealed to the old key open for 24 h)", "owner", true, false},
	{"POST", "/api/v1/github/connect", "Connectors", "Start one-click GitHub: returns the GitHub App manifest form (org=, base_url= for GitHub Enterprise Server)", "admin", true, false},
	{"GET", "/api/v1/github/connect/start", "Connectors", "Browser entry for one-click GitHub: a page that posts the app manifest to GitHub (org=, base_url=, name=)", "admin", false, false},
	{"GET", "/api/v1/github/connect/callback", "Connectors", "GitHub's redirect after the app is created: stores it as a connector, then opens the install page", "admin", false, false},
	{"GET", "/api/v1/github/connect/setup", "Connectors", "GitHub's redirect after installation: records the installation", "admin", false, false},
	{"POST", "/api/v1/github/connect/existing", "Connectors", "Connect a GitHub App you already have, from its App ID and a private key (sealed); finds its installation", "admin", true, false},
	{"POST", "/api/v1/github/connect/existing/{id}/refresh", "Connectors", "Look again for the App's installation after installing it on GitHub (account= when it has several)", "admin", true, false},
	{"GET", "/api/v1/connectors/{id}/available-repos", "Connectors", "Repositories the git host lets this connector read, with which are tracked", "admin", false, false},
	{"GET", "/api/v1/architecture", "Architecture", "Repositories you can see, with what their architecture contains", "viewer", false, false},
	{"GET", "/api/v1/architecture/repos/{id}", "Architecture", "A repository's generated architecture (layers, components, links, env, docs) and its authored diagrams", "viewer", false, false},
	{"POST", "/api/v1/architecture/repos/{id}/scan", "Architecture", "Re-read the repository's authored (archify) diagrams from its tracked branch", "editor", true, false},
	{"GET", "/api/v1/architecture/diagrams/{id}", "Architecture", "An authored diagram's HTML, served sandboxed for an iframe", "viewer", false, false},
	{"POST", "/api/v1/settings/apply", "Settings", "Apply a YAML/JSON settings document (providers, routes, connectors, repos, spend); dry_run previews. Secret references resolve per DTH_SETTINGS_SECRET_SOURCES", "admin", true, false},
	{"GET", "/api/v1/settings/export", "Settings", "Current settings as YAML, secrets as ${env:…} references", "admin", false, false},
	{"GET", "/api/v1/settings/sources", "Settings", "Secret reference sources this Hub resolves for pasted settings", "admin", false, false},
	{"POST", "/api/v1/reindex", "Spend", "Re-embed every chunk on the current embedding model", "owner", true, false},
	{"GET", "/api/v1/jobs", "Jobs", "Jobs", "viewer", false, false},
	{"GET", "/api/v1/jobs/{id}", "Jobs", "A job with its result", "viewer", false, false},
	{"POST", "/api/v1/jobs/{id}/retry", "Jobs", "Replay a job (override_ceiling for spend-blocked)", "admin", true, false},
	{"GET", "/api/v1/activity", "Analytics", "Activity feed", "viewer", false, false},
	{"GET", "/api/v1/analytics/usage", "Analytics", "Token and cost usage series", "viewer", false, false},
	{"GET", "/api/v1/analytics/savings", "Analytics", "Usage avoided (triage, cache, reuse)", "viewer", false, false},
	{"GET", "/api/v1/analytics/sift", "Analytics", "What Ask's source picker trimmed and saved (totals and per day)", "viewer", false, false},
	{"GET", "/api/v1/analytics/pipeline", "Analytics", "Job health and index freshness", "viewer", false, false},
	{"GET", "/api/v1/analytics/connectors", "Analytics", "Connector health", "admin", false, false},
	{"GET", "/api/v1/issues", "Inbox", "Issues by last seen (filters: status, source, service, env, severity, kind, q, since)", "viewer", false, false},
	{"GET", "/api/v1/issues/{id}", "Inbox", "Issue with its decode, last 50 events, hourly counts, rule, and similar issues", "viewer", false, false},
	{"PATCH", "/api/v1/issues/{id}", "Inbox", "Acknowledge, resolve, reopen, or assign", "editor", true, false},
	{"POST", "/api/v1/issues/{id}/decode", "Inbox", "Explain again (force skips reuse and the decision gate) → 202 {job_id}", "editor", true, false},
	{"POST", "/api/v1/issues/{id}/mark-known", "Inbox", "Create a known-issue rule from this issue and suppress it", "editor", true, false},
	{"GET", "/api/v1/known-issues", "Known issues", "Rules", "viewer", false, false},
	{"GET", "/api/v1/known-issues/{id}", "Known issues", "One rule", "viewer", false, false},
	{"POST", "/api/v1/known-issues", "Known issues", "Create a rule", "editor", true, false},
	{"PATCH", "/api/v1/known-issues/{id}", "Known issues", "Edit, enable, disable, or expire a rule", "editor", true, false},
	{"DELETE", "/api/v1/known-issues/{id}", "Known issues", "Delete a rule", "editor", false, false},
	{"POST", "/api/v1/known-issues/test", "Known issues", "Dry-run a rule against the last 7 days", "editor", true, false},
	{"POST", "/api/v1/known-issues/from-text", "Known issues", "Propose a rule from pasted text (nothing is enabled)", "editor", true, false},
	{"POST", "/api/v1/known-issues/from-link", "Known issues", "Propose a rule from a Jira issue or Confluence page URL, fetched through its connector (nothing is enabled)", "editor", true, false},
	{"GET", "/api/v1/known-issues/suggestions", "Known issues", "Auto-suggested rules awaiting review", "editor", false, false},
	{"POST", "/api/v1/known-issues/suggestions/{id}/{decision}", "Known issues", "Accept (enables the rule) or reject a suggestion", "editor", true, false},
	{"GET", "/api/v1/openapi.json", "Meta", "This document", "", false, false},
	{"GET", "/healthz", "Meta", "Liveness", "", false, false},
	{"GET", "/readyz", "Meta", "Readiness", "", false, false},
}

var (
	specOnce sync.Once
	spec     map[string]any
)

// OpenAPI builds the OpenAPI 3.1 document.
func OpenAPI() map[string]any {
	specOnce.Do(func() {
		errResp := map[string]any{"description": "Error", "content": map[string]any{"application/json": map[string]any{
			"schema": map[string]any{"$ref": "#/components/schemas/Error"}}}}
		paths := map[string]any{}
		for _, o := range Operations {
			item, _ := paths[o.Path].(map[string]any)
			if item == nil {
				item = map[string]any{}
				paths[o.Path] = item
			}
			ok := map[string]any{"description": "OK", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object"}}}}
			if o.Stream {
				ok["content"].(map[string]any)["text/event-stream"] = map[string]any{"schema": map[string]any{"type": "string",
					"description": "events: delta {text}, citation {...}, done {thread_id, message_id, answer, citations, usage}, error {error}"}}
			}
			operation := map[string]any{"tags": []string{o.Tag}, "summary": o.Summary,
				"operationId": strings.ToLower(o.Method) + strings.NewReplacer("/api/v1", "", "/", "_", "{", "", "}", "", "-", "_", ".", "_").Replace(o.Path),
				"responses":   map[string]any{"200": ok, "400": errResp, "401": errResp, "403": errResp, "404": errResp, "429": errResp}}
			if o.Role != "" {
				operation["security"] = []any{map[string]any{"session": []string{}}, map[string]any{"bearer": []string{}}}
				operation["x-min-role"] = o.Role
			}
			var params []any
			for _, seg := range strings.Split(o.Path, "/") {
				if strings.HasPrefix(seg, "{") {
					params = append(params, map[string]any{"name": strings.Trim(seg, "{}"), "in": "path", "required": true, "schema": map[string]any{"type": "string"}})
				}
			}
			if params != nil {
				operation["parameters"] = params
			}
			if o.Body {
				operation["requestBody"] = map[string]any{"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object"}}}}
			}
			item[strings.ToLower(o.Method)] = operation
		}
		spec = map[string]any{
			"openapi": "3.1.0",
			"info": map[string]any{"title": "DocTheRepo Hub API", "version": "1",
				"description": "Repos → docs → Q&A. Cookie sessions send X-CSRF-Token on mutations; API clients use Authorization: Bearer dth_pat_…. Lists use cursor pagination (?limit, ?cursor → next_cursor)."},
			"paths": paths,
			"components": map[string]any{
				"securitySchemes": map[string]any{
					"session": map[string]any{"type": "apiKey", "in": "cookie", "name": SessionCookie},
					"bearer":  map[string]any{"type": "http", "scheme": "bearer"},
				},
				"schemas": map[string]any{"Error": map[string]any{"type": "object", "required": []string{"error"}, "properties": map[string]any{
					"error": map[string]any{"type": "object", "required": []string{"code", "message", "correlation_id"}, "properties": map[string]any{
						"code": map[string]any{"type": "string"}, "message": map[string]any{"type": "string"},
						"details": map[string]any{"type": "object"}, "correlation_id": map[string]any{"type": "string"}}}}}},
			},
		}
	})
	return spec
}

func openapiHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	WriteJSON(w, http.StatusOK, OpenAPI())
}
