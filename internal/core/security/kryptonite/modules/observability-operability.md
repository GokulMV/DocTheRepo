---
name: observability-operability
description: "Probes structured logging, metrics, health/readiness endpoints, tracing, correlation IDs, alertability, and log-injection/PII risk."
applies-to: [web, api, mobile, cli, library, language:any, framework:any]
---

# Observability and Operability

## Purpose

Surface an app that fails silently or fails loud in the wrong place — missing/unstructured logs, absent health signals, no request correlation, no tracing, and log content that leaks PII or is itself injectable — any of which means an operator can't detect, diagnose, or trust a production incident until a user reports it.

## Applies-to (detail)

Targets any long-running or invoked-repeatedly app that an operator needs to monitor: web frontends (client-side error reporting), APIs and backend services (structured logs, metrics, health checks, tracing), mobile apps (crash/error reporting pipelines), CLIs (exit codes, `--verbose` logging, machine-parseable output), and libraries (log/metric hooks the consuming app can wire up). Applies regardless of language or framework — probes adapt to whatever logging library, metrics backend, and orchestration platform (Kubernetes, ECS, systemd, bare process) the surface map records. Does not apply to a stateless static-content site with no server-side process to monitor (note the skip reason explicitly rather than silently matching).

## Probes

- **Structured logs**: trigger a normal request, an error request, and a slow request; inspect emitted log lines for machine-parseable structure (JSON or consistent key=value fields) versus free-text string concatenation; check every log line carries at minimum a timestamp, level, and service/component name.
- **Metrics**: check for an exposed metrics endpoint (`/metrics` Prometheus-style, StatsD emission, cloud-native metrics API) and verify it reports request rate, error rate, and latency (the RED method) or utilization/saturation/errors (the USE method) for at least the primary request path.
- **Health/readiness endpoints**: hit `/health` or `/healthz` (liveness — "is the process alive") and `/ready` or `/readyz` (readiness — "can it serve traffic, are dependencies reachable") separately; verify liveness returns OK even when a downstream dependency is down (so orchestrators don't kill a recoverable pod) while readiness correctly flips to failing when a required dependency (DB, cache, upstream API) is unreachable.
- **Distributed tracing**: make a multi-hop request (through a gateway, into a service, out to a downstream call) and check whether a trace ID propagates across every hop via a standard header (`traceparent` W3C Trace Context, or `X-B3-*` B3 propagation) and whether spans appear in a tracing backend (Jaeger/Zipkin/OTel collector) with parent-child relationships intact.
- **Correlation IDs**: send a request with no correlation header and verify the app generates one (`X-Request-Id` or equivalent) and echoes it back in the response and in every log line produced while handling that request; send a request with a caller-supplied correlation ID and verify it's honored (not overwritten) so client-side and server-side logs can be joined.
- **Alertability**: check whether error-rate and latency-SLO breaches, and readiness-probe failures, actually reach an alerting channel (alertmanager rule, cloud monitoring alert policy, PagerDuty/Slack integration) rather than only being visible if someone manually queries a dashboard; check for at least one alert rule per critical dependency (DB connection pool exhaustion, queue backlog depth, disk/memory saturation).
- **Log injection**: inject newline characters (`\n`), ANSI escape sequences, and log-format-breaking payloads (`" WHERE 1=1 --`, a fake log line `2026-01-01 ERROR fake admin login`) into any user-controlled field that gets logged, and check whether the injected content forges a fake log entry or corrupts the log's structured format.
- **PII in logs**: trigger flows that handle emails, passwords, tokens, SSNs/national IDs, payment card data, or health data, and grep the resulting log output for that raw PII appearing unmasked/unredacted.
- **Silent failure detection**: find a code path that catches an exception and does nothing observable (empty `catch`/`except` block, swallowed promise rejection, a background job that fails without requeue or alert) and verify whether that failure is visible anywhere — log, metric, or trace — or vanishes entirely.

## What "safe" looks like

Every log line is structured (JSON or consistent parseable fields) with timestamp, level, service name, and — for request-scoped lines — a correlation/request ID. A metrics endpoint reports request rate, error rate, and latency for primary paths, scraped and retained by a metrics backend. Liveness and readiness are distinct endpoints with distinct failure semantics: liveness fails only on process-level unrecoverable state, readiness fails whenever a required dependency is unreachable. Trace context propagates across every service hop and is queryable end-to-end in a tracing backend. Every request carries a correlation ID — generated if absent, honored if supplied — present in every log line that request produces. Error-rate/latency-SLO breaches and dependency failures generate an alert that reaches a human channel, not just a dashboard someone might check. No user-controlled input can forge or corrupt a log entry (log fields are structured/escaped, not raw string concatenation). No raw PII appears in log output — it's masked, tokenized, or omitted. Every caught failure is observable somewhere (log line at `error`/`warn` level, incremented error metric, or a trace span marked failed) — nothing fails into the void.

## Severity rubric

- `critical`: a critical dependency outage (DB down, payment provider down) produces zero log/metric/alert signal — an operator has no way to know production is broken except a user report.
- `high`: readiness doesn't reflect a real dependency failure (orchestrator keeps routing traffic to a pod that can't serve requests); raw PII (passwords, full card numbers, SSNs) logged unmasked; log injection lets an attacker forge a fake audit-log entry (e.g., a fake successful-login line) undermining incident forensics.
- `medium`: missing correlation IDs make cross-service debugging require manual log correlation; no tracing on a multi-hop critical path; an alert rule exists but doesn't actually reach a notification channel (misconfigured integration).
- `low`: unstructured (free-text) logs that are still human-readable but not machine-parseable; a metrics endpoint missing latency percentiles (only has averages).
- `info`: missing `/metrics` documentation; a health endpoint that conflates liveness and readiness but both dependencies happen to be reliable today.

## Detection method

For structured logs, detection is whether emitted lines parse as JSON/key-value or are free-text (attempt `json.loads`/structured-parse on captured output). For metrics, detection is whether the `/metrics` (or equivalent) endpoint returns a non-empty response containing rate/error/latency series names. For health/readiness, detection is the liveness/readiness status code and body diverging correctly when a dependency is manually killed (stop the DB container, kill the cache) — readiness must flip to failing, liveness must not. For tracing, detection is the presence (or absence) of a propagated trace ID header across hops, confirmed by a matching trace appearing in the tracing backend's query API. For correlation IDs, detection is the response header and corresponding log line containing the same ID as the request (or a freshly generated one if none was sent). For alertability, detection is triggering the failure condition (breach the error-rate threshold, kill a dependency) and confirming a firing alert appears in the alerting system, not just a metric change. For log injection, detection is a forged-looking line appearing in raw log output that wasn't produced by the app itself. For PII, detection is a regex match (email pattern, card-number pattern, SSN pattern) against raw (non-masked) log content. For silent failures, detection is triggering the failure path and confirming zero log/metric/trace signal was produced. Evidence for the Finding is the captured log excerpt, the health/readiness response diff under dependency failure, the trace query result, or the alert-firing confirmation.

## References

Skills: `security-audit` (log-injection/PII overlap with security), `voice-agents` (observability patterns for long-running agent processes, where relevant). Standards: The Twelve-Factor App (Logs), OpenTelemetry semantic conventions (traces/metrics/logs), W3C Trace Context, Google SRE Book (the four golden signals, RED/USE methods), OWASP Logging Cheat Sheet (log injection, PII masking).
