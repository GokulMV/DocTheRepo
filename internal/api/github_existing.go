package api

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Reusing a GitHub App the user already has (for example one created by an earlier "Connect with GitHub"):
// GitHub never shows an App's private key again, but its owner can generate a new one. With the App ID and
// that key the Hub signs in as the App, reads its name and finds where it is installed, so nothing else has
// to be copied.

type ghExistingApp struct {
	Slug  string `json:"slug"`
	Name  string `json:"name"`
	Owner struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"owner"`
}

type ghInstallation struct {
	ID      int64 `json:"id"`
	Account struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"account"`
	SuspendedAt *time.Time `json:"suspended_at"`
}

// parseAppKey reads a GitHub App private key (PKCS#1 as GitHub issues it, or PKCS#8).
func parseAppKey(pemText string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(pemText)))
	if block == nil {
		return nil, errors.New("the private key is not a PEM file: choose the .pem file GitHub downloaded")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("the private key could not be read: choose the .pem file GitHub downloaded")
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("the private key is not an RSA key")
	}
	return rk, nil
}

// appJWT is the short-lived token that authenticates as the App itself (RS256, at most 10 minutes).
func appJWT(appID string, key *rsa.PrivateKey, now time.Time) (string, error) {
	enc := base64.RawURLEncoding
	head := enc.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{"iat": now.Add(-60 * time.Second).Unix(), "exp": now.Add(9 * time.Minute).Unix(), "iss": appID})
	signing := head + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + enc.EncodeToString(sig), nil
}

// ghAppGet calls the GitHub API as the App.
func ghAppGet(ctx context.Context, c *http.Client, api, jwt, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(api, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return &ports.ValidationError{Code: "GITHUB_APP_REJECTED", Message: "GitHub did not accept this App ID and private key together. Check the App ID, and use a key generated for that App."}
	case resp.StatusCode == http.StatusNotFound:
		return &ports.ValidationError{Code: "GITHUB_APP_NOT_FOUND", Message: "GitHub has no App with this ID."}
	case resp.StatusCode >= 300:
		return fmt.Errorf("GitHub answered %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, out)
}

// inspectApp signs in as the App and lists its installations.
func inspectApp(ctx context.Context, c *http.Client, api, appID, pemText string) (ghExistingApp, []ghInstallation, error) {
	var app ghExistingApp
	key, err := parseAppKey(pemText)
	if err != nil {
		return app, nil, &ports.ValidationError{Code: "BAD_PRIVATE_KEY", Message: err.Error()}
	}
	jwt, err := appJWT(appID, key, time.Now())
	if err != nil {
		return app, nil, err
	}
	if err := ghAppGet(ctx, c, api, jwt, "/app", &app); err != nil {
		return app, nil, err
	}
	var insts []ghInstallation
	if err := ghAppGet(ctx, c, api, jwt, "/app/installations?per_page=100", &insts); err != nil {
		return app, nil, err
	}
	return app, insts, nil
}

func installURL(web, slug string) string {
	if web != "https://github.com" {
		return web + "/github-apps/" + url.PathEscape(slug) + "/installations/new"
	}
	return web + "/apps/" + url.PathEscape(slug) + "/installations/new"
}

// pickInstallation chooses the installation for account (or the only one); choices lists them otherwise.
func pickInstallation(insts []ghInstallation, account string) (inst *ghInstallation, choices []string) {
	for i := range insts {
		if insts[i].SuspendedAt != nil {
			continue
		}
		if account != "" && strings.EqualFold(insts[i].Account.Login, account) {
			return &insts[i], nil
		}
		choices = append(choices, insts[i].Account.Login)
	}
	if account == "" && len(choices) == 1 {
		for i := range insts {
			if insts[i].SuspendedAt == nil {
				return &insts[i], nil
			}
		}
	}
	return nil, choices
}

func validAppID(s string) bool {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return err == nil && n > 0
}
