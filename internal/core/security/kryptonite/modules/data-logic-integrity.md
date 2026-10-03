---
name: data-logic-integrity
description: "Probes boundary/fuzz inputs, races and double-spend, corrupt/partial data, migration safety, invariant violations, and error-path correctness."
applies-to: [web, api, cli, database, language:any]
---

# Data & Logic Integrity

## Purpose

Surface weaknesses where the app's business logic or data layer produces an incorrect, inconsistent, or corrupted result — not because of an external attacker bypassing auth, but because an edge-case input, a race, a partial failure, or a migration breaks an invariant the app relies on.

## Applies-to (detail)

Targets any app with business logic operating over persisted state: web/API backends with a database, CLI tools that transform or migrate data, and any surface with numeric, financial, or stateful invariants (balances, counts, statuses, ordering). Applies to both runnable targets (live fuzzing, race probes) and static-only targets (code-level review of boundary handling, migration scripts, and invariant checks). Does not apply to a pure presentational frontend with no logic of its own beyond calling a backend already covered by this module elsewhere.

## Probes

- **Boundary/fuzz inputs**: submit empty string, null/undefined, zero, negative numbers, `MAX_INT`/`MAX_INT + 1`, floating-point values where an integer is expected, extremely long strings, Unicode edge cases (emoji, RTL marks, null bytes, homoglyphs), and empty arrays/objects to every field with a numeric, string-length, or enum constraint; check each against its documented valid range.
- **Race conditions & double-spend**: fire concurrent requests that read-then-write the same balance/counter/stock field (e.g. two simultaneous "apply discount code" or "transfer funds" calls) and check whether the final state reflects both operations correctly serialized or whether one silently overwrote the other (lost update) or both succeeded when only one should have (double-spend).
- **Corrupt/partial data**: submit a request that fails partway through a multi-step write (kill the connection mid-request, or submit a payload that's valid for step 1 but invalid for step 2 of a multi-table write) and check whether the operation is atomic (all-or-nothing) or leaves partial rows/orphaned foreign keys.
- **Migration safety**: for any pending or recent schema migration, check whether it's backward-compatible with the currently-deployed code during a rolling deploy (old code + new schema, and new code + old schema, both function); check whether the migration is reversible or has a tested rollback path; check whether it handles existing rows with NULL/legacy-shaped data in a newly-constrained column.
- **Invariant violations**: identify the app's core invariants (e.g. "balance never negative," "order total equals sum of line items," "a resource has exactly one owner," "status transitions only move forward") and attempt to construct a sequence of valid-looking individual requests that collectively violates one.
- **Error-path correctness**: force each documented failure mode (a downstream call errors, a validation fails, a write conflicts) and check that the error path itself doesn't do more damage than the happy path would have — no partial commit before the error, no resource leaked, no misleading success response after an internal failure.

## What "safe" looks like

Every boundary/fuzz value is validated against its documented range and rejected with a clear error, or handled correctly within range (no overflow wraparound, no truncation, no crash, no injection through the value's serialized form). Concurrent operations against shared state use a transaction, lock, or atomic/conditional update so the final state matches what strictly-sequential execution would have produced — no lost updates, no double-spend. Multi-step writes are wrapped in a transaction (or compensating-action saga) so a mid-operation failure leaves either the pre-state or the fully-applied post-state, never a partial mix. Migrations apply cleanly to production-shaped data including legacy/NULL rows, remain compatible with the previous code version during a rolling deploy window, and have a verified rollback path. No sequence of individually-valid requests can drive a tracked invariant (balance, total, status) out of its defined valid range. Every forced failure mode returns an honest error response, commits nothing partial, and releases any resource/lock it held.

## Severity rubric

- `critical`: a race condition or invariant violation enabling real double-spend, negative balance, or unauthorized value creation with direct financial/business impact; a migration that corrupts or silently drops production data with no rollback.
- `high`: partial-write corruption leaving orphaned or inconsistent records reachable by normal app flows; an invariant violation with material but non-financial impact (e.g. duplicate order fulfillment, incorrect access grant via a status-transition bug); a migration incompatible with the previous code version, breaking a rolling deploy.
- `medium`: boundary-input mishandling causing incorrect (not crashing) output — an off-by-one, a silent truncation — in a non-critical field; an error path that leaks a held resource (connection, lock) under a specific failure mode without broader corruption.
- `low`: fuzz input causes a crash/500 on a single request with no persisted side effect and no way to chain it further; cosmetic invariant deviation (a count off by one in a non-financial display) with no downstream consequence.
- `info`: missing input-length/type validation that hasn't been shown to cause incorrect behavior yet; a migration lacking a documented (but not yet needed) rollback plan.

## Detection method

For boundary/fuzz: compare the response/behavior against the documented valid range — the fail signal is a `2xx`/silent-acceptance where rejection was documented, or a crash/exception where graceful rejection was expected, or a stored value that doesn't match the input after normalization (truncation, wraparound). For races/double-spend: after firing the concurrent burst, query the actual final state directly (balance, row count, status) and compare to the value a correctly-serialized execution would produce — any divergence is the fail signal, with the query result as evidence. For partial-write corruption: after a forced mid-operation failure, query for orphaned rows (foreign keys pointing to nothing, or a parent row with no expected children) — any such row is the fail signal. For migrations: run the migration against a snapshot of production-shaped data (including NULL/legacy rows) and check for migration-script errors or rows left in a non-conforming state; check that the previous app version still starts and serves basic requests against the migrated schema. For invariants: after the constructed request sequence, directly check the invariant's defining condition (e.g. `balance >= 0`) against stored state — violation is binary and needs no judgment call. For error paths: force the failure and inspect both the client-visible response and the actual persisted/locked state — the fail signal is any mismatch between what the response claims happened and what the data layer shows actually happened.

## References

Skills: `security-audit` (data-integrity-adjacent checks), general property-based/fuzz-testing practice, database transaction-isolation and locking patterns (`SELECT ... FOR UPDATE`, optimistic concurrency with version columns). Concepts: ACID transaction guarantees, expand/contract migration pattern for backward-compatible schema changes, invariant-based testing (define the invariant first, then search for a sequence that breaks it).
