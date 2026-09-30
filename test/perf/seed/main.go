// Command seed prepares a running E2E stack (started with -auth oidc) for the Q&A load test: it signs in as
// the owner through the OIDC mock, connects the GitHub mock, routes the stub model (qa + embedding), tracks
// and indexes every repository (which initialises the vector index in the stub's embedding space), and
// then writes synthetic code chunks with the stub's embeddings straight into the store, to plan scale
// (250k chunks):
//
//	go run ./test/perf/seed -control http://127.0.0.1:18199 -chunks 250000
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/adapters/vector/pgvector"
	"github.com/GokulMV/DocTheRepo/internal/core/pipeline"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/test/mocks/stubllm"
)

//go:embed vocab.json
var vocabJSON []byte

func main() {
	control := flag.String("control", "http://127.0.0.1:18099", "stack control API")
	n := flag.Int("chunks", 250000, "chunks to write")
	batch := flag.Int("batch", 1000, "chunks per transaction")
	flag.Parse()
	log.SetFlags(log.Ltime)
	if err := run(context.Background(), *control, *n, *batch); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, control string, n, batch int) error {
	var state struct {
		DatabaseURL  string   `json:"database_url"`
		HubURL       string   `json:"hub_url"`
		GitHubAPIURL string   `json:"github_api_url"`
		LLMURL       string   `json:"llm_url"`
		Repos        []string `json:"repos"`
	}
	resp, err := http.Get(control + "/state")
	if err != nil {
		return fmt.Errorf("stack control API: %w", err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil || state.DatabaseURL == "" {
		return fmt.Errorf("stack state has no database_url (%v)", err)
	}
	var vocab []string
	if err := json.Unmarshal(vocabJSON, &vocab); err != nil {
		return err
	}
	if err := prepare(ctx, control, state.HubURL, state.GitHubAPIURL, state.LLMURL, state.Repos); err != nil {
		return fmt.Errorf("prepare the hub: %w", err)
	}
	st, err := store.Open(ctx, store.Options{URL: state.DatabaseURL, MaxConns: 8})
	if err != nil {
		return err
	}
	defer st.Close()
	idx := pgvector.New(st.Pool)
	is, err := idx.State(ctx)
	if err != nil {
		return err
	}
	if is.Live == nil {
		return fmt.Errorf("the vector index is not initialised: track and index a repository first (with the stub embedding route)")
	}
	if is.Live.Dimensions != stubllm.Dims {
		return fmt.Errorf("the live index is %s/%s with %d dims; the stub embeds %d", is.Live.ProviderKind, is.Live.Model, is.Live.Dimensions, stubllm.Dims)
	}
	rows, err := st.Pool.Query(ctx, "SELECT id::text, full_name FROM repos WHERE enabled ORDER BY full_name")
	if err != nil {
		return err
	}
	type repo struct{ id, name string }
	var repos []repo
	for rows.Next() {
		var r repo
		if err := rows.Scan(&r.id, &r.name); err != nil {
			return err
		}
		repos = append(repos, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(repos) == 0 {
		return fmt.Errorf("no tracked repositories")
	}
	chunks := store.NewChunks(st)
	rng := rand.New(rand.NewPCG(42, 7))
	word := func() string { return vocab[rng.IntN(len(vocab))] }
	title := func(s string) string { return strings.ToUpper(s[:1]) + s[1:] }
	start := time.Now()
	for done := 0; done < n; {
		size := min(batch, n-done)
		w := store.ChunkWrite{}
		vs := make([]ports.ChunkVector, 0, size)
		for i := 0; i < size; i++ {
			k := done + i
			r := repos[k%len(repos)]
			a, b, c := word(), word(), word()
			sym := title(a) + title(b)
			path := fmt.Sprintf("gen/%s/%s_%d.go", c, a, k/10)
			var body strings.Builder
			fmt.Fprintf(&body, "// %s %ss the %s %s when the %s %s is %s.\nfunc %s(", sym, a, b, word(), c, word(), word(), sym)
			for j := 0; j < 3; j++ {
				fmt.Fprintf(&body, "%s %s, ", word(), title(word()))
			}
			body.WriteString(") error {\n")
			for j := 0; j < 6; j++ {
				fmt.Fprintf(&body, "\tif err := %s%s(%s); err != nil { return err }\n", word(), title(word()), word())
			}
			body.WriteString("\treturn nil\n}\n")
			content := body.String()
			sum := sha256.Sum256([]byte(r.name + "::" + path + "::" + sym))
			h := sha256.Sum256([]byte(content))
			ch := ports.Chunk{ID: "seed" + hex.EncodeToString(sum[:6]), RepoID: r.id, Scope: r.name, Source: ports.SourceCode, Path: path,
				Symbol: sym, Language: "go", Content: content, ContentHash: hex.EncodeToString(h[:8]), Signature: "func " + sym + "(…) error", CommitSHA: "seed"}
			w.Upserts = append(w.Upserts, ch)
			vs = append(vs, ports.ChunkVector{ChunkID: ch.ID, RepoID: r.id, Source: ch.Source, Vector: stubllm.Embed(pipeline.EmbedText(ch))})
		}
		if _, err := chunks.Apply(ctx, w); err != nil {
			return err
		}
		if err := idx.Upsert(ctx, 0, vs); err != nil {
			return err
		}
		done += size
		if done%(batch*25) == 0 || done == n {
			log.Printf("%d/%d chunks (%.0f/s)", done, n, float64(done)/time.Since(start).Seconds())
		}
	}
	if _, err := st.Pool.Exec(ctx, "ANALYZE chunks"); err != nil {
		return err
	}
	var total int64
	if err := st.Pool.QueryRow(ctx, "SELECT count(*) FROM chunks WHERE deleted_at IS NULL").Scan(&total); err != nil {
		return err
	}
	log.Printf("done in %s; %d live chunks in the index", time.Since(start).Round(time.Second), total)
	return nil
}

// hubClient calls the hub API with a session cookie and CSRF token.
type hubClient struct {
	base string
	c    *http.Client
	csrf string
}

func (h *hubClient) do(method, path string, body, out any) (int, error) {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, h.base+"/api/v1"+path, rd)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", h.csrf)
	resp, err := h.c.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode, nil
}

// prepare signs in as the owner (the first SSO user) and configures and indexes every repository.
func prepare(ctx context.Context, control, hub, ghAPI, llmURL string, repos []string) error {
	b, _ := json.Marshal(map[string]string{"email": "owner@acme.test"})
	if resp, err := http.Post(control+"/oidc/user", "application/json", bytes.NewReader(b)); err != nil {
		return err
	} else {
		resp.Body.Close()
	}
	jar, _ := cookiejar.New(nil)
	h := &hubClient{base: hub, c: &http.Client{Jar: jar, Timeout: time.Minute}}
	resp, err := h.c.Get(hub + "/api/v1/auth/login?return=/")
	if err != nil {
		return err
	}
	resp.Body.Close()
	var me struct {
		CSRF string `json:"csrf_token"`
		Role string `json:"role"`
	}
	if code, err := h.do("GET", "/me", nil, &me); err != nil || code != 200 {
		return fmt.Errorf("owner sign-in failed (%d, %v)", code, err)
	}
	h.csrf = me.CSRF
	var list struct {
		Items []map[string]any `json:"items"`
	}
	var connID, provID string
	h.do("GET", "/connectors", nil, &list)
	for _, c := range list.Items {
		if c["type"] == "github" {
			connID, _ = c["id"].(string)
		}
	}
	var created map[string]any
	if connID == "" {
		code, err := h.do("POST", "/connectors", map[string]any{"type": "github", "name": "GitHub", "credentials": "ghp_perf", "webhook_secret": "perf",
			"mode": "webhook", "config": map[string]string{"base_url": ghAPI, "auth": "token"}}, &created)
		if err != nil || code != 201 {
			return fmt.Errorf("create connector: %d %v %v", code, err, created)
		}
		connID = created["id"].(string)
	}
	h.do("GET", "/providers", nil, &list)
	for _, p := range list.Items {
		if p["kind"] == "openai_compat" {
			provID, _ = p["id"].(string)
		}
	}
	if provID == "" {
		code, err := h.do("POST", "/providers", map[string]any{"kind": "openai_compat", "name": "Stub", "base_url": llmURL, "api_key": "sk-perf"}, &created)
		if err != nil || code != 201 {
			return fmt.Errorf("create provider: %d %v %v", code, err, created)
		}
		provID = created["id"].(string)
	}
	for f, m := range map[string]string{"qa": "stub", "embedding": "stub-embed"} {
		if code, err := h.do("PUT", "/routes/"+f, map[string]any{"provider_id": provID, "model": m}, nil); err != nil || code != 204 {
			return fmt.Errorf("route %s: %d %v", f, code, err)
		}
	}
	// A load test measures latency, not the spend guard: lift the seeded 2M tokens/day global ceiling (at
	// ~4k prompt tokens per answer it would block the run after ~500 questions with HTTP 402).
	if code, err := h.do("PUT", "/spend/limits", map[string]any{"items": []map[string]any{{"scope": "global", "window": "day", "max_tokens": 2_000_000_000}}}, nil); err != nil || code != 204 {
		return fmt.Errorf("spend limits: %d %v", code, err)
	}
	for _, r := range repos {
		if code, err := h.do("POST", "/repos", map[string]any{"connector_id": connID, "full_name": r, "push_mode": "direct"}, nil); err != nil || code != 201 {
			return fmt.Errorf("track %s: %d %v", r, code, err)
		}
	}
	if code, err := h.do("POST", "/connectors/"+connID+"/sync", nil, nil); err != nil || code != 202 {
		return fmt.Errorf("sync: %d %v", code, err)
	}
	log.Printf("indexing %d repositories …", len(repos))
	deadline := time.Now().Add(15 * time.Minute)
	for time.Now().Before(deadline) {
		var rl struct {
			Items []struct {
				SHA string `json:"last_processed_sha"`
			} `json:"items"`
		}
		h.do("GET", "/repos", nil, &rl)
		pending := 0
		for _, r := range rl.Items {
			if r.SHA == "" {
				pending++
			}
		}
		if len(rl.Items) > 0 && pending == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return errors.New("repositories not indexed within 15 minutes")
}
