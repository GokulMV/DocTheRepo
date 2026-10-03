---
name: failure-ux-degradation
description: "Probes error-handling paths, leaked stack traces, partial-failure behavior, timeout/retry/circuit-breaking sanity, and empty/loading/error UI states."
applies-to: [web, api, mobile, language:any, framework:any]
---

# Failure UX and Degradation

## Purpose

Surface what a user actually sees and experiences when something goes wrong — a leaked stack trace, a frozen spinner, a half-completed action left inconsistent, an unbounded retry storm — any of which turns a backend hiccup into a user-visible break of trust or data integrity.

## Applies-to (detail)

Targets apps with a user-facing surface that can fail in front of a human: web frontends, APIs consumed by a client that renders their errors, and mobile apps. Applies regardless of language or framework — probes adapt to whatever UI framework and error-boundary/retry mechanism the surface map records. Does not apply to a headless backend service or library with no direct user-facing surface and no client-rendered error path (note the skip reason explicitly, and route pure-backend failure-handling concerns to `load-chaos-resilience` instead).

## Probes

- **Error-handling paths**: for every primary user flow (signup, checkout, search, upload), force each dependency it touches to fail one at a time (kill the DB connection, return a 500 from a downstream API, drop a network call) and observe what the flow does — versus what happens when nothing is forced to fail, to confirm the difference is actually handled and not just accidentally working.
- **Leaked stack traces / internals**: trigger an unhandled exception (malformed input, a null the code doesn't check, a forced 500) against a production-mode build and inspect the rendered page/response for a framework stack trace, file paths, SQL fragments, or internal service names exposed directly to the user.
- **Partial-failure behavior**: for any multi-step or multi-resource operation (an order that debits payment then creates a shipment record, a batch upload that writes N of M files), fail it mid-way (kill the process, throw after step 1 of 3) and check whether the system is left in a coherent state — a compensating rollback, a resumable/idempotent retry, or a clearly-flagged partial-success record — versus silently corrupted or duplicated state.
- **Timeouts surfaced sanely**: force a slow/hanging dependency (delay a downstream response by 30s+) and check the user-facing behavior: does the UI show a timeout-specific message within a bounded, documented time, or does it hang indefinitely / show a generic crash / silently return stale-looking data.
- **Retry/backoff**: inspect client and server retry logic for every external call; verify retries use exponential backoff with jitter and a capped attempt count (not a tight fixed-interval loop) and that non-idempotent operations (POST payment, POST order) aren't blindly retried without an idempotency key.
- **Circuit breaking**: repeatedly fail a downstream dependency past its configured failure threshold and check whether the caller stops issuing new calls to it for a cooldown window (open-circuit) rather than continuing to pile up timeouts/retries against an already-down dependency, and whether the circuit correctly half-opens/closes again once the dependency recovers.
- **Empty state**: load every list/collection view (search results, dashboard, inbox, history) with genuinely zero underlying data and check for a designed empty state (explanatory copy, a call-to-action) versus a blank screen, a raw `[]`, or a broken layout.
- **Loading state**: check every async view for a loading indicator that appears within a perceptible delay and disappears correctly on both success and failure — not a spinner that persists forever after an error, and not content that flashes/layout-shifts once data arrives.
- **Error state (UI)**: check every view that can fail for a human-readable, action-oriented error message (what happened, what the user can do — retry, contact support) rather than a raw error code, a blank screen, or the app silently reverting to a stale/default view with no indication anything failed.

## What "safe" looks like

Every forced-dependency-failure test produces a user-visible, bounded, non-generic response — never an indefinite hang, a raw framework error page, or leaked internals (stack trace, file path, SQL, internal hostname). Multi-step operations either roll back cleanly on partial failure, are safely resumable/idempotent, or persist an explicit partial-success record the user and support tooling can see — never silent duplication or silent data loss. Timeouts fire within a documented bound and produce a timeout-specific message, not an indefinite spinner. Retries are capped, backed off with jitter, and gated by idempotency keys for non-idempotent operations — no retry storm amplifies an outage. A circuit breaker opens after a defined failure threshold against any given downstream dependency and stops hammering it until a cooldown/half-open probe confirms recovery. Every list/collection view has a designed empty state; every async view shows and correctly clears a loading indicator on both success and failure; every failable view shows a specific, human-readable, actionable error message — nothing renders a raw error code, a blank screen, or silently stale data with no failure indication.

## Severity rubric

- `critical`: a partial-failure path leaves financial or irreversible state corrupted (double-charged payment, silently lost order) with no rollback, no idempotency, and no operator-visible flag; a stack trace or internal query is rendered directly to an unauthenticated user.
- `high`: no circuit breaker on a critical downstream dependency causes a retry storm that measurably worsens an outage; a core flow (checkout, login) hangs indefinitely on a slow dependency instead of timing out with a message; leaked internal file paths/hostnames to authenticated users.
- `medium`: a non-critical view has no error state (shows a blank screen instead of a message) on failure; retries lack backoff/jitter but are capped and idempotent so impact is limited; a loading spinner persists after an error on a secondary view.
- `low`: a missing or generic (non-actionable) empty state on a low-traffic view; a timeout message that's accurate but not specific about next steps.
- `info`: minor UX inconsistency in error copy tone/style across views; a loading indicator that appears slightly later than ideal but still resolves correctly.

## Detection method

For error-handling and timeouts, detection is injecting the failure (kill the dependency, delay the response) and diffing observed UI/response behavior against the no-failure baseline — a fail is a hang past the expected bound, an unhandled exception, or output indistinguishable from success. For leaked internals, detection is the response body/rendered DOM containing a stack-trace pattern, absolute file path, or SQL fragment. For partial-failure, detection is inspecting the data store/API state after a mid-operation kill — duplicate records, a debited-but-unfulfilled order, or an orphaned resource is a fail; a rollback, a resumed-to-completion state, or an explicit `status: partial` record is a pass. For retry/backoff, detection is capturing the actual request timing/count against a failing dependency (constant-interval hammering vs increasing intervals, uncapped vs capped attempts). For circuit breaking, detection is whether request volume against a failing dependency drops to near-zero after the threshold is crossed, then resumes after the cooldown. For UI states, detection is a screenshot/DOM diff of the view under each condition (zero data, mid-load, and forced-error) against a designed-state baseline — a blank render, raw JSON, or a stuck spinner is a fail. Evidence for the Finding is the before/after screenshot or DOM snapshot, the response body excerpt, the data-store state dump, or the request-timing trace.

## References

Skills: `web-design-guidelines`, `webapp-testing` (driving/screenshotting the UI states), `load-chaos-resilience` module (overlapping backend-side timeout/retry/circuit-breaking mechanics — this module covers the user-visible surface of the same failures). Standards: Nielsen Norman Group error-message heuristics, Google Cloud's "Release It!" circuit-breaker/bulkhead patterns (Nygard), Stripe API idempotency-key documentation as a reference implementation.
