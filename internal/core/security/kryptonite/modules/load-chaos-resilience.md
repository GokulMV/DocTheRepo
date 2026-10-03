---
name: load-chaos-resilience
description: "Probes throughput/latency under load, resource exhaustion, dependency failure handling, retry storms, and crash recovery."
applies-to: [web, api, cli, framework:any]
---

# Load, Chaos & Resilience

## Purpose

Surface weaknesses that only appear under load or failure conditions — degraded throughput, resource exhaustion, cascading failures from a slow/down dependency, and an inability to recover cleanly from a crash — that a single-request functional test never exercises.

## Applies-to (detail)

Targets any runnable app with a server process that serves concurrent requests or runs background work: web backends, APIs, worker/queue consumers, and long-running CLI daemons. Requires a runnable (not static-only) target, since these probes need a live process to load-test and crash. Does not apply to a static-only surface-map result — note the skip reason (no live process to load) rather than silently matching.

## Probes

- **Throughput/latency under load**: ramp concurrent request volume (e.g. 1 → 10 → 50 → 200 concurrent) against a representative mix of the app's real endpoints and record p50/p95/p99 latency and error rate at each step; identify the concurrency level where latency or error rate breaks from linear/flat to a cliff.
- **Resource exhaustion**: drive sustained load while watching process memory, open file descriptors, DB connection-pool usage, and thread/goroutine counts; look for unbounded growth (a leak) rather than a plateau; specifically target endpoints that allocate per-request buffers, hold connections open, or spawn subprocesses.
- **Dependency failure & timeouts**: with the app pointed at a real (non-production) dependency — DB, cache, third-party API, queue — kill or block that dependency (stop the container, drop its network route, or point at a black-hole port) and observe how the app's own requests behave: do they hang indefinitely, fail fast, or fail gracefully with a fallback.
- **Retry storms**: with a dependency degraded (slow, not down), observe whether the app's own retry logic backs off (exponential/jittered) or hammers the dependency at a fixed short interval, and whether concurrent request-level retries compound into a thundering herd that a healthy dependency also couldn't absorb.
- **Graceful degradation**: with a non-critical dependency (recommendations, analytics, a non-essential enrichment call) failing, check whether the core user-facing path still completes (degraded but functional) or fails outright because of a hard dependency that should have been optional.
- **Restart/crash recovery**: forcibly kill the app process (`SIGKILL`, not `SIGTERM`) mid-request and mid-background-job, then restart it; check whether in-flight requests fail cleanly (client gets an error, not a hang) and whether the app comes back to a consistent state (no half-written records, no stuck locks, no orphaned background jobs left claimed-but-dead).

## What "safe" looks like

Latency degrades gracefully (roughly linear) as concurrency rises and the app sheds load (rejects with `503`/`429`) rather than falling over once past its capacity — no unbounded latency growth or process crash under load within the tested range. Memory, connections, and file descriptors plateau under sustained load rather than growing without bound. Every outbound call to a dependency has an explicit timeout; when a dependency is down, dependent requests fail within that timeout (not hang), with an error that's actionable, not a bare 500. Retry logic uses bounded attempts with exponential backoff and jitter, and circuit-breaks (stops retrying) after repeated failures rather than continuing to hammer a down dependency. Non-critical dependency failures degrade the affected feature only — the core path still succeeds. After a hard kill and restart, the app comes back serving traffic within a bounded time, in-flight work is either completed, cleanly rolled back, or safely resumable (no stuck locks, no records left in an impossible intermediate state), and previously-claimed background jobs are requeued or clearly marked failed rather than silently lost.

## Severity rubric

- `critical`: sustained load or a single dependency outage crashes the process entirely (not just degrades) with no auto-recovery, taking down all traffic including unrelated features; crash recovery leaves data in a corrupted/inconsistent state.
- `high`: unbounded resource growth (memory/connection leak) that will exhaust the process under realistic sustained load; a non-critical dependency failure takes down the entire core path instead of just its own feature; retry storm from the app itself amplifies a dependency outage into a full outage.
- `medium`: latency degrades sharply (order-of-magnitude, not gradual) under load without full failure; missing timeout on a dependency call causes slow requests to pile up but the process itself survives; crash recovery loses a background job without corrupting persisted data.
- `low`: latency degradation under load is present but within tolerable bounds for the app's stated use case; minor resource growth that plateaus well within realistic operating limits.
- `info`: no formal load ceiling documented for the app, making "safe" limits a judgment call rather than a measured pass/fail; retry backoff present but not jittered.

## Detection method

For throughput/latency: plot latency percentiles and error rate against concurrency level; the fail signal is a latency/error-rate discontinuity (a "cliff") rather than gradual degradation, or any full process crash. For resource exhaustion: sample memory/FD/connection-pool metrics at fixed intervals during sustained load; the fail signal is a monotonically increasing trend with no plateau over the test duration. For dependency failure/timeouts: measure how long a dependent request takes to return an error after the dependency is killed — the fail signal is no response within a generous multiple (e.g. 3x) of the dependency's configured timeout, or an unhandled exception/hang. For retry storms: count actual outbound retry attempts per unit time against the degraded dependency — the fail signal is a flat high-frequency retry rate with no backoff growth. For graceful degradation: check whether the core-path response still succeeds (`2xx`, with the non-critical feature's output empty/fallback) versus failing outright. For crash recovery: after restart, query the data store directly for orphaned/half-committed records and stuck job-claim rows, and hit a health endpoint to measure time-to-serving; the fail signal is any orphaned record or a health check that never returns healthy.

## References

Skills: `security-audit` (includes availability/DoS-adjacent checks), general chaos-engineering practice (kill -9 / dependency-blackhole testing), circuit-breaker and retry-with-backoff patterns. Concepts: SRE load-testing methodology (percentile latency under ramping concurrency), the "fail fast with a bounded timeout" principle for every outbound call.
