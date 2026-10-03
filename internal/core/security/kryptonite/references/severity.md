# Severity × Exploitability → Priority

Every confirmed Finding is assigned a `severity` and an `exploitability` (see
`finding-schema.md`). The matrix below maps that pair to a single **priority**
— P0 (drop everything) through P3 (track, don't block) — used to order the
Fix Loop and to populate "Findings by priority" in the readiness report.

The mapping is monotonic in both directions: moving down a column (less
severe) never raises urgency, and moving right along a row (harder to
exploit) never raises urgency either.

## Severity levels

| Severity | Definition |
|----------|------------|
| `critical` | Full compromise, data breach, or complete service outage — unrestricted access to sensitive data or systems, or total loss of availability. |
| `high` | Significant damage to security, data integrity, or availability affecting many users or core functionality, but stopping short of full compromise. |
| `medium` | Limited-scope impact — a single user, non-sensitive data, or degraded (not broken) functionality — or impact that requires unusual preconditions. |
| `low` | Minor impact: cosmetic weakness, negligible effect on security, reliability, or data, with no realistic path to escalation. |
| `info` | An observation or deviation from best practice with no direct exploitable impact — a hardening opportunity, not a vulnerability. |

## Exploitability levels

| Exploitability | Definition |
|-----------------|------------|
| `trivial` | No special access, tooling, or skill required — a single unauthenticated request or ordinary UI action triggers it. |
| `easy` | Requires only common tooling or public knowledge (browser devtools, `curl`, a documented CVE) and no deep expertise. |
| `moderate` | Requires specific preconditions, timing, or a chained multi-step sequence, and some technical skill to execute reliably. |
| `hard` | Requires privileged access, deep domain expertise, race-condition timing, or heavy resources — low likelihood of real-world exploitation. |

## The matrix

| Severity \ Exploitability | `trivial` | `easy` | `moderate` | `hard` |
|----------------------------|:---------:|:------:|:----------:|:------:|
| `critical` | **P0** | **P0** | **P0** | **P1** |
| `high`     | **P0** | **P0** | **P1** | **P1** |
| `medium`   | **P0** | **P1** | **P1** | **P2** |
| `low`      | **P1** | **P1** | **P2** | **P2** |
| `info`     | **P1** | **P2** | **P2** | **P3** |

## Priority definitions

| Priority | Meaning | Handling |
|----------|---------|----------|
| `P0` | Drop everything. Real, easily-triggered, high-impact. | Must be fixed (or explicitly risk-accepted by the user) before GO. |
| `P1` | Fix in this hardening pass. | Blocks GO unless the user explicitly accepts the residual risk. |
| `P2` | Fix when convenient; track as a backlog item. | Does not block GO; recorded under Residual risks. |
| `P3` | Informational / low-value to chase. | Recorded for awareness only; no action required to reach GO. |
