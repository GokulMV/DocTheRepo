// Package idempotency makes payment API calls safe to retry.
package idempotency

import (
	"net/http"
	"time"
)

// KeyTTL is how long a stored Idempotency-Key response is replayed.
const KeyTTL = 24 * time.Hour

// Store keeps responses by idempotency key (Redis in production).
type Store interface {
	Get(key string) (status int, body []byte, ok bool)
	Put(key string, status int, body []byte, ttl time.Duration)
}

// Middleware replays the stored response when a request repeats an Idempotency-Key header, so a client
// retrying a timed-out charge never charges the card twice. Keys expire after KeyTTL.
func Middleware(s Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" || r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		if status, body, ok := s.Get(key); ok {
			w.WriteHeader(status)
			_, _ = w.Write(body)
			return
		}
		rec := &recorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.Put(key, rec.status, rec.body, KeyTTL)
	})
}

type recorder struct {
	http.ResponseWriter
	status int
	body   []byte
}

func (r *recorder) WriteHeader(s int) { r.status = s; r.ResponseWriter.WriteHeader(s) }
func (r *recorder) Write(b []byte) (int, error) {
	r.body = append(r.body, b...)
	return r.ResponseWriter.Write(b)
}
