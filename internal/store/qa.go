package store

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/GokulMV/DocTheRepo/internal/core/rag"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store/gen"
)

// QA implements rag.Store and Q&A thread persistence.
type QA struct{ s *Store }

// NewQA returns the Q&A store.
func NewQA(s *Store) *QA { return &QA{s: s} }

func scopeArgs(sc rag.Scope) (bool, []string, []string) {
	ids := sc.RepoIDs
	if ids == nil {
		ids = []string{}
	}
	srcs := make([]string, len(sc.Sources))
	for i, x := range sc.Sources {
		srcs[i] = string(x)
	}
	return sc.All, ids, srcs
}

// FullText returns keyword candidates.
func (q *QA) FullText(ctx context.Context, question string, sc rag.Scope, k int) ([]ports.VectorHit, error) {
	out, err := q.fullText(ctx, question, sc, k)
	if err != nil {
		return nil, err
	}
	// Every word must match at first. A broad or wordy question ("explain how this repo works") then
	// finds little, so widen to any of its words, ranked by how many and how well they match.
	if len(out) < k/2 {
		if alt := anyWords(question); alt != "" && alt != question {
			more, err := q.fullText(ctx, alt, sc, k)
			if err != nil {
				return nil, err
			}
			seen := map[string]bool{}
			for _, h := range out {
				seen[h.ChunkID] = true
			}
			for _, h := range more {
				if !seen[h.ChunkID] && len(out) < k {
					out = append(out, h)
				}
			}
		}
	}
	return out, nil
}

func (q *QA) fullText(ctx context.Context, question string, sc rag.Scope, k int) ([]ports.VectorHit, error) {
	all, ids, srcs := scopeArgs(sc)
	rows, err := q.s.Q.SearchChunksFTS(ctx, gen.SearchChunksFTSParams{Question: question, AllRepos: all, RepoIds: ids, Sources: srcs, Lim: int32(k)})
	if err != nil {
		return nil, fmt.Errorf("full-text search: %w", err)
	}
	out := make([]ports.VectorHit, len(rows))
	for i, r := range rows {
		out[i] = ports.VectorHit{ChunkID: r.ChunkID, Score: r.Rank}
	}
	return out, nil
}

var wordRE = regexp.MustCompile(`[\p{L}\p{N}_]{2,}`)

// anyWords rewrites a question as "w1 or w2 or …" for websearch_to_tsquery (stop words drop out there).
func anyWords(question string) string {
	return strings.Join(wordRE.FindAllString(question, 24), " or ")
}

// Overview finds material that describes repositories as a whole (READMEs, architecture docs, docs of
// entry points), for broad questions.
func (q *QA) Overview(ctx context.Context, sc rag.Scope, k int) ([]ports.VectorHit, error) {
	all, ids, srcs := scopeArgs(sc)
	rows, err := q.s.Q.OverviewChunks(ctx, gen.OverviewChunksParams{AllRepos: all, RepoIds: ids, Sources: srcs, Lim: int32(k)})
	if err != nil {
		return nil, fmt.Errorf("overview chunks: %w", err)
	}
	out := make([]ports.VectorHit, len(rows))
	for i, r := range rows {
		out[i] = ports.VectorHit{ChunkID: r.ChunkID, Score: r.Rank}
	}
	return out, nil
}

// Chunks loads live chunks by ID within the scope.
func (q *QA) Chunks(ctx context.Context, ids []string, sc rag.Scope) ([]ports.Chunk, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	all, rids, _ := scopeArgs(sc)
	rows, err := q.s.Q.ChunksByIDsScoped(ctx, gen.ChunksByIDsScopedParams{Ids: ids, AllRepos: all, RepoIds: rids})
	if err != nil {
		return nil, err
	}
	return toChunks(rows), nil
}

// SymbolNeighbors returns one-hop symbol neighbours.
func (q *QA) SymbolNeighbors(ctx context.Context, keys []string, limit int) ([]string, error) {
	return q.s.Q.SymbolNeighbors(ctx, gen.SymbolNeighborsParams{Keys: keys, Lim: int32(limit)})
}

// CachedAnswer returns a cached answer whose sources have not changed since (see GetCachedAnswer).
func (q *QA) CachedAnswer(ctx context.Context, key string) (rag.Answer, bool, error) {
	row, err := q.s.Q.GetCachedAnswer(ctx, key)
	if IsNoRows(err) {
		return rag.Answer{}, false, nil
	}
	if err != nil {
		return rag.Answer{}, false, err
	}
	a := rag.Answer{Text: row.Answer}
	_ = json.Unmarshal(row.Citations, &a.Citations)
	return a, true, nil
}

// PutAnswer caches an answer.
func (q *QA) PutAnswer(ctx context.Context, key string, a rag.Answer) error {
	b, _ := json.Marshal(a.Citations)
	ids := make([]string, 0, len(a.Citations))
	for _, c := range a.Citations {
		ids = append(ids, c.ChunkID)
	}
	return q.s.Q.PutCachedAnswer(ctx, gen.PutCachedAnswerParams{Key: key, Answer: a.Text, Citations: b, ChunkIds: ids})
}

// GCCache drops expired answers.
func (q *QA) GCCache(ctx context.Context) error {
	_, err := q.s.Q.GCAnswerCache(ctx)
	return err
}

// IndexVersion is the current index version.
func (q *QA) IndexVersion(ctx context.Context) (int64, error) { return q.s.Q.GetIndexVersion(ctx) }

// Thread is a Q&A conversation.
type Thread struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Scope     json.RawMessage `json:"scope"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	Messages  []Message       `json:"messages,omitempty"`
}

// Message is one turn.
type Message struct {
	ID        string          `json:"id"`
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	Citations json.RawMessage `json:"citations"`
	Model     string          `json:"model,omitempty"`
	Usage     map[string]any  `json:"usage,omitempty"`
	Cached    bool            `json:"cached"`
	Feedback  *string         `json:"feedback,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// CreateThread starts a thread.
func (q *QA) CreateThread(ctx context.Context, userID, title string, scope any) (string, error) {
	id := ports.NewID()
	b, _ := json.Marshal(scope)
	if len(title) > 120 {
		title = title[:117] + "..."
	}
	return id, q.s.Q.CreateThread(ctx, gen.CreateThreadParams{ID: id, UserID: userID, Title: title, Scope: b})
}

// GetThread loads a user's thread with its messages. Other users' threads are ErrNotFound.
func (q *QA) GetThread(ctx context.Context, userID, id string, withMessages bool) (Thread, error) {
	if !uuidRE.MatchString(id) {
		return Thread{}, ports.ErrNotFound
	}
	t, err := q.s.Q.GetThread(ctx, gen.GetThreadParams{ID: id, UserID: userID})
	if IsNoRows(err) {
		return Thread{}, ports.ErrNotFound
	}
	if err != nil {
		return Thread{}, err
	}
	out := Thread{ID: t.ID, Title: t.Title, Scope: t.Scope, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
	if !withMessages {
		return out, nil
	}
	rows, err := q.s.Q.ThreadMessages(ctx, id)
	if err != nil {
		return out, err
	}
	out.Messages = make([]Message, len(rows))
	for i, m := range rows {
		msg := Message{ID: m.ID, Role: string(m.Role), Content: m.Content, Citations: m.Citations, Model: m.Model, Cached: m.Cached, CreatedAt: m.CreatedAt}
		if m.Role == gen.QaRoleAssistant {
			msg.Usage = map[string]any{"input_tokens": m.InputTokens, "output_tokens": m.OutputTokens, "cost_usd": m.CostUsd}
		}
		if m.Feedback != nil {
			f := string(*m.Feedback)
			msg.Feedback = &f
		}
		out.Messages[i] = msg
	}
	return out, nil
}

// ListThreads pages a user's threads, most recent first.
func (q *QA) ListThreads(ctx context.Context, userID string, before *time.Time, limit int) ([]Thread, error) {
	rows, err := q.s.Q.ListThreads(ctx, gen.ListThreadsParams{UserID: userID, Before: before, Lim: int32(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]Thread, len(rows))
	for i, t := range rows {
		out[i] = Thread{ID: t.ID, Title: t.Title, Scope: t.Scope, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
	}
	return out, nil
}

// DeleteThread deletes a user's thread.
func (q *QA) DeleteThread(ctx context.Context, userID, id string) error {
	if !uuidRE.MatchString(id) {
		return ports.ErrNotFound
	}
	n, err := q.s.Q.DeleteThread(ctx, gen.DeleteThreadParams{ID: id, UserID: userID})
	if err == nil && n == 0 {
		return ports.ErrNotFound
	}
	return err
}

// AddExchange stores a question and its answer; it returns the assistant message ID.
func (q *QA) AddExchange(ctx context.Context, threadID, question string, a rag.Answer) (string, error) {
	aid := ports.NewID()
	cits, _ := json.Marshal(a.Citations)
	err := q.s.InTx(ctx, func(tq *gen.Queries, _ pgx.Tx) error {
		if err := tq.InsertMessage(ctx, gen.InsertMessageParams{ID: ports.NewID(), ThreadID: threadID, Role: gen.QaRoleUser, Content: question, Citations: []byte("[]")}); err != nil {
			return err
		}
		if err := tq.InsertMessage(ctx, gen.InsertMessageParams{ID: aid, ThreadID: threadID, Role: gen.QaRoleAssistant, Content: a.Text, Citations: cits,
			Provider: a.Provider, Model: a.Model, InputTokens: a.Usage.InputTokens, OutputTokens: a.Usage.OutputTokens, CostUsd: a.CostUSD, Cached: a.Cached}); err != nil {
			return err
		}
		return tq.TouchThread(ctx, threadID)
	})
	return aid, err
}

// SetFeedback records thumbs up/down on the caller's own assistant message.
func (q *QA) SetFeedback(ctx context.Context, userID, messageID, value, comment string) error {
	if !uuidRE.MatchString(messageID) {
		return ports.ErrNotFound
	}
	fb := gen.QaFeedback(value)
	n, err := q.s.Q.SetFeedback(ctx, gen.SetFeedbackParams{ID: messageID, UserID: userID, Feedback: &fb, Comment: comment})
	if err == nil && n == 0 {
		return ports.ErrNotFound
	}
	return err
}
