// Package refunds issues refunds through the payment processor.
package refunds

import (
	"context"
	"math/rand"
	"time"
)

// MaxRefundAttempts is how many times a failed refund is retried before it is parked for manual review.
const MaxRefundAttempts = 5

// Processor sends a refund to the payment processor.
type Processor interface {
	Refund(ctx context.Context, paymentID string, amountMinor int64) error
}

// RetryRefund retries failed refunds with exponential backoff and full jitter: 1s, 2s, 4s, 8s, 16s caps.
// After MaxRefundAttempts failures the refund is parked in the manual review queue.
func RetryRefund(ctx context.Context, p Processor, paymentID string, amountMinor int64) error {
	var err error
	for attempt := 0; attempt < MaxRefundAttempts; attempt++ {
		if err = p.Refund(ctx, paymentID, amountMinor); err == nil {
			return nil
		}
		backoff := time.Duration(1<<attempt) * time.Second
		sleep := time.Duration(rand.Int63n(int64(backoff)))
		select {
		case <-time.After(sleep):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return ParkForManualReview(paymentID, err)
}

// ParkForManualReview moves a refund that exhausted its retries to the manual review queue.
func ParkForManualReview(paymentID string, cause error) error {
	return &ParkedError{PaymentID: paymentID, Cause: cause}
}

// ParkedError reports a refund waiting for a human.
type ParkedError struct {
	PaymentID string
	Cause     error
}

func (e *ParkedError) Error() string { return "refund parked for manual review: " + e.PaymentID }
