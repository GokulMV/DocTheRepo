// Package ports is the hexagon boundary: the plain domain types that cross it and the port interfaces
// every adapter implements. Nothing in this package performs I/O or imports an adapter.
package ports

import (
	"encoding/json"
	"time"
)

// JobType names a unit of background work.
type JobType string

const (
	JobCodePush      JobType = "code_push"
	JobDecodeIssue   JobType = "decode_issue"
	JobKnowledgeSync JobType = "knowledge_sync"
	JobImportDocs    JobType = "import_docs"
	JobReindex       JobType = "reindex"
	JobSignalBatch   JobType = "signal_batch"
	// JobRepoDocs writes a repository's documents (Docs v2).
	JobRepoDocs JobType = "repo_docs"
	// JobSystemDocs writes the System architecture across repositories.
	JobSystemDocs JobType = "system_docs"
	// JobPRReview advances a docs PR after its approver's review (webhook ingress does no git I/O itself).
	JobPRReview JobType = "pr_review"
	// JobSecurityScan and JobSecurityFix run the Security page's scans and requested fixes.
	JobSecurityScan JobType = "security_scan"
	JobSecurityFix  JobType = "security_fix"
)

// AllJobTypes lists every job type the worker pool knows about.
var AllJobTypes = []JobType{JobCodePush, JobRepoDocs, JobSystemDocs, JobDecodeIssue, JobKnowledgeSync, JobImportDocs, JobReindex, JobSignalBatch, JobPRReview,
	JobSecurityScan, JobSecurityFix}

// JobStatus is the lifecycle state of a job (plan § 6.5).
type JobStatus string

const (
	JobQueued          JobStatus = "queued"
	JobProcessing      JobStatus = "processing"
	JobDone            JobStatus = "done"
	JobFailed          JobStatus = "failed"
	JobAborted         JobStatus = "aborted"
	JobSpendBlocked    JobStatus = "spend_blocked"
	JobPendingApproval JobStatus = "pending_approval"
	JobNeedsHuman      JobStatus = "needs_human"
	JobDead            JobStatus = "dead"
)

// AllJobStatuses lists every valid status, used to validate API filters.
var AllJobStatuses = []JobStatus{JobQueued, JobProcessing, JobDone, JobFailed, JobAborted, JobSpendBlocked,
	JobPendingApproval, JobNeedsHuman, JobDead}

// Terminal reports whether a job in this status will never be claimed again.
func (s JobStatus) Terminal() bool { return s != JobQueued && s != JobProcessing }

// Job is a queued unit of work.
type Job struct {
	ID            string          `json:"job_id"`
	Seq           int64           `json:"seq"`
	Type          JobType         `json:"type"`
	RepoID        string          `json:"repo_id,omitempty"`
	SerialKey     string          `json:"serial_key,omitempty"`
	DedupeKey     string          `json:"dedupe_key,omitempty"`
	Payload       json.RawMessage `json:"payload"`
	Status        JobStatus       `json:"status"`
	Priority      int16           `json:"priority"`
	Attempts      int             `json:"attempts"`
	MaxAttempts   int             `json:"max_attempts"`
	RunAfter      time.Time       `json:"run_after"`
	LockedBy      string          `json:"locked_by,omitempty"`
	LockedUntil   *time.Time      `json:"locked_until,omitempty"`
	CorrelationID string          `json:"correlation_id"`
	Error         string          `json:"error,omitempty"`
	Result        json.RawMessage `json:"result,omitempty"`
	// Progress is a running job's latest progress report (JobProgress), if it reports one.
	Progress     json.RawMessage `json:"progress,omitempty"`
	ReplayedFrom string          `json:"replayed_from,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// JobProgress is what a running job reports as it goes: a stage, and how far through it is.
type JobProgress struct {
	Stage string `json:"stage"`          // e.g. "documenting", "writing"
	Done  int    `json:"done"`           // items finished
	Total int    `json:"total"`          // items in this stage
	Item  string `json:"item,omitempty"` // the latest item, e.g. a file path
}

// NewJob describes a job to enqueue.
type NewJob struct {
	Type   JobType
	RepoID string
	// SerialKey, when set, makes jobs sharing the key run one at a time in enqueue order
	// (e.g. "repo:<id>" so pushes to one repo never run in parallel).
	SerialKey string
	// DedupeKey, when set, collapses an enqueue onto an existing queued/processing job with the same key.
	DedupeKey     string
	Payload       any
	Priority      int16
	MaxAttempts   int
	RunAfter      time.Time
	CorrelationID string
	ReplayedFrom  string
}

// Outcome is what a job handler reports on success (a non-error return).
type Outcome struct {
	// Status must be a terminal status other than failed/dead; empty means done.
	Status JobStatus
	Result any
	// Message is a short human-readable note stored in the job's error field for non-done outcomes
	// (e.g. the spend ceiling that was breached).
	Message string
}
