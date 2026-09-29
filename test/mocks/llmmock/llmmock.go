// Package llmmock serves the four LLM wire dialects the Hub speaks — Anthropic Messages, OpenAI Chat
// Completions (+Embeddings), Bedrock Converse (+InvokeModel embeddings), and Vertex generateContent
// (+predict embeddings) — from one httptest server. The requested model name selects the scenario, so
// the same scenario table runs against every adapter (the parity suite).
package llmmock

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// Scenarios, selected by model name.
const (
	OK           = "scenario-ok"
	Refusal      = "scenario-refusal"
	Truncated    = "scenario-truncated"
	RateLimited  = "scenario-ratelimited"
	Unauthorized = "scenario-unauthorized"
)

// Text is what OK returns.
const Text = `{"answer":"parity"}`

// Usage figures OK reports (input, output).
const (
	InputTokens  = 42
	OutputTokens = 7
)

// Request is one captured call.
type Request struct {
	Dialect string
	Path    string
	Model   string
	Body    map[string]any
	Header  http.Header
}

// Server is a running mock.
type Server struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []Request
}

// Requests returns captured calls.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.reqs...)
}

// Last returns the last captured call.
func (s *Server) Last() Request {
	r := s.Requests()
	if len(r) == 0 {
		return Request{}
	}
	return r[len(r)-1]
}

// New starts the mock.
func New() *Server {
	s := &Server{}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	body := map[string]any{}
	_ = json.Unmarshal(raw, &body)
	req := Request{Path: r.URL.Path, Body: body, Header: r.Header.Clone()}
	switch {
	case r.URL.Path == "/v1/messages":
		req.Dialect, req.Model = "anthropic", str(body["model"])
	case strings.HasSuffix(r.URL.Path, "/chat/completions"):
		req.Dialect, req.Model = "openai", str(body["model"])
	case strings.HasSuffix(r.URL.Path, "/embeddings"):
		req.Dialect, req.Model = "openai-embed", str(body["model"])
	case strings.HasSuffix(r.URL.Path, "/converse"):
		req.Dialect, req.Model = "converse", between(r.URL.Path, "/model/", "/converse")
	case strings.HasSuffix(r.URL.Path, "/invoke"):
		req.Dialect, req.Model = "bedrock-embed", between(r.URL.Path, "/model/", "/invoke")
	case strings.HasSuffix(r.URL.Path, ":generateContent"):
		req.Dialect, req.Model = "gemini", between(r.URL.Path, "/models/", ":generateContent")
	case strings.HasSuffix(r.URL.Path, ":predict"):
		req.Dialect, req.Model = "vertex-embed", between(r.URL.Path, "/models/", ":predict")
	default:
		w.WriteHeader(http.StatusNotFound)
		return
	}
	s.mu.Lock()
	s.reqs = append(s.reqs, req)
	s.mu.Unlock()

	scenario := req.Model
	for _, p := range []string{"anthropic.", "us.anthropic."} {
		scenario = strings.TrimPrefix(scenario, p)
	}
	switch scenario {
	case RateLimited:
		w.Header().Set("Retry-After", "3")
		w.Header().Set("X-Amzn-ErrorType", "ThrottlingException")
		writeErr(w, req.Dialect, http.StatusTooManyRequests, "rate_limit_error", "slow down")
		return
	case Unauthorized:
		w.Header().Set("X-Amzn-ErrorType", "AccessDeniedException")
		writeErr(w, req.Dialect, http.StatusUnauthorized, "authentication_error", "bad credentials")
		return
	}
	if stream, _ := body["stream"].(bool); stream && scenario == OK {
		writeSSE(w, req.Dialect, req.Model)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch req.Dialect {
	case "anthropic":
		stop, text := "end_turn", Text
		switch scenario {
		case Refusal:
			stop, text = "refusal", ""
		case Truncated:
			stop, text = "max_tokens", `{"answ`
		}
		fmt.Fprintf(w, `{"id":"m","type":"message","role":"assistant","model":%q,"content":[{"type":"text","text":%q}],
		  "stop_reason":%q,"stop_details":{"type":"refusal","category":"cyber","explanation":"no"},
		  "usage":{"input_tokens":%d,"output_tokens":%d}}`, req.Model, text, stop, InputTokens, OutputTokens)
	case "openai":
		finish, text, refusal := "stop", Text, ""
		switch scenario {
		case Refusal:
			text, refusal = "", "I can't help with that"
		case Truncated:
			finish, text = "length", `{"answ`
		}
		fmt.Fprintf(w, `{"model":%q,"choices":[{"message":{"content":%q,"refusal":%q},"finish_reason":%q}],
		  "usage":{"prompt_tokens":%d,"completion_tokens":%d}}`, req.Model, text, refusal, finish, InputTokens, OutputTokens)
	case "converse":
		stop, text := "end_turn", Text
		switch scenario {
		case Refusal:
			stop, text = "content_filtered", ""
		case Truncated:
			stop, text = "max_tokens", `{"answ`
		}
		fmt.Fprintf(w, `{"output":{"message":{"role":"assistant","content":[{"text":%q}]}},"stopReason":%q,
		  "usage":{"inputTokens":%d,"outputTokens":%d,"totalTokens":%d}}`, text, stop, InputTokens, OutputTokens, InputTokens+OutputTokens)
	case "gemini":
		finish, text := "STOP", Text
		switch scenario {
		case Refusal:
			finish, text = "SAFETY", ""
		case Truncated:
			finish, text = "MAX_TOKENS", `{"answ`
		}
		fmt.Fprintf(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":%q}]},"finishReason":%q}],
		  "usageMetadata":{"promptTokenCount":%d,"candidatesTokenCount":%d}}`, text, finish, InputTokens, OutputTokens)
	case "openai-embed":
		inputs, _ := body["input"].([]any)
		var data []string
		for i := range inputs {
			data = append(data, fmt.Sprintf(`{"index":%d,"embedding":[%d,1,0]}`, i, i))
		}
		fmt.Fprintf(w, `{"data":[%s],"usage":{"prompt_tokens":%d}}`, strings.Join(data, ","), len(inputs))
	case "bedrock-embed":
		fmt.Fprint(w, `{"embedding":[0,1,0],"inputTextTokenCount":1}`)
	case "vertex-embed":
		inst, _ := body["instances"].([]any)
		var preds []string
		for i := range inst {
			preds = append(preds, fmt.Sprintf(`{"embeddings":{"values":[%d,1,0],"statistics":{"token_count":1}}}`, i))
		}
		fmt.Fprintf(w, `{"predictions":[%s]}`, strings.Join(preds, ","))
	}
}

// writeSSE streams the OK text in two deltas in the dialect's event format.
func writeSSE(w http.ResponseWriter, dialect, model string) {
	w.Header().Set("Content-Type", "text/event-stream")
	half := len(Text) / 2
	parts := []string{Text[:half], Text[half:]}
	switch dialect {
	case "anthropic":
		fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"type\":\"message\",\"role\":\"assistant\",\"model\":%q,\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":%d,\"output_tokens\":0}}}\n\n", model, InputTokens)
		fmt.Fprint(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		for _, p := range parts {
			b, _ := json.Marshal(p)
			fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", b)
		}
		fmt.Fprint(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		fmt.Fprintf(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":%d}}\n\n", OutputTokens)
		fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	case "openai":
		for i, p := range parts {
			b, _ := json.Marshal(p)
			fin := "null"
			if i == len(parts)-1 {
				fin = `"stop"`
			}
			fmt.Fprintf(w, "data: {\"model\":%q,\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":%s}]}\n\n", model, b, fin)
		}
		fmt.Fprintf(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":%d}}\n\ndata: [DONE]\n\n", InputTokens, OutputTokens)
	}
}

func writeErr(w http.ResponseWriter, dialect string, status int, typ, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if dialect == "anthropic" {
		fmt.Fprintf(w, `{"type":"error","error":{"type":%q,"message":%q}}`, typ, msg)
		return
	}
	fmt.Fprintf(w, `{"error":{"message":%q},"message":%q}`, msg, msg)
}

func str(v any) string { s, _ := v.(string); return s }

func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	s = s[i+len(a):]
	if j := strings.Index(s, b); j >= 0 {
		s = s[:j]
	}
	return s
}
