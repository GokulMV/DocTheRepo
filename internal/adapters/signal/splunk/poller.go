package splunk

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/adapters/httpx"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/loglines"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/sigutil"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Poller implements ports.SignalPoller for "splunk" connectors in poll mode. Config: base_url (the REST
// management port, e.g. https://splunk.example.com:8089), saved_searches (names, comma-separated) and/or
// queries (SPL, one per line), app (namespace, default search), lookback_minutes (first poll, default 15),
// lag_seconds (indexing delay left out of each window, default 60), max_results (per search and poll,
// default 5000), min_severity (default warning), field.<name> mappings. Credentials: an authentication
// token, or {"username","password"}.
type Poller struct {
	HTTP *httpx.Client
	Now  func() time.Time
}

// NewPoller returns the poller.
func NewPoller() *Poller { return &Poller{HTTP: httpx.New("splunk")} }

// Type implements ports.SignalPoller.
func (*Poller) Type() string { return "splunk" }

func (p *Poller) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func invalid(msg string) error {
	return &ports.ValidationError{Code: "INVALID_CONFIG", Message: "splunk: " + msg}
}

// search is one configured saved search or query; Stream keys its cursor.
type search struct{ Stream, SPL, Name string }

func searches(cfg map[string]string) []search {
	var out []search
	for _, n := range strings.Split(cfg["saved_searches"], ",") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, search{Stream: "saved:" + n, Name: n, SPL: "| savedsearch " + strconv.Quote(n)})
		}
	}
	for _, q := range strings.Split(cfg["queries"], "\n") {
		if q = strings.TrimSpace(q); q == "" {
			continue
		}
		spl := q
		if !strings.HasPrefix(spl, "|") && !strings.HasPrefix(strings.ToLower(spl), "search ") {
			spl = "search " + spl
		}
		h := sha256.Sum256([]byte(q))
		out = append(out, search{Stream: "query:" + hex.EncodeToString(h[:6]), Name: q, SPL: spl})
	}
	return out
}

func intCfg(cfg map[string]string, key string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(cfg[key])); err == nil && n > 0 {
		return n
	}
	return def
}

func authHeader(creds string) (string, error) {
	creds = strings.TrimSpace(creds)
	if creds == "" {
		return "", invalid(`credentials are required: an authentication token, or {"username","password"}`)
	}
	if strings.HasPrefix(creds, "{") {
		var c struct{ Username, Password string }
		if err := json.Unmarshal([]byte(creds), &c); err != nil || c.Username == "" {
			return "", invalid(`credentials must be a token or {"username","password"}`)
		}
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(c.Username+":"+c.Password)), nil
	}
	return "Bearer " + creds, nil
}

// Poll implements ports.SignalPoller. Each search's window is [cursor, now - lag); the cursor is the
// window's end, stored after the window's events are committed.
func (p *Poller) Poll(ctx context.Context, cc ports.ConnectorConfig, cursors map[string]string,
	emit func(stream, cursor string, events []ports.SignalEvent) error) error {
	base := strings.TrimRight(strings.TrimSpace(cc.Config["base_url"]), "/")
	if u, err := url.Parse(base); base == "" || err != nil || u.Scheme != "https" && u.Scheme != "http" {
		return invalid("base_url must be the REST API URL, e.g. https://splunk.example.com:8089")
	}
	ss := searches(cc.Config)
	if len(ss) == 0 {
		return invalid("set saved_searches and/or queries")
	}
	auth, err := authHeader(cc.Credentials)
	if err != nil {
		return err
	}
	app := strings.TrimSpace(cc.Config["app"])
	if app == "" {
		app = "search"
	}
	latest := p.now().Add(-time.Duration(intCfg(cc.Config, "lag_seconds", 60)) * time.Second).Unix()
	min := loglines.MinSeverity(cc.Config)
	for _, s := range ss {
		earliest, err := strconv.ParseInt(cursors[s.Stream], 10, 64)
		if err != nil || earliest <= 0 {
			earliest = latest - int64(intCfg(cc.Config, "lookback_minutes", 15))*60
		}
		if earliest >= latest {
			continue
		}
		rows, err := p.run(ctx, base, app, auth, s.SPL, earliest, latest, intCfg(cc.Config, "max_results", 5000))
		if err != nil {
			return fmt.Errorf("search %q: %w", s.Name, err)
		}
		var events []ports.SignalEvent
		for _, row := range rows {
			if ev, ok := rowEvent(row, s, cc); ok && loglines.Keep(ev, min) {
				events = append(events, ev)
			}
		}
		if err := emit(s.Stream, strconv.FormatInt(latest, 10), events); err != nil {
			return err
		}
	}
	return nil
}

// run executes a oneshot search job (results returned inline, nothing left running).
func (p *Poller) run(ctx context.Context, base, app, auth, spl string, earliest, latest int64, maxResults int) ([]map[string]any, error) {
	form := url.Values{"search": {spl}, "exec_mode": {"oneshot"}, "output_mode": {"json"}, "count": {strconv.Itoa(maxResults)},
		"earliest_time": {strconv.FormatInt(earliest, 10)}, "latest_time": {strconv.FormatInt(latest, 10)}}
	endpoint := base + "/servicesNS/-/" + url.PathEscape(app) + "/search/jobs"
	resp, err := p.HTTP.Do(ctx, func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Authorization", auth)
		}
		return req, err
	})
	if code := httpx.StatusOf(err); code == http.StatusUnauthorized || code == http.StatusForbidden {
		return nil, invalid("Splunk rejected the credentials (the token or user needs the search capability)")
	}
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, ports.Transient(fmt.Errorf("splunk: decode results: %w", err))
	}
	return out.Results, nil
}

// rowEvent turns one result row into a log event. Rows without a message (a stats search) are rendered
// as sorted key=value pairs so each distinct row groups on its own.
func rowEvent(row map[string]any, s search, cc ports.ConnectorConfig) (ports.SignalEvent, bool) {
	msg := field(row, cc.Config, "message")
	if msg == "" {
		keys := make([]string, 0, len(row))
		for k := range row {
			if !strings.HasPrefix(k, "_") {
				keys = append(keys, k)
			}
		}
		if len(keys) == 0 {
			return ports.SignalEvent{}, false
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = k + "=" + fmt.Sprint(row[k])
		}
		msg = strings.Join(parts, " ")
	}
	id := firstNonEmpty(str(row["_cd"]), "")
	if id != "" {
		id = str(row["_bkt"]) + ":" + id
	} else {
		h := sha256.Sum256([]byte(s.Stream + "\x00" + str(row["_time"]) + "\x00" + str(row["host"]) + "\x00" + str(row["source"]) + "\x00" + msg))
		id = hex.EncodeToString(h[:12])
	}
	ev := ports.SignalEvent{ConnectorID: cc.ID, Source: "splunk", ExternalID: id, OccurredAt: sigutil.Time(str(row["_time"])),
		Service: field(row, cc.Config, "service"), Environment: field(row, cc.Config, "environment"),
		Attrs: map[string]string{"splunk.search": s.Name, "host": str(row["host"]), "source": str(row["source"]),
			"sourcetype": str(row["sourcetype"]), "index": str(row["index"])}}
	loglines.Fill(&ev, msg, field(row, cc.Config, "severity"))
	for k, v := range ev.Attrs {
		if v == "" {
			delete(ev.Attrs, k)
		}
	}
	return ev, true
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	case []any: // multivalue fields
		if len(x) > 0 {
			return str(x[0])
		}
		return ""
	}
	return fmt.Sprint(v)
}
