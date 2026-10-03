---
name: privacy-compliance
description: "Probes PII handling, data minimization, retention/erasure, consent, audit trails, subject access, cross-border transfer, and encryption against GDPR/CCPA-style expectations."
applies-to: [web, api, mobile, database, language:any, framework:any]
---

# Privacy Compliance

## Purpose

Surface weaknesses in how the app identifies, collects, stores, moves, and destroys personal data — the class of failure that turns a bug into a regulatory incident (GDPR/CCPA fines, breach-notification duty, user harm) rather than just a broken feature.

## Applies-to (detail)

Targets any surface that collects, stores, processes, or displays data tied to an identifiable person: web frontends and forms, REST/GraphQL APIs, mobile clients, and the database/storage layer behind them, regardless of language or framework. Applies to apps with user accounts, analytics/telemetry, uploaded content, support tickets, payment or health data, or any third-party integration that receives user data (analytics SDKs, ad pixels, email providers). Does not apply to a pure internal CLI/library with no user-identifying input and no network egress carrying such data — note the skip reason explicitly rather than silently matching.

## Probes

- **PII inventory**: grep the schema/models, API request/response payloads, and logs for identifier-shaped fields — `email`, `phone`, `ssn`, `dob`, `address`, `ip_address`, `device_id`, free-text fields (`notes`, `bio`, `support_message`) that plausibly carry PII — and confirm each is tracked, not just discovered ad hoc.
- **Data minimization**: for each collection point (signup form, checkout, profile edit, analytics event), check whether a field is collected but never read by any downstream code path (`grep -r` for the field name outside the write path); flag fields marked `required` in a form/schema with no corresponding feature that uses them.
- **Over-broad API responses**: call every endpoint returning a user-shaped object as a *different* authenticated user or an admin-scoped view, and diff the response fields against what that screen/consumer actually renders — look for full SSN/full card number/other users' emails riding along in a payload that only needed a display name.
- **Right-to-erasure**: submit an account-deletion request (API call or UI flow) for a seeded test user, then query the database directly, check backups/exports, object storage (uploaded avatars/documents), search-index rows, cache entries, and any third-party sync (analytics, email list, CRM webhook) for residual rows keyed to that user's ID/email after the deletion completes.
- **Right-to-access / data portability**: submit a data-subject-access request (self-service export feature, or the documented support path) and verify the export contains all PII categories found in the inventory step, in a re-usable format, delivered within the app's stated SLA.
- **Consent gating**: for every non-essential data use (marketing email, analytics/tracking pixels, third-party ad SDKs, cookie categories beyond strictly-necessary), check whether the corresponding call/pixel/cookie fires *before* the user makes an affirmative consent choice — inspect network requests and cookies set on first page load with no consent interaction yet.
- **Consent withdrawal**: after granting consent then revoking it (toggle off in settings / reject in the cookie banner), re-check network traffic and cookie jar for the same tracking calls firing on the next page load.
- **Audit trail coverage**: perform a read, an update, and a delete on a PII-bearing record as an authenticated user, then check for a corresponding audit-log entry (actor, action, target record ID, timestamp) — flag any PII read/write path with zero audit logging, especially admin/support "view as user" tooling.
- **Cross-border transfer**: identify every third-party service receiving PII (payment processor, email/SMS provider, analytics, AI/LLM API calls that include user content) and check for a documented legal basis/data-processing agreement reference in code comments, config, or `references/`/`docs/` — flag PII sent to a sub-processor with no transfer mechanism noted (SCCs, adequacy decision, or equivalent).
- **Encryption at rest**: inspect the database/storage config for column- or disk-level encryption on high-sensitivity fields (SSN, payment data, health data, auth secrets); check whether backups and any read-replica/data-warehouse copy inherit the same protection.
- **Encryption in transit**: check that every endpoint carrying PII is served over TLS only (no plaintext HTTP fallback, no `http://` links to PII-bearing pages), and that internal service-to-service calls carrying PII (app → analytics, app → data warehouse) are also TLS-protected, not plaintext on an internal network assumed "trusted."
- **Retention limits**: check for a scheduled job/policy that purges or anonymizes PII-bearing rows (inactive accounts, expired sessions, old support tickets, raw analytics events) past a stated retention window; flag PII with no retention policy at all (rows accumulating indefinitely with no deletion path).
- **Log leakage**: grep application logs, error-tracking output (Sentry-style breadcrumbs), and request-tracing spans for raw PII (full email, password, token, card number) written in cleartext rather than masked/redacted.

## What "safe" looks like

Every PII field is accounted for in an inventory with a stated purpose; no field is collected without a downstream consumer. API responses return only the fields the requesting principal is entitled to and the consumer needs — no incidental PII riding along. A deletion request results in zero residual PII-linked rows across primary store, backups reachable within the stated retention window, object storage, search index, and third-party syncs (backups pending their own retention/purge cycle are documented, not silently exempt). A data export contains the full PII inventory in machine-readable form, delivered inside the stated SLA. No tracking call, pixel, or non-essential cookie fires before affirmative consent, and revoking consent stops those calls on the very next page load — no reload-and-still-fires gap. Every read/write/delete of PII by a human actor (including support/admin tooling) produces an audit-log entry with actor, action, target, and timestamp. Every third-party PII recipient has a documented transfer basis. High-sensitivity fields are encrypted at rest (including in backups), and all PII-bearing transport uses TLS with no plaintext fallback anywhere in the path. A retention policy exists and executes for every PII category; nothing accumulates unbounded. No log or trace line contains unmasked PII.

## Severity rubric

- `critical`: right-to-erasure request leaves PII fully readable/queryable afterward (data breach exposure risk); PII sent or stored in plaintext over the network or at rest for high-sensitivity categories (SSN, payment, health); tracking/third-party data sharing with zero consent gate on a production surface.
- `high`: API response leaks another user's PII (cross-user exposure) even without malicious intent; no audit trail at all for admin "view as user"/support access to PII; consent withdrawal doesn't stop tracking calls; no retention policy for a PII category that accumulates indefinitely.
- `medium`: data minimization violation (collecting unused PII) with no current exposure path; DSAR export missing a non-sensitive PII category; audit log present but missing actor or timestamp; cross-border transfer undocumented but to a reputable processor with its own controls.
- `low`: PII appears masked-but-incomplete in logs (e.g., last 4 digits shown where full masking is the norm); retention policy exists but runs less frequently than stated.
- `info`: PII inventory gap for a low-sensitivity field (e.g., display name) with no handling issue found; documentation of transfer basis exists but isn't linked from the code path that performs the transfer.

## Detection method

For minimization/inventory, detection is a grep/code-search diff: field declared in schema or form vs. field referenced by any reader — an unreferenced PII field is a finding. For over-broad responses, detection is a response-body diff between what the consuming UI renders and the full JSON payload returned. For erasure, detection is a direct database query (and backup/index/third-party API check) for the deleted user's ID/email returning zero rows past the stated grace window. For consent, detection is the browser network log (via `mcp__claude-in-chrome__read_network_requests` or equivalent) showing a tracking request fire timestamp earlier than the consent-click timestamp, or firing again after a withdrawal action. For audit trails, detection is the audit-log table/index returning zero matching rows for a performed PII action. For encryption, detection is inspecting the storage engine/column definitions for encryption-at-rest settings and capturing the raw TLS handshake (or its absence — a successful plaintext `http://` connection) for in-transit claims. For retention, detection is a row's `created_at`/`last_active_at` timestamp older than the stated policy window with no corresponding deletion/anonymization job in the scheduler config. Evidence for the Finding is the literal query result, response diff, network log entry, or config excerpt proving the condition.

## References

Skills: `security-audit` (data-exposure and logging sections), `secrets-management` (encryption-at-rest patterns), `data-logic-integrity` module (adjacent: data correctness vs. this module's data handling/compliance focus). Standards: GDPR (Articles 5 data minimization, 17 right to erasure, 15 right of access, 30 records of processing, 44–49 transfers), CCPA/CPRA (right to know, right to delete, right to opt out of sale/sharing), OWASP Top 10 (A02 Cryptographic Failures, A09 Security Logging and Monitoring Failures).
