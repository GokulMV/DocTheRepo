package refunds

import "time"

// RefundWindow is how long after capture a payment can still be refunded.
const RefundWindow = 30 * 24 * time.Hour

// Order is the part of an order the refund rules need.
type Order struct {
	CapturedAt    time.Time
	AmountMinor   int64
	RefundedMinor int64
}

// IsRefundable reports whether amountMinor can still be refunded: inside the 30-day refund window and
// not more than what remains after earlier partial refunds.
func IsRefundable(o Order, amountMinor int64, now time.Time) bool {
	if now.Sub(o.CapturedAt) > RefundWindow {
		return false
	}
	return amountMinor > 0 && o.RefundedMinor+amountMinor <= o.AmountMinor
}
