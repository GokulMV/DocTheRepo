<!-- dth:generated source="internal/observability/metrics.go" — edit only inside dth:human blocks -->
# `internal/observability/metrics.go`

<!-- dth:chunk fe4eb4c33b30793d -->
## `Metrics`

A container for all Prometheus metrics exported by the Hub (plan § 11). It holds metric collectors for HTTP traffic, job execution, queue state, Q&A retrieval stages, signal ingestion, issue tracking, and LLM API calls. The Registry field holds the Prometheus registry instance. Other fields are vectors or scalar collectors subdivided by labels like route, job type, signal source, LLM feature, and provider kind, tracking counts, durations, and queue depths across system operations.

<!-- dth:chunk 479a3417255f4e56 -->
## `NewMetrics`

Creates and registers a new isolated Prometheus registry with all Hub metric collectors. It instantiates the metrics container with predefined metric names, help text, label dimensions, and histogram buckets (e.g., HTTP latency uses default buckets; job durations use 0.1–1200s; LLM calls use 0.25–160s). Go runtime and process collectors are automatically included. Returns the fully initialized Metrics struct with all collectors registered on the fresh registry, ensuring test isolation from any global registry.
