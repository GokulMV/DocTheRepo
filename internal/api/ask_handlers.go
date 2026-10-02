package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/rag"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// AskPerMinute is the per-user question rate (plan § 7.3).
const AskPerMinute = 30

// HistoryTurns is how many previous messages a follow-up question carries.
const HistoryTurns = 6

// AskRoutes mounts /ask, /threads, and /messages.
func AskRoutes(eng *rag.Engine, qa *store.QA, svc *auth.Service) func(chi.Router) {
	h := &askHandlers{eng: eng, qa: qa, auth: svc, limits: map[string]*rate.Limiter{}}
	return func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleViewer))
			r.Post("/ask", h.ask)
			r.Get("/threads", h.listThreads)
			r.Get("/threads/{id}", h.getThread)
			r.Delete("/threads/{id}", h.deleteThread)
			r.Post("/messages/{id}/feedback", h.feedback)
		})
	}
}

type askHandlers struct {
	eng    *rag.Engine
	qa     *store.QA
	auth   *auth.Service
	mu     sync.Mutex
	limits map[string]*rate.Limiter
}

func (h *askHandlers) allow(userID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	l, ok := h.limits[userID]
	if !ok {
		l = rate.NewLimiter(rate.Every(time.Minute/AskPerMinute), 10)
		h.limits[userID] = l
	}
	return l.Allow()
}

type askRequest struct {
	Question string  `json:"question"`
	ThreadID *string `json:"thread_id"`
	Scope    struct {
		RepoIDs []string `json:"repo_ids"`
		Include []string `json:"include"`
	} `json:"scope"`
}

type askResponse struct {
	ThreadID  string         `json:"thread_id"`
	MessageID string         `json:"message_id"`
	Answer    string         `json:"answer"`
	Citations []rag.Citation `json:"citations"`
	Cached    bool           `json:"cached"`
	// Investigated: retrieval found too little, so the model looked further before answering.
	Investigated bool           `json:"investigated,omitempty"`
	Usage        map[string]any `json:"usage"`
}

// sse writes server-sent events.
type sse struct {
	w http.ResponseWriter
	f http.Flusher
}

func (s *sse) event(name string, v any) {
	b, _ := json.Marshal(v)
	fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, b)
	s.f.Flush()
}

func (h *askHandlers) ask(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	if !h.allow(p.UserID) {
		w.Header().Set("Retry-After", "2")
		WriteError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", fmt.Sprintf("at most %d questions per minute", AskPerMinute), nil)
		return
	}
	var in askRequest
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	q := rag.Normalize(in.Question)
	if q == "" || len(q) > 4000 {
		WriteError(w, r, http.StatusBadRequest, "QUESTION_EMPTY", rag.ErrEmptyQuestion.Error(), nil)
		return
	}
	scope, err := h.auth.RepoScope(r.Context(), p)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	ids, all, ok := scope.Restrict(in.Scope.RepoIDs)
	if !ok {
		WriteError(w, r, http.StatusForbidden, "SCOPE_FORBIDDEN", "the scope includes repositories you cannot read", nil)
		return
	}
	sources, err := rag.Sources(in.Scope.Include)
	if err != nil {
		fail(w, r, errBadParam(err.Error()))
		return
	}
	var history []ports.ChatMessage
	threadID := ""
	if in.ThreadID != nil && *in.ThreadID != "" {
		t, err := h.qa.GetThread(r.Context(), p.UserID, *in.ThreadID, true)
		if err != nil {
			WriteErr(w, r, err)
			return
		}
		threadID = t.ID
		msgs := t.Messages
		if len(msgs) > HistoryTurns {
			msgs = msgs[len(msgs)-HistoryTurns:]
		}
		for _, m := range msgs {
			history = append(history, ports.ChatMessage{Role: m.Role, Content: m.Content})
		}
	}
	query := rag.Query{Question: q, Scope: rag.Scope{All: all, RepoIDs: ids, Sources: sources}, History: history, UserID: p.UserID}

	var stream *sse
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		f, ok := w.(http.Flusher)
		if !ok {
			WriteError(w, r, http.StatusNotAcceptable, "STREAMING_UNSUPPORTED", "streaming is not supported here", nil)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		stream = &sse{w: w, f: f}
		query.OnDelta = func(t string) { stream.event("delta", map[string]string{"text": t}) }
		query.OnStatus = func(st rag.Status) { stream.event("status", st) }
		query.OnReset = func() { stream.event("reset", map[string]any{}) }
	}
	ans, err := h.eng.Ask(r.Context(), query)
	if err != nil {
		h.askErr(w, r, stream, err)
		return
	}
	if threadID == "" {
		if threadID, err = h.qa.CreateThread(r.Context(), p.UserID, q, in.Scope); err != nil {
			h.askErr(w, r, stream, err)
			return
		}
	}
	mid, err := h.qa.AddExchange(r.Context(), threadID, q, ans)
	if err != nil {
		h.askErr(w, r, stream, err)
		return
	}
	resp := askResponse{ThreadID: threadID, MessageID: mid, Answer: ans.Text, Citations: ans.Citations, Cached: ans.Cached, Investigated: ans.Investigated,
		Usage: map[string]any{"input_tokens": ans.Usage.InputTokens, "output_tokens": ans.Usage.OutputTokens, "cost_usd": ans.CostUSD}}
	if stream != nil {
		for _, c := range ans.Citations {
			stream.event("citation", c)
		}
		stream.event("done", resp)
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}

func (h *askHandlers) askErr(w http.ResponseWriter, r *http.Request, stream *sse, err error) {
	status, code, msg := http.StatusInternalServerError, "INTERNAL_ERROR", "internal error"
	var sb *ports.SpendBlockedError
	switch {
	case errors.Is(err, llmgateway.ErrNoRoute):
		status, code, msg = http.StatusServiceUnavailable, "NO_LLM_ROUTE", "no LLM provider is configured for Q&A; add one under Providers & Routing"
	case errors.As(err, &sb):
		status, code, msg = http.StatusPaymentRequired, "SPEND_BLOCKED", sb.Error()
	case errors.Is(err, ports.ErrNotFound):
		status, code, msg = http.StatusNotFound, "NOT_FOUND", "thread not found"
	case errors.Is(err, rag.ErrEmptyQuestion):
		status, code, msg = http.StatusBadRequest, "QUESTION_EMPTY", err.Error()
	default:
		if _, ok := ports.AsTransient(err); ok {
			status, code, msg = http.StatusServiceUnavailable, "UPSTREAM_UNAVAILABLE", "the LLM provider is temporarily unavailable; retry shortly"
		}
	}
	if status >= 500 {
		loggerFor(r).Error("ask failed", "err", err)
	}
	if stream != nil {
		stream.event("error", ErrorBody{Error: ErrorDetail{Code: code, Message: msg, CorrelationID: correlationFor(r)}})
		return
	}
	WriteError(w, r, status, code, msg, nil)
}

func (h *askHandlers) listThreads(w http.ResponseWriter, r *http.Request) {
	var cur struct {
		Before *time.Time `json:"b"`
	}
	limit, err := pageParams(r, &cur)
	if err != nil {
		fail(w, r, err)
		return
	}
	ts, err := h.qa.ListThreads(r.Context(), auth.FromContext(r.Context()).UserID, cur.Before, limit)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, newPage(ts, limit, func(t store.Thread) any { return map[string]any{"b": t.UpdatedAt} }))
}

func (h *askHandlers) getThread(w http.ResponseWriter, r *http.Request) {
	t, err := h.qa.GetThread(r.Context(), auth.FromContext(r.Context()).UserID, chi.URLParam(r, "id"), true)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, t)
}

func (h *askHandlers) deleteThread(w http.ResponseWriter, r *http.Request) {
	if err := h.qa.DeleteThread(r.Context(), auth.FromContext(r.Context()).UserID, chi.URLParam(r, "id")); err != nil {
		WriteErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *askHandlers) feedback(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Value   string `json:"value"`
		Comment string `json:"comment"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	if in.Value != "up" && in.Value != "down" {
		fail(w, r, errBadParam("value must be up or down"))
		return
	}
	if len(in.Comment) > 2000 {
		fail(w, r, errBadParam("comment is at most 2000 characters"))
		return
	}
	if err := h.qa.SetFeedback(r.Context(), auth.FromContext(r.Context()).UserID, chi.URLParam(r, "id"), in.Value, in.Comment); err != nil {
		WriteErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
