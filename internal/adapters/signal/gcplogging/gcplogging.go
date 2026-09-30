// Package gcplogging polls Cloud Logging entries.list for installs without a Log Router sink to Pub/Sub
// (plan § 8.16): per project, severity>=ERROR (configurable) and timestamp at or after the cursor.
// entries.list allows 60 requests a minute per project, so a poll reads at most a few pages; a project
// noisier than that belongs on a sink.
package gcplogging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/pubsub"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/sigutil"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Scope is the read-only logging scope.
const Scope = "https://www.googleapis.com/auth/logging.read"

// DefaultEndpoint is the Cloud Logging API.
const DefaultEndpoint = "https://logging.googleapis.com/v2/entries:list"

const (
	maxPages = 5
	pageSize = 1000
)

// Poller implements ports.SignalPoller for "gcp" connectors in poll mode.
type Poller struct {
	// Client returns an authenticated HTTP client (default: service-account JSON or ADC); cached per
	// connector credentials.
	Client   func(ctx context.Context, cc ports.ConnectorConfig) (*http.Client, error)
	Endpoint string
	Now      func() time.Time

	mu    sync.Mutex
	cache map[string]cachedClient
}

type cachedClient struct {
	creds string
	c     *http.Client
}

// New returns a poller using Google credentials.
func New() *Poller { return &Poller{Client: HTTPClient, Endpoint: DefaultEndpoint} }

// Type implements ports.SignalPoller.
func (*Poller) Type() string { return "gcp" }

// Poll implements ports.SignalPoller. Streams: "logs:<project>".
func (p *Poller) Poll(ctx context.Context, cc ports.ConnectorConfig, cursors map[string]string,
	emit func(stream, cursor string, events []ports.SignalEvent) error) error {
	projects := projects(cc.Config)
	if len(projects) == 0 {
		return &ports.ValidationError{Code: "INVALID_CONFIG", Message: "set project (or projects, comma-separated) to poll Cloud Logging"}
	}
	hc, err := p.client(ctx, cc)
	if err != nil {
		return err
	}
	for _, proj := range projects {
		if err := p.pollProject(ctx, hc, cc, proj, cursors["logs:"+proj], emit); err != nil {
			return fmt.Errorf("project %s: %w", proj, err)
		}
	}
	return nil
}

func (p *Poller) pollProject(ctx context.Context, hc *http.Client, cc ports.ConnectorConfig, project, raw string,
	emit func(string, string, []ports.SignalEvent) error) error {
	now := p.now().UTC()
	cur := sigutil.ParseCursor(raw)
	start := cur.At
	if _, err := time.Parse(time.RFC3339Nano, start); err != nil {
		start = now.Add(-lookback(cc.Config)).Format(time.RFC3339Nano)
		cur = sigutil.Cursor{At: start, Seen: map[string]bool{}}
	}
	sev := strings.TrimSpace(cc.Config["filter"])
	if sev == "" {
		sev = "severity>=ERROR"
	}
	req := map[string]any{"resourceNames": []string{"projects/" + project}, "orderBy": "timestamp asc", "pageSize": pageSize,
		"filter": fmt.Sprintf(`(%s) AND timestamp>="%s" AND timestamp<="%s"`, sev, start, now.Format(time.RFC3339Nano))}
	next := cur
	var events []ports.SignalEvent
	for page := 0; page < maxPages; page++ {
		var out struct {
			Entries       []json.RawMessage `json:"entries"`
			NextPageToken string            `json:"nextPageToken"`
		}
		if err := p.call(ctx, hc, req, &out); err != nil {
			return err
		}
		for _, raw := range out.Entries {
			var head struct {
				InsertID  string `json:"insertId"`
				Timestamp string `json:"timestamp"`
			}
			_ = json.Unmarshal(raw, &head)
			at := normalize(head.Timestamp)
			if cur.Skip(at, head.InsertID) {
				continue
			}
			next.Advance(at, head.InsertID, lessTime)
			events = append(events, pubsub.Events(pubsub.Message{Data: raw}, cc)...)
		}
		if out.NextPageToken == "" {
			break
		}
		req["pageToken"] = out.NextPageToken
	}
	return emit("logs:"+project, next.String(), events)
}

// StatusError is a non-2xx answer.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("cloud logging: HTTP %d: %s", e.Status, e.Body)
}

func (p *Poller) call(ctx context.Context, hc *http.Client, in, out any) error {
	b, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.Endpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode/100 != 2 {
		if resp.StatusCode == http.StatusTooManyRequests {
			return ports.TransientAfter(fmt.Errorf("cloud logging rate limit"), time.Minute)
		}
		if len(body) > 512 {
			body = body[:512]
		}
		return &StatusError{Status: resp.StatusCode, Body: string(body)}
	}
	return json.Unmarshal(body, out)
}

func (p *Poller) client(ctx context.Context, cc ports.ConnectorConfig) (*http.Client, error) {
	p.mu.Lock()
	if c, ok := p.cache[cc.ID]; ok && c.creds == cc.Credentials {
		p.mu.Unlock()
		return c.c, nil
	}
	p.mu.Unlock()
	hc, err := p.Client(ctx, cc)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	if p.cache == nil {
		p.cache = map[string]cachedClient{}
	}
	p.cache[cc.ID] = cachedClient{creds: cc.Credentials, c: hc}
	p.mu.Unlock()
	return hc, nil
}

// HTTPClient authenticates with a service-account key JSON from the connector, or Application Default
// Credentials (Workload Identity) when none is set.
func HTTPClient(ctx context.Context, cc ports.ConnectorConfig) (*http.Client, error) {
	var ts oauth2.TokenSource
	if strings.TrimSpace(cc.Credentials) != "" {
		creds, err := google.CredentialsFromJSONWithType(ctx, []byte(cc.Credentials), google.ServiceAccount, Scope)
		if err != nil {
			return nil, &ports.ValidationError{Code: "INVALID_CREDENTIALS", Message: "credentials must be a service-account key JSON"}
		}
		ts = creds.TokenSource
	} else {
		var err error
		if ts, err = google.DefaultTokenSource(ctx, Scope); err != nil {
			return nil, fmt.Errorf("cloud logging credentials (Application Default Credentials): %w", err)
		}
	}
	hc := oauth2.NewClient(context.WithoutCancel(ctx), ts)
	hc.Timeout = time.Minute
	return hc, nil
}

func projects(cfg map[string]string) []string {
	var out []string
	for _, p := range strings.Split(cfg["projects"]+","+cfg["project"], ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func lookback(cfg map[string]string) time.Duration {
	n, err := strconv.Atoi(cfg["lookback_minutes"])
	if err != nil || n <= 0 {
		n = 5
	}
	return time.Duration(min(n, 1440)) * time.Minute
}

// normalize re-formats a timestamp so cursor comparisons are exact (the API returns varying precision).
func normalize(s string) string {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC().Format(time.RFC3339Nano)
	}
	return s
}

func lessTime(a, b string) bool {
	x, _ := time.Parse(time.RFC3339Nano, a)
	y, _ := time.Parse(time.RFC3339Nano, b)
	return x.Before(y)
}

func (p *Poller) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}
