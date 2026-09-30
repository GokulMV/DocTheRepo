// Package firehose decodes Amazon Data Firehose HTTP endpoint deliveries (plan § 8.8). Each record is
// base64; a record may be gzip (CloudWatch Logs subscriptions always are). Records are CloudWatch Logs
// DATA_MESSAGEs (one event per log event), EventBridge events, or plain log lines. Firehose authenticates
// with the endpoint's access key in X-Amz-Firehose-Access-Key and expects {requestId, timestamp} back.
package firehose

import (
	"bytes"
	"compress/gzip"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/aws"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/loglines"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/sigutil"
	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// AccessKeyHeader carries the endpoint access key configured on the delivery stream.
const AccessKeyHeader = "X-Amz-Firehose-Access-Key"

// MaxDecoded bounds a delivery after decompression (Firehose buffers up to 64 MiB; CloudWatch Logs
// payloads compress roughly 10x, so a gzip bomb is cut off here rather than exhausting memory).
const MaxDecoded = 64 << 20

// Request is a Firehose HTTP endpoint delivery.
type Request struct {
	RequestID string `json:"requestId"`
	Timestamp int64  `json:"timestamp"`
	Records   []struct {
		Data string `json:"data"`
	} `json:"records"`
}

// Response is what Firehose expects: the request ID echoed, and an error message on failure.
type Response struct {
	RequestID    string `json:"requestId"`
	Timestamp    int64  `json:"timestamp"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// Verify checks the access key in constant time; an unset secret never accepts anything.
func Verify(req ports.WebhookRequest, secret string) error {
	got := req.HeaderValue(AccessKeyHeader)
	if secret == "" || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
		return ports.ErrInvalidSignature
	}
	return nil
}

// DecodeBody parses the delivery envelope, undoing Content-Encoding: gzip.
func DecodeBody(req ports.WebhookRequest) (Request, error) {
	body := req.Body
	if strings.EqualFold(req.HeaderValue("Content-Encoding"), "gzip") || isGzip(body) {
		b, err := gunzip(body)
		if err != nil {
			return Request{}, err
		}
		body = b
	}
	var r Request
	if err := json.Unmarshal(body, &r); err != nil {
		return Request{}, sigutil.Malformed("not a Firehose delivery: %v", err)
	}
	if r.RequestID == "" {
		r.RequestID = req.HeaderValue("X-Amz-Firehose-Request-Id")
	}
	return r, nil
}

// Parse maps every record in a delivery to events. Log lines below the connector's min_severity are
// dropped; CONTROL_MESSAGEs and non-problem EventBridge events map to none.
func Parse(r Request, cc ports.ConnectorConfig) ([]ports.SignalEvent, error) {
	min := loglines.MinSeverity(cc.Config)
	budget := MaxDecoded
	var out []ports.SignalEvent
	for i, rec := range r.Records {
		raw, err := base64.StdEncoding.DecodeString(rec.Data)
		if err != nil {
			return nil, sigutil.Malformed("record %d: data is not base64", i)
		}
		if isGzip(raw) {
			if raw, err = gunzip(raw); err != nil {
				return nil, err
			}
		}
		if budget -= len(raw); budget < 0 {
			return nil, sigutil.Malformed("delivery exceeds %d bytes decoded", MaxDecoded)
		}
		evs, err := record(raw, r, i, cc)
		if err != nil {
			return nil, err
		}
		for _, ev := range evs {
			if ev.Kind != ports.KindLogMatch || loglines.Keep(ev, min) {
				out = append(out, ev)
			}
		}
	}
	return out, nil
}

// cwlMessage is a CloudWatch Logs subscription payload.
type cwlMessage struct {
	MessageType string `json:"messageType"`
	Owner       string `json:"owner"`
	LogGroup    string `json:"logGroup"`
	LogStream   string `json:"logStream"`
	LogEvents   []struct {
		ID        string `json:"id"`
		Timestamp int64  `json:"timestamp"`
		Message   string `json:"message"`
	} `json:"logEvents"`
}

func record(raw []byte, r Request, idx int, cc ports.ConnectorConfig) ([]ports.SignalEvent, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var probe map[string]json.RawMessage
		if json.Unmarshal(trimmed, &probe) == nil {
			if _, ok := probe["logEvents"]; ok {
				var m cwlMessage
				if err := json.Unmarshal(trimmed, &m); err != nil {
					return nil, sigutil.Malformed("record %d: bad CloudWatch Logs payload", idx)
				}
				return cwl(m, cc), nil
			}
			if _, ok := probe["detail-type"]; ok {
				v, err := sigutil.Decode(trimmed)
				if err != nil {
					return nil, err
				}
				if ev, ok := aws.Event(v, cc.ID); ok {
					return []ports.SignalEvent{ev}, nil
				}
				return nil, nil
			}
		}
	}
	// Anything else is newline-delimited log lines (Firehose from an agent, Lambda, or a JSON log record).
	var out []ports.SignalEvent
	for n, line := range splitLines(string(raw)) {
		ev := ports.SignalEvent{ConnectorID: cc.ID, Source: "cloudwatch", OccurredAt: millis(r.Timestamp),
			ExternalID: r.RequestID + ":" + itoa(idx) + ":" + itoa(n), Attrs: map[string]string{"firehose.delivery": cc.Config["stream"]}}
		loglines.Fill(&ev, line, "")
		out = append(out, ev)
	}
	return out, nil
}

func cwl(m cwlMessage, cc ports.ConnectorConfig) []ports.SignalEvent {
	if m.MessageType != "DATA_MESSAGE" {
		return nil // CONTROL_MESSAGE: CloudWatch checking the destination is reachable
	}
	out := make([]ports.SignalEvent, 0, len(m.LogEvents))
	for _, le := range m.LogEvents {
		ev := ports.SignalEvent{ConnectorID: cc.ID, Source: "cloudwatch", ExternalID: le.ID, OccurredAt: millis(le.Timestamp),
			Attrs: map[string]string{"log_group": m.LogGroup, "log_stream": m.LogStream, "aws.account": m.Owner}}
		loglines.Fill(&ev, le.Message, "")
		ev.Attrs[signals.DerivedServiceAttr] = serviceFromLogGroup(m.LogGroup)
		out = append(out, ev)
	}
	return out
}

// serviceFromLogGroup reads the conventional names: /aws/lambda/<fn>, /ecs/<svc>, /aws/ecs/<cluster>/<svc>,
// /aws/eks/<cluster>/…, or the last path segment.
func serviceFromLogGroup(g string) string {
	parts := strings.FieldsFunc(g, func(r rune) bool { return r == '/' })
	if len(parts) == 0 {
		return ""
	}
	if len(parts) >= 3 && parts[0] == "aws" && parts[1] == "lambda" {
		return parts[2]
	}
	return parts[len(parts)-1]
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func isGzip(b []byte) bool { return len(b) > 2 && b[0] == 0x1f && b[1] == 0x8b }

func gunzip(b []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, sigutil.Malformed("bad gzip data")
	}
	defer zr.Close()
	out, err := io.ReadAll(io.LimitReader(zr, MaxDecoded+1))
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, sigutil.Malformed("bad gzip data")
	}
	if err != nil {
		return nil, sigutil.Malformed("truncated gzip data")
	}
	if len(out) > MaxDecoded {
		return nil, sigutil.Malformed("delivery exceeds %d bytes decoded", MaxDecoded)
	}
	return out, nil
}

func millis(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

func itoa(n int) string { return strconv.Itoa(n) }
