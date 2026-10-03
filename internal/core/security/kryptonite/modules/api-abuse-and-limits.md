---
name: api-abuse-and-limits
description: "Probes rate limits, quotas, pagination, payload size, token handling, idempotency, and request races for abuse resistance."
applies-to: [api, web, mobile, cli]
---

# API Abuse & Limits

## Purpose

Surface weaknesses that let a client extract, disrupt, or corrupt more than intended by abusing the boundaries of an API's contract — limits that don't hold, requests that aren't deduplicated, and races that aren't locked.

## Applies-to (detail)

Targets any surface exposing a programmatic API — REST/GraphQL/RPC endpoints, webhooks, CLI-driven API clients, and mobile-app backends — where requests are structured and repeatable. Applies whenever the surface map records rate limits, quotas, auth tokens, paginated list endpoints, or any state-mutating (non-GET) endpoint. Does not apply to a pure static site with no API layer.

## Probes

- **Rate-limit / quota bypass**: burst requests past the documented limit from one IP/account/token and check for enforcement; retry the same burst while rotating IP-spoofable headers (`X-Forwarded-For`, `X-Real-IP`), varying casing/trailing-slash on the route, or switching between token and cookie auth on the same account to see if limits key on the wrong dimension.
- **Pagination bombs**: request `limit`/`page_size` far beyond any documented cap (e.g. `?limit=1000000`) and check whether the server clamps it or attempts to materialize the full result; request a negative or zero limit/offset and check for an error vs. undefined behavior; deep-paginate (`offset=10000000`) and check for a timeout or degraded response instead of a controlled empty result.
- **Payload bombs**: submit an oversized request body (multi-MB JSON/XML), a deeply nested JSON object (1000+ levels), a JSON array with hundreds of thousands of elements, or a decompression-bomb file upload, and check for a size/depth limit rejecting it before it reaches business logic.
- **Auth-token abuse**: reuse a token after logout/revocation; reuse a password-reset or email-verification token a second time; use a short-lived token past its stated expiry with clock-skew tolerance; use a token scoped to one resource/action against a different one; check whether refresh tokens are single-use or replayable.
- **Idempotency**: replay the exact same state-mutating request (same `Idempotency-Key` if the API defines one, or the same body with no key) multiple times in quick succession and check whether the effect happens once or N times (e.g. duplicate charges, duplicate orders, duplicate resource creation).
- **Concurrent-request races**: fire N identical mutating requests (e.g. "redeem coupon", "withdraw balance", "claim reward") simultaneously against the same account/resource and check whether the server serializes them correctly or lets more than the intended number succeed (TOCTOU on a check-then-act balance/inventory decrement).
- **Contract / schema violations**: send requests missing required fields, with wrong types (string where int expected), with extra undocumented fields, and with an unexpected `Content-Type`, and check whether the server validates and rejects cleanly (`400`) rather than 500ing or silently coercing in a way that changes behavior.

## What "safe" looks like

Rate limits and quotas hold under burst load and cannot be sidestepped by spoofable headers, route-casing tricks, or auth-method switching — the limit key is tied to a stable identity (authenticated user/account, not just a client-supplied header). Pagination and payload inputs are clamped or rejected at a documented, enforced ceiling before touching business logic or the data layer; oversized/malformed payloads get a fast `400`/`413`, not a timeout or OOM. Revoked, expired, or wrongly-scoped tokens are rejected with `401`/`403` on every use, not just the first check. A repeated mutating request with the same idempotency key (or, where none is defined, the same logical operation replayed within a short window) produces the state change exactly once, with subsequent replays returning the original result rather than a new one. Concurrent requests against the same constrained resource are serialized (lock, atomic decrement, DB constraint) so the total effect never exceeds what a single valid request should have produced. Malformed requests are validated and rejected with a clear `4xx` and no partial side effects.

## Severity rubric

- `critical`: race condition enabling unlimited fund withdrawal, balance duplication, or inventory oversell with real financial/business impact; complete rate-limit bypass enabling unrestricted resource consumption or credential-stuffing at scale.
- `high`: idempotency failure causing duplicate charges/orders; token-scope violation letting a token act on resources outside its intended scope; pagination/payload bomb causing a full service outage (not just single-request failure).
- `medium`: rate limit bypassable via a moderate-effort technique (header spoofing) but not fully open; contract violation causing incorrect (not crashing) behavior; deep-pagination causing a single slow request rather than an outage.
- `low`: minor over-limit tolerance (limit enforced at 110% of documented value); non-sensitive duplicate side effects (duplicate log entry, not duplicate charge).
- `info`: missing `Idempotency-Key` support as a hardening gap with no demonstrated duplicate-effect exploit; undocumented but harmless extra-field tolerance.

## Detection method

For rate/quota limits: count successful (`2xx`) responses across a burst and compare to the documented threshold; a bypass is confirmed if requests keep succeeding past the threshold under a spoofing variant that failed without it. For pagination/payload bombs: measure response latency and server resource usage (CPU/memory, from logs or a monitoring probe) — an unbounded spike or timeout past a defined threshold (e.g. >5s or OOM) is a fail; a clamped/rejected (`400`/`413`) fast response is a pass. For token abuse: the detection signal is a `2xx` response using a token that should have been rejected — capture the token state (revoked/expired/wrong-scope) and the response in evidence. For idempotency/races: fire the replay/concurrent burst and count the actual number of resulting side effects (rows created, balance delta, emails sent) against the expected count of one — any excess is the fail signal. For contract violations: a `5xx` or a `2xx` with silently-coerced/incorrect data where `400` was expected is the fail signal.

## References

Skills: `api-security-testing`, `api-security-best-practices`, `security-audit` (rate-limiting and abuse-testing phases). Concepts: OWASP API Security Top 10 (API4: Unrestricted Resource Consumption, API5: Broken Function-Level Authorization), TOCTOU race-condition testing patterns.
