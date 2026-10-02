package wiz

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/adapters/httpx"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/sigutil"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// DefaultAuthURL is Wiz's OAuth token endpoint (service accounts, client credentials).
const DefaultAuthURL = "https://auth.app.wiz.io/oauth/token"

// Query reads open issues updated since the cursor (Wiz GraphQL issuesV2; read-only scope read:issues).
const Query = `query DTHIssues($filterBy: IssueFilters, $first: Int, $after: String) {
  issues: issuesV2(filterBy: $filterBy, first: $first, after: $after) {
    nodes {
      id status severity createdAt updatedAt statusChangedAt
      sourceRule { __typename ... on Control { id name description } ... on CloudEventRule { id name description } ... on CloudConfigurationRule { id name description } }
      entitySnapshot { id type nativeType name providerId externalId cloudPlatform region subscriptionName subscriptionExternalId tags }
      projects { name }
    }
    pageInfo { hasNextPage endCursor }
  }
}`

const (
	pageSize = 500
	maxPages = 20
)

// Poller implements ports.SignalPoller for "wiz" connectors in poll mode. Config: api_url (the tenant's
// GraphQL endpoint, e.g. https://api.us17.app.wiz.io/graphql), auth_url (default DefaultAuthURL), statuses
// (default OPEN,IN_PROGRESS), severities (default all), lookback_hours (first poll, default 24),
// time_filter (the IssueFilters field compared with the cursor, default updatedAt). Credentials:
// {"client_id","client_secret"} of a service account with read:issues.
type Poller struct {
	HTTP *httpx.Client
	Now  func() time.Time

	mu     sync.Mutex
	tokens map[string]token
}

type token struct {
	creds, value string
	expires      time.Time
}

// NewPoller returns the poller.
func NewPoller() *Poller { return &Poller{HTTP: httpx.New("wiz")} }

// Type implements ports.SignalPoller.
func (*Poller) Type() string { return "wiz" }

func (p *Poller) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func invalid(msg string) error {
	return &ports.ValidationError{Code: "INVALID_CONFIG", Message: "wiz: " + msg}
}

func list(s, def string) []string {
	if strings.TrimSpace(s) == "" {
		s = def
	}
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.ToUpper(strings.TrimSpace(x)); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// Poll implements ports.SignalPoller. One stream: "issues".
func (p *Poller) Poll(ctx context.Context, cc ports.ConnectorConfig, cursors map[string]string,
	emit func(stream, cursor string, events []ports.SignalEvent) error) error {
	api := strings.TrimSpace(cc.Config["api_url"])
	if u, err := url.Parse(api); api == "" || err != nil || u.Scheme != "https" && u.Scheme != "http" {
		return invalid("api_url must be the tenant GraphQL endpoint, e.g. https://api.us17.app.wiz.io/graphql")
	}
	tok, err := p.token(ctx, cc)
	if err != nil {
		return err
	}
	since := sigutil.Time(cursors["issues"])
	if since.IsZero() {
		hours := 24
		fmt.Sscan(cc.Config["lookback_hours"], &hours)
		since = p.now().Add(-time.Duration(max(hours, 1)) * time.Hour)
	}
	field := strings.TrimSpace(cc.Config["time_filter"])
	if field == "" {
		field = "updatedAt"
	}
	filter := map[string]any{"status": list(cc.Config["statuses"], "OPEN,IN_PROGRESS"), field: map[string]any{"after": since.UTC().Format(time.RFC3339Nano)}}
	if sev := list(cc.Config["severities"], ""); len(sev) > 0 {
		filter["severity"] = sev
	}
	vars := map[string]any{"filterBy": filter, "first": pageSize}
	newest := since
	var events []ports.SignalEvent
	for page := 0; page < maxPages; page++ {
		var out struct {
			Data struct {
				Issues struct {
					Nodes    []json.RawMessage `json:"nodes"`
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"issues"`
			} `json:"data"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		err := p.HTTP.JSON(ctx, http.MethodPost, api, map[string]string{"Authorization": "Bearer " + tok},
			map[string]any{"query": Query, "variables": vars}, &out)
		if httpx.StatusOf(err) == http.StatusUnauthorized {
			p.forget(cc.ID)
		}
		if err != nil {
			return err
		}
		if len(out.Errors) > 0 {
			return ports.Permanent(fmt.Errorf("wiz graphql: %s", out.Errors[0].Message))
		}
		for _, raw := range out.Data.Issues.Nodes {
			v, err := sigutil.Decode(raw)
			if err != nil {
				continue
			}
			if closed[strings.ToUpper(sigutil.Str(v, "status"))] {
				continue
			}
			ev, ok := Event(v, v, cc)
			if !ok {
				continue
			}
			if t := sigutil.Time(sigutil.First(v, "updatedAt", "statusChangedAt", "createdAt")); t.After(newest) {
				newest = t
			}
			events = append(events, ev)
		}
		pi := out.Data.Issues.PageInfo
		if !pi.HasNextPage || pi.EndCursor == "" {
			break
		}
		vars["after"] = pi.EndCursor
	}
	return emit("issues", newest.UTC().Format(time.RFC3339Nano), events)
}

// token returns a cached OAuth access token for the connector's service account.
func (p *Poller) token(ctx context.Context, cc ports.ConnectorConfig) (string, error) {
	var c struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := json.Unmarshal([]byte(cc.Credentials), &c); err != nil || c.ClientID == "" || c.ClientSecret == "" {
		return "", invalid(`credentials must be {"client_id":"…","client_secret":"…"} of a service account with read:issues`)
	}
	p.mu.Lock()
	if t, ok := p.tokens[cc.ID]; ok && t.creds == cc.Credentials && p.now().Before(t.expires) {
		p.mu.Unlock()
		return t.value, nil
	}
	p.mu.Unlock()
	authURL := strings.TrimSpace(cc.Config["auth_url"])
	if authURL == "" {
		authURL = DefaultAuthURL
	}
	form := url.Values{"grant_type": {"client_credentials"}, "audience": {"wiz-api"}, "client_id": {c.ClientID}, "client_secret": {c.ClientSecret}}
	resp, err := p.HTTP.Do(ctx, func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPost, authURL, strings.NewReader(form.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		return req, err
	})
	if code := httpx.StatusOf(err); code == http.StatusUnauthorized || code == http.StatusBadRequest || code == http.StatusForbidden {
		return "", invalid("the service account was rejected: check client_id, client_secret, and auth_url")
	}
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string  `json:"access_token"`
		ExpiresIn   float64 `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.AccessToken == "" {
		return "", ports.Transient(fmt.Errorf("wiz: no access token in the auth response"))
	}
	ttl := time.Duration(out.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}
	p.mu.Lock()
	if p.tokens == nil {
		p.tokens = map[string]token{}
	}
	p.tokens[cc.ID] = token{creds: cc.Credentials, value: out.AccessToken, expires: p.now().Add(ttl - time.Minute)}
	p.mu.Unlock()
	return out.AccessToken, nil
}

func (p *Poller) forget(id string) {
	p.mu.Lock()
	delete(p.tokens, id)
	p.mu.Unlock()
}
