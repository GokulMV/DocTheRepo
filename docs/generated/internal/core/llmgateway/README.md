<!-- dth:generated index — edit only inside dth:human blocks -->
# `internal/core/llmgateway`

- [`decide.go`](decide.go.md) — Implements LLM decision routing with native and fallback decider paths, integrating token budget enforcement and observability.
- [`gateway.go`](gateway.go.md) — LLMGateway enforces spend guard limits and records usage for all external LLM, embedding, and documentation generation calls.
- [`judge.go`](judge.go.md) — This file implements token budget enforcement and observability for LLM gateway judgment operations.
