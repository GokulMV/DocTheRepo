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
	{"GET", "/api/v1/auth/config", "Auth", "Available sign-in methods", "", false, false},
	{"GET", "/api/v1/auth/login", "Auth", "Start single sign-on (redirects to the identity provider)", "", false, false},
	{"GET", "/api/v1/auth/callback", "Auth", "Single sign-on callback", "", false, false},
	{"POST", "/api/v1/auth/local/login", "Auth", "Local-mode password login", "", true, false},
	{"POST", "/api/v1/auth/logout", "Auth", "End the session", "", false, false},
	{"GET", "/api/v1/me", "Auth", "Current user, role, repo access, CSRF token", "viewer", false, false},
	{"GET", "/api/v1/tokens", "Auth", "List your personal access tokens", "viewer", false, false},
	{"POST", "/api/v1/tokens", "Auth", "Create a personal access token (returned once)", "viewer", true, false},
	{"DELETE", "/api/v1/tokens/{id}", "Auth", "Revoke a token", "viewer", false, false},
	{"GET", "/api/v1/users", "Users", "List users", "admin", false, false},
	{"PATCH", "/api/v1/users/{id}", "Users", "Change role or disable", "admin", true, false},
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
	{"GET", "/api/v1/library/shelves", "Library", "Library shelves", "viewer", false, false},
	{"GET", "/api/v1/library/shelves/{slug}", "Library", "A shelf with its items", "viewer", false, false},
	{"POST", "/api/v1/library/shelves", "Library", "Create a shelf", "editor", true, false},
	{"PATCH", "/api/v1/library/shelves/{id}", "Library", "Edit a shelf", "editor", true, false},
	{"DELETE", "/api/v1/library/shelves/{id}", "Library", "Delete a shelf", "editor", false, false},
	{"POST", "/api/v1/library/shelves/{id}/items", "Library", "Pin an item to a shelf", "editor", true, false},
	{"GET", "/api/v1/repos", "Repos", "Tracked repositories", "viewer", false, false},
	{"POST", "/api/v1/repos", "Repos", "Track a repository", "admin", true, false},
	{"PATCH", "/api/v1/repos/{id}", "Repos", "Edit repository docs settings", "admin", true, false},
	{"POST", "/api/v1/repos/{id}/dry-run", "Repos", "What the next push would document, at what cost (no side effects)", "editor", true, false},
	{"POST", "/api/v1/repos/{id}/import", "Repos", "Import existing Markdown", "admin", true, false},
	{"GET", "/api/v1/connectors", "Connectors", "Connectors (credentials never returned)", "admin", false, false},
	{"POST", "/api/v1/connectors", "Connectors", "Add a connector", "admin", true, false},
	{"PATCH", "/api/v1/connectors/{id}", "Connectors", "Edit a connector (secrets write-only)", "admin", true, false},
	{"DELETE", "/api/v1/connectors/{id}", "Connectors", "Remove a connector", "admin", false, false},
	{"POST", "/api/v1/connectors/{id}/test", "Connectors", "Read-only permission probe", "admin", false, false},
	{"POST", "/api/v1/connectors/{id}/sync", "Connectors", "Poll now", "admin", false, false},
	{"GET", "/api/v1/providers", "Providers", "LLM providers (keys never returned)", "admin", false, false},
	{"POST", "/api/v1/providers", "Providers", "Add an LLM provider with your key", "admin", true, false},
	{"PATCH", "/api/v1/providers/{id}", "Providers", "Edit a provider", "admin", true, false},
	{"DELETE", "/api/v1/providers/{id}", "Providers", "Remove a provider", "admin", false, false},
	{"POST", "/api/v1/providers/{id}/test", "Providers", "Test a provider with a model", "admin", true, false},
	{"GET", "/api/v1/routes", "Providers", "Feature routing", "admin", false, false},
	{"PUT", "/api/v1/routes/{feature}", "Providers", "Route a feature to a provider and model", "admin", true, false},
	{"GET", "/api/v1/spend/limits", "Spend", "Spend ceilings", "admin", false, false},
	{"PUT", "/api/v1/spend/limits", "Spend", "Replace spend ceilings", "admin", true, false},
	{"POST", "/api/v1/reindex", "Spend", "Re-embed every chunk on the current embedding model", "owner", true, false},
	{"GET", "/api/v1/jobs", "Jobs", "Jobs", "viewer", false, false},
	{"GET", "/api/v1/jobs/{id}", "Jobs", "A job with its result", "viewer", false, false},
	{"POST", "/api/v1/jobs/{id}/retry", "Jobs", "Replay a job (override_ceiling for spend-blocked)", "admin", true, false},
	{"GET", "/api/v1/activity", "Analytics", "Activity feed", "viewer", false, false},
	{"GET", "/api/v1/analytics/usage", "Analytics", "Token and cost usage series", "viewer", false, false},
	{"GET", "/api/v1/analytics/savings", "Analytics", "Usage avoided (triage, cache, reuse)", "viewer", false, false},
	{"GET", "/api/v1/analytics/pipeline", "Analytics", "Job health and index freshness", "viewer", false, false},
	{"GET", "/api/v1/analytics/connectors", "Analytics", "Connector health", "admin", false, false},
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
