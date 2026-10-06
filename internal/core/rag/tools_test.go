package rag_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/rag"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/test/mocks/fakegw"
)

type fakeTools struct {
	calls []string
	roles []string
}

func (f *fakeTools) AgentTools(_ context.Context, role string) []rag.AgentTool {
	f.roles = append(f.roles, role)
	if role == "viewer" {
		return nil // this connection needs editors
	}
	return []rag.AgentTool{{Name: "sentry.search_issues", Server: "Sentry", Description: "Find Sentry issues", Schema: `{"type":"object","properties":{"query":{"type":"string"}}}`}}
}

func (f *fakeTools) CallTool(_ context.Context, _ string, name string, args json.RawMessage) (string, error) {
	f.calls = append(f.calls, name+" "+string(args))
	return "PAY-123 NullPointerException in RefundService.refund, 42 events in the last hour", nil
}

// A live question goes to the connected tools first; the result is cited like any source, and the answer
// is not cached because it describes a moment.
func TestAgentCallsAConnectedToolForALiveQuestion(t *testing.T) {
	st := &memStore{chunks: []ports.Chunk{
		chunk("a", "refund.go", "func refund() handles refunds"), chunk("b", "pay.go", "payments errors"), chunk("c", "x.go", "errors util"),
	}}
	env := fakegw.New(fakegw.Options{})
	var sawTools bool
	env.Model.Handler = func(r ports.ChatRequest) (ports.ChatResponse, error) {
		last := r.Messages[len(r.Messages)-1].Content
		if r.JSONSchema != nil {
			if strings.Contains(last, "sentry.search_issues") {
				sawTools = true
			}
			if !strings.Contains(last, "use_tool") || !strings.Contains(last, "Actions so far") {
				return ports.ChatResponse{Text: `{"action":"use_tool","input":"{\"tool\":\"sentry.search_issues\",\"args\":{\"query\":\"is:unresolved\"}}","reason":"checking Sentry"}`}, nil
			}
			return ports.ChatResponse{Text: `{"action":"answer","input":"","reason":"enough"}`}, nil
		}
		// The tool result is the first source.
		text := "The top error is PAY-123, a NullPointerException in RefundService.refund [1]."
		if !strings.Contains(last, `type="tool"`) {
			text = rag.NotFoundAnswer
		}
		return ports.ChatResponse{Text: text}, nil
	}
	tools := &fakeTools{}
	e := &rag.Engine{Store: st, GW: env.GW, AgentSteps: 4, Tools: tools}
	a, err := e.Ask(context.Background(), rag.Query{Question: "what errors are happening in payments right now?", Scope: rag.Scope{All: true}, Role: "editor"})
	require.NoError(t, err)
	assert.True(t, sawTools, "the agent is shown the tools")
	assert.Equal(t, []string{`sentry.search_issues {"query":"is:unresolved"}`}, tools.calls)
	require.NotEmpty(t, a.Citations)
	assert.Equal(t, "tool", a.Citations[0].Type)
	assert.Equal(t, "Sentry", a.Citations[0].Repo)
	assert.Equal(t, "sentry.search_issues", a.Citations[0].Path)
	assert.Equal(t, 0, st.puts, "answers from live tools are not cached")
}

// Someone whose role the connection does not allow never gets its tools.
func TestToolsFollowTheAskersRole(t *testing.T) {
	st := &memStore{chunks: []ports.Chunk{chunk("a", "a.go", "errors are logged"), chunk("b", "b.go", "errors b"), chunk("c", "c.go", "errors c")}}
	env := fakegw.New(fakegw.Options{})
	env.Model.Handler = func(r ports.ChatRequest) (ports.ChatResponse, error) {
		if r.JSONSchema != nil {
			return ports.ChatResponse{Text: `{"action":"answer","input":"","reason":"done"}`}, nil
		}
		return ports.ChatResponse{Text: "Errors are logged [1]."}, nil
	}
	tools := &fakeTools{}
	_, err := (&rag.Engine{Store: st, GW: env.GW, AgentSteps: 4, Tools: tools}).Ask(context.Background(),
		rag.Query{Question: "what errors happened today?", Scope: rag.Scope{All: true}, Role: "viewer"})
	require.NoError(t, err)
	assert.Empty(t, tools.calls)
	assert.Contains(t, tools.roles, "viewer")
}
