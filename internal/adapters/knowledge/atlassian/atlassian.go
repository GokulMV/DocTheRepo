// Package atlassian holds what the Confluence and Jira adapters share: site configuration and auth
// (Cloud: e-mail + API token; Data Center: personal access token), read-only JSON GETs, query quoting, and
// the time-zone handling CQL/JQL date filters need.
package atlassian

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/adapters/httpx"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Site is one configured Atlassian site.
type Site struct {
	// Base is the site's REST base without a trailing slash (Confluence Cloud: https://x.atlassian.net/wiki).
	Base   string
	base   *url.URL
	header map[string]string
	HTTP   *httpx.Client
}

func invalid(format string, args ...any) error {
	return &ports.ValidationError{Code: "INVALID_CONNECTOR", Message: fmt.Sprintf(format, args...)}
}

// NewSite reads base_url, email (optional), and the credential (API token or personal access token).
func NewSite(service string, cc ports.ConnectorConfig) (*Site, error) {
	raw := strings.TrimRight(strings.TrimSpace(cc.Config["base_url"]), "/")
	u, err := url.Parse(raw)
	if raw == "" || err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, invalid("%s: base_url must be an http(s) URL", service)
	}
	token := strings.TrimSpace(cc.Credentials)
	if token == "" {
		return nil, invalid("%s: an API token is required", service)
	}
	h := map[string]string{}
	if email := strings.TrimSpace(cc.Config["email"]); email != "" {
		h["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(email+":"+token))
	} else {
		h["Authorization"] = "Bearer " + token
	}
	return &Site{Base: raw, base: u, header: h, HTTP: httpx.New(service)}, nil
}

// Get fetches pathAndQuery (relative to Base, starting with "/") as JSON. 404 maps to ports.ErrNotFound;
// 401/403 to a validation error, since only an admin fixing the token helps.
func (s *Site) Get(ctx context.Context, pathAndQuery string, out any) error {
	if !strings.HasPrefix(pathAndQuery, "/") {
		return ports.Permanent(fmt.Errorf("%s: refusing non-relative link %q", s.HTTP.Service, pathAndQuery))
	}
	err := s.HTTP.JSON(ctx, http.MethodGet, s.Base+pathAndQuery, s.header, nil, out)
	switch httpx.StatusOf(err) {
	case http.StatusNotFound:
		return fmt.Errorf("%s: %w", s.HTTP.Service, ports.ErrNotFound)
	case http.StatusUnauthorized, http.StatusForbidden:
		return invalid("%s rejected the credentials (HTTP %d): check the e-mail and API token", s.HTTP.Service, httpx.StatusOf(err))
	}
	return err
}

// Owns reports whether a browser URL is on this site (same host, under the base path).
func (s *Site) Owns(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Hostname(), s.base.Hostname()) {
		return false
	}
	return strings.HasPrefix(u.Path+"/", strings.TrimRight(s.base.Path, "/")+"/")
}

var keyRE = regexp.MustCompile(`^~?[A-Za-z0-9_-]{1,255}$`)

// Keys parses a comma- or space-separated list of space/project keys.
func Keys(service, field, raw string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, k := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == ';' }) {
		if !keyRE.MatchString(k) {
			return nil, invalid("%s: %q is not a valid key in %s", service, k, field)
		}
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	if len(out) == 0 {
		return nil, invalid("%s: %s is required", service, field)
	}
	return out, nil
}

// Quote quotes a CQL/JQL string literal.
func Quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// QuoteAll quotes and comma-joins values for an `in (...)` clause.
func QuoteAll(vs []string) string {
	q := make([]string, len(vs))
	for i, v := range vs {
		q[i] = Quote(v)
	}
	return strings.Join(q, ", ")
}

// UnknownZoneOverlap is subtracted from cursors when the site's time zone is unknown: CQL and JQL compare
// dates in the reading user's zone, which is at most 14 hours from UTC. Re-reading a few pages is cheap
// (unchanged content is not re-embedded); missing one is not.
const UnknownZoneOverlap = 14 * time.Hour

// QueryTime renders a cursor (RFC 3339) as a minute-precision CQL/JQL date literal in loc, rounded down,
// so a ">=" filter never skips anything. A nil loc applies UnknownZoneOverlap.
func QueryTime(cursor time.Time, loc *time.Location) string {
	if loc == nil {
		cursor, loc = cursor.Add(-UnknownZoneOverlap), time.UTC
	}
	return cursor.In(loc).Truncate(time.Minute).Format("2006-01-02 15:04")
}

// Zone loads an IANA zone name ("" or unknown → nil).
func Zone(name string) *time.Location {
	if name == "" {
		return nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil
	}
	return loc
}

// ParseCursor reads a stored cursor; "" or garbage → zero time.
func ParseCursor(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// FormatCursor writes a cursor.
func FormatCursor(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// IsNotFound reports a 404 from the site.
func IsNotFound(err error) bool { return errors.Is(err, ports.ErrNotFound) }
