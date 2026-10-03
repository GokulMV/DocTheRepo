<!-- dth:generated source="internal/adapters/llm/jev/jev.go" — edit only inside dth:human blocks -->
# `internal/adapters/llm/jev/jev.go`

<!-- dth:chunk d2cda5a42dd9732f -->
## `answer`

Represents a parsed answer from the JEV API. The `Probability` field (pointer to float64) is used specifically for boolean questions and is populated by the Judge method when processing yes/no evaluations.

<!-- dth:chunk e787fba7f024befc -->
## `Client.Judge`

Implements ports.Judger to evaluate yes/no questions via the JEV API. Takes a model name, state text, and questions to send to the `/systemone` endpoint, then extracts probability values from the response and wraps them in a `Judgment` with calibration metadata. Validates that 1–128 questions are provided and that each response contains a valid probability (0–1 range); returns `Transient` error if validation fails, `Permanent` error if question count is invalid.

<!-- dth:chunk ac4ec4326e9e9fab -->
## `__module__`

Configures the JEV adapter: sets the API base URL and default model name, limits concurrent questions to 128, reserves the question ID "decision" for internal use, and defines `MaxOptions` (255) for non-boolean questions. `ErrChatUnsupported` indicates that chat operations should be routed to a different provider.
