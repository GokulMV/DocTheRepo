// Package dunning retries failed subscription payments.
package dunning

import "time"

// Schedule is when failed payments are retried, counted from the first failure: day 3, 7, and 14. After
// the last retry fails the account is suspended.
var Schedule = []time.Duration{3 * 24 * time.Hour, 7 * 24 * time.Hour, 14 * 24 * time.Hour}

// NextAction returns the next retry time, or suspend=true once the schedule is exhausted.
func NextAction(firstFailure time.Time, attempts int) (retryAt time.Time, suspend bool) {
	if attempts >= len(Schedule) {
		return time.Time{}, true
	}
	return firstFailure.Add(Schedule[attempts]), false
}
