package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/GokulMV/DocTheRepo/internal/adapters/awsrole"
	"github.com/GokulMV/DocTheRepo/internal/auth"
)

// AWSRoleDeps serve the read-only role setup: a CloudFormation template (or quick-create link) that makes
// a role in the user's AWS account trusting only this Hub, with an External ID made per connection.
type AWSRoleDeps struct {
	Auth *auth.Service
	Hub  *awsrole.Hub
}

type awsRoleHandlers struct{ d AWSRoleDeps }

// AWSRoleRoutes mounts /aws/role/* (admins, like adding connectors).
func AWSRoleRoutes(d AWSRoleDeps) func(chi.Router) {
	h := &awsRoleHandlers{d: d}
	return func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleAdmin))
			r.Post("/aws/role/setup", h.setup)
			r.Get("/aws/role/template", h.template)
			r.Post("/aws/role/check", h.check)
		})
	}
}

func (h *awsRoleHandlers) audit(r *http.Request, action, target string, details any) {
	if h.d.Auth != nil {
		_ = h.d.Auth.Audit(r.Context(), auth.FromContext(r.Context()), action, "aws_role", target, details, clientIP(r))
	}
}

// setup makes a new External ID and the way to create the role. A Hub without an AWS identity of its own
// answers available: false and why, so the UI offers keys (or browser sign-in for the MCP server).
func (h *awsRoleHandlers) setup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Access string `json:"access"`
		Region string `json:"region"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	in.Region = strings.TrimSpace(in.Region)
	if (in.Access != "" && !awsrole.ValidAccess(in.Access)) || !awsrole.ValidRegion(in.Region) {
		fail(w, r, errBadParam("access must be hub or readonly; region an AWS region such as eu-west-1"))
		return
	}
	s, err := h.d.Hub.NewSetup(r.Context(), in.Access, in.Region)
	if err != nil {
		WriteJSON(w, http.StatusOK, map[string]any{"available": false, "reason": err.Error()})
		return
	}
	h.audit(r, "aws_role.setup", s.RoleName, map[string]any{"access": s.Access, "hub_principal": s.HubPrincipal, "permissions": s.Permissions})
	WriteJSON(w, http.StatusOK, map[string]any{"available": true, "setup": s,
		"template_path": "/api/v1/aws/role/template?external_id=" + s.ExternalID + "&access=" + s.Access})
}

// template downloads the CloudFormation template. Without external_id it is the generic one an operator
// uploads to S3 for quick-create links. It holds no secrets: the External ID only names the connection.
func (h *awsRoleHandlers) template(w http.ResponseWriter, r *http.Request) {
	ext, access := r.URL.Query().Get("external_id"), r.URL.Query().Get("access")
	if access == "" {
		access = awsrole.AccessHub
	}
	if !awsrole.ValidAccess(access) || (ext != "" && awsrole.RoleName(ext) == "") {
		fail(w, r, errBadParam("external_id must be one the Hub made; access hub or readonly"))
		return
	}
	body, err := h.d.Hub.Template(r.Context(), ext, access)
	if errors.Is(err, awsrole.ErrNoIdentity) {
		WriteError(w, r, http.StatusConflict, "NO_AWS_IDENTITY", err.Error(), nil)
		return
	}
	if err != nil {
		fail(w, r, errBadParam(err.Error()))
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+awsrole.TemplateFile(ext)+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(body))
}

// check assumes the role with the External ID and reports success or exactly what is wrong.
func (h *awsRoleHandlers) check(w http.ResponseWriter, r *http.Request) {
	var in awsrole.CheckInput
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	if _, ok := awsrole.Uses[in.Uses]; in.Uses != "" && !ok {
		fail(w, r, errBadParam("uses must be an AWS connector type or mcp"))
		return
	}
	res := h.d.Hub.Check(r.Context(), in)
	h.audit(r, "aws_role.check", res.RoleARN, map[string]any{"ok": res.OK, "problem": res.Problem, "account_id": res.AccountID, "uses": in.Uses})
	WriteJSON(w, http.StatusOK, res)
}
