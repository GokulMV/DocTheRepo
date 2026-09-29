// Package httpx is the shared HTTP plumbing for adapters that speak plain REST: bounded retries that
// honour Retry-After, typed error classification (plan § 9), and capped response reads.
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// MaxBody caps how much of a response body is read (and how much of an error body is quoted).
const MaxBody = 64 << 20

// Client wraps http.Client with retry policy.
type Client struct {
	HTTP       *http.Client
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
	// Service names the upstream in error messages ("openai", "wiz", ...).
	Service string
}

// New returns a client with sane defaults: 2 retries, 500ms base backoff, 20s cap, 5 minute timeout.
func New(service string) *Client {
	return &Client{HTTP: &http.Client{Timeout: 5 * time.Minute}, MaxRetries: 2, BaseDelay: 500 * time.Millisecond,
		MaxDelay: 20 * time.Second, Service: service}
}

// StatusError is a non-2xx response.
type StatusError struct {
	Service    string
	StatusCode int
	Body       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s returned HTTP %d: %s", e.Service, e.StatusCode, e.Body)
}

// Do sends the request built by build (called once per attempt, so bodies can be re-read) and returns the
// response with a 2xx status; the caller closes the body. 408/425/429/5xx and transport errors are retried;
// the final failure is classified as ports.TransientError or ports.PermanentError.
func (c *Client) Do(ctx context.Context, build func() (*http.Request, error)) (*http.Response, error) {
	var last error
	for attempt := 0; ; attempt++ {
		req, err := build()
		if err != nil {
			return nil, ports.Permanent(fmt.Errorf("%s: build request: %w", c.Service, err))
		}
		resp, err := c.HTTP.Do(req.WithContext(ctx))
		var wait time.Duration
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			last = ports.Transient(fmt.Errorf("%s: %w", c.Service, err))
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return resp, nil
		default:
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			resp.Body.Close()
			se := &StatusError{Service: c.Service, StatusCode: resp.StatusCode, Body: string(bytes.TrimSpace(body))}
			wait = RetryAfter(resp.Header)
			if !Retryable(resp.StatusCode) {
				return nil, ports.Permanent(se)
			}
			last = ports.TransientAfter(se, wait)
		}
		if attempt >= c.MaxRetries {
			return nil, last
		}
		if wait <= 0 {
			wait = c.backoff(attempt)
		}
		if wait > c.MaxDelay {
			return nil, last // the upstream asks for longer than we block a worker; let the queue reschedule
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// JSON sends a JSON body and decodes a JSON response into out (skipped when out is nil).
func (c *Client) JSON(ctx context.Context, method, url string, headers map[string]string, in, out any) error {
	var payload []byte
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return ports.Permanent(fmt.Errorf("%s: encode request: %w", c.Service, err))
		}
		payload = b
	}
	resp, err := c.Do(ctx, func() (*http.Request, error) {
		var body io.Reader
		if payload != nil {
			body = bytes.NewReader(payload)
		}
		req, err := http.NewRequest(method, url, body)
		if err != nil {
			return nil, err
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		return req, nil
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, MaxBody))
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, MaxBody)).Decode(out); err != nil {
		return ports.Transient(fmt.Errorf("%s: decode response: %w", c.Service, err))
	}
	return nil
}

func (c *Client) backoff(attempt int) time.Duration {
	d := c.BaseDelay << attempt
	if d > c.MaxDelay || d <= 0 {
		d = c.MaxDelay
	}
	return time.Duration(float64(d) * (0.8 + 0.4*rand.Float64()))
}

// Retryable reports statuses worth retrying.
func Retryable(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooEarly || status == http.StatusTooManyRequests ||
		status >= 500
}

// RetryAfter parses Retry-After (seconds or HTTP date), plus the retry-after-ms variant some APIs send.
func RetryAfter(h http.Header) time.Duration {
	if ms, err := strconv.ParseFloat(h.Get("retry-after-ms"), 64); err == nil && ms > 0 {
		return time.Duration(ms * float64(time.Millisecond))
	}
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	if s, err := strconv.ParseFloat(v, 64); err == nil && s > 0 {
		return time.Duration(s * float64(time.Second))
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// StatusOf returns the HTTP status inside err, if any.
func StatusOf(err error) int {
	var se *StatusError
	if errors.As(err, &se) {
		return se.StatusCode
	}
	return 0
}
