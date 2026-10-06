// Package mcp serves the Hub to coding agents over the Model Context Protocol (stdio transport): opencode,
// Claude Code, Cursor, and any other MCP client can ask questions, browse the knowledge graph and team docs, and read
// Inbox issues with their decodes. Every tool is a thin call to the Hub REST API with the user's personal
// access token, so repository access, roles, and audit apply exactly as in the UI.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// SupportedVersions are the MCP protocol revisions this server speaks, newest first.
var SupportedVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// Tool is one callable tool.
type Tool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`
	// Call runs the tool; the returned text is shown to the model. An error becomes a tool result with
	// isError set (the model can read it and recover), not a protocol error.
	Call func(ctx context.Context, args json.RawMessage) (string, error) `json:"-"`
}

// Server is an MCP server over newline-delimited JSON-RPC 2.0.
type Server struct {
	Name, Version, Instructions string
	Tools                       []Tool
	// Log receives diagnostics (stderr in the CLI; stdout is the protocol channel).
	Log io.Writer
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// JSON-RPC error codes.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// MaxMessage bounds one inbound message.
const MaxMessage = 8 << 20

// Serve reads requests from in and writes responses to out until in closes or ctx ends. Tool calls run
// concurrently; writes are serialised.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), MaxMessage)
	var mu sync.Mutex
	var wg sync.WaitGroup
	send := func(r response) {
		b, err := json.Marshal(r)
		if err != nil {
			b, _ = json.Marshal(response{JSONRPC: "2.0", ID: r.ID, Error: &rpcError{Code: -32603, Message: "encode result: " + err.Error()}})
		}
		mu.Lock()
		defer mu.Unlock()
		_, _ = out.Write(append(b, '\n'))
	}
	for sc.Scan() {
		line := append([]byte(nil), sc.Bytes()...)
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			send(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: codeParse, Message: "parse error"}})
			continue
		}
		if req.Method == "" {
			if len(req.ID) > 0 {
				send(response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeInvalidRequest, Message: "method is required"}})
			}
			continue // a response to a request we never sent
		}
		if len(req.ID) == 0 { // notification: never answered
			continue
		}
		wg.Add(1)
		go func(req request) {
			defer wg.Done()
			res, rerr := s.handle(ctx, req)
			send(response{JSONRPC: "2.0", ID: req.ID, Result: res, Error: rerr})
		}(req)
	}
	wg.Wait()
	if err := sc.Err(); err != nil {
		return err
	}
	return ctx.Err()
}

func (s *Server) handle(ctx context.Context, req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		v := SupportedVersions[0]
		for _, sv := range SupportedVersions {
			if sv == p.ProtocolVersion {
				v = sv
			}
		}
		res := map[string]any{"protocolVersion": v, "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo": map[string]any{"name": s.Name, "version": s.Version}}
		if s.Instructions != "" {
			res["instructions"] = s.Instructions
		}
		return res, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.Tools}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Name == "" {
			return nil, &rpcError{Code: codeInvalidParams, Message: "tools/call needs a tool name"}
		}
		for _, t := range s.Tools {
			if t.Name != p.Name {
				continue
			}
			args := p.Arguments
			if len(args) == 0 || string(args) == "null" {
				args = json.RawMessage("{}")
			}
			text, err := t.Call(ctx, args)
			if err != nil {
				return toolResult(err.Error(), true), nil
			}
			return toolResult(text, false), nil
		}
		return nil, &rpcError{Code: codeInvalidParams, Message: fmt.Sprintf("unknown tool %q", p.Name)}
	case "resources/list":
		return map[string]any{"resources": []any{}}, nil
	case "prompts/list":
		return map[string]any{"prompts": []any{}}, nil
	}
	return nil, &rpcError{Code: codeMethodNotFound, Message: "method not found: " + req.Method}
}

func toolResult(text string, isErr bool) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": isErr}
}
