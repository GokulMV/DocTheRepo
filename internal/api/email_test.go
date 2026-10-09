package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/email"
	"github.com/GokulMV/DocTheRepo/internal/adapters/email/emailtest"
	"github.com/GokulMV/DocTheRepo/internal/api"
)

func TestInviteAndResetEmails(t *testing.T) {
	smtp := emailtest.Start(t)
	m, err := email.Parse(smtp.URL(), "DocTheRepo <docs@acme.com>")
	require.NoError(t, err)
	e := newAuthEnv(t, "local", true, func(d *api.Deps) { d.Mailer = m; d.PublicURL = "https://hub.acme.com/" })
	_, err = e.svc.BootstrapOwner(context.Background(), "owner@acme.com", "correct horse battery staple")
	require.NoError(t, err)
	owner := e.localLogin(t, "owner@acme.com", "correct horse battery staple")

	code, out, _ := newClient(t, e.srv.URL).do("GET", "/api/v1/auth/config", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, true, out["email"])

	// An invite is emailed with the full link; the admin still gets the link to copy.
	code, out, _ = owner.do("POST", "/api/v1/users", map[string]any{"email": "ann@acme.com", "name": "Ann", "invite": true})
	require.Equal(t, http.StatusCreated, code, out)
	inv := out["invite"].(map[string]any)
	assert.Equal(t, true, inv["emailed"])
	link := "https://hub.acme.com" + inv["path"].(string)
	mail := smtp.Mail()
	require.Len(t, mail, 1)
	assert.Equal(t, "ann@acme.com", mail[0].To)
	assert.Contains(t, mail[0].Data, "Subject: You're invited to DocTheRepo\r\n")
	assert.Contains(t, mail[0].Data, link+"\r\n")
	assert.Contains(t, mail[0].Data, "Hello Ann,")

	// Once Ann has a password, a new link is worded as a reset.
	_, out, _ = newClient(t, e.srv.URL).do("POST", "/api/v1/auth/invite/"+strings.TrimPrefix(inv["path"].(string), "/invite/"), map[string]string{"password": "anns long passphrase"})
	annID := out["user"].(map[string]any)["id"].(string)
	code, out, _ = owner.do("POST", "/api/v1/users/"+annID+"/invite", nil)
	require.Equal(t, http.StatusCreated, code, out)
	assert.Equal(t, true, out["emailed"])
	mail = smtp.Mail()
	require.Len(t, mail, 2)
	assert.Contains(t, mail[1].Data, "Subject: Reset your DocTheRepo password")
	assert.Contains(t, mail[1].Data, "https://hub.acme.com"+out["path"].(string))

	// A refused message is reported, and the link is still there to pass on.
	smtp.SetReject("mailbox unavailable")
	code, out, _ = owner.do("POST", "/api/v1/users/"+annID+"/invite", nil)
	require.Equal(t, http.StatusCreated, code, out)
	assert.Equal(t, false, out["emailed"])
	assert.Contains(t, out["email_error"], "mailbox unavailable")
	assert.NotEmpty(t, out["path"])

	// Test email goes to the owner.
	smtp.SetReject("")
	code, out, _ = owner.do("POST", "/api/v1/auth/email/test", nil)
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, "owner@acme.com", out["sent_to"])
	assert.Equal(t, "owner@acme.com", smtp.Mail()[2].To)
}

func TestInviteWithoutEmail(t *testing.T) {
	e := newAuthEnv(t, "local", true)
	_, err := e.svc.BootstrapOwner(context.Background(), "owner@acme.com", "correct horse battery staple")
	require.NoError(t, err)
	owner := e.localLogin(t, "owner@acme.com", "correct horse battery staple")
	code, out, _ := owner.do("POST", "/api/v1/users", map[string]any{"email": "ann@acme.com", "invite": true})
	require.Equal(t, http.StatusCreated, code, out)
	_, emailed := out["invite"].(map[string]any)["emailed"]
	assert.False(t, emailed, "nothing about email when it is off")
	code, out, _ = owner.do("POST", "/api/v1/auth/email/test", nil)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "EMAIL_NOT_CONFIGURED", errCode(out))
	_, out, _ = newClient(t, e.srv.URL).do("GET", "/api/v1/auth/config", nil)
	assert.Equal(t, false, out["email"])
}
