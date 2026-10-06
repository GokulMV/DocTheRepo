package rag

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Connected tools: the agent can call read-only tools of the MCP connections (Sentry issues, Datadog logs,
// Jira tickets, AWS or Google Cloud state) for questions the index cannot answer. Results become sources
// like any other, cited in the answer, but are never stored or cached: they describe a moment.

// AgentTool is a tool the agent may call.
type AgentTool struct {
	Name        string // unique, e.g. sentry.search_issues
	Server      string // the connection's name, shown in citations
	Description string
	Schema      string // JSON Schema of the arguments
}

// ToolBox lists and calls the tools someone may use.
type ToolBox interface {
	AgentTools(ctx context.Context, role string) []AgentTool
	CallTool(ctx context.Context, role, name string, args json.RawMessage) (string, error)
}

// maxToolsInPrompt bounds the tool list the agent reads each step.
const maxToolsInPrompt = 40

// maxToolResult bounds one tool result kept as a source.
const maxToolResult = 6000

var liveWords = regexp.MustCompile(`(?i)\b(errors?|exceptions?|crash\w*|alerts?|alarms?|incidents?|outages?|on-?call|pag(e|ed|ing)|logs?|metrics?|latency|traces?|cpu|memory|costs?|spend|bill(ing)?|invoices?|deploy(s|ed|ment|ments)?|releases?|pods?|clusters?|instances?|buckets?|tickets?|sprints?|backlog|today|yesterday|this (week|morning|month)|last (hour|night|day|week|month|\d+)|right now|currently)\b`)

func (e *Engine) agentTools(ctx context.Context, q Query) []AgentTool {
	if e.Tools == nil || q.Role == "" {
		return nil
	}
	return e.Tools.AgentTools(ctx, q.Role)
}

// wantsTools reports a question to send to the connected tools first: it names a connection, or asks about
// live state while tools exist.
func (e *Engine) wantsTools(ctx context.Context, q Query) bool {
	tools := e.agentTools(ctx, q)
	if len(tools) == 0 {
		return false
	}
	lower := strings.ToLower(q.Question)
	for _, t := range tools {
		if name := strings.ToLower(t.Server); len(name) >= 3 && strings.Contains(lower, name) {
			return true
		}
		if head, _, _ := strings.Cut(t.Name, "."); len(head) >= 3 && strings.Contains(lower, strings.ReplaceAll(head, "_", " ")) {
			return true
		}
	}
	return liveWords.MatchString(q.Question)
}

func citesTool(cs []Citation) bool {
	for _, c := range cs {
		if c.Type == "tool" {
			return true
		}
	}
	return false
}

// toolCall is the agent's input for use_tool.
type toolCall struct {
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
}

// runTool calls a tool and turns its output into a source.
func (e *Engine) runTool(ctx context.Context, q Query, tools []AgentTool, input string) (ports.Chunk, string, error) {
	var tc toolCall
	if err := json.Unmarshal([]byte(input), &tc); err != nil || tc.Tool == "" {
		return ports.Chunk{}, "", fmt.Errorf(`input must be JSON: {"tool": name, "args": {...}}`)
	}
	server := ""
	for _, t := range tools {
		if t.Name == tc.Tool {
			server = t.Server
		}
	}
	if server == "" {
		return ports.Chunk{}, "", fmt.Errorf("no tool named %q", tc.Tool)
	}
	if len(tc.Args) == 0 || string(tc.Args) == "null" {
		tc.Args = json.RawMessage(`{}`)
	}
	text, err := e.Tools.CallTool(ctx, q.Role, tc.Tool, tc.Args)
	if err != nil {
		return ports.Chunk{}, "", err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		text = "(no results)"
	}
	if len(text) > maxToolResult {
		text = text[:maxToolResult] + "\n… (trimmed)"
	}
	args := compactJSON(tc.Args)
	sum := sha256.Sum256([]byte(tc.Tool + "\x00" + args))
	c := ports.Chunk{
		ID: "tool:" + hex.EncodeToString(sum[:8]), Scope: server, Source: ports.SourceTool,
		Path: tc.Tool, Symbol: args, Content: text, UpdatedAt: time.Now(),
	}
	return c, text, nil
}

func compactJSON(b json.RawMessage) string {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return string(b)
	}
	out, _ := json.Marshal(v)
	s := string(out)
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// toolList renders the tools for the agent's prompt.
func toolList(tools []AgentTool) string {
	var b strings.Builder
	for i, t := range tools {
		if i == maxToolsInPrompt {
			fmt.Fprintf(&b, "… and %d more\n", len(tools)-maxToolsInPrompt)
			break
		}
		desc := strings.Join(strings.Fields(t.Description), " ")
		if len(desc) > 220 {
			desc = desc[:220] + "…"
		}
		schema := strings.Join(strings.Fields(t.Schema), " ")
		if len(schema) > 500 {
			schema = schema[:500] + "…"
		}
		fmt.Fprintf(&b, "- %s (%s): %s\n  args: %s\n", t.Name, t.Server, desc, schema)
	}
	return b.String()
}
