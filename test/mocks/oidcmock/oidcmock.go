// Package oidcmock is a minimal OpenID Connect provider for login tests: discovery, JWKS, an authorize
// endpoint that immediately "signs in" the configured user, and a token endpoint that checks the PKCE
// verifier and returns an RS256 ID token carrying the request's nonce.
package oidcmock

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

// User is who the provider signs in.
type User struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Groups        []string
}

type grant struct {
	challenge, nonce, clientID string
	user                       User
}

// Server is a running provider.
type Server struct {
	*httptest.Server
	key    *rsa.PrivateKey
	mu     sync.Mutex
	grants map[string]grant
	// User is signed in by the next authorize request.
	User User
	// ClientID is the only accepted client.
	ClientID string
	// WrongNonce makes the ID token carry a different nonce (replay test).
	WrongNonce bool
}

// New starts a provider.
func New(clientID string) *Server {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	s := &Server{key: key, grants: map[string]grant{}, ClientID: clientID}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"issuer": s.URL, "authorization_endpoint": s.URL + "/authorize", "token_endpoint": s.URL + "/token",
			"jwks_uri": s.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"},
			"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"}})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &s.key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/authorize", s.authorize)
	mux.HandleFunc("/token", s.token)
	s.Server = httptest.NewServer(mux)
	return s
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != s.ClientID || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		http.Error(w, "bad authorize request", http.StatusBadRequest)
		return
	}
	code := base64.RawURLEncoding.EncodeToString([]byte(time.Now().String()))
	s.mu.Lock()
	s.grants[code] = grant{challenge: q.Get("code_challenge"), nonce: q.Get("nonce"), clientID: q.Get("client_id"), user: s.User}
	s.mu.Unlock()
	u, _ := url.Parse(q.Get("redirect_uri"))
	v := u.Query()
	v.Set("code", code)
	v.Set("state", q.Get("state"))
	u.RawQuery = v.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	s.mu.Lock()
	g, ok := s.grants[r.PostForm.Get("code")]
	delete(s.grants, r.PostForm.Get("code"))
	s.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid_grant"})
		return
	}
	nonce := g.nonce
	if s.WrongNonce {
		nonce = "replayed"
	}
	now := time.Now()
	claims := map[string]any{"iss": s.URL, "sub": g.user.Subject, "aud": g.clientID, "exp": now.Add(time.Hour).Unix(), "iat": now.Unix(),
		"nonce": nonce, "email": g.user.Email, "email_verified": g.user.EmailVerified, "name": g.user.Name, "groups": g.user.Groups}
	signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: s.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
	payload, _ := json.Marshal(claims)
	jws, _ := signer.Sign(payload)
	idToken, _ := jws.CompactSerialize()
	writeJSON(w, map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": idToken})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
