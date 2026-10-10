// Package atlassian holds what the Confluence and Jira adapters share: site configuration and auth
// (Cloud: "Connect with Atlassian" OAuth or e-mail + API token; Data Center: personal access token),
// read-only JSON GETs, query quoting, and the time-zone handling CQL/JQL date filters need.
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
	// Base is the site's browser base without a trailing slash (Confluence Cloud: https://x.atlassian.net/wiki);
	// with an API token or PAT the REST APIs live under it too.
	Base   string
	base   *url.URL
	header map[string]string
	HTTP   *httpx.Client
	// OAuth connectors call the API gateway (api is <gateway>/ex/<product>/<cloudid>) with a bearer token
	// from tokens; api is "" otherwise.
	api    string
	tokens TokenSource
	cc     ports.ConnectorConfig
}

func invalid(format string, args ...any) error {
	return &ports.ValidationError{Code: "INVALID_CONNECTOR", Message: fmt.Sprintf(format, args...)}
}

var cloudIDRE = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`) // a UUID in practice; safe in a path either way

// NewSite reads base_url, email (optional), and the credential (API token or personal access token). An
// OAuth connector (auth=oauth) instead names its site's cloud_id and takes tokens from ts.
func NewSite(service string, cc ports.ConnectorConfig, ts TokenSource) (*Site, error) {
	raw := strings.TrimRight(strings.TrimSpace(cc.Config["base_url"]), "/")
	if IsOAuth(cc) {
		switch {
		case cc.Config["oauth_status"] == "choose_site":
			return nil, invalid("%s: choose which Atlassian site to read on the Connections page", service)
		case ts == nil:
			return nil, invalid("%s: Atlassian sign-in is not available on this Hub", service)
		case !cloudIDRE.MatchString(cc.Config["cloud_id"]):
			return nil, invalid("%s: the connector has no Atlassian site; connect it again", service)
		}
	}
	u, err := url.Parse(raw)
	if raw == "" || err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, invalid("%s: base_url must be an http(s) URL", service)
	}
	if IsOAuth(cc) {
		api := strings.TrimRight(ts.APIBase(), "/") + "/ex/" + service + "/" + cc.Config["cloud_id"]
		return &Site{Base: raw, base: u, header: map[string]string{}, HTTP: httpx.New(service), api: api, tokens: ts, cc: cc}, nil
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

// APIBase is where REST paths are appended: the API gateway for OAuth connectors, else Base.
func (s *Site) APIBase() string {
	if s.api != "" {
		return s.api
	}
	return s.Base
}

// Get fetches pathAndQuery (relative to the REST base, starting with "/") as JSON. 404 maps to
// ports.ErrNotFound; 401/403 to a validation error, since only an admin fixing the credentials helps. An
// OAuth connector retries a 401 once with a freshly refreshed token.
func (s *Site) Get(ctx context.Context, pathAndQuery string, out any) error {
	if !strings.HasPrefix(pathAndQuery, "/") {
		return ports.Permanent(fmt.Errorf("%s: refusing non-relative link %q", s.HTTP.Service, pathAndQuery))
	}
	if s.tokens == nil {
		_, err := s.get(ctx, s.Base+pathAndQuery, s.header, out, "check the e-mail and API token")
		return err
	}
	for attempt := 0; ; attempt++ {
		tok, err := s.tokens.Token(ctx, s.cc, attempt > 0)
		if err != nil {
			return err
		}
		status, err := s.get(ctx, s.api+pathAndQuery, map[string]string{"Authorization": "Bearer " + tok}, out,
			"check that the account that connected can read it, and the OAuth app's scopes")
		if attempt == 0 && status == http.StatusUnauthorized {
			continue
		}
		return err
	}
}

func (s *Site) get(ctx context.Context, u string, h map[string]string, out any, fix string) (int, error) {
	err := s.HTTP.JSON(ctx, http.MethodGet, u, h, nil, out)
	switch st := httpx.StatusOf(err); st {
	case http.StatusNotFound:
		return st, fmt.Errorf("%s: %w", s.HTTP.Service, ports.ErrNotFound)
	case http.StatusUnauthorized, http.StatusForbidden:
		return st, invalid("%s rejected the credentials (HTTP %d): %s", s.HTTP.Service, st, fix)
	}
	return 0, err
}

// Owns reports whether a browser URL is on this site (same host, under the base path).
func (s *Site) Owns(raw string) bool {
	return under(raw, s.base)
}

// RelativeAPI turns an absolute link the API returned (a next page) into a path under the REST base, or ""
// when it points anywhere else.
func (s *Site) RelativeAPI(raw string) string {
	for _, b := range []string{s.APIBase(), s.Base} {
		base, err := url.Parse(b)
		if err != nil || !under(raw, base) {
			continue
		}
		u, _ := url.Parse(strings.TrimSpace(raw))
		return strings.TrimPrefix(u.RequestURI(), strings.TrimRight(base.Path, "/"))
	}
	return ""
}

func under(raw string, base *url.URL) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Hostname(), base.Hostname()) {
		return false
	}
	return strings.HasPrefix(u.Path+"/", strings.TrimRight(base.Path, "/")+"/")
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
