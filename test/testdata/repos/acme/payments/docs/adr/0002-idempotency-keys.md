# ADR 0002: Idempotency keys on every charge

## Status

Accepted

## Context

Mobile clients retry charges after network timeouts, and we saw duplicate charges in production.

## Decision

Every POST to the payments API must carry an Idempotency-Key header. Responses are stored for 24 hours and
replayed for repeated keys.

## Consequences

Clients generate a UUID per checkout attempt. Redis holds the stored responses.
