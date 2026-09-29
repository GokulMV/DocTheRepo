// Package api serves the REST API, webhook ingress, and the embedded web UI.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// ErrorBody is the single error contract every endpoint returns (plan § 7).
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail carries a stable code, a human message, optional details, and the correlation ID.
type ErrorDetail struct {
	Code          string         `json:"code"`
	Message       string         `json:"message"`
	Details       map[string]any `json:"details,omitempty"`
	CorrelationID string         `json:"correlation_id"`
}

// WriteJSON writes v with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes the error contract.
func WriteError(w http.ResponseWriter, r *http.Request, status int, code, msg string, details map[string]any) {
	WriteJSON(w, status, ErrorBody{Error: ErrorDetail{
		Code: code, Message: msg, Details: details, CorrelationID: observability.CorrelationID(r.Context()),
	}})
}

// WriteErr maps a typed domain error onto the contract. Unknown errors become 500 INTERNAL_ERROR with a
// generic message; the full error is logged with the correlation ID, never returned to the client.
func WriteErr(w http.ResponseWriter, r *http.Request, err error) {
	var v *ports.ValidationError
	var s *ports.SpendBlockedError
	var t *ports.TransientError
	switch {
	case errors.As(err, &v):
		WriteError(w, r, http.StatusBadRequest, v.Code, v.Message, v.Details)
	case errors.As(err, &s):
		WriteError(w, r, http.StatusPaymentRequired, "SPEND_BLOCKED", s.Error(),
			map[string]any{"scope": s.Scope, "estimated_tokens": s.EstimatedTokens})
	case errors.Is(err, ports.ErrNotFound):
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found", nil)
	case errors.Is(err, ports.ErrConflict):
		WriteError(w, r, http.StatusConflict, "CONFLICT", err.Error(), nil)
	case errors.As(err, &t):
		observability.Logger(r.Context()).Warn("upstream unavailable", "err", err)
		WriteError(w, r, http.StatusServiceUnavailable, "UPSTREAM_UNAVAILABLE", "a dependency is temporarily unavailable; retry shortly", nil)
	default:
		observability.Logger(r.Context()).Error("internal error", "err", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error", nil)
	}
}

func loggerFor(r *http.Request) *slog.Logger { return observability.Logger(r.Context()) }

func correlationFor(r *http.Request) string { return observability.CorrelationID(r.Context()) }
