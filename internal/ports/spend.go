package ports

import (
	"context"
	"time"
)

// UsageRecord is one paid (or estimated) call, written to the ledger after the call returns.
type UsageRecord struct {
	At           time.Time
	Feature      string
	ProviderID   string
	ProviderKind string
	Model        string
	InputTokens  int64
	OutputTokens int64
	CostUSD      float64
	LatencyMS    int64
	Cached       bool
	// Estimated is true when the provider reported no usage and the estimate was recorded instead.
	Estimated bool
	RepoID    string
	UserID    string
	JobID     string
	IssueID   string
	Outcome   string // ok | error | blocked
}

// SpendTotals is consumption within a window.
type SpendTotals struct {
	Tokens  int64
	CostUSD float64
}

// SpendFilter selects the ledger rows a limit counts.
type SpendFilter struct {
	Feature    string
	ProviderID string
	RepoID     string
	Since      time.Time
}

// SpendLedger is the usage ledger the spend guard reads before, and writes after, every paid call.
type SpendLedger interface {
	Spent(ctx context.Context, f SpendFilter) (SpendTotals, error)
	Record(ctx context.Context, r UsageRecord) error
}

// SpendAlert is sent when a limit with on_breach=block_and_alert blocks a call.
type SpendAlert struct {
	LimitID         string    `json:"limit_id"`
	Scope           string    `json:"scope"`
	ScopeKey        string    `json:"scope_key"`
	Window          string    `json:"window"`
	EstimatedTokens int64     `json:"estimated_tokens"`
	SpentTokens     int64     `json:"spent_tokens"`
	MaxTokens       int64     `json:"max_tokens"`
	Reason          string    `json:"reason"`
	At              time.Time `json:"at"`
	URL             string    `json:"-"`
}

// Alerter delivers spend alerts (an HTTP webhook adapter in production).
type Alerter interface {
	Alert(ctx context.Context, a SpendAlert) error
}
