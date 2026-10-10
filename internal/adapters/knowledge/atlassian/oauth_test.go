package atlassian

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

func TestRefreshAnswers(t *testing.T) {
	answer := ""
	status := 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(answer))
	}))
	defer srv.Close()
	ep := Endpoints{Token: srv.URL}
	ctx := context.Background()

	answer = `{"access_token":"a2","refresh_token":"r2","expires_in":3600}`
	tok, err := Refresh(ctx, nil, ep, "c", "s", "r1")
	if err != nil || tok.AccessToken != "a2" || tok.RefreshToken != "r2" || !tok.Valid() {
		t.Fatalf("rotated: %+v %v", tok, err)
	}
	answer = `{"access_token":"a3","expires_in":3600}`
	if tok, _ = Refresh(ctx, nil, ep, "c", "s", "r2"); tok.RefreshToken != "r2" {
		t.Fatalf("a non-rotating answer keeps the refresh token: %+v", tok)
	}
	status, answer = 403, `{"error":"invalid_grant","error_description":"Unknown or invalid refresh token."}`
	if _, err = Refresh(ctx, nil, ep, "c", "s", "r2"); !errors.Is(err, ErrSignInAgain) {
		t.Fatalf("invalid_grant = %v", err)
	}
	status, answer = 503, ``
	if _, err = Refresh(ctx, nil, ep, "c", "s", "r2"); err == nil || errors.Is(err, ErrSignInAgain) {
		t.Fatalf("an outage is not a revoked sign-in: %v", err)
	}
}

type staticTokens struct{}

func (staticTokens) Token(context.Context, ports.ConnectorConfig, bool) (string, error) {
	return "t", nil
}
func (staticTokens) APIBase() string { return "https://api.atlassian.com" }

func TestOAuthSiteLinks(t *testing.T) {
	cc := ports.ConnectorConfig{Config: map[string]string{"auth": "oauth", "cloud_id": "abc-123", "base_url": "https://acme.atlassian.net/wiki"}}
	s, err := NewSite("confluence", cc, staticTokens{})
	if err != nil {
		t.Fatal(err)
	}
	if s.APIBase() != "https://api.atlassian.com/ex/confluence/abc-123" {
		t.Fatalf("api base = %s", s.APIBase())
	}
	for in, want := range map[string]string{
		"https://api.atlassian.com/ex/confluence/abc-123/rest/api/content/search?cursor=x": "/rest/api/content/search?cursor=x",
		"https://acme.atlassian.net/wiki/rest/api/content/search?cursor=y":                 "/rest/api/content/search?cursor=y",
		"https://api.atlassian.com/ex/confluence/other/rest/api/content":                   "",
		"https://evil.example/rest/api/content":                                            "",
	} {
		if got := s.RelativeAPI(in); got != want {
			t.Errorf("RelativeAPI(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := NewSite("confluence", cc, nil); err == nil {
		t.Fatal("an OAuth connector needs a token source")
	}
	cc.Config["cloud_id"] = "../x"
	if _, err := NewSite("confluence", cc, staticTokens{}); err == nil {
		t.Fatal("cloud_id must be path-safe")
	}
}
