package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Caller sends one Hub API request (path relative to /api/v1) and decodes the JSON response into out.
type Caller func(ctx context.Context, method, path string, body, out any) error

// MaxText caps a tool result so one call cannot flood the agent's context.
const MaxText = 60_000

// Instructions tell the agent when to use the Hub.
const Instructions = `DocTheRepo Hub knows this organisation's code, generated docs, knowledge graph (services, endpoints,
topics, owners), Library shelves (runbooks, decisions, APIs), and production issues with plain-English decodes.
Use "ask" for questions about how systems work (answers cite sources); "search_entities" and "entity_graph" to
see what calls, publishes to, or owns what; "list_issues" and "get_issue" to see what is failing in production
and why; "list_known_issues" to check whether an error is already understood.`

func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

func strs(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}

func integer(desc string, min, max int) map[string]any {
	return map[string]any{"type": "integer", "description": desc, "minimum": min, "maximum": max}
}

var readOnly = map[string]any{"readOnlyHint": true, "openWorldHint": false}

// HubTools returns the Hub's tools, all read-only.
func HubTools(call Caller) []Tool {
	return []Tool{
		{Name: "ask", Title: "Ask the Hub", Annotations: readOnly,
			Description: "Answer a question about the organisation's code, docs, Confluence/Jira, and decoded issues, with numbered citations. Optionally limit to repositories (owner/name) or sources.",
			InputSchema: obj(map[string]any{
				"question": str("The question, in plain language."),
				"repos":    strs("Limit to these repositories (owner/name)."),
				"include":  strs("Sources to search: code, docs, confluence, issues (default: all)."),
			}, "question"),
			Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var in struct {
					Question string   `json:"question"`
					Repos    []string `json:"repos"`
					Include  []string `json:"include"`
				}
				if err := json.Unmarshal(raw, &in); err != nil || strings.TrimSpace(in.Question) == "" {
					return "", fmt.Errorf("question is required")
				}
				ids, err := repoIDs(ctx, call, in.Repos)
				if err != nil {
					return "", err
				}
				var out struct {
					Answer    string `json:"answer"`
					Citations []struct {
						N     int    `json:"n"`
						Type  string `json:"type"`
						Title string `json:"title"`
						URL   string `json:"url"`
						Repo  string `json:"repo"`
						Path  string `json:"path"`
						Lines string `json:"lines"`
					} `json:"citations"`
				}
				body := map[string]any{"question": in.Question, "scope": map[string]any{"repo_ids": ids, "include": in.Include}}
				if err := call(ctx, "POST", "/ask", body, &out); err != nil {
					return "", err
				}
				var b strings.Builder
				b.WriteString(out.Answer)
				if len(out.Citations) > 0 {
					b.WriteString("\n\nSources:\n")
					for _, c := range out.Citations {
						loc := strings.Trim(strings.Join([]string{c.Repo, c.Path}, " "), " ")
						if c.Lines != "" {
							loc += ":" + c.Lines
						}
						fmt.Fprintf(&b, "[%d] %s (%s) %s %s\n", c.N, c.Title, c.Type, loc, c.URL)
					}
				}
				return clip(b.String()), nil
			}},
		{Name: "search_entities", Title: "Search the knowledge graph", Annotations: readOnly,
			Description: "Find Palace entities (services, repos, endpoints, env vars, topics, datastores, dependencies, Confluence pages, owners) by name.",
			InputSchema: obj(map[string]any{
				"q":     str("Name or part of a name."),
				"kind":  str("Entity kind filter, e.g. service, endpoint, queue_topic, env_var, datastore, dependency, repo."),
				"limit": integer("Maximum results (default 20).", 1, 100),
			}, "q"),
			Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var in struct {
					Q, Kind string
					Limit   int
				}
				_ = json.Unmarshal(raw, &in)
				q := url.Values{"q": {in.Q}, "limit": {strconv.Itoa(orInt(in.Limit, 20))}}
				if in.Kind != "" {
					q.Set("kind", in.Kind)
				}
				return getJSON(ctx, call, "/palace/entities?"+q.Encode())
			}},
		{Name: "entity_graph", Title: "Entity neighbourhood", Annotations: readOnly,
			Description: "Show what an entity is connected to (calls, exposes, publishes, subscribes, reads_env, depends_on, owned_by, runbook_for, ...). Use an id from search_entities.",
			InputSchema: obj(map[string]any{
				"id":         str("Entity id."),
				"depth":      integer("Hops (1-3, default 1).", 1, 3),
				"edge_kinds": strs("Only these edge kinds."),
			}, "id"),
			Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var in struct {
					ID        string   `json:"id"`
					Depth     int      `json:"depth"`
					EdgeKinds []string `json:"edge_kinds"`
				}
				if err := json.Unmarshal(raw, &in); err != nil || in.ID == "" {
					return "", fmt.Errorf("id is required")
				}
				q := url.Values{"depth": {strconv.Itoa(orInt(in.Depth, 1))}}
				if len(in.EdgeKinds) > 0 {
					q.Set("edge_kinds", strings.Join(in.EdgeKinds, ","))
				}
				return getJSON(ctx, call, "/palace/entities/"+url.PathEscape(in.ID)+"/graph?"+q.Encode())
			}},
		{Name: "list_issues", Title: "Production issues", Annotations: readOnly,
			Description: "List Inbox issues (grouped production errors, alerts, event-bus problems), newest activity first.",
			InputSchema: obj(map[string]any{
				"status":   str("new, decoded, suppressed, acknowledged, resolved, or regressed."),
				"service":  str("Service name."),
				"env":      str("Environment."),
				"severity": str("info, warning, error, or critical."),
				"q":        str("Text search over titles and messages."),
				"limit":    integer("Maximum results (default 20).", 1, 100),
			}),
			Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var in struct {
					Status, Service, Env, Severity, Q string
					Limit                             int
				}
				_ = json.Unmarshal(raw, &in)
				q := url.Values{"limit": {strconv.Itoa(orInt(in.Limit, 20))}}
				for k, v := range map[string]string{"status": in.Status, "service": in.Service, "env": in.Env, "severity": in.Severity, "q": in.Q} {
					if v != "" {
						q.Set(k, v)
					}
				}
				return getJSON(ctx, call, "/issues?"+q.Encode())
			}},
		{Name: "get_issue", Title: "Issue with decode", Annotations: readOnly,
			Description: "One issue with its plain-English decode (summary, probable cause, impact, affected code, next steps), recent samples, and linked entities.",
			InputSchema: obj(map[string]any{"id": str("Issue id from list_issues.")}, "id"),
			Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var in struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(raw, &in); err != nil || in.ID == "" {
					return "", fmt.Errorf("id is required")
				}
				return getJSON(ctx, call, "/issues/"+url.PathEscape(in.ID))
			}},
		{Name: "list_known_issues", Title: "Known issues", Annotations: readOnly,
			Description: "Known-issue rules: problems the team already understands, why they are suppressed or labelled, and their tickets.",
			InputSchema: obj(map[string]any{}),
			Call: func(ctx context.Context, _ json.RawMessage) (string, error) {
				return getJSON(ctx, call, "/known-issues")
			}},
		{Name: "library", Title: "Library shelves", Annotations: readOnly,
			Description: "Without a slug: list Library shelves (Architecture, Services, APIs, Runbooks, Decisions, ...). With a slug: the items on that shelf.",
			InputSchema: obj(map[string]any{"slug": str("Shelf slug, e.g. runbooks, decisions, apis.")}),
			Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var in struct {
					Slug string `json:"slug"`
				}
				_ = json.Unmarshal(raw, &in)
				if in.Slug == "" {
					return getJSON(ctx, call, "/library/shelves")
				}
				return getJSON(ctx, call, "/library/shelves/"+url.PathEscape(in.Slug))
			}},
		{Name: "read_doc", Title: "Read a doc", Annotations: readOnly,
			Description: "Read one docs Tree node (a generated or imported doc file or section) as Markdown. Ids come from library or ask citations.",
			InputSchema: obj(map[string]any{"id": str("Doc node id.")}, "id"),
			Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var in struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(raw, &in); err != nil || in.ID == "" {
					return "", fmt.Errorf("id is required")
				}
				var out struct {
					Title     string `json:"title"`
					Markdown  string `json:"markdown"`
					SourceURL string `json:"source_url"`
				}
				if err := call(ctx, "GET", "/docs/node/"+url.PathEscape(in.ID), nil, &out); err != nil {
					return "", err
				}
				return clip("# " + out.Title + "\n\n" + out.Markdown + "\n\nSource: " + out.SourceURL), nil
			}},
	}
}

func repoIDs(ctx context.Context, call Caller, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	var repos struct {
		Items []struct {
			ID       string `json:"id"`
			FullName string `json:"full_name"`
		} `json:"items"`
	}
	if err := call(ctx, "GET", "/repos", nil, &repos); err != nil {
		return nil, err
	}
	var ids []string
	for _, n := range names {
		found := false
		for _, r := range repos.Items {
			if strings.EqualFold(r.FullName, n) {
				ids, found = append(ids, r.ID), true
			}
		}
		if !found {
			return nil, fmt.Errorf("repository %q is not tracked or not visible to you", n)
		}
	}
	return ids, nil
}

func getJSON(ctx context.Context, call Caller, path string) (string, error) {
	var out json.RawMessage
	if err := call(ctx, "GET", path, nil, &out); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(out, "", " ")
	if err != nil {
		return string(out), nil
	}
	return clip(string(b)), nil
}

func clip(s string) string {
	if len(s) > MaxText {
		return s[:MaxText] + "\n… (truncated)"
	}
	return s
}

func orInt(v, d int) int {
	if v > 0 {
		return v
	}
	return d
}
