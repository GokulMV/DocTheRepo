package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/core/knownissues"
	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/suggest"
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// IssueDeps are the Inbox and known-issue collaborators (plan § 7.5).
type IssueDeps struct {
	Auth        *auth.Service
	Issues      *store.Issues
	KnownIssues *store.KnownIssues
	Suggestions *store.Suggestions
	Suggest     *suggest.Service
	Queue       Enqueuer
	// ReloadRules recompiles the live matcher after a rule change (so it applies to the next event).
	ReloadRules func(ctx context.Context) error
}

// IssueRoutes mounts the Inbox, known issues, and suggestions.
func IssueRoutes(d IssueDeps) func(chi.Router) {
	h := &issueHandlers{d: d}
	return func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleViewer))
			r.Get("/issues", h.list)
			r.Get("/issues/{id}", h.get)
			r.Get("/known-issues", h.listKnown)
			r.Get("/known-issues/{id}", h.getKnown)
		})
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleEditor))
			r.Patch("/issues/{id}", h.update)
			r.Post("/issues/{id}/decode", h.decode)
			r.Post("/issues/{id}/mark-known", h.markKnown)
			r.Post("/known-issues", h.createKnown)
			r.Patch("/known-issues/{id}", h.updateKnown)
			r.Delete("/known-issues/{id}", h.deleteKnown)
			r.Post("/known-issues/test", h.testRule)
			r.Post("/known-issues/from-text", h.fromText)
			r.Get("/known-issues/suggestions", h.listSuggestions)
			r.Post("/known-issues/suggestions/{id}/{decision:accept|reject}", h.decide)
		})
	}
}

type issueHandlers struct{ d IssueDeps }

func (h *issueHandlers) audit(r *http.Request, action, typ, id string, details any) {
	_ = h.d.Auth.Audit(r.Context(), auth.FromContext(r.Context()), action, typ, id, details, clientIP(r))
}

func (h *issueHandlers) reload(r *http.Request) {
	if h.d.ReloadRules != nil {
		if err := h.d.ReloadRules(r.Context()); err != nil {
			observability.Logger(r.Context()).Warn("reload known-issue rules failed", "err", err)
		}
	}
}

var severities = map[string]bool{"info": true, "warning": true, "error": true, "critical": true}
var issueStatusFilter = map[string]bool{"new": true, "decoded": true, "suppressed": true, "acknowledged": true, "resolved": true, "regressed": true}

func (h *issueHandlers) list(w http.ResponseWriter, r *http.Request) {
	sc, err := scopeOf(r, h.d.Auth)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	var cur struct {
		Seen time.Time `json:"t"`
		ID   string    `json:"i"`
	}
	limit, err := pageParams(r, &cur)
	if err != nil {
		fail(w, r, err)
		return
	}
	q := r.URL.Query()
	f := store.IssueFilter{Status: q.Get("status"), Source: q.Get("source"), Service: q.Get("service"), Environment: q.Get("env"),
		Severity: q.Get("severity"), Kind: q.Get("kind"), Q: q.Get("q"), AllRepos: sc.All, RepoIDs: sc.RepoIDs, Limit: limit}
	if f.Status != "" && !issueStatusFilter[f.Status] {
		fail(w, r, errBadParam("unknown status"))
		return
	}
	if f.Severity != "" && !severities[f.Severity] {
		fail(w, r, errBadParam("severity must be info, warning, error, or critical"))
		return
	}
	if s := q.Get("since"); s != "" {
		t, err := parseSince(s)
		if err != nil {
			fail(w, r, err)
			return
		}
		f.Since = t
	}
	if cur.ID != "" {
		f.BeforeSeen, f.BeforeID = &cur.Seen, cur.ID
	}
	items, err := h.d.Issues.List(r.Context(), f)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, newPage(items, limit, func(last store.IssueRow) any {
		return map[string]any{"t": last.LastSeen, "i": last.ID}
	}))
}

// parseSince accepts RFC 3339 or a duration back from now (24h, 7d).
func parseSince(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if strings.HasSuffix(s, "d") {
		if d, err := time.ParseDuration(strings.TrimSuffix(s, "d") + "h"); err == nil {
			return time.Now().Add(-24 * d), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return time.Now().Add(-d), nil
	}
	return time.Time{}, errBadParam("since must be RFC 3339 or a duration such as 24h or 7d")
}

func (h *issueHandlers) issueID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "id")
	if !uuidLike(id) {
		WriteErr(w, r, ports.ErrNotFound)
		return "", false
	}
	return id, true
}

// visible loads an issue the caller may read (404 otherwise, never 403: existence is not disclosed).
func (h *issueHandlers) visible(w http.ResponseWriter, r *http.Request) (store.IssueDetail, bool) {
	id, ok := h.issueID(w, r)
	if !ok {
		return store.IssueDetail{}, false
	}
	sc, err := scopeOf(r, h.d.Auth)
	if err != nil {
		WriteErr(w, r, err)
		return store.IssueDetail{}, false
	}
	det, err := h.d.Issues.Get(r.Context(), id, sc.All, sc.RepoIDs)
	if err != nil {
		WriteErr(w, r, err)
		return store.IssueDetail{}, false
	}
	return det, true
}

func (h *issueHandlers) get(w http.ResponseWriter, r *http.Request) {
	if det, ok := h.visible(w, r); ok {
		WriteJSON(w, http.StatusOK, det)
	}
}

func (h *issueHandlers) update(w http.ResponseWriter, r *http.Request) {
	det, ok := h.visible(w, r)
	if !ok {
		return
	}
	var in struct {
		Status         string  `json:"status"`
		AssigneeUserID *string `json:"assignee_user_id"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	assignee := ""
	if in.AssigneeUserID != nil {
		assignee = *in.AssigneeUserID
		if assignee == "" {
			assignee = "-"
		} else if !uuidLike(assignee) {
			fail(w, r, errBadParam("assignee_user_id must be a user ID"))
			return
		}
	}
	if err := h.d.Issues.Update(r.Context(), det.ID, in.Status, assignee); err != nil {
		WriteErr(w, r, err)
		return
	}
	h.audit(r, "issue.update", "issue", det.ID, in)
	if det, ok = h.visible(w, r); ok {
		WriteJSON(w, http.StatusOK, det.IssueRow)
	}
}

func (h *issueHandlers) decode(w http.ResponseWriter, r *http.Request) {
	det, ok := h.visible(w, r)
	if !ok {
		return
	}
	var in struct {
		Force bool `json:"force"`
	}
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &in); err != nil {
			fail(w, r, err)
			return
		}
	}
	key := "decode:" + det.ID
	if in.Force {
		key = "decode-force:" + det.ID // a forced request must not collapse onto a queued reuse check
	}
	job, _, err := h.d.Queue.Enqueue(r.Context(), ports.NewJob{Type: ports.JobDecodeIssue, DedupeKey: key,
		Payload: map[string]any{"issue_id": det.ID, "force": in.Force}, CorrelationID: correlationFor(r)})
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	h.audit(r, "issue.decode", "issue", det.ID, in)
	WriteJSON(w, http.StatusAccepted, map[string]string{"job_id": job.ID})
}

type knownIn struct {
	Title       *string            `json:"title"`
	Description *string            `json:"description"`
	Reason      *string            `json:"reason"`
	Match       *knownissues.Match `json:"match"`
	Action      *string            `json:"action"`
	Enabled     *bool              `json:"enabled"`
	ExpiresAt   *time.Time         `json:"expires_at"`
	TicketURL   *string            `json:"ticket_url"`
	// mark-known only:
	MatchOverride *knownissues.Match `json:"match_override"`
}

func (in knownIn) apply(k *store.KnownIssue) {
	if in.Title != nil {
		k.Title = *in.Title
	}
	if in.Description != nil {
		k.Description = *in.Description
	}
	if in.Reason != nil {
		k.Reason = *in.Reason
	}
	if in.Match != nil {
		k.Match = *in.Match
	}
	if in.Action != nil {
		k.Action = *in.Action
	}
	if in.Enabled != nil {
		k.Enabled = *in.Enabled
	}
	if in.ExpiresAt != nil {
		k.ExpiresAt = in.ExpiresAt
	}
	if in.TicketURL != nil {
		k.TicketURL = *in.TicketURL
	}
}

func (h *issueHandlers) markKnown(w http.ResponseWriter, r *http.Request) {
	det, ok := h.visible(w, r)
	if !ok {
		return
	}
	var in knownIn
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	k := store.KnownIssue{Title: det.Title, Reason: "known_bug", OwnerUserID: auth.FromContext(r.Context()).UserID}
	in.apply(&k)
	if det.Decode != nil {
		k.Explanation = det.Decode.Summary
	}
	id, err := h.d.Issues.MarkKnown(r.Context(), det.ID, k, in.MatchOverride)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	h.audit(r, "issue.mark_known", "issue", det.ID, map[string]string{"known_issue_id": id})
	h.reload(r)
	ki, _ := h.d.KnownIssues.Get(r.Context(), id)
	WriteJSON(w, http.StatusCreated, ki)
}

func (h *issueHandlers) listKnown(w http.ResponseWriter, r *http.Request) {
	ks, err := h.d.KnownIssues.List(r.Context(), 1000)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": ks})
}

func (h *issueHandlers) knownID(w http.ResponseWriter, r *http.Request) (store.KnownIssue, bool) {
	id := chi.URLParam(r, "id")
	if !uuidLike(id) {
		WriteErr(w, r, ports.ErrNotFound)
		return store.KnownIssue{}, false
	}
	k, err := h.d.KnownIssues.Get(r.Context(), id)
	if err != nil {
		WriteErr(w, r, err)
		return store.KnownIssue{}, false
	}
	return k, true
}

func (h *issueHandlers) getKnown(w http.ResponseWriter, r *http.Request) {
	if k, ok := h.knownID(w, r); ok {
		WriteJSON(w, http.StatusOK, k)
	}
}

func (h *issueHandlers) createKnown(w http.ResponseWriter, r *http.Request) {
	var in knownIn
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	k := store.KnownIssue{Action: "suppress", Enabled: true, Source: "manual", OwnerUserID: auth.FromContext(r.Context()).UserID}
	in.apply(&k)
	id, err := h.d.KnownIssues.Create(r.Context(), k)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	h.audit(r, "known_issue.create", "known_issue", id, map[string]any{"title": k.Title, "match": k.Match})
	h.reload(r)
	k, _ = h.d.KnownIssues.Get(r.Context(), id)
	WriteJSON(w, http.StatusCreated, k)
}

func (h *issueHandlers) updateKnown(w http.ResponseWriter, r *http.Request) {
	k, ok := h.knownID(w, r)
	if !ok {
		return
	}
	var in knownIn
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	in.apply(&k)
	if err := h.d.KnownIssues.Update(r.Context(), k); err != nil {
		WriteErr(w, r, err)
		return
	}
	h.audit(r, "known_issue.update", "known_issue", k.ID, in)
	h.reload(r)
	k, _ = h.d.KnownIssues.Get(r.Context(), k.ID)
	WriteJSON(w, http.StatusOK, k)
}

func (h *issueHandlers) deleteKnown(w http.ResponseWriter, r *http.Request) {
	k, ok := h.knownID(w, r)
	if !ok {
		return
	}
	if err := h.d.KnownIssues.Delete(r.Context(), k.ID); err != nil {
		WriteErr(w, r, err)
		return
	}
	h.audit(r, "known_issue.delete", "known_issue", k.ID, map[string]string{"title": k.Title})
	h.reload(r)
	w.WriteHeader(http.StatusNoContent)
}

func (h *issueHandlers) testRule(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Match knownissues.Match `json:"match"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	res, err := h.d.Suggest.Test(r.Context(), in.Match)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, res)
}

func (h *issueHandlers) fromText(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text     string   `json:"text"`
		Services []string `json:"services"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	res, err := h.d.Suggest.FromText(r.Context(), in.Text, in.Services, auth.FromContext(r.Context()).UserID)
	switch {
	case errors.Is(err, suggest.ErrEmptyText):
		fail(w, r, errBadParam("text is required"))
		return
	case errors.Is(err, llmgateway.ErrNoRoute):
		WriteError(w, r, http.StatusConflict, "NO_ROUTE", "configure a model for the suggest feature first", nil)
		return
	case err != nil:
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, res)
}

func (h *issueHandlers) listSuggestions(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status != "" && status != "pending" && status != "accepted" && status != "rejected" {
		fail(w, r, errBadParam("status must be pending, accepted, or rejected"))
		return
	}
	items, err := h.d.Suggestions.ListSuggestions(r.Context(), status, 200)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	if items == nil {
		items = []store.Suggestion{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *issueHandlers) decide(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !uuidLike(id) {
		WriteErr(w, r, ports.ErrNotFound)
		return
	}
	var in struct {
		Title  string `json:"title"`
		Reason string `json:"reason"`
	}
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &in); err != nil {
			fail(w, r, err)
			return
		}
	}
	accept := chi.URLParam(r, "decision") == "accept"
	s, err := h.d.Suggestions.DecideSuggestion(r.Context(), id, auth.FromContext(r.Context()).UserID, accept, in.Title, in.Reason)
	if errors.Is(err, store.ErrAlreadyDecided) {
		WriteError(w, r, http.StatusConflict, "ALREADY_DECIDED", "this suggestion was already accepted or rejected", nil)
		return
	}
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	h.audit(r, "known_issue.suggestion."+s.Status, "suggestion", id, map[string]string{"known_issue_id": s.KnownIssueID})
	if accept {
		h.reload(r)
	}
	WriteJSON(w, http.StatusOK, s)
}
