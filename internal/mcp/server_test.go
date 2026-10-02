package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

type fakeHub struct{ paths []string }

func (f *fakeHub) call(_ context.Context, method, path string, body, out any) error {
	f.paths = append(f.paths, method+" "+path)
	var v string
	switch {
	case path == "/repos":
		v = `{"items":[{"id":"r1","full_name":"acme/payments"}]}`
	case path == "/ask":
		b, _ := json.Marshal(body)
		if !strings.Contains(string(b), `"repo_ids":["r1"]`) {
			return errors.New("repo not resolved: " + string(b))
		}
		v = `{"answer":"Refunds retry 3 times [1].","citations":[{"n":1,"type":"code","title":"retry.go","repo":"acme/payments","path":"refund/retry.go","lines":"10-42"}]}`
	case strings.HasPrefix(path, "/issues/missing"):
		return errors.New("NOT_FOUND: issue not found")
	case strings.HasPrefix(path, "/issues"):
		v = `{"items":[{"id":"i1","title":"TimeoutError"}]}`
	default:
		v = `{}`
	}
	return json.Unmarshal([]byte(v), out)
}

func session(t *testing.T, lines ...string) map[string]map[string]any {
	t.Helper()
	hub := &fakeHub{}
	s := &Server{Name: "dth", Version: "test", Instructions: Instructions, Tools: HubTools(hub.call)}
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		for _, l := range lines {
			_, _ = pw.Write([]byte(l + "\n"))
		}
	}()
	var out strings.Builder
	if err := s.Serve(context.Background(), pr, &out); err != nil {
		t.Fatal(err)
	}
	got := map[string]map[string]any{}
	sc := bufio.NewScanner(strings.NewReader(out.String()))
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("bad line %q", sc.Text())
		}
		id, _ := json.Marshal(m["id"])
		got[string(id)] = m
	}
	return got
}

func text(t *testing.T, m map[string]any) (string, bool) {
	t.Helper()
	res, ok := m["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result: %v", m)
	}
	c := res["content"].([]any)[0].(map[string]any)
	return c["text"].(string), res["isError"].(bool)
}

func TestProtocol(t *testing.T) {
	got := session(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"opencode"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":"p","method":"ping"}`,
		`{"jsonrpc":"2.0","id":3,"method":"nope"}`,
		`not json`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"ask","arguments":{"question":"how are refunds retried?","repos":["acme/payments"]}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"get_issue","arguments":{"id":"missing"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"ask","arguments":{"question":"x","repos":["acme/unknown"]}}}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"list_issues","arguments":{"status":"new"}}}`,
	)
	if len(got) != 9 { // the notification gets no response; the parse error answers with id null
		t.Fatalf("responses = %d: %v", len(got), got)
	}
	init := got["1"]["result"].(map[string]any)
	if init["protocolVersion"] != "2025-06-18" || init["serverInfo"].(map[string]any)["name"] != "dth" {
		t.Errorf("initialize = %v", init)
	}
	tools := got["2"]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 8 || tools[0].(map[string]any)["inputSchema"] == nil {
		t.Errorf("tools = %v", tools)
	}
	if got["3"]["error"].(map[string]any)["code"].(float64) != codeMethodNotFound {
		t.Errorf("unknown method = %v", got["3"])
	}
	if got["null"]["error"].(map[string]any)["code"].(float64) != codeParse {
		t.Errorf("parse error = %v", got["null"])
	}
	if txt, isErr := text(t, got["4"]); isErr || !strings.Contains(txt, "Refunds retry 3 times [1].") || !strings.Contains(txt, "[1] retry.go (code) acme/payments refund/retry.go:10-42") {
		t.Errorf("ask = %q", txt)
	}
	if txt, isErr := text(t, got["5"]); !isErr || !strings.Contains(txt, "NOT_FOUND") {
		t.Errorf("tool error must be a result with isError: %q", txt)
	}
	if txt, isErr := text(t, got["6"]); !isErr || !strings.Contains(txt, "not tracked") {
		t.Errorf("unknown repo = %q", txt)
	}
	if txt, _ := text(t, got["7"]); !strings.Contains(txt, "TimeoutError") {
		t.Errorf("list_issues = %q", txt)
	}
}

func TestUnknownVersionGetsLatest(t *testing.T) {
	got := session(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	if v := got["1"]["result"].(map[string]any)["protocolVersion"]; v != SupportedVersions[0] {
		t.Errorf("version = %v", v)
	}
}
