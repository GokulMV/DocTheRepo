package ports

import (
	"errors"
	"fmt"
	"time"
)

// Typed errors (plan § 9). Adapters wrap failures in these so the core decides retry vs. fail by type,
// never by matching error strings.

// TransientError is retried with backoff.
type TransientError struct {
	Err error
	// RetryAfter, when non-zero, replaces the standard backoff and does not consume an attempt
	// (e.g. an upstream Retry-After header).
	RetryAfter time.Duration
}

func (e *TransientError) Error() string { return "transient: " + e.Err.Error() }
func (e *TransientError) Unwrap() error { return e.Err }

// PermanentError fails the job without further retries.
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string { return "permanent: " + e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

// ValidationError is a bad input; never retried.
type ValidationError struct {
	Code    string
	Message string
	Details map[string]any
}

func (e *ValidationError) Error() string { return e.Code + ": " + e.Message }

// SpendBlockedError reports a spend ceiling breach detected before a paid call.
type SpendBlockedError struct {
	Scope           string
	EstimatedTokens int64
	Reason          string
}

func (e *SpendBlockedError) Error() string {
	return fmt.Sprintf("spend blocked (%s): %s", e.Scope, e.Reason)
}

// Transient wraps err as a TransientError.
func Transient(err error) error { return &TransientError{Err: err} }

// TransientAfter wraps err as a TransientError honouring an upstream retry hint.
func TransientAfter(err error, after time.Duration) error {
	return &TransientError{Err: err, RetryAfter: after}
}

// Permanent wraps err as a PermanentError.
func Permanent(err error) error { return &PermanentError{Err: err} }

// Invalid builds a ValidationError.
func Invalid(code, format string, args ...any) error {
	return &ValidationError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// AsTransient returns the TransientError in err's chain, if any.
func AsTransient(err error) (*TransientError, bool) {
	var t *TransientError
	ok := errors.As(err, &t)
	return t, ok
}

// Sentinel errors shared across adapters and the store.
var (
	ErrNotFound         = errors.New("not found")
	ErrConflict         = errors.New("conflict")
	ErrProtectedBranch  = errors.New("protected branch rejected the write")
	ErrRebaseConflict   = errors.New("rebase could not be completed cleanly")
	ErrDimensionLocked  = errors.New("index exists with a different embedding model or dimension")
	ErrUnsupported      = errors.New("operation not supported by this adapter")
	ErrPathOutsideDocs  = errors.New("write targets a path outside the generated-docs path")
	ErrInvalidSignature = errors.New("webhook signature verification failed")
)
