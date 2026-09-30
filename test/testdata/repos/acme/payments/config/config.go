// Package config loads the payments service configuration from the environment.
package config

import "os"

// Config is the service configuration.
type Config struct {
	DatabaseURL  string // PAYMENTS_DB_URL
	StripeAPIKey string // STRIPE_API_KEY
	RefundQueue  string // REFUND_QUEUE, the SQS queue refunds are retried from
}

// Load reads PAYMENTS_DB_URL, STRIPE_API_KEY, and REFUND_QUEUE.
func Load() Config {
	return Config{DatabaseURL: os.Getenv("PAYMENTS_DB_URL"), StripeAPIKey: os.Getenv("STRIPE_API_KEY"), RefundQueue: os.Getenv("REFUND_QUEUE")}
}
