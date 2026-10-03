package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/adapters/email"
	"github.com/GokulMV/DocTheRepo/internal/auth"
)

// Mailer sends the Hub's emails (nil: links are only shown to the admin, to pass on themselves).
type Mailer interface {
	Send(ctx context.Context, msg email.Message) error
	From() string
	Server() string
}

// baseURL is the Hub's external URL: the configured public URL, or what the request came in on.
func (h *authHandlers) baseURL(r *http.Request) string {
	base := strings.TrimRight(h.publicURL, "/")
	if base == "" {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	return base
}

// emailLink sends a set-password link to u and reports the outcome for the response: emailed, and
// email_error when sending failed (the admin can still copy the link). Without a mailer it adds nothing.
func (h *authHandlers) emailLink(r *http.Request, by *auth.Principal, u auth.User, token string, exp time.Time, out map[string]any) {
	if h.mailer == nil {
		return
	}
	link := h.baseURL(r) + invitePath(token)
	who := "An administrator"
	if by != nil && by.Via != "system" {
		who = by.Name
		if who == "" {
			who = by.Email
		}
	}
	var msg email.Message
	expires := exp.UTC().Format("Mon 2 Jan 2006 15:04 MST")
	if u.HasPassword {
		msg = email.Message{To: u.Email, Subject: "Reset your DocTheRepo password",
			Text: fmt.Sprintf("Hello %s,\n\n%s created a link for you to set a new password for DocTheRepo:\n\n%s\n\n"+
				"The link works once and expires %s. Setting a new password signs you out everywhere else.\n"+
				"If you did not expect this, ignore this email; your password stays the same.\n", nameOr(u), who, link, expires)}
	} else {
		msg = email.Message{To: u.Email, Subject: "You're invited to DocTheRepo",
			Text: fmt.Sprintf("Hello %s,\n\n%s added you to DocTheRepo (%s), where your team's code docs and answers live.\n\n"+
				"Choose your password to sign in:\n\n%s\n\nThe link works once and expires %s.\n", nameOr(u), who, h.baseURL(r), link, expires)}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := h.mailer.Send(ctx, msg); err != nil {
		out["emailed"] = false
		out["email_error"] = err.Error()
		return
	}
	out["emailed"] = true
}

func nameOr(u auth.User) string {
	if u.Name != "" {
		return u.Name
	}
	return u.Email
}

// testEmail sends a test message to the signed-in owner, to check the SMTP settings.
func (h *authHandlers) testEmail(w http.ResponseWriter, r *http.Request) {
	if h.mailer == nil {
		WriteError(w, r, http.StatusBadRequest, "EMAIL_NOT_CONFIGURED", "email is not set up: set email.smtp_url and email.from (DTH_SMTP_URL, DTH_EMAIL_FROM) and restart", nil)
		return
	}
	p := auth.FromContext(r.Context())
	if p.Email == "" {
		WriteError(w, r, http.StatusBadRequest, "NO_EMAIL", "your account has no email address to send to", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	err := h.mailer.Send(ctx, email.Message{To: p.Email, Subject: "DocTheRepo test email",
		Text: "This is a test from DocTheRepo (" + h.baseURL(r) + "). Email works: invite and password links will be sent from " + h.mailer.From() + ".\n"})
	if err != nil {
		WriteError(w, r, http.StatusBadGateway, "EMAIL_FAILED", err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"sent_to": p.Email, "from": h.mailer.From(), "server": h.mailer.Server()})
}
