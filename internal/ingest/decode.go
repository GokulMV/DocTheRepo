package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/GokulMV/DocTheRepo/internal/core/decode"
	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// DecodePayload is a decode_issue job's payload.
type DecodePayload struct {
	IssueID string `json:"issue_id"`
	// Force re-decodes even when the current decode still stands (a user asked).
	Force bool `json:"force,omitempty"`
}

// DecodeHandler runs decode_issue jobs. A spend block parks the job (spend_blocked) instead of failing it;
// a deleted issue ends the job quietly.
func DecodeHandler(d *decode.Decoder) func(ctx context.Context, job ports.Job) (ports.Outcome, error) {
	return func(ctx context.Context, job ports.Job) (ports.Outcome, error) {
		var pl DecodePayload
		if err := json.Unmarshal(job.Payload, &pl); err != nil || pl.IssueID == "" {
			return ports.Outcome{}, ports.Permanent(fmt.Errorf("decode payload: %v", err))
		}
		out, err := d.Decode(ctx, pl.IssueID, pl.Force, job.ID)
		var sb *ports.SpendBlockedError
		switch {
		case errors.As(err, &sb):
			return ports.Outcome{Status: ports.JobSpendBlocked, Message: sb.Error()}, nil
		case errors.Is(err, llmgateway.ErrNoRoute):
			return ports.Outcome{Status: ports.JobAborted, Message: "no decode model route is configured"}, nil
		case errors.Is(err, ports.ErrNotFound):
			return ports.Outcome{Status: ports.JobAborted, Message: "issue no longer exists"}, nil
		case err != nil:
			return ports.Outcome{}, err
		}
		return ports.Outcome{Result: out}, nil
	}
}
