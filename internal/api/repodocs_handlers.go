package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/core/repodocs"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/queue"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// RepoDocsDeps serves Docs v2 documents.
type RepoDocsDeps struct {
	Auth  *auth.Service
	Store *store.RepoDocs
	Repos *store.Repos
	Queue *queue.Queue
	// Budget returns a repository's monthly docs cap and spend; SetCap changes the cap.
	Budget func(ctx context.Context, repoID string) (capUSD, spentUSD float64, err error)
	SetCap func(ctx context.Context, repoID string, capUSD float64) error
	// Estimate prices writing what is missing (a dry run).
	Estimate func(ctx context.Context, repoID string, full bool) (repodocs.Result, error)
	// Export opens a pull request with the documents as Markdown.
	Export func(ctx context.Context, repoID string) (ports.PullRequest, error)
}

// systemLinksFor keeps the links whose both ends the caller can read.
func systemLinksFor(links []repodocs.SystemLink, can func(string) bool) []repodocs.SystemLink {
	out := []repodocs.SystemLink{}
	for _, l := range links {
		if can(l.FromRepo) && can(l.ToRepo) {
			out = append(out, l)
		}
	}
	return out
}

type repoDocsHandlers struct{ d RepoDocsDeps }

// RepoDocsRoutes mounts /repo-docs and the per-repository docs actions.
func RepoDocsRoutes(d RepoDocsDeps) func(chi.Router) {
	h := &repoDocsHandlers{d: d}
	return func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleViewer))
			r.Get("/repo-docs", h.list)
			r.Get("/repo-docs/find", h.find)
			r.Get("/repo-docs/{id}", h.get)
			r.Get("/system", h.system)
		})
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleEditor))
			r.Post("/repos/{id}/docs/estimate", h.estimate)
			r.Get("/repos/{id}/docs/report", h.report)
		})
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleAdmin))
			r.Post("/repos/{id}/docs/write", h.write)
			r.Put("/repos/{id}/docs/budget", h.setBudget)
			r.Post("/repos/{id}/docs/export", h.export)
			r.Post("/system/write", h.writeSystem)
		})
	}
}

func (h *repoDocsHandlers) readable(w http.ResponseWriter, r *http.Request, repoID string) bool {
	sc, err := scopeOf(r, h.d.Auth)
	if err != nil {
		WriteErr(w, r, err)
		return false
	}
	if repoID == "" || !allows(sc, repoID) {
		WriteErr(w, r, ports.ErrNotFound) // unreadable repositories look missing
		return false
	}
	return true
}

// docSummary is a document without its sections (the navigation).
type docSummary struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	Key        string   `json:"key"`
	Title      string   `json:"title"`
	Group      string   `json:"group"`
	Order      int      `json:"order"`
	AtAGlance  string   `json:"at_a_glance"`
	Confidence float64  `json:"confidence"`
	Label      string   `json:"label"`
	Why        []string `json:"why,omitempty"`
	Changed    float64  `json:"changed,omitempty"`
	Status     string   `json:"status"`
	Error      string   `json:"error,omitempty"`
	// SourceSHA is the commit the document was written from.
	SourceSHA string `json:"source_sha"`
	UpdatedAt string `json:"updated_at"`
}

func (h *repoDocsHandlers) list(w http.ResponseWriter, r *http.Request) {
	repoID := r.URL.Query().Get("repo_id")
	if !h.readable(w, r, repoID) {
		return
	}
	docs, err := h.d.Store.Docs(r.Context(), repoID)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	items := make([]docSummary, 0, len(docs))
	for _, d := range docs {
		items = append(items, docSummary{ID: d.ID, Type: d.Type, Key: d.Key, Title: d.Title, Group: d.Group, Order: d.Order, AtAGlance: d.AtAGlance,
			Confidence: d.Confidence, Label: repodocs.Label(d.Confidence), Why: d.Why, Changed: d.Changed, Status: d.Status, Error: d.Error,
			SourceSHA: d.SourceSHA, UpdatedAt: d.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")})
	}
	out := map[string]any{"repo_id": repoID, "items": items}
	if h.d.Budget != nil {
		if c, s, err := h.d.Budget(r.Context(), repoID); err == nil {
			out["budget"] = map[string]float64{"cap_usd": c, "spent_usd": s}
		}
	}
	if h.d.Queue != nil {
		if jobs, _, err := h.d.Queue.List(r.Context(), queue.Filter{Type: ports.JobRepoDocs, RepoID: repoID, Limit: 1}); err == nil && len(jobs) > 0 {
			j := jobs[0]
			out["job"] = map[string]any{"id": j.ID, "status": j.Status, "progress": j.Progress, "error": j.Error, "updated_at": j.UpdatedAt, "result": j.Result}
		}
	}
	WriteJSON(w, http.StatusOK, out)
}

func (h *repoDocsHandlers) send(w http.ResponseWriter, r *http.Request, d repodocs.Doc) {
	if !h.readable(w, r, d.RepoID) {
		return
	}
	type section struct {
		repodocs.DocSection
		Label string `json:"label"`
	}
	secs := make([]section, len(d.Sections))
	for i, s := range d.Sections {
		secs[i] = section{DocSection: s, Label: repodocs.Label(s.Score)}
	}
	repoName := ""
	if rc, err := h.d.Repos.Get(r.Context(), d.RepoID); err == nil {
		repoName = rc.FullName
	}
	WriteJSON(w, http.StatusOK, map[string]any{"id": d.ID, "repo_id": d.RepoID, "repo": repoName, "type": d.Type, "key": d.Key, "title": d.Title,
		"group": d.Group, "at_a_glance": d.AtAGlance, "sections": secs, "gaps": d.Gaps, "confidence": d.Confidence, "label": repodocs.Label(d.Confidence),
		"why": d.Why, "calibrated": d.Calibrated, "changed": d.Changed, "source_sha": d.SourceSHA, "model": d.Model, "status": d.Status, "error": d.Error,
		"updated_at": d.UpdatedAt})
}

func (h *repoDocsHandlers) get(w http.ResponseWriter, r *http.Request) {
	d, err := h.d.Store.Doc(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	h.send(w, r, d)
}

func (h *repoDocsHandlers) find(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	repoID, typ, key := q.Get("repo_id"), q.Get("type"), q.Get("key")
	if key == "" {
		key = typ
	}
	if !h.readable(w, r, repoID) {
		return
	}
	docs, err := h.d.Store.Docs(r.Context(), repoID)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	for _, d := range docs {
		if d.Type == typ && d.Key == key {
			h.send(w, r, d)
			return
		}
	}
	WriteErr(w, r, ports.ErrNotFound)
}

func (h *repoDocsHandlers) estimate(w http.ResponseWriter, r *http.Request) {
	repoID := chi.URLParam(r, "id")
	if !h.readable(w, r, repoID) {
		return
	}
	if h.d.Estimate == nil {
		WriteErr(w, r, ports.ErrNotFound)
		return
	}
	full := r.URL.Query().Get("full") == "true"
	res, err := h.d.Estimate(r.Context(), repoID, full)
	if err != nil {
		WriteErr(w, r, &ports.ValidationError{Code: "ESTIMATE_FAILED", Message: err.Error()})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"would_write": res.WouldWrite, "documents": len(res.WouldWrite), "unchanged": res.Unchanged,
		"modules": res.Modules, "estimated_tokens": res.EstimatedTokens, "estimated_usd": res.EstimatedUSD})
}

// report summarises the repository's documents for review and prompt tuning (?text=true keeps the
// prose, ?format=md renders Markdown).
func (h *repoDocsHandlers) report(w http.ResponseWriter, r *http.Request) {
	repoID := chi.URLParam(r, "id")
	if !h.readable(w, r, repoID) {
		return
	}
	rc, err := h.d.Repos.Get(r.Context(), repoID)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	docs, err := h.d.Store.Docs(r.Context(), repoID)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	q := r.URL.Query()
	rep := repodocs.BuildReport(rc.FullName, docs, q.Get("text") == "true")
	if h.d.Budget != nil {
		if c, s, err := h.d.Budget(r.Context(), repoID); err == nil {
			rep.CapUSD, rep.SpentUSD = c, s
		}
	}
	if q.Get("format") == "md" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write([]byte(rep.Markdown()))
		return
	}
	WriteJSON(w, http.StatusOK, rep)
}

func (h *repoDocsHandlers) write(w http.ResponseWriter, r *http.Request) {
	repoID := chi.URLParam(r, "id")
	var in struct {
		Full bool     `json:"full"`
		Only []string `json:"only"`
	}
	if r.ContentLength > 0 {
		if err := decodeJSON(w, r, &in); err != nil {
			fail(w, r, err)
			return
		}
	}
	if _, err := h.d.Repos.Get(r.Context(), repoID); err != nil {
		WriteErr(w, r, err)
		return
	}
	payload, _ := json.Marshal(map[string]any{"repo_id": repoID, "full": in.Full, "only": in.Only, "retry_failed": true, "reason": "requested in the Hub"})
	job, _, err := h.d.Queue.Enqueue(r.Context(), ports.NewJob{Type: ports.JobRepoDocs, RepoID: repoID, SerialKey: "repo:" + repoID,
		DedupeKey: "repo-docs-manual:" + repoID, Payload: json.RawMessage(payload)})
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	_ = h.d.Auth.Audit(r.Context(), auth.FromContext(r.Context()), "repo_docs.write", "repo", repoID, map[string]any{"full": in.Full, "only": in.Only}, clientIP(r))
	WriteJSON(w, http.StatusAccepted, map[string]string{"job_id": job.ID})
}

func (h *repoDocsHandlers) setBudget(w http.ResponseWriter, r *http.Request) {
	repoID := chi.URLParam(r, "id")
	var in struct {
		CapUSD float64 `json:"cap_usd"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	if in.CapUSD < 0 || h.d.SetCap == nil {
		fail(w, r, errBadParam("cap_usd must be zero or more"))
		return
	}
	if err := h.d.SetCap(r.Context(), repoID, in.CapUSD); err != nil {
		WriteErr(w, r, err)
		return
	}
	_ = h.d.Auth.Audit(r.Context(), auth.FromContext(r.Context()), "repo_docs.budget", "repo", repoID, map[string]any{"cap_usd": in.CapUSD}, clientIP(r))
	WriteJSON(w, http.StatusOK, map[string]string{"cap_usd": strconv.FormatFloat(in.CapUSD, 'f', 2, 64)})
}

func (h *repoDocsHandlers) export(w http.ResponseWriter, r *http.Request) {
	repoID := chi.URLParam(r, "id")
	if h.d.Export == nil {
		WriteErr(w, r, ports.ErrNotFound)
		return
	}
	pr, err := h.d.Export(r.Context(), repoID)
	if err != nil {
		WriteErr(w, r, &ports.ValidationError{Code: "EXPORT_FAILED", Message: err.Error()})
		return
	}
	_ = h.d.Auth.Audit(r.Context(), auth.FromContext(r.Context()), "repo_docs.export", "repo", repoID, map[string]any{"pr": pr.Number}, clientIP(r))
	WriteJSON(w, http.StatusOK, map[string]any{"number": pr.Number, "url": pr.URL})
}

// ExportMarkdown renders documents as Markdown files for a repository, with citations linked to the code.
func ExportMarkdown(docs []repodocs.Doc, blobURL func(path string, line int) string, dir string) map[string]string {
	dir = strings.TrimSuffix(dir, "/")
	files := map[string]string{}
	name := func(d repodocs.Doc) string {
		if d.Type == "module" {
			return dir + "/modules/" + d.Key + ".md"
		}
		return dir + "/" + d.Type + ".md"
	}
	var index strings.Builder
	index.WriteString("<!-- dth:generated by DocTheRepo Hub. Edits are replaced on the next export. -->\n# Documentation\n\n")
	for _, d := range docs {
		if d.Status != "ok" {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "<!-- dth:generated by DocTheRepo Hub from %s. Edits are replaced on the next export. -->\n# %s\n\n", d.SourceSHA, d.Title)
		fmt.Fprintf(&b, "> %s\n\n", strings.ReplaceAll(d.AtAGlance, "\n", "\n> "))
		fmt.Fprintf(&b, "_Confidence: %s (%.0f%%)._\n\n", repodocs.Label(d.Confidence), d.Confidence*100)
		for _, s := range d.Sections {
			fmt.Fprintf(&b, "## %s\n\n%s\n\n", s.Title, linkCitations(s.Markdown, blobURL))
		}
		p := name(d)
		files[p] = b.String()
		rel := strings.TrimPrefix(p, dir+"/")
		fmt.Fprintf(&index, "- [%s](%s): %s\n", d.Title, rel, firstSentence(d.AtAGlance))
	}
	files[dir+"/README.md"] = index.String()
	return files
}

func linkCitations(md string, blobURL func(string, int) string) string {
	if blobURL == nil {
		return md
	}
	return citeLinkRE.ReplaceAllStringFunc(md, func(m string) string {
		sub := citeLinkRE.FindStringSubmatch(m)
		line, _ := strconv.Atoi(sub[2])
		return fmt.Sprintf("[%s:%s](%s)", sub[1], sub[2], blobURL(sub[1], line))
	})
}

func firstSentence(s string) string {
	if i := strings.Index(s, ". "); i > 0 {
		return s[:i+1]
	}
	return s
}

var citeLinkRE = regexp.MustCompile(`\[([A-Za-z0-9_./@+\-]+\.[A-Za-z0-9]+):(\d+)(?:-\d+)?\]`)

// LinkCitationsRelative links [path:line] citations to the code with relative links (up climbs from the
// document's directory to the repository root), which every git host renders.
func LinkCitationsRelative(md, up string) string {
	return citeLinkRE.ReplaceAllStringFunc(md, func(m string) string {
		sub := citeLinkRE.FindStringSubmatch(m)
		return fmt.Sprintf("[%s:%s](%s%s#L%s)", sub[1], sub[2], up, sub[1], sub[2])
	})
}

// system is the System architecture: how the repositories the caller can read talk to each other, and the
// written document when the caller can read every repository it covers.
func (h *repoDocsHandlers) system(w http.ResponseWriter, r *http.Request) {
	sc, err := scopeOf(r, h.d.Auth)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	all, err := h.d.Store.SystemLinks(r.Context())
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	can := func(id string) bool { return allows(sc, id) }
	links := systemLinksFor(all, can)
	type repo struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	seen := map[string]bool{}
	repos := []repo{}
	for _, l := range links {
		for _, x := range [][2]string{{l.FromRepo, l.FromName}, {l.ToRepo, l.ToName}} {
			if !seen[x[0]] {
				seen[x[0]] = true
				repos = append(repos, repo{x[0], x[1]})
			}
		}
	}
	out := map[string]any{"links": links, "repos": repos, "complete": len(links) == len(all)}
	in := repodocs.SystemInput{Links: links}
	for _, rp := range repos {
		in.Repos = append(in.Repos, repodocs.SystemRepo{ID: rp.ID, Name: rp.Name})
	}
	out["diagram"] = repodocs.SystemDiagram(in)
	// The write-up spans every linked repository, so only someone who can read all of them sees it.
	if len(links) > 0 && len(links) == len(all) {
		if d, err := h.d.Store.SystemDoc(r.Context()); err == nil {
			secs := make([]map[string]any, len(d.Sections))
			for i, s := range d.Sections {
				secs[i] = map[string]any{"key": s.Key, "title": s.Title, "markdown": s.Markdown, "score": s.Score, "label": repodocs.Label(s.Score), "why": s.Why}
			}
			out["doc"] = map[string]any{"id": d.ID, "title": d.Title, "at_a_glance": d.AtAGlance, "sections": secs, "gaps": d.Gaps, "confidence": d.Confidence,
				"label": repodocs.Label(d.Confidence), "why": d.Why, "status": d.Status, "error": d.Error, "updated_at": d.UpdatedAt}
		}
	}
	if h.d.Queue != nil {
		if jobs, _, err := h.d.Queue.List(r.Context(), queue.Filter{Type: ports.JobSystemDocs, Limit: 1}); err == nil && len(jobs) > 0 {
			out["job"] = map[string]any{"id": jobs[0].ID, "status": jobs[0].Status, "error": jobs[0].Error, "updated_at": jobs[0].UpdatedAt}
		}
	}
	WriteJSON(w, http.StatusOK, out)
}

func (h *repoDocsHandlers) writeSystem(w http.ResponseWriter, r *http.Request) {
	payload, _ := json.Marshal(map[string]any{"full": true, "reason": "requested in the Hub"})
	job, _, err := h.d.Queue.Enqueue(r.Context(), ports.NewJob{Type: ports.JobSystemDocs, SerialKey: "system", DedupeKey: "system-docs-manual", Payload: json.RawMessage(payload)})
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	_ = h.d.Auth.Audit(r.Context(), auth.FromContext(r.Context()), "system_docs.write", "system", "system", nil, clientIP(r))
	WriteJSON(w, http.StatusAccepted, map[string]string{"job_id": job.ID})
}
