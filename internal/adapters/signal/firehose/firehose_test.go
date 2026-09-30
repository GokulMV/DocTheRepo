package firehose

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

func gz(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write(b)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func delivery(t *testing.T, records ...[]byte) []byte {
	t.Helper()
	r := map[string]any{"requestId": "req-1", "timestamp": int64(1790000000000)}
	var recs []map[string]string
	for _, rec := range records {
		recs = append(recs, map[string]string{"data": base64.StdEncoding.EncodeToString(rec)})
	}
	r["records"] = recs
	b, err := json.Marshal(r)
	require.NoError(t, err)
	return b
}

const cwlData = `{"messageType":"DATA_MESSAGE","owner":"123456789012","logGroup":"/aws/lambda/checkout-fn","logStream":"2026/09/30/[$LATEST]abc",
"subscriptionFilters":["errors"],"logEvents":[
{"id":"3700000000000000000000000000000000000000000000000001","timestamp":1790000000123,"message":"[ERROR] 2026-09-30T10:00:00Z req-9 Traceback (most recent call last):\n  File \"/var/task/app.py\", line 12, in handler\n    charge(order)\n  File \"/var/task/billing.py\", line 40, in charge\n    raise TimeoutError(\"stripe timed out after 3000ms\")\nTimeoutError: stripe timed out after 3000ms"},
{"id":"3700000000000000000000000000000000000000000000000002","timestamp":1790000000456,"message":"{\"level\":\"info\",\"msg\":\"order placed\",\"service\":\"checkout\"}"},
{"id":"3700000000000000000000000000000000000000000000000003","timestamp":1790000000789,"message":"{\"level\":\"error\",\"msg\":\"payment declined\",\"service\":\"checkout\",\"error.type\":\"CardError\"}"}]}`

func TestCloudWatchLogsViaFirehose(t *testing.T) {
	cc := ports.ConnectorConfig{ID: "c1", Type: "firehose", WebhookSecret: "k"}
	req := ports.WebhookRequest{Header: map[string][]string{AccessKeyHeader: {"k"}}, Body: delivery(t, gz(t, []byte(cwlData)),
		gz(t, []byte(`{"messageType":"CONTROL_MESSAGE","owner":"CloudwatchLogs","logGroup":"","logStream":"","logEvents":[{"id":"","timestamp":1,"message":"CWL CONTROL MESSAGE: Checking health of destination Firehose."}]}`)))}
	require.NoError(t, Verify(req, cc.WebhookSecret))
	r, err := DecodeBody(req)
	require.NoError(t, err)
	assert.Equal(t, "req-1", r.RequestID)
	evs, err := Parse(r, cc)
	require.NoError(t, err)
	require.Len(t, evs, 2, "the info line is below min_severity and the control message is not an event")

	py := evs[0]
	assert.Equal(t, ports.KindLogMatch, py.Kind)
	assert.Equal(t, ports.SeverityError, py.Severity)
	assert.Equal(t, "TimeoutError", py.ExceptionType)
	assert.Equal(t, "3700000000000000000000000000000000000000000000000001", py.ExternalID)
	assert.Equal(t, "/aws/lambda/checkout-fn", py.Attrs["log_group"])
	assert.Equal(t, "123456789012", py.Attrs["aws.account"])
	assert.Equal(t, time.UnixMilli(1790000000123).UTC(), py.OccurredAt)
	require.NoError(t, signals.Prepare(&py, signals.Options{}))
	assert.Equal(t, "checkout-fn", py.Service, "a Lambda log group names the service")
	assert.Equal(t, []string{"/var/task/billing.py:charge", "/var/task/app.py:handler"}, signals.Frames(py.Stack))

	js := evs[1]
	assert.Equal(t, "CardError", js.ExceptionType)
	require.NoError(t, signals.Prepare(&js, signals.Options{
		ServiceRules: []signals.ServiceRule{{Service: "mapped", Patterns: map[string]string{"log_group": "/aws/lambda/*"}}}}))
	assert.Equal(t, "checkout", js.Service, "an explicit service field wins")

	again := py
	again.Attrs = map[string]string{"log_group": "/aws/lambda/checkout-fn", signals.DerivedServiceAttr: "checkout-fn"}
	again.Service = ""
	require.NoError(t, signals.Prepare(&again, signals.Options{
		ServiceRules: []signals.ServiceRule{{Service: "mapped", Patterns: map[string]string{"log_group": "/aws/lambda/*"}}}}))
	assert.Equal(t, "mapped", again.Service, "service_map beats the derived name")
}

func TestEventBridgeAndPlainRecords(t *testing.T) {
	cc := ports.ConnectorConfig{ID: "c1", Type: "firehose", Config: map[string]string{"min_severity": "error"}}
	alarm := `{"id":"e1","detail-type":"CloudWatch Alarm State Change","source":"aws.cloudwatch","time":"2026-09-30T10:00:00Z","detail":{"alarmName":"api-5xx","state":{"value":"ALARM","reason":"x"}}}`
	ok := `{"id":"e2","detail-type":"CloudWatch Alarm State Change","source":"aws.cloudwatch","detail":{"alarmName":"api-5xx","state":{"value":"OK"}}}`
	plain := "WARN disk nearly full\nERROR worker crashed: nil map\n\n"
	body := delivery(t, []byte(alarm), []byte(ok), []byte(plain))
	req := ports.WebhookRequest{Header: map[string][]string{"Content-Encoding": {"gzip"}}, Body: gz(t, body)}
	r, err := DecodeBody(req)
	require.NoError(t, err)
	evs, err := Parse(r, cc)
	require.NoError(t, err)
	require.Len(t, evs, 2, "OK alarms are not problems; WARN is below min_severity=error")
	assert.Equal(t, ports.KindAlert, evs[0].Kind)
	assert.Equal(t, "api-5xx", evs[0].Title)
	assert.Equal(t, "req-1:2:1", evs[1].ExternalID, "plain lines get stable IDs so redeliveries dedupe")
	assert.Equal(t, "ERROR worker crashed: nil map", evs[1].Title)
}

func TestVerifyAndMalformed(t *testing.T) {
	h := func(k string) ports.WebhookRequest {
		return ports.WebhookRequest{Header: map[string][]string{"x-amz-firehose-access-key": {k}}}
	}
	assert.NoError(t, Verify(h("secret"), "secret"))
	assert.ErrorIs(t, Verify(h("wrong"), "secret"), ports.ErrInvalidSignature)
	assert.ErrorIs(t, Verify(h(""), "secret"), ports.ErrInvalidSignature)
	assert.ErrorIs(t, Verify(h(""), ""), ports.ErrInvalidSignature)

	var ve *ports.ValidationError
	_, err := DecodeBody(ports.WebhookRequest{Body: []byte("{nope")})
	assert.ErrorAs(t, err, &ve)
	r, err := DecodeBody(ports.WebhookRequest{Body: []byte(`{"requestId":"r","records":[{"data":"!!!"}]}`)})
	require.NoError(t, err)
	_, err = Parse(r, ports.ConnectorConfig{})
	assert.ErrorAs(t, err, &ve)
}

func TestGzipBombIsRefused(t *testing.T) {
	bomb := gz(t, []byte(strings.Repeat("A", MaxDecoded+10)))
	assert.Less(t, len(bomb), 1<<20)
	r, err := DecodeBody(ports.WebhookRequest{Body: delivery(t, bomb)})
	require.NoError(t, err)
	_, err = Parse(r, ports.ConnectorConfig{})
	var ve *ports.ValidationError
	assert.ErrorAs(t, err, &ve)
}
