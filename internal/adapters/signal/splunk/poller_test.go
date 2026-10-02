package splunk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

func TestPoller(t *testing.T) {
	var forms []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer splunk-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		require.Equal(t, "/servicesNS/-/ops/search/jobs", r.URL.Path)
		require.NoError(t, r.ParseForm())
		forms = append(forms, r.PostForm)
		if r.PostForm.Get("search") == `| savedsearch "Checkout errors"` {
			_, _ = w.Write([]byte(`{"results":[
				{"_time":"2026-10-01T23:58:00.000+00:00","_raw":"2026-10-01 23:58:00 ERROR request failed\njava.lang.IllegalStateException: pool exhausted\n\tat com.acme.pay.Pool.take(Pool.java:42)","service":"checkout","host":"web-1","source":"/var/log/app.log","sourcetype":"app","index":"main","_cd":"12:345","_bkt":"main~42~ABC"},
				{"_time":"2026-10-01T23:58:01.000+00:00","_raw":"2026-10-01 23:58:01 INFO request served","service":"checkout"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"results":[{"service":"ledger","errors":"40"}]}`))
	}))
	defer srv.Close()
	p := NewPoller()
	p.Now = func() time.Time { return time.Unix(1759363200, 0) } // 2026-10-02T00:00:00Z
	cc := ports.ConnectorConfig{ID: "s1", Type: "splunk", Credentials: "splunk-token",
		Config: map[string]string{"base_url": srv.URL, "app": "ops", "saved_searches": "Checkout errors", "queries": "index=ledger level=error | stats count as errors by service"}}
	got := map[string][]ports.SignalEvent{}
	cursors := map[string]string{}
	emit := func(stream, c string, evs []ports.SignalEvent) error {
		got[stream], cursors[stream] = evs, c
		return nil
	}
	require.NoError(t, p.Poll(context.Background(), cc, map[string]string{}, emit))
	require.Len(t, forms, 2)
	assert.Equal(t, "oneshot", forms[0].Get("exec_mode"))
	assert.Equal(t, "json", forms[0].Get("output_mode"))
	assert.Equal(t, "1759362240", forms[0].Get("earliest_time"), "first poll: 15 minutes before the lagged end")
	assert.Equal(t, "1759363140", forms[0].Get("latest_time"), "60 s indexing lag")
	assert.Equal(t, "search index=ledger level=error | stats count as errors by service", forms[1].Get("search"))

	saved := got["saved:Checkout errors"]
	require.Len(t, saved, 1, "the INFO line is below min_severity")
	ev := saved[0]
	assert.Equal(t, "checkout", ev.Service)
	assert.Equal(t, ports.SeverityError, ev.Severity)
	assert.Equal(t, "java.lang.IllegalStateException", ev.ExceptionType, "multi-line events keep their stack")
	assert.Equal(t, "pool exhausted", ev.Message)
	require.Len(t, ev.Stack, 1)
	assert.Equal(t, "main~42~ABC:12:345", ev.ExternalID)
	assert.Equal(t, "web-1", ev.Attrs["host"])
	assert.Equal(t, "1759363140", cursors["saved:Checkout errors"])

	var queryStream string
	for k := range got {
		if k != "saved:Checkout errors" {
			queryStream = k
		}
	}
	require.Len(t, got[queryStream], 1)
	assert.Equal(t, "errors=40 service=ledger", got[queryStream][0].Message, "stats rows render as key=value")
	assert.Equal(t, "ledger", got[queryStream][0].Service)

	// Next poll starts at the stored cursor; a window that has not advanced is skipped.
	forms = nil
	require.NoError(t, p.Poll(context.Background(), cc, cursors, emit))
	assert.Empty(t, forms)
	p.Now = func() time.Time { return time.Unix(1759363500, 0) }
	require.NoError(t, p.Poll(context.Background(), cc, cursors, emit))
	require.Len(t, forms, 2)
	assert.Equal(t, "1759363140", forms[0].Get("earliest_time"))

	var ve *ports.ValidationError
	bad := cc
	bad.Credentials = "wrong"
	require.ErrorAs(t, p.Poll(context.Background(), bad, map[string]string{}, emit), &ve)
	bad.Credentials = `{"username":"svc","password":"pw"}`
	_, err := authHeader(bad.Credentials)
	require.NoError(t, err)
	bad.Config = map[string]string{"base_url": srv.URL}
	require.ErrorAs(t, p.Poll(context.Background(), bad, nil, emit), &ve)
}
