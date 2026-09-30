// Package stubllm is a deterministic, behaviour-shaped stand-in for a real model behind the OpenAI
// protocol, for E2E, performance, and retrieval-quality runs. Unlike llmmock (fixed text per scenario, for
// adapter parity), it does each Hub task plausibly:
//
//   - docgen: valid DocGen JSON documenting exactly the requested chunk IDs;
//   - triage: "not cosmetic, confident";
//   - Q&A: cites the supplied sources that share the most terms with the question, or answers the
//     not-found sentence when none do;
//   - embeddings: feature-hashed bags of identifier-aware terms, so similar text is near in vector space.
//
// Per-feature latency is configurable (a 90 s docgen models a slow agent in the push-burst run) and the
// server counts calls and the peak number in flight per feature, which tests assert on (a blocked spend
// guard means zero calls; the docgen concurrency cap is never exceeded).
package stubllm

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Features as the stub classifies requests (they match the Hub's llmgateway feature names).
const (
	DocGen    = "docgen"
	QA        = "qa"
	Triage    = "triage"
	Embedding = "embedding"
	Other     = "other"
)

// NotFound is the Hub's exact "not in the sources" sentence (rag.NotFoundAnswer).
const NotFound = "I could not find this in the connected sources."

// Dims is the embedding width.
const Dims = 256

// Stats is one feature's counters.
type Stats struct {
	Calls       int `json:"calls"`
	InFlight    int `json:"in_flight"`
	MaxInFlight int `json:"max_in_flight"`
}

// Server is the stub. Use New for an httptest server, or Handler to mount it on your own listener.
type Server struct {
	*httptest.Server
	mu    sync.Mutex
	delay map[string]time.Duration
	stats map[string]*Stats
	// CiteTop caps how many sources a Q&A answer cites.
	CiteTop int
}

// NewHandler returns an unstarted stub (serve Handler yourself).
func NewHandler() *Server {
	return &Server{delay: map[string]time.Duration{}, stats: map[string]*Stats{}, CiteTop: 3}
}

// New starts the stub on a random local port.
func New() *Server {
	s := NewHandler()
	s.Server = httptest.NewServer(s.Handler())
	return s
}

// SetDelay makes every call of feature take d (before responding).
func (s *Server) SetDelay(feature string, d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delay[feature] = d
}

// Snapshot returns a copy of the counters.
func (s *Server) Snapshot() map[string]Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]Stats{}
	for k, v := range s.stats {
		out[k] = *v
	}
	return out
}

// Calls is the number of calls of feature so far.
func (s *Server) Calls(feature string) int { return s.Snapshot()[feature].Calls }

// Reset zeroes the counters (delays stay).
func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats = map[string]*Stats{}
}

// Handler serves the OpenAI protocol under /v1 plus a control API under /_stub:
// GET /_stub/stats, POST /_stub/reset, POST /_stub/delay {"docgen_ms": 90000, ...}.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"object": "list", "data": []any{map[string]any{"id": "stub", "object": "model"}}})
	})
	mux.HandleFunc("POST /v1/chat/completions", s.chat)
	mux.HandleFunc("POST /v1/embeddings", s.embeddings)
	mux.HandleFunc("GET /_stub/stats", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, s.Snapshot()) })
	mux.HandleFunc("POST /_stub/reset", func(w http.ResponseWriter, _ *http.Request) { s.Reset(); w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("POST /_stub/delay", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]int
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for k, ms := range in {
			s.SetDelay(strings.TrimSuffix(k, "_ms"), time.Duration(ms)*time.Millisecond)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

// begin records a call and applies the feature's latency; the returned func ends it.
func (s *Server) begin(r *http.Request, feature string) (func(), bool) {
	s.mu.Lock()
	st := s.stats[feature]
	if st == nil {
		st = &Stats{}
		s.stats[feature] = st
	}
	st.Calls++
	st.InFlight++
	if st.InFlight > st.MaxInFlight {
		st.MaxInFlight = st.InFlight
	}
	d := s.delay[feature]
	s.mu.Unlock()
	end := func() {
		s.mu.Lock()
		st.InFlight--
		s.mu.Unlock()
	}
	if d > 0 {
		select {
		case <-time.After(d):
		case <-r.Context().Done():
			end()
			return nil, false
		}
	}
	return end, true
}

type chatReq struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Messages []struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	} `json:"messages"`
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var in chatReq
	if err := json.Unmarshal(raw, &in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var system, user strings.Builder
	for _, m := range in.Messages {
		text := contentText(m.Content)
		if m.Role == "system" || m.Role == "developer" {
			system.WriteString(text)
		} else if m.Role == "user" {
			user.Reset() // the last user turn is the prompt
			user.WriteString(text)
		}
	}
	feature := classify(system.String())
	end, ok := s.begin(r, feature)
	if !ok {
		return
	}
	defer end()
	var out string
	switch feature {
	case DocGen:
		out = docgen(user.String())
	case Triage:
		out = `{"cosmetic":false,"confident":true,"reason":"stub: every change is documented"}`
	case QA:
		out = s.answer(user.String())
	default:
		out = "ok"
	}
	promptTokens, completionTokens := len(raw)/4+1, len(out)/4+1
	if in.Stream {
		stream(w, in.Model, out, promptTokens, completionTokens)
		return
	}
	writeJSON(w, map[string]any{"model": in.Model,
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": out}, "finish_reason": "stop"}},
		"usage":   map[string]int{"prompt_tokens": promptTokens, "completion_tokens": completionTokens}})
}

func (s *Server) embeddings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Model string `json:"model"`
		Input any    `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	end, ok := s.begin(r, Embedding)
	if !ok {
		return
	}
	defer end()
	var texts []string
	switch v := in.Input.(type) {
	case string:
		texts = []string{v}
	case []any:
		for _, x := range v {
			t, _ := x.(string)
			texts = append(texts, t)
		}
	}
	data := make([]any, len(texts))
	tokens := 0
	for i, t := range texts {
		data[i] = map[string]any{"object": "embedding", "index": i, "embedding": Embed(t)}
		tokens += len(t)/4 + 1
	}
	writeJSON(w, map[string]any{"object": "list", "model": in.Model, "data": data, "usage": map[string]int{"prompt_tokens": tokens, "total_tokens": tokens}})
}

func classify(system string) string {
	switch {
	case strings.HasPrefix(system, "You write reference documentation"):
		return DocGen
	case strings.HasPrefix(system, "You answer engineers"):
		return QA
	case strings.HasPrefix(system, "You classify a file change"):
		return Triage
	}
	return Other
}

var (
	repoRE  = regexp.MustCompile(`(?m)^Repository: (.+)$`)
	fileRE  = regexp.MustCompile(`(?m)^File: (.+)$`)
	chunkRE = regexp.MustCompile("(?m)^- chunk_id ([^:]+): `([^`]*)` \\(([^)]*)\\)")
)

// docgen documents exactly the requested chunks. The prose repeats the symbol's words so the generated
// doc is retrievable by the same terms as the code.
func docgen(prompt string) string {
	file := ""
	if m := fileRE.FindStringSubmatch(prompt); m != nil {
		file = strings.TrimSpace(m[1])
	}
	repo := ""
	if m := repoRE.FindStringSubmatch(prompt); m != nil {
		repo = strings.TrimSpace(m[1])
	}
	type doc struct {
		ChunkID string `json:"chunk_id"`
		Symbol  string `json:"symbol"`
		Content string `json:"content"`
	}
	docs := []doc{}
	for _, m := range chunkRE.FindAllStringSubmatch(prompt, -1) {
		sym := m[2]
		words := strings.Join(terms(sym), " ")
		docs = append(docs, doc{ChunkID: m[1], Symbol: sym, Content: fmt.Sprintf(
			"`%s` in `%s` handles %s. It was %s in the latest change.\n\nSee the source for parameters and return values.",
			sym, file, words, m[3])})
	}
	b, _ := json.Marshal(map[string]any{"file_summary": fmt.Sprintf("Code for %s in %s.", file, repo), "docs": docs})
	return string(b)
}

var sourceRE = regexp.MustCompile(`(?s)<source n="(\d+)" type="([^"]*)" repo="([^"]*)" path="([^"]*)" symbol="([^"]*)">\n(.*?)\n</source>`)

// answer cites the best-matching sources: a source qualifies when it shares at least 60% of the best
// source's overlap with the question, up to CiteTop sources.
func (s *Server) answer(prompt string) string {
	q := prompt
	if i := strings.LastIndex(prompt, "\nQuestion: "); i >= 0 {
		q = prompt[i+len("\nQuestion: "):]
	}
	qt := set(terms(q))
	type scored struct {
		n          string
		path, sym  string
		score      int
		firstIndex int
	}
	var all []scored
	for i, m := range sourceRE.FindAllStringSubmatch(prompt, -1) {
		st := set(terms(m[4] + " " + m[5] + " " + m[6]))
		score := 0
		for t := range qt {
			if st[t] {
				score++
			}
		}
		if score > 0 {
			all = append(all, scored{n: m[1], path: m[4], sym: m[5], score: score, firstIndex: i})
		}
	}
	if len(all) == 0 {
		return NotFound
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].score > all[j].score })
	best := all[0].score
	var parts []string
	for _, c := range all {
		if len(parts) == s.CiteTop || float64(c.score) < 0.6*float64(best) {
			break
		}
		name := c.sym
		if name == "" || strings.HasPrefix(name, "__") {
			name = c.path
		}
		parts = append(parts, fmt.Sprintf("`%s` in %s is relevant [%s].", name, c.path, c.n))
	}
	return strings.Join(parts, " ")
}

// Embed is the stub's embedding: signed feature hashing of terms, L2-normalised.
func Embed(text string) []float32 {
	v := make([]float64, Dims)
	for _, t := range terms(text) {
		h := fnv.New64a()
		_, _ = h.Write([]byte(t))
		x := h.Sum64()
		sign := 1.0
		if x&(1<<63) != 0 {
			sign = -1
		}
		v[x%Dims] += sign
	}
	var norm float64
	for _, x := range v {
		norm += x * x
	}
	out := make([]float32, Dims)
	if norm == 0 {
		out[0] = 1
		return out
	}
	norm = math.Sqrt(norm)
	for i, x := range v {
		out[i] = float32(x / norm)
	}
	return out
}

var stop = set(strings.Fields(`a an and are as at be by do does for from how i in is it of on or our the this to
we what when where which who why will with you your use used using can there their them`))

// terms lowercases text into identifier-aware words: camelCase and snake_case split, stop words and
// one-letter tokens dropped, a trailing plural "s" stripped.
func terms(text string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 1 {
			w := strings.ToLower(string(cur))
			if len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") {
				w = w[:len(w)-1]
			}
			if !stop[w] {
				out = append(out, w)
			}
		}
		cur = cur[:0]
	}
	rs := []rune(text)
	for i, r := range rs {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			// camelCase boundary: lower→Upper, or the last Upper of an acronym before a lower.
			if unicode.IsUpper(r) && len(cur) > 0 && (unicode.IsLower(cur[len(cur)-1]) ||
				(i+1 < len(rs) && unicode.IsLower(rs[i+1]) && unicode.IsUpper(cur[len(cur)-1]))) {
				flush()
			}
			cur = append(cur, r)
		default:
			flush()
		}
	}
	flush()
	return out
}

func set(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func contentText(c any) string {
	switch v := c.(type) {
	case string:
		return v
	case []any: // content parts
		var b strings.Builder
		for _, p := range v {
			if m, ok := p.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					b.WriteString(t)
				}
			}
		}
		return b.String()
	}
	return ""
}

func stream(w http.ResponseWriter, model, text string, in, out int) {
	w.Header().Set("Content-Type", "text/event-stream")
	fl, _ := w.(http.Flusher)
	words := strings.SplitAfter(text, " ")
	for i, part := range words {
		fin := any(nil)
		if i == len(words)-1 {
			fin = "stop"
		}
		b, _ := json.Marshal(map[string]any{"model": model, "choices": []any{map[string]any{"delta": map[string]any{"content": part}, "finish_reason": fin}}})
		fmt.Fprintf(w, "data: %s\n\n", b)
		if fl != nil {
			fl.Flush()
		}
	}
	fmt.Fprintf(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":%d}}\n\ndata: [DONE]\n\n", in, out)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
