package rag

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strings"
)

// DefaultSimilarAnswer is the cosine similarity a reworded question needs to reuse a cached answer.
// It is deliberately high: serving the answer to a different question is worse than paying for a call.
const DefaultSimilarAnswer = 0.95

// SemanticCache is a Store that can also find a cached answer by the meaning of the question: same
// scope and embedding model, embedding similarity at least minSim, sources unchanged. Optional.
type SemanticCache interface {
	SimilarAnswer(ctx context.Context, scopeKey, model string, vec []float32, minSim float64, accept func(question string) bool) (Answer, bool, error)
	PutAnswerMeaning(ctx context.Context, key, question, scopeKey, model string, vec []float32, a Answer) error
}

// ScopeKey identifies what a question may read, without the question (the semantic cache's partition).
func ScopeKey(s Scope) string {
	repos := append([]string(nil), s.RepoIDs...)
	sort.Strings(repos)
	srcs := make([]string, len(s.Sources))
	for i, x := range s.Sources {
		srcs[i] = string(x)
	}
	sort.Strings(srcs)
	b, _ := json.Marshal([]any{s.All, repos, srcs})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Cosine is the cosine similarity of two vectors (0 when their lengths differ or one is zero).
func Cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

var identRE = regexp.MustCompile(`[A-Za-z_][\w./-]*[\w]`)

// Identifiers are the names in a question that point at specific code: file paths, dotted or snake_case
// names, CamelCase and words with digits. Embeddings see "PayRetry" and "CartRetry" as close; a
// question about one must not reuse the answer about the other.
func Identifiers(q string) []string {
	var out []string
	for _, w := range identRE.FindAllString(q, -1) {
		lower := strings.ToLower(w)
		if strings.ContainsAny(w, "./_") || strings.ContainsAny(w, "0123456789") || (w != lower && w[1:] != strings.ToLower(w[1:])) {
			out = append(out, lower)
		}
	}
	sort.Strings(out)
	return out
}

// sameIdentifiers reports whether two questions name the same code: every identifier in either one
// appears, ignoring case, among the other's words.
func sameIdentifiers(a, b string) bool {
	return namesIn(a, b) && namesIn(b, a)
}

func namesIn(from, in string) bool {
	ids := Identifiers(from)
	if len(ids) == 0 {
		return true
	}
	words := map[string]bool{}
	for _, w := range identRE.FindAllString(strings.ToLower(in), -1) {
		words[w] = true
	}
	for _, id := range ids {
		if !words[id] {
			return false
		}
	}
	return true
}
