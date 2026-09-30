// Package webhooks receives Stripe webhook events.
package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

// SignatureTolerance rejects webhook events whose signed timestamp is older than five minutes (replay
// protection).
const SignatureTolerance = 5 * time.Minute

// ErrBadSignature means the Stripe-Signature header did not verify.
var ErrBadSignature = errors.New("invalid webhook signature")

// VerifyStripeSignature checks the Stripe-Signature header ("t=<unix>,v1=<hex hmac>") against the
// endpoint secret with HMAC-SHA256 and enforces SignatureTolerance.
func VerifyStripeSignature(payload []byte, header, secret string, now time.Time) error {
	var ts, sig string
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(part, "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sig = v
		}
	}
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || now.Sub(time.Unix(unix, 0)) > SignatureTolerance {
		return ErrBadSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "." + string(payload)))
	if !hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(sig)) {
		return ErrBadSignature
	}
	return nil
}
