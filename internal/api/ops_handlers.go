package api

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/queue"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// Jobs reads and replays jobs.
type Jobs interface {
	Enqueuer
	Get(ctx context.Context, id string) (ports.Job, error)
	List(ctx context.Context, f queue.Filter) ([]ports.Job, int64, error)
}

// OpsRoutes mounts § 7.7: jobs, activity, analytics.
func OpsRoutes(jobs Jobs, b *store.Browse, svc *auth.Service) func(chi.Router) {
	h := &opsHandlers{jobs: jobs, b: b, auth: svc}
	return func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleViewer))
			r.Get("/jobs", h.listJobs)
			r.Get("/jobs/{id}", h.getJob)
			r.Get("/activity", h.activity)
			r.Get("/analytics/usage", h.usage)
			r.Get("/analytics/savings", h.savings)
			r.Get("/analytics/pipeline", h.pipeline)
		})
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleAdmin))
			r.Post("/jobs/{id}/retry", h.retry)
			r.Get("/analytics/connectors", h.connectors)
		})
	}
}

type opsHandlers struct {
	jobs Jobs
	b    *store.Browse
	auth *auth.Service
}

func (h *opsHandlers) listJobs(w http.ResponseWriter, r *http.Request) {
	sc, err := scopeOf(r, h.auth)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	var cur struct {
		Seq int64 `json:"s"`
	}
	limit, err := pageParams(r, &cur)
	if err != nil {
		fail(w, r, err)
		return
	}
	q := r.URL.Query()
	st := ports.JobStatus(q.Get("status"))
	if st != "" && !slices.Contains(ports.AllJobStatuses, st) {
		fail(w, r, errBadParam("unknown status"))
		return
	}
	repoID := q.Get("repo_id")
	if repoID != "" && !allows(sc, repoID) {
		WriteErr(w, r, ports.ErrNotFound)
		return
	}
	js, next, err := h.jobs.List(r.Context(), queue.Filter{Status: st, Type: ports.JobType(q.Get("type")), RepoID: repoID, BeforeSeq: cur.Seq, Limit: limit})
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	out := make([]ports.Job, 0, len(js))
	for _, j := range js {
		if j.RepoID == "" || allows(sc, j.RepoID) {
			out = append(out, j)
		}
	}
	page := Page[ports.Job]{Items: out}
	if next > 0 {
		b, _ := json.Marshal(map[string]int64{"s": next})
		c := encodeCursor(b)
		page.NextCursor = &c
	}
	WriteJSON(w, http.StatusOK, page)
}

func (h *opsHandlers) job(r *http.Request) (ports.Job, error) {
	sc, err := scopeOf(r, h.auth)
	if err != nil {
		return ports.Job{}, err
	}
	id := chi.URLParam(r, "id")
	if !uuidLike(id) {
		return ports.Job{}, ports.ErrNotFound
	}
	j, err := h.jobs.Get(r.Context(), id)
	if err == nil && j.RepoID != "" && !allows(sc, j.RepoID) {
		return ports.Job{}, ports.ErrNotFound
	}
	return j, err
}

func (h *opsHandlers) getJob(w http.ResponseWriter, r *http.Request) {
	j, err := h.job(r)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, j)
}

// retry replays a finished job. A spend-blocked job needs override_ceiling, which is audited.
func (h *opsHandlers) retry(w http.ResponseWriter, r *http.Request) {
	var in struct {
		OverrideCeiling bool `json:"override_ceiling"`
	}
	if r.ContentLength > 0 {
		if err := decodeJSON(w, r, &in); err != nil {
			fail(w, r, err)
			return
		}
	}
	j, err := h.job(r)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	if !j.Status.Terminal() || j.Status == ports.JobDone {
		WriteError(w, r, http.StatusConflict, "NOT_RETRYABLE", "only failed, dead, aborted, spend-blocked, or needs-human jobs can be retried", nil)
		return
	}
	if j.Status == ports.JobSpendBlocked && !in.OverrideCeiling {
		WriteError(w, r, http.StatusForbidden, "CEILING_OVERRIDE_REQUIRED", "this job was blocked by a spend ceiling; retry with override_ceiling: true or raise the limit", nil)
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(j.Payload, &payload); err != nil || payload == nil {
		payload = map[string]any{}
	}
	if in.OverrideCeiling {
		payload["override_ceiling"] = true
	}
	serial := j.SerialKey
	nj, _, err := h.jobs.Enqueue(r.Context(), ports.NewJob{Type: j.Type, RepoID: j.RepoID, SerialKey: serial, Payload: payload,
		ReplayedFrom: j.ID, CorrelationID: correlationFor(r)})
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	action := "job.retry"
	if in.OverrideCeiling {
		action = "job.retry.override_ceiling"
	}
	_ = h.auth.Audit(r.Context(), auth.FromContext(r.Context()), action, "job", j.ID, map[string]string{"new_job_id": nj.ID}, clientIP(r))
	WriteJSON(w, http.StatusAccepted, map[string]string{"job_id": nj.ID})
}

func timeRange(r *http.Request, def time.Duration) (time.Time, time.Time, error) {
	to, from := time.Now(), time.Now().Add(-def)
	var err error
	if v := r.URL.Query().Get("from"); v != "" {
		if from, err = time.Parse(time.RFC3339, v); err != nil {
			return from, to, errBadParam("from must be RFC 3339")
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if to, err = time.Parse(time.RFC3339, v); err != nil {
			return from, to, errBadParam("to must be RFC 3339")
		}
	}
	if !from.Before(to) || to.Sub(from) > 400*24*time.Hour {
		return from, to, errBadParam("from must be before to, within 400 days")
	}
	return from, to, nil
}

func (h *opsHandlers) activity(w http.ResponseWriter, r *http.Request) {
	sc, err := scopeOf(r, h.auth)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	var cur struct {
		Before *time.Time `json:"b"`
	}
	limit, err := pageParams(r, &cur)
	if err != nil {
		fail(w, r, err)
		return
	}
	since := time.Now().Add(-7 * 24 * time.Hour)
	if v := r.URL.Query().Get("since"); v != "" {
		if since, err = time.Parse(time.RFC3339, v); err != nil {
			fail(w, r, errBadParam("since must be RFC 3339"))
			return
		}
	}
	before := time.Now().Add(time.Second)
	if cur.Before != nil {
		before = *cur.Before
	}
	kinds := map[string]bool{}
	for _, k := range strings.Split(r.URL.Query().Get("types"), ",") {
		if k = strings.TrimSpace(k); k != "" {
			kinds[k] = true
		}
	}
	p := auth.FromContext(r.Context())
	items, err := h.b.Activity(r.Context(), kinds, since, before, sc, p.Role.AtLeast(auth.RoleAdmin), limit)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, newPage(items, limit, func(a store.Activity) any { return map[string]any{"b": a.At} }))
}

// usage: viewers see their own calls; admins see everyone's.
func (h *opsHandlers) usage(w http.ResponseWriter, r *http.Request) {
	from, to, err := timeRange(r, 7*24*time.Hour)
	if err != nil {
		fail(w, r, err)
		return
	}
	q := r.URL.Query()
	gran := q.Get("granularity")
	if gran == "" {
		gran = "day"
	}
	p := auth.FromContext(r.Context())
	userID := ""
	if !p.Role.AtLeast(auth.RoleAdmin) {
		userID = p.UserID
	}
	rep, err := h.b.Usage(r.Context(), q.Get("group_by"), gran, from, to, userID)
	if err != nil {
		fail(w, r, errBadParam(err.Error()))
		return
	}
	WriteJSON(w, http.StatusOK, rep)
}

func (h *opsHandlers) savings(w http.ResponseWriter, r *http.Request) {
	from, to, err := timeRange(r, 30*24*time.Hour)
	if err != nil {
		fail(w, r, err)
		return
	}
	by, total, err := h.b.Savings(r.Context(), from, to)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"by_kind": by, "total": total})
}

func (h *opsHandlers) pipeline(w http.ResponseWriter, r *http.Request) {
	sc, err := scopeOf(r, h.auth)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	from, to, err := timeRange(r, 7*24*time.Hour)
	if err != nil {
		fail(w, r, err)
		return
	}
	rep, err := h.b.Pipeline(r.Context(), from, to, sc)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, rep)
}

func (h *opsHandlers) connectors(w http.ResponseWriter, r *http.Request) {
	cs, err := h.b.ConnectorHealth(r.Context())
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": cs})
}
