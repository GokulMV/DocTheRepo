package api

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/registry"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/pipeline"
	"github.com/GokulMV/DocTheRepo/internal/ingest"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// Enqueuer inserts jobs.
type Enqueuer interface {
	Enqueue(ctx context.Context, nj ports.NewJob) (ports.Job, bool, error)
}

// AdminDeps are the collaborators of the repository, connector, provider, routing, and spend APIs.
type AdminDeps struct {
	Auth       *auth.Service
	Repos      *store.Repos
	Connectors *store.Connectors
	Providers  *store.Providers
	Routes     *store.Routes
	Browse     *store.Browse
	Queue      Enqueuer
	// Seal encrypts a secret bound to aad (secrets.Box.Seal); ProviderAAD binds provider keys.
	Seal        func(ctx context.Context, plaintext, aad []byte) ([]byte, error)
	ProviderAAD func(id string) []byte
	// ProviderKinds lists the registered LLM provider kinds.
	ProviderKinds []string
	// TestProvider pings a stored provider with a model.
	TestProvider func(ctx context.Context, id, model string) (time.Duration, error)
	// InvalidateProvider drops a cached provider adapter after an edit.
	InvalidateProvider func(id string)
	// Host returns a git connector's adapter; InvalidateHost drops it after an edit.
	Host           func(ctx context.Context, connectorID string) (ports.CodeHost, error)
	InvalidateHost func(id string)
	// HostAny builds a fresh adapter for any connector, enabled or not (to suspend or uninstall a GitHub App).
	HostAny func(ctx context.Context, connectorID string) (ports.CodeHost, error)
	// RegisterWebhook points the git host's push webhook for repo at this Hub; it reports "registered", or
	// "skipped: …" when the connector polls. Best effort: a failure never blocks tracking the repository.
	RegisterWebhook func(ctx context.Context, connectorID, repo string) (string, error)
	// GenerateDocs queues documentation of every file in a repository (a full code_push).
	GenerateDocs func(ctx context.Context, repoID, reason string) (jobID string, err error)
	// ReposWithoutDocs lists synced repositories that have no docs yet (synced before docgen was routed).
	ReposWithoutDocs func(ctx context.Context) ([]string, error)
	// Settings holds hub-wide settings changed in the UI; DefaultDocMode is the deployment's docs mode.
	Settings       AppSettings
	DefaultDocMode string
	// DryRun runs code_push in dry-run mode synchronously.
	DryRun func(ctx context.Context, job ports.Job) (ports.Outcome, error)
	// ReloadSpend applies edited ceilings immediately on this replica.
	ReloadSpend func(ctx context.Context) error
	// SealKeys opens secrets the browser or CLI sealed to the Hub, and keeps their hints (nil: plain only).
	SealKeys *store.SealKeys
	// RequireSealed refuses secrets that arrive unsealed (DTH_REQUIRE_SEALED_SECRETS).
	RequireSealed bool
}

// openSecret replaces a sealed secret with its value, in place; plain values pass unless sealing is required.
func (h *adminHandlers) openSecret(r *http.Request, v *string, purpose string) error {
	return openSealed(r.Context(), h.d.SealKeys, h.d.RequireSealed, v, purpose)
}

// openSealed replaces a sealed value with its plaintext (plain values pass unless requireSealed).
func openSealed(ctx context.Context, keys *store.SealKeys, requireSealed bool, v *string, purpose string) error {
	if v == nil || *v == "" {
		return nil
	}
	if !secrets.IsSealed(*v) {
		if requireSealed {
			return &ports.ValidationError{Code: "SEAL_REQUIRED", Message: "this Hub accepts secrets only sealed to its key (the UI and dth do this)"}
		}
		return nil
	}
	if keys == nil {
		return &ports.ValidationError{Code: "SEAL_UNAVAILABLE", Message: "sealing is not configured on this Hub"}
	}
	out, err := keys.Unseal(ctx, *v, purpose)
	if err != nil {
		return &ports.ValidationError{Code: "SEAL_INVALID", Message: "the sealed value could not be opened (reload the page and enter it again)"}
	}
	*v = out
	return nil
}

// AdminRoutes mounts § 7.6.
func AdminRoutes(d AdminDeps) func(chi.Router) {
	h := &adminHandlers{d: d}
	return func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleViewer))
			r.Get("/repos", h.listRepos)
		})
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleEditor))
			r.Post("/repos/{id}/dry-run", h.dryRun)
		})
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleAdmin))
			r.Post("/repos", h.createRepo)
			r.Patch("/repos/{id}", h.patchRepo)
			r.Post("/repos/{id}/import", h.importDocs)
			r.Post("/repos/{id}/generate-docs", h.generateDocs) // paid calls: admins
			r.Get("/connectors", h.listConnectors)
			r.Post("/connectors", h.createConnector)
			r.Patch("/connectors/{id}", h.patchConnector)
			r.Delete("/connectors/{id}", h.deleteConnector)
			r.Post("/connectors/{id}/test", h.testConnector)
			r.Get("/connectors/{id}/available-repos", h.availableRepos)
			r.Post("/connectors/{id}/sync", h.syncConnector)
			r.Get("/providers", h.listProviders)
			r.Post("/providers", h.createProvider)
			r.Patch("/providers/{id}", h.patchProvider)
			r.Delete("/providers/{id}", h.deleteProvider)
			r.Post("/providers/{id}/test", h.testProvider)
			r.Get("/routes", h.listRoutes)
			r.Put("/routes/{feature}", h.putRoute)
			r.Get("/docs/mode", h.getDocMode)
			r.Put("/docs/mode", h.putDocMode)
			r.Get("/spend/limits", h.getLimits)
			r.Put("/spend/limits", h.putLimits)
		})
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleOwner))
			r.Post("/reindex", h.reindex)
		})
	}
}

type adminHandlers struct{ d AdminDeps }

func (h *adminHandlers) audit(r *http.Request, action, typ, id string, details any) {
	_ = h.d.Auth.Audit(r.Context(), auth.FromContext(r.Context()), action, typ, id, details, clientIP(r))
}

// --- repos ---

func (h *adminHandlers) listRepos(w http.ResponseWriter, r *http.Request) {
	sc, err := scopeOf(r, h.d.Auth)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	all, err := h.d.Repos.ListAll(r.Context())
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	out := []ports.RepoConfig{}
	for _, rc := range all {
		if allows(sc, rc.ID) {
			out = append(out, rc)
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *adminHandlers) createRepo(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ConnectorID   string `json:"connector_id"`
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
		TrackedBranch string `json:"tracked_branch"`
		DocsPath      string `json:"docs_path"`
		PushMode      string `json:"push_mode"`
		Approver      string `json:"approver"`
		ServiceName   string `json:"service_name"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	if in.ConnectorID == "" || in.FullName == "" {
		fail(w, r, errBadParam("connector_id and full_name are required"))
		return
	}
	if in.DocsPath != "" && (!strings.HasSuffix(in.DocsPath, "/") || strings.Contains(in.DocsPath, "..") || strings.HasPrefix(in.DocsPath, "/")) {
		fail(w, r, errBadParam("docs_path must be a relative directory ending in / (e.g. docs/generated/)"))
		return
	}
	if in.DefaultBranch == "" && h.d.Host != nil {
		if host, err := h.d.Host(r.Context(), in.ConnectorID); err == nil {
			in.DefaultBranch, _ = host.DefaultBranch(r.Context(), in.FullName)
		}
	}
	id, err := h.d.Repos.Upsert(r.Context(), ports.RepoConfig{ConnectorID: in.ConnectorID, FullName: in.FullName, DefaultBranch: in.DefaultBranch,
		TrackedBranch: in.TrackedBranch, DocsPath: in.DocsPath, ServiceName: in.ServiceName,
		Push: ports.PushConfig{Mode: ports.PushMode(in.PushMode), Approver: in.Approver}})
	if err != nil {
		fail(w, r, &ports.ValidationError{Code: "VALIDATION_FAILED", Message: "could not add repository (unknown connector?)"})
		return
	}
	h.audit(r, "repo.create", "repo", id, in)
	out := map[string]string{"id": id}
	// Start right away: index the code and write its docs (docs need a docgen route; without one, routing
	// docgen later catches up). Later commits arrive by webhook or polling.
	if h.d.GenerateDocs != nil {
		if job, err := h.d.GenerateDocs(r.Context(), id, "repository added"); err == nil {
			out["job_id"] = job
		}
	}
	if h.d.RegisterWebhook != nil {
		status, err := h.d.RegisterWebhook(r.Context(), in.ConnectorID, in.FullName)
		if err != nil {
			status = "failed: " + err.Error() + " (the scheduler's polling still picks up pushes when the connector polls)"
		}
		out["webhook"] = status
	}
	WriteJSON(w, http.StatusCreated, out)
}

func (h *adminHandlers) patchRepo(w http.ResponseWriter, r *http.Request) {
	var in store.RepoPatch
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	if in.DocsPath != nil && (!strings.HasSuffix(*in.DocsPath, "/") || strings.Contains(*in.DocsPath, "..") || strings.HasPrefix(*in.DocsPath, "/")) {
		fail(w, r, errBadParam("docs_path must be a relative directory ending in /"))
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.d.Repos.UpdateRepo(r.Context(), id, in); err != nil {
		WriteErr(w, r, err)
		return
	}
	h.audit(r, "repo.update", "repo", id, in)
	rc, err := h.d.Repos.Get(r.Context(), id)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, rc)
}

func (h *adminHandlers) dryRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sc, err := scopeOf(r, h.d.Auth)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	if !allows(sc, id) {
		WriteErr(w, r, ports.ErrNotFound)
		return
	}
	var in struct {
		Before string `json:"before"`
	}
	if r.ContentLength > 0 {
		if err := decodeJSON(w, r, &in); err != nil {
			fail(w, r, err)
			return
		}
	}
	b, _ := json.Marshal(pipeline.CodePushPayload{RepoID: id, Before: in.Before, DryRun: true})
	out, err := h.d.DryRun(r.Context(), ports.Job{ID: ports.NewID(), RepoID: id, Payload: b, CorrelationID: correlationFor(r)})
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"status": out.Status, "message": out.Message, "result": out.Result})
}

// generateDocs documents every file in the repository now, e.g. code synced before docgen had a route.
func (h *adminHandlers) generateDocs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.d.GenerateDocs == nil {
		WriteErr(w, r, ports.ErrNotFound)
		return
	}
	if _, err := h.d.Repos.Get(r.Context(), id); err != nil {
		WriteErr(w, r, err)
		return
	}
	if h.d.Routes != nil {
		if _, err := h.d.Routes.Route(r.Context(), llmgateway.FeatureDocGen); errors.Is(err, llmgateway.ErrNoRoute) {
			fail(w, r, &ports.ValidationError{Code: "NO_DOCGEN_ROUTE", Message: "choose a provider and model for docgen under Providers & routing first"})
			return
		}
	}
	job, err := h.d.GenerateDocs(r.Context(), id, "requested")
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	h.audit(r, "repo.generate_docs", "repo", id, nil)
	WriteJSON(w, http.StatusAccepted, map[string]string{"job_id": job})
}

func (h *adminHandlers) importDocs(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SourcePaths []string `json:"source_paths"`
	}
	if r.ContentLength > 0 {
		if err := decodeJSON(w, r, &in); err != nil {
			fail(w, r, err)
			return
		}
	}
	id := chi.URLParam(r, "id")
	if _, err := h.d.Repos.Get(r.Context(), id); err != nil {
		WriteErr(w, r, err)
		return
	}
	job, _, err := h.d.Queue.Enqueue(r.Context(), ports.NewJob{Type: ports.JobImportDocs, RepoID: id, DedupeKey: "import:" + id,
		CorrelationID: correlationFor(r), Payload: pipeline.ImportPayload{RepoID: id, Prefixes: in.SourcePaths}})
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	h.audit(r, "repo.import", "repo", id, in)
	WriteJSON(w, http.StatusAccepted, map[string]string{"job_id": job.ID})
}

// --- connectors ---

var connectorTypes = []string{"github", "gitlab", "cloudwatch", "firehose", "gcp", "pubsub", "datadog", "grafana", "alertmanager", "sentry",
	"pagerduty", "opsgenie", "wiz", "splunk", "generic", "kafka", "sqs", "sns", "eventbridge", "kinesis", "pubsub_bus", "rabbitmq", "confluence", "jira", "notion"}

func (h *adminHandlers) listConnectors(w http.ResponseWriter, r *http.Request) {
	cs, err := h.d.Connectors.ListPublic(r.Context())
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	type view struct {
		store.ConnectorView
		CredentialsMeta *store.SecretMeta `json:"credentials_meta,omitempty"` // hint only; never the value
	}
	hints := map[string]store.SecretMeta{}
	if h.d.SealKeys != nil {
		hints, _ = h.d.SealKeys.Hints(r.Context(), "connectors")
	}
	out := make([]view, len(cs))
	for i, c := range cs {
		out[i] = view{ConnectorView: c}
		if m, ok := hints[c.ID]; ok && c.HasCredentials {
			out[i].CredentialsMeta = &m
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *adminHandlers) createConnector(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Type          string            `json:"type"`
		Name          string            `json:"name"`
		Mode          string            `json:"mode"`
		PollSeconds   int64             `json:"poll_seconds"`
		Config        map[string]string `json:"config"`
		Credentials   string            `json:"credentials"`
		WebhookSecret string            `json:"webhook_secret"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	if !slices.Contains(connectorTypes, in.Type) || strings.TrimSpace(in.Name) == "" {
		fail(w, r, errBadParam("type must be a known connector type and name is required"))
		return
	}
	if in.Mode != "" && in.Mode != "webhook" && in.Mode != "poll" && in.Mode != "both" {
		fail(w, r, errBadParam("mode must be webhook, poll or both"))
		return
	}
	if err := h.openSecret(r, &in.Credentials, secrets.PurposeConnectorCreds); err != nil {
		fail(w, r, err)
		return
	}
	if err := h.openSecret(r, &in.WebhookSecret, secrets.PurposeConnectorWebhook); err != nil {
		fail(w, r, err)
		return
	}
	id, err := h.d.Connectors.Create(r.Context(), store.NewConnector{Type: in.Type, Name: in.Name, Mode: in.Mode, PollSeconds: in.PollSeconds,
		Config: in.Config, Credentials: in.Credentials, WebhookSecret: in.WebhookSecret})
	if err != nil {
		WriteError(w, r, http.StatusConflict, "CONFLICT", "could not create connector (is the name taken?)", nil)
		return
	}
	if h.d.SealKeys != nil && in.Credentials != "" {
		_ = h.d.SealKeys.SetConnectorCredsHint(r.Context(), id, in.Credentials)
	}
	h.audit(r, "connector.create", "connector", id, map[string]any{"type": in.Type, "name": in.Name, "mode": in.Mode})
	WriteJSON(w, http.StatusCreated, map[string]string{"id": id, "webhook_path": registry.WebhookPath(in.Type, id)})
}

func (h *adminHandlers) patchConnector(w http.ResponseWriter, r *http.Request) {
	var in store.ConnectorPatch
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.openSecret(r, in.Credentials, secrets.PurposeConnectorCreds); err != nil {
		fail(w, r, err)
		return
	}
	if err := h.openSecret(r, in.WebhookSecret, secrets.PurposeConnectorWebhook); err != nil {
		fail(w, r, err)
		return
	}
	if err := h.d.Connectors.Update(r.Context(), id, in); err != nil {
		WriteErr(w, r, err)
		return
	}
	if h.d.InvalidateHost != nil {
		h.d.InvalidateHost(id)
	}
	if h.d.SealKeys != nil && in.Credentials != nil {
		_ = h.d.SealKeys.SetConnectorCredsHint(r.Context(), id, *in.Credentials)
	}
	// Disabling suspends a GitHub App installation, so GitHub stops sending events and the App's access
	// pauses; enabling resumes it. The Hub-side change stands even if GitHub refuses.
	var remote *remoteResult
	if in.Enabled != nil {
		remote = h.setSuspended(r, id, !*in.Enabled)
	}
	h.audit(r, "connector.update", "connector", id, map[string]any{"name": in.Name, "mode": in.Mode, "enabled": in.Enabled,
		"credentials_changed": in.Credentials != nil, "webhook_secret_changed": in.WebhookSecret != nil, "github": remote})
	WriteJSON(w, http.StatusOK, map[string]any{"id": id, "github": remote})
}

// remoteResult reports what happened on the git host when a connector was disabled, enabled or removed.
type remoteResult struct {
	Action      string `json:"action"` // suspended | resumed | uninstalled
	OK          bool   `json:"ok"`
	Error       string `json:"error,omitempty"`
	SettingsURL string `json:"settings_url,omitempty"` // where the App itself can be deleted
}

func (h *adminHandlers) installation(r *http.Request, id string) ports.AppInstallation {
	if h.d.HostAny == nil {
		return nil
	}
	host, err := h.d.HostAny(r.Context(), id)
	if err != nil {
		return nil
	}
	inst, _ := host.(ports.AppInstallation)
	return inst
}

func (h *adminHandlers) setSuspended(r *http.Request, id string, suspend bool) *remoteResult {
	inst := h.installation(r, id)
	if inst == nil {
		return nil
	}
	res := &remoteResult{Action: "resumed"}
	if suspend {
		res.Action = "suspended"
	}
	applied, err := inst.SetInstallationSuspended(r.Context(), suspend)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if !applied {
		return nil
	}
	res.OK = true
	return res
}

func (h *adminHandlers) deleteConnector(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// Uninstall a GitHub App first, while its key is still stored: its access to the repositories ends and
	// GitHub stops sending events. The connector is removed even if GitHub refuses (reported back).
	var remote *remoteResult
	if inst := h.installation(r, id); inst != nil {
		res := &remoteResult{Action: "uninstalled"}
		settings, applied, err := inst.Uninstall(r.Context())
		res.SettingsURL = settings
		switch {
		case err != nil:
			res.Error = err.Error()
			remote = res
		case applied:
			res.OK = true
			remote = res
		}
	}
	if err := h.d.Connectors.Delete(r.Context(), id); err != nil {
		WriteErr(w, r, err)
		return
	}
	if h.d.InvalidateHost != nil {
		h.d.InvalidateHost(id)
	}
	h.audit(r, "connector.delete", "connector", id, map[string]any{"github": remote})
	WriteJSON(w, http.StatusOK, map[string]any{"id": id, "github": remote})
}

type check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// testConnector runs read-only probes (git hosts: identity and repository listing).
func (h *adminHandlers) testConnector(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.d.InvalidateHost != nil {
		h.d.InvalidateHost(id)
	}
	host, err := h.d.Host(r.Context(), id)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			WriteErr(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "checks": []check{{Name: "configuration", OK: false, Detail: err.Error()}}})
		return
	}
	var checks []check
	ok := true
	if id, err := host.BotIdentity(r.Context()); err != nil {
		checks, ok = append(checks, check{Name: "authenticate", Detail: err.Error()}), false
	} else {
		checks = append(checks, check{Name: "authenticate", OK: true, Detail: "signed in as " + id.Login})
	}
	if repos, err := host.ListRepos(r.Context()); err != nil {
		checks, ok = append(checks, check{Name: "list_repositories", Detail: err.Error()}), false
	} else {
		checks = append(checks, check{Name: "list_repositories", OK: true, Detail: strings.Join(firstN(repos, 20), ", ")})
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": ok, "checks": checks})
}

// availableRepos lists what the git host lets the connector read, marking the repositories already tracked.
func (h *adminHandlers) availableRepos(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.d.Host == nil {
		WriteErr(w, r, ports.ErrNotFound)
		return
	}
	host, err := h.d.Host(r.Context(), id)
	if err != nil {
		WriteErr(w, r, &ports.ValidationError{Code: "CONNECTOR_NOT_READY", Message: err.Error()})
		return
	}
	names, err := host.ListRepos(r.Context())
	if err != nil {
		WriteErr(w, r, &ports.ValidationError{Code: "HOST_ERROR", Message: "could not list repositories: " + err.Error()})
		return
	}
	all, err := h.d.Repos.ListAll(r.Context())
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	tracked := map[string]bool{}
	for _, rc := range all {
		if rc.ConnectorID == id {
			tracked[rc.FullName] = true
		}
	}
	type item struct {
		FullName string `json:"full_name"`
		Tracked  bool   `json:"tracked"`
	}
	out := make([]item, 0, len(names))
	for _, n := range names {
		out = append(out, item{FullName: n, Tracked: tracked[n]})
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func firstN(xs []string, n int) []string {
	if len(xs) > n {
		return append(xs[:n:n], "…")
	}
	return xs
}

// syncConnector enqueues a push for every tracked repo of the connector whose branch moved.
func (h *adminHandlers) syncConnector(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if cc, err := h.d.Connectors.Get(r.Context(), id); err == nil && (cc.Type == "confluence" || cc.Type == "jira" || cc.Type == "notion") {
		job, _, err := h.d.Queue.Enqueue(r.Context(), ports.NewJob{Type: ports.JobKnowledgeSync, SerialKey: "knowledge:" + id,
			DedupeKey: "knowledge:" + id, CorrelationID: correlationFor(r), Payload: ingest.SyncPayload{ConnectorID: id}, MaxAttempts: 3})
		if err != nil {
			WriteErr(w, r, err)
			return
		}
		h.audit(r, "connector.sync", "connector", id, map[string]int{"jobs": 1})
		WriteJSON(w, http.StatusAccepted, map[string]any{"job_ids": []string{job.ID}})
		return
	}
	host, err := h.d.Host(r.Context(), id)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	repos, err := h.d.Repos.ListAll(r.Context())
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	var jobs []string
	for _, rc := range repos {
		if rc.ConnectorID != id || !rc.Enabled {
			continue
		}
		head, err := host.BranchHead(r.Context(), rc.FullName, rc.Branch())
		if err != nil || head == rc.LastProcessedSHA {
			continue
		}
		job, _, err := h.d.Queue.Enqueue(r.Context(), ports.NewJob{Type: ports.JobCodePush, RepoID: rc.ID, SerialKey: "repo:" + rc.ID,
			DedupeKey: "push:" + rc.ID + ":" + head, CorrelationID: correlationFor(r),
			Payload: pipeline.CodePushPayload{RepoID: rc.ID, Before: rc.LastProcessedSHA, After: head}})
		if err != nil {
			WriteErr(w, r, err)
			return
		}
		jobs = append(jobs, job.ID)
	}
	h.audit(r, "connector.sync", "connector", id, map[string]int{"jobs": len(jobs)})
	if jobs == nil {
		jobs = []string{}
	}
	WriteJSON(w, http.StatusAccepted, map[string]any{"job_ids": jobs})
}

// --- providers & routes ---

type providerView struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Name      string            `json:"name"`
	BaseURL   string            `json:"base_url,omitempty"`
	Extra     map[string]string `json:"extra"`
	HasKey    bool              `json:"has_key"`
	RedactPII bool              `json:"redact_pii"`
	Enabled   bool              `json:"enabled"`
	// Key is the write-only key's hint and when it was set; the key itself is never returned.
	KeyMeta *store.SecretMeta `json:"key_meta,omitempty"`
}

func (h *adminHandlers) listProviders(w http.ResponseWriter, r *http.Request) {
	ps, err := h.d.Providers.List(r.Context())
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	hints := map[string]store.SecretMeta{}
	if h.d.SealKeys != nil {
		hints, _ = h.d.SealKeys.Hints(r.Context(), "llm_providers")
	}
	out := make([]providerView, len(ps))
	for i, p := range ps {
		out[i] = providerView{ID: p.ID, Kind: p.Kind, Name: p.Name, BaseURL: p.BaseURL, Extra: p.Extra, HasKey: len(p.KeyCiphertext) > 0,
			RedactPII: p.RedactPII, Enabled: p.Enabled}
		if m, ok := hints[p.ID]; ok && out[i].HasKey {
			out[i].KeyMeta = &m
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": out, "kinds": h.d.ProviderKinds})
}

type providerInput struct {
	Kind      string            `json:"kind"`
	Name      *string           `json:"name"`
	BaseURL   *string           `json:"base_url"`
	APIKey    *string           `json:"api_key"`
	Extra     map[string]string `json:"extra"`
	RedactPII *bool             `json:"redact_pii"`
	Enabled   *bool             `json:"enabled"`
}

func (h *adminHandlers) createProvider(w http.ResponseWriter, r *http.Request) {
	var in providerInput
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	if !slices.Contains(h.d.ProviderKinds, in.Kind) || in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		fail(w, r, errBadParam("kind must be one of "+strings.Join(h.d.ProviderKinds, ", ")+" and name is required"))
		return
	}
	if err := h.openSecret(r, in.APIKey, secrets.PurposeProviderKey); err != nil {
		fail(w, r, err)
		return
	}
	rec := store.ProviderRecord{ID: ports.NewID(), Kind: in.Kind, Name: *in.Name, Extra: in.Extra, Enabled: true}
	if in.BaseURL != nil {
		rec.BaseURL = *in.BaseURL
	}
	if in.RedactPII != nil {
		rec.RedactPII = *in.RedactPII
	}
	if in.Enabled != nil {
		rec.Enabled = *in.Enabled
	}
	if in.APIKey != nil && *in.APIKey != "" {
		ct, err := h.d.Seal(r.Context(), []byte(*in.APIKey), h.d.ProviderAAD(rec.ID))
		if err != nil {
			WriteErr(w, r, err)
			return
		}
		rec.KeyCiphertext = ct
	}
	id, err := h.d.Providers.Create(r.Context(), rec)
	if err != nil {
		WriteError(w, r, http.StatusConflict, "CONFLICT", "could not create provider (is the name taken?)", nil)
		return
	}
	if h.d.SealKeys != nil && in.APIKey != nil && *in.APIKey != "" {
		_ = h.d.SealKeys.SetProviderKeyHint(r.Context(), id, *in.APIKey)
	}
	h.audit(r, "provider.create", "llm_provider", id, map[string]any{"kind": in.Kind, "name": rec.Name})
	WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (h *adminHandlers) patchProvider(w http.ResponseWriter, r *http.Request) {
	var in providerInput
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.openSecret(r, in.APIKey, secrets.PurposeProviderKey); err != nil {
		fail(w, r, err)
		return
	}
	rec, err := h.d.Providers.Get(r.Context(), id)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	if in.Name != nil {
		rec.Name = *in.Name
	}
	if in.BaseURL != nil {
		rec.BaseURL = *in.BaseURL
	}
	if in.Extra != nil {
		rec.Extra = in.Extra
	}
	if in.RedactPII != nil {
		rec.RedactPII = *in.RedactPII
	}
	if in.Enabled != nil {
		rec.Enabled = *in.Enabled
	}
	rec.KeyCiphertext = nil
	if in.APIKey != nil {
		if rec.KeyCiphertext, err = h.d.Seal(r.Context(), []byte(*in.APIKey), h.d.ProviderAAD(id)); err != nil {
			WriteErr(w, r, err)
			return
		}
	}
	if err := h.d.Providers.Update(r.Context(), rec); err != nil {
		WriteErr(w, r, err)
		return
	}
	if h.d.InvalidateProvider != nil {
		h.d.InvalidateProvider(id)
	}
	if h.d.SealKeys != nil && in.APIKey != nil {
		_ = h.d.SealKeys.SetProviderKeyHint(r.Context(), id, *in.APIKey)
	}
	h.audit(r, "provider.update", "llm_provider", id, map[string]any{"name": rec.Name, "key_changed": in.APIKey != nil, "enabled": rec.Enabled})
	w.WriteHeader(http.StatusNoContent)
}

func (h *adminHandlers) deleteProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.d.Providers.Delete(r.Context(), id); err != nil {
		WriteErr(w, r, err)
		return
	}
	if h.d.InvalidateProvider != nil {
		h.d.InvalidateProvider(id)
	}
	h.audit(r, "provider.delete", "llm_provider", id, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (h *adminHandlers) testProvider(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Model string `json:"model"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	id := chi.URLParam(r, "id")
	if _, err := h.d.Providers.Get(r.Context(), id); err != nil {
		WriteErr(w, r, err)
		return
	}
	lat, err := h.d.TestProvider(r.Context(), id, in.Model)
	if err != nil {
		WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "latency_ms": lat.Milliseconds()})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "latency_ms": lat.Milliseconds()})
}

var features = []string{llmgateway.FeatureDocGen, llmgateway.FeatureDocGenFast, llmgateway.FeatureQA, llmgateway.FeatureDecode, llmgateway.FeatureTriage,
	llmgateway.FeatureEmbedding, llmgateway.FeatureSuggest, llmgateway.FeatureDecide, llmgateway.FeatureSecurity}

func (h *adminHandlers) listRoutes(w http.ResponseWriter, r *http.Request) {
	rs, err := h.d.Routes.ListRoutes(r.Context())
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": rs, "features": features})
}

func (h *adminHandlers) putRoute(w http.ResponseWriter, r *http.Request) {
	feature := chi.URLParam(r, "feature")
	if !slices.Contains(features, feature) {
		WriteErr(w, r, ports.ErrNotFound)
		return
	}
	var in struct {
		ProviderID         string   `json:"provider_id"`
		Model              string   `json:"model"`
		MaxOutputTokens    int      `json:"max_output_tokens"`
		ContextTokenBudget int      `json:"context_token_budget"`
		Effort             string   `json:"effort"`
		Temperature        *float64 `json:"temperature"`
		FallbackProviderID string   `json:"fallback_provider_id"`
		FallbackModel      string   `json:"fallback_model"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	if in.ProviderID == "" || in.Model == "" {
		fail(w, r, errBadParam("provider_id and model are required"))
		return
	}
	if in.Effort != "" && !slices.Contains([]string{"low", "medium", "high", "xhigh", "max"}, in.Effort) {
		fail(w, r, errBadParam("effort must be low, medium, high, xhigh or max"))
		return
	}
	// Decision-only providers (TypeSafe Jev) cannot write text; chat-only routes cannot use them.
	for _, id := range []string{in.ProviderID, in.FallbackProviderID} {
		if id == "" {
			continue
		}
		if p, err := h.d.Providers.Get(r.Context(), id); err == nil && p.Kind == "jev" && feature != llmgateway.FeatureDecide {
			fail(w, r, errBadParam("jev answers decisions only: route it to the decide feature"))
			return
		}
	}
	if in.MaxOutputTokens == 0 {
		in.MaxOutputTokens = 8000
	}
	if in.ContextTokenBudget == 0 {
		in.ContextTokenBudget = 16000
	}
	rt := llmgateway.Route{Feature: feature, ProviderID: in.ProviderID, Model: in.Model, MaxOutputTokens: in.MaxOutputTokens,
		ContextBudget: in.ContextTokenBudget, Effort: in.Effort, Temperature: in.Temperature}
	if in.FallbackProviderID != "" {
		rt.Fallback = &llmgateway.Route{ProviderID: in.FallbackProviderID, Model: in.FallbackModel}
	}
	if err := h.d.Routes.SetRoute(r.Context(), rt); err != nil {
		fail(w, r, &ports.ValidationError{Code: "VALIDATION_FAILED", Message: "unknown provider_id or fallback_provider_id"})
		return
	}
	h.audit(r, "route.set", "model_route", feature, in)
	// Repositories synced before docgen had a route were indexed without docs: document them now.
	queued := 0
	if feature == llmgateway.FeatureDocGen && h.d.ReposWithoutDocs != nil && h.d.GenerateDocs != nil {
		if ids, err := h.d.ReposWithoutDocs(r.Context()); err == nil {
			for _, id := range ids {
				if _, err := h.d.GenerateDocs(r.Context(), id, "docgen route set"); err == nil {
					queued++
				}
			}
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"feature": feature, "docs_queued": queued})
}

// --- spend ---

func (h *adminHandlers) getLimits(w http.ResponseWriter, r *http.Request) {
	ls, err := h.d.Browse.SpendLimits(r.Context())
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": ls})
}

func (h *adminHandlers) putLimits(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Items []store.SpendLimit `json:"items"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	if err := h.d.Browse.ReplaceSpendLimits(r.Context(), in.Items); err != nil {
		WriteErr(w, r, err)
		return
	}
	if h.d.ReloadSpend != nil {
		if err := h.d.ReloadSpend(r.Context()); err != nil {
			WriteErr(w, r, &ports.ValidationError{Code: "VALIDATION_FAILED", Message: err.Error()})
			return
		}
	}
	h.audit(r, "spend.limits", "spend_limits", "", in.Items)
	w.WriteHeader(http.StatusNoContent)
}

func (h *adminHandlers) reindex(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AcknowledgeDestructive bool `json:"acknowledge_destructive"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	if !in.AcknowledgeDestructive {
		fail(w, r, errBadParam("set acknowledge_destructive: true; every chunk is re-embedded on the embedding route's current model"))
		return
	}
	n, tokens, err := h.d.Browse.ReindexEstimate(r.Context())
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	job, _, err := h.d.Queue.Enqueue(r.Context(), ports.NewJob{Type: ports.JobReindex, DedupeKey: "reindex", CorrelationID: correlationFor(r),
		Payload: pipeline.ReindexPayload{}})
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	h.audit(r, "index.reindex", "job", job.ID, map[string]int64{"chunks": n})
	WriteJSON(w, http.StatusAccepted, map[string]any{"job_id": job.ID, "chunks_to_reembed": n, "estimated_tokens": tokens})
}

// AppSettings stores hub-wide settings changed in the UI.
type AppSettings interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
}

// DocModeKey is the app setting that overrides the deployment's docs generation mode.
const DocModeKey = "docs.generation_mode"

var docModes = []string{"thorough", "balanced", "economy"}

func (h *adminHandlers) docMode(ctx context.Context) (mode, source string, err error) {
	def := h.d.DefaultDocMode
	if !slices.Contains(docModes, def) {
		def = "balanced"
	}
	if h.d.Settings != nil {
		v, err := h.d.Settings.Get(ctx, DocModeKey)
		if err != nil {
			return "", "", err
		}
		if slices.Contains(docModes, v) {
			return v, "settings", nil
		}
	}
	return def, "default", nil
}

func (h *adminHandlers) getDocMode(w http.ResponseWriter, r *http.Request) {
	mode, source, err := h.docMode(r.Context())
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"mode": mode, "source": source, "modes": docModes})
}

func (h *adminHandlers) putDocMode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Mode string `json:"mode"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	if !slices.Contains(docModes, in.Mode) {
		fail(w, r, errBadParam("mode must be thorough, balanced or economy"))
		return
	}
	if h.d.Settings == nil {
		WriteError(w, r, http.StatusNotImplemented, "NOT_AVAILABLE", "settings are not available on this Hub", nil)
		return
	}
	if err := h.d.Settings.Set(r.Context(), DocModeKey, in.Mode); err != nil {
		WriteErr(w, r, err)
		return
	}
	_ = h.d.Auth.Audit(r.Context(), auth.FromContext(r.Context()), "docs.mode", "app_setting", DocModeKey, in, clientIP(r))
	h.getDocMode(w, r)
}
