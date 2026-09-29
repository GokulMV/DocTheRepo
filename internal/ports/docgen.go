package ports

import (
	"context"

	"github.com/GokulMV/DocTheRepo/pkg/contract"
)

// DocGenerator is an external documentation engine (a headless agent CLI speaking the DocGen contract).
// Direct-API doc generation does not use this port: it is an LLM chat call built by core/docgen.
type DocGenerator interface {
	Kind() string
	// ReportsUsage declares whether the engine reports token usage in its result (spend-guard coverage).
	ReportsUsage() bool
	Generate(ctx context.Context, task contract.DocGenTask) (contract.DocGenResult, error)
}
