// Package sigutil holds what every signal adapter needs: authentication schemes for tools that sign and
// for tools that cannot, tolerant JSON field access, and time parsing.
package sigutil

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// TokenHeader is the header operators can set in tools that send custom headers.
const TokenHeader = "X-DTH-Token"

// VerifyToken authenticates tools that cannot sign payloads (Alertmanager, Grafana, Datadog, Opsgenie, GCP
// Monitoring, EventBridge API destinations): the connector's secret as a bearer token, as the basic-auth
// password, in the X-DTH-Token header, or as ?token= for tools that only allow a URL. Constant-time.
func VerifyToken(req ports.WebhookRequest, secret string) error {
	if secret == "" {
		return ports.ErrInvalidSignature // an unset secret never accepts unauthenticated deliveries
	}
	candidates := []string{req.HeaderValue(TokenHeader)}
	if auth := req.HeaderValue("Authorization"); auth != "" {
		scheme, cred, _ := strings.Cut(auth, " ")
		switch strings.ToLower(scheme) {
		case "bearer", "token":
			candidates = append(candidates, strings.TrimSpace(cred))
		case "basic":
			if raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(cred)); err == nil {
				if _, pw, ok := strings.Cut(string(raw), ":"); ok {
					candidates = append(candidates, pw)
				}
			}
		}
	}
	if q := req.Query["token"]; len(q) > 0 {
		candidates = append(candidates, q[0])
	}
	ok := 0
	for _, c := range candidates {
		if c != "" && subtle.ConstantTimeCompare([]byte(c), []byte(secret)) == 1 {
			ok = 1
		}
	}
	if ok == 0 {
		return ports.ErrInvalidSignature
	}
	return nil
}

// VerifyHMAC checks a hex HMAC-SHA256 of the body.
func VerifyHMAC(body []byte, secret, sigHex string) error {
	if secret == "" || sigHex == "" {
		return ports.ErrInvalidSignature
	}
	want, err := hex.DecodeString(strings.TrimSpace(sigHex))
	if err != nil {
		return ports.ErrInvalidSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	if !hmac.Equal(mac.Sum(nil), want) {
		return ports.ErrInvalidSignature
	}
	return nil
}

// Malformed wraps a payload error for a 400 response.
func Malformed(format string, args ...any) error {
	return &ports.ValidationError{Code: "MALFORMED_PAYLOAD", Message: fmt.Sprintf(format, args...)}
}

// Decode unmarshals a JSON body into a generic value (numbers stay json.Number).
func Decode(body []byte) (any, error) {
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, Malformed("body is not JSON: %v", err)
	}
	return v, nil
}

// Get walks a dotted path ("incident.resource.labels.service_name", "alerts.0.labels") through maps and
// slices; missing segments yield nil.
func Get(v any, path string) any {
	if path == "" {
		return v
	}
	for _, seg := range strings.Split(path, ".") {
		switch cur := v.(type) {
		case map[string]any:
			v = cur[seg]
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(cur) {
				return nil
			}
			v = cur[i]
		default:
			return nil
		}
	}
	return v
}

// Str returns a scalar at path as a string ("" when missing or not scalar).
func Str(v any, path string) string {
	switch x := Get(v, path).(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

// First returns the first non-empty Str among paths.
func First(v any, paths ...string) string {
	for _, p := range paths {
		if s := Str(v, p); s != "" {
			return s
		}
	}
	return ""
}

// StrMap returns the scalar fields of the object at path as strings.
func StrMap(v any, path string) map[string]string {
	m, ok := Get(v, path).(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k := range m {
		if s := Str(m, k); s != "" {
			out[k] = s
		}
	}
	return out
}

// Slice returns the array at path.
func Slice(v any, path string) []any {
	s, _ := Get(v, path).([]any)
	return s
}

// Time parses RFC 3339 strings, epoch seconds, or epoch milliseconds (by magnitude); zero when unparseable.
func Time(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000000Z07:00", "2006-01-02 15:04:05Z07:00", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0 {
		switch {
		case f > 1e17: // nanoseconds
			return time.Unix(0, int64(f)).UTC()
		case f > 1e11: // milliseconds
			return time.UnixMilli(int64(f)).UTC()
		default:
			sec := int64(f)
			return time.Unix(sec, int64((f-float64(sec))*1e9)).UTC()
		}
	}
	return time.Time{}
}

// Tags parses "k:v" tag lists (Datadog "service:api,env:prod", Opsgenie ["service:api"]) into a map;
// tags without a colon map to "true".
func Tags(v any) map[string]string {
	var items []string
	switch x := v.(type) {
	case string:
		for _, t := range strings.Split(x, ",") {
			items = append(items, t)
		}
	case []any:
		for _, t := range x {
			if s, ok := t.(string); ok {
				items = append(items, s)
			}
		}
	}
	out := map[string]string{}
	for _, t := range items {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		k, val, ok := strings.Cut(t, ":")
		if !ok {
			val = "true"
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(val)
	}
	return out
}

// Merge copies non-empty values of src into dst (dst wins on conflict when keep is true).
func Merge(dst, src map[string]string, keep bool) map[string]string {
	if dst == nil {
		dst = map[string]string{}
	}
	for k, v := range src {
		if v == "" {
			continue
		}
		if _, exists := dst[k]; exists && keep {
			continue
		}
		dst[k] = v
	}
	return dst
}
