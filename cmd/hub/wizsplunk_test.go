package main

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/decode"
	"github.com/GokulMV/DocTheRepo/internal/ingest"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/test/mocks/stubllm"
)

// TestWizSplunkEndToEnd: a Wiz webhook on a never-send-to-LLM connector opens a security issue that is
// never decoded or offered to the suggest model; a Splunk alert opens an alert issue that is decoded.
func TestWizSplunkEndToEnd(t *testing.T) {
	ctx := context.Background()
	env := newSignalEnv(t, "decode", "suggest")
	a, st, q, c := env.a, env.st, env.q, env.c

	code, out := c.call("POST", "/connectors", map[string]any{"type": "wiz", "name": "Wiz", "webhook_secret": "wiz-secret",
		"config": map[string]string{"never_send_to_llm": "true"}})
	require.Equal(t, http.StatusCreated, code, out)
	wizID := out["id"].(string)
	assert.Equal(t, "/hooks/wiz/"+wizID, out["webhook_path"])
	code, out = c.call("POST", "/connectors", map[string]any{"type": "splunk", "name": "Splunk", "webhook_secret": "splunk-secret"})
	require.Equal(t, http.StatusCreated, code, out)
	splunkID := out["id"].(string)

	wiz := `{"trigger":{"type":"Created","ruleId":"wc-1","ruleName":"Publicly exposed bucket"},"issue":{"id":"wiz-1","status":"OPEN","severity":"CRITICAL"},
		"resource":{"providerId":"arn:aws:s3:::exports","name":"exports","tags":{"service":"payments"}},"control":{"id":"wc-1","name":"Publicly exposed bucket"}}`
	n, err := a.signals.Webhook(ctx, "wiz", wizID, ports.WebhookRequest{Header: map[string][]string{"Authorization": {"Bearer wiz-secret"}}, Body: []byte(wiz)})
	require.NoError(t, err)
	require.Equal(t, 1, n)
	_, err = a.signals.Webhook(ctx, "wiz", wizID, ports.WebhookRequest{Header: map[string][]string{"Authorization": {"Bearer nope"}}, Body: []byte(wiz)})
	assert.ErrorIs(t, err, ports.ErrInvalidSignature)

	splunk := `{"result":{"service":"checkout","message":"upstream request timeout calling payments-gw","level":"ERROR"},"sid":"sid-1",
		"results_link":"https://splunk/app/search/@go?sid=sid-1","search_name":"Checkout timeouts","app":"search","owner":"admin"}`
	_, err = a.signals.Webhook(ctx, "splunk", splunkID, ports.WebhookRequest{Query: map[string][]string{"token": {"splunk-secret"}}, Body: []byte(splunk)})
	require.NoError(t, err)
	require.NoError(t, a.agg.Flush(ctx))

	var wizIssue, splunkIssue, wizSev string
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT id, severity_max::text FROM issues WHERE kind = 'security_finding'`).Scan(&wizIssue, &wizSev))
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT id FROM issues WHERE kind = 'alert'`).Scan(&splunkIssue))
	assert.Equal(t, "critical", wizSev)

	out1, err := a.decoder.Decode(ctx, wizIssue, true, "")
	require.NoError(t, err)
	assert.Equal(t, "skipped", out1.Status)
	assert.Equal(t, decode.NoLLMReason, out1.Reason, "even a forced decode stays local")
	assert.Zero(t, env.stub.Calls(stubllm.Decode))

	runJob(t, a, q, ports.JobDecodeIssue)
	runJob(t, a, q, ports.JobDecodeIssue)
	assert.Equal(t, 1, env.stub.Calls(stubllm.Decode), "only the Splunk alert was sent to the model")
	var decoded int
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT count(*) FROM issues WHERE decode_id IS NOT NULL AND id = $1`, splunkIssue).Scan(&decoded))
	assert.Equal(t, 1, decoded)

	// Rule proposals never show the model a never-send issue.
	got, err := a.suggest.FromText(ctx, "The payments service bucket exports is publicly exposed; also checkout timeouts are expected.", []string{"payments", "checkout"}, "")
	require.NoError(t, err)
	for _, cand := range got.Candidates {
		assert.NotEqual(t, wizIssue, cand.IssueID)
	}

	// The source filter works for the new sources.
	code, out = c.call("GET", "/issues?source=wiz", nil)
	require.Equal(t, http.StatusOK, code, out)
	require.Len(t, out["items"], 1)
	assert.Equal(t, "Publicly exposed bucket on exports", out["items"].([]any)[0].(map[string]any)["title"])
	var marked bool
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT attrs->>$2 = 'true' FROM event_samples WHERE issue_id = $1 LIMIT 1`, wizIssue, ingest.NoLLMAttr).Scan(&marked))
	assert.True(t, marked, "samples record the connector policy")
}
