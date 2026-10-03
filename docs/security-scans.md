# Security scans

**Security** (editors and above) scans a repository for weaknesses and fixes the ones you choose. It is
built on the attack modules of [kryptonite](https://github.com/levitasOrg/kryptonite) (MIT), vendored in
`internal/core/security/kryptonite/` at the commit noted in its README.

## How kryptonite's loop maps onto the Hub

kryptonite attacks a running test instance where one exists, and falls back to static analysis and
code-level probes where none does. The Hub never sends requests to your systems, so it always runs the
static form: it reads the tracked branch.

| kryptonite phase | In the Hub |
|---|---|
| 1. Recon & baseline | Picks the files each module needs from the tracked branch (handlers, auth, config, manifests, CI). No model call. |
| 2. Scenario design (gate) | **New scan**: choose modules, then **Show estimate** lists what will be read and the token estimate. Nothing runs until **Start scan**. |
| 3. Attack loop | One attacker call per module over its files, citing file and line for every finding. |
| 4. Verify findings | One verifier call per module re-checks each candidate against the cited code: **confirmed**, **plausible** (depends on code not shown) or **rejected**. Severity and exploitability are re-derived and ranked P0–P3 with kryptonite's matrix. |
| 5. Design fixes | Only for findings you select, when you click **Fix selected**. |
| 6. Approval gate | That click and its confirmation. Nothing is written before it. |
| 7. Apply & re-verify | One pull request with a minimal fix and two tests per finding (vuln closed, feature intact). The Hub never merges it, whatever the repository's docs push mode; your CI runs the tests and people review. Re-scan after merging. |
| 8. Readiness report | Each scan's verdict: **NO-GO** while a confirmed or plausible P0/P1 finding is open, else **GO**. |

## Modules

Defaults: security pentest, config and secrets, supply chain and dependencies, API abuse and limits, data
and logic integrity, privacy and compliance. Also available: observability, failure UX, cost efficiency,
accessibility and i18n. Load and chaos resilience is listed but cannot run, because it needs a running
instance under load.

| Priority | Meaning (kryptonite) |
|---|---|
| P0 | Real, easily triggered, high impact: fix before release |
| P1 | Fix in this pass; blocks GO unless the risk is accepted |
| P2 | Backlog |
| P3 | Informational |

## Cost

- Each module reads at most 20 files and 120 KB, from at most 80 files per scan; the estimate is shown
  first. Calls go through the spend guard like every other model call.
- The **Security scans** route (Providers & routing) chooses the model; without one, scans use the Ask
  model. A strong model finds more and rejects more false positives.
- Results are cached per module and exact file contents: re-scanning code that has not changed reuses
  the verified findings and makes no model call.

## What it does not do

- It does not run the application, fuzz endpoints or test load. A static finding marked *plausible* may
  be mitigated somewhere the scan did not read.
- It does not replace dependency scanners (`govulncheck`, `npm audit`, Trivy) or a human review. It finds
  the kind of issues a careful reviewer would, and is honest about its confidence.
- A fix never changes CI workflows, lockfiles, or files outside the repository, and files over 60 KB are
  left for a person to fix.

## API

```sh
curl -sS -X POST "$HUB/api/v1/security/repos/$REPO/plan"  -H "Authorization: Bearer $DTH_TOKEN" -d '{"modules":["security-pentest"]}'
curl -sS -X POST "$HUB/api/v1/security/repos/$REPO/scans" -H "Authorization: Bearer $DTH_TOKEN" -d '{"modules":["security-pentest"]}'
curl -sS "$HUB/api/v1/security/scans/$SCAN" -H "Authorization: Bearer $DTH_TOKEN"
curl -sS -X POST "$HUB/api/v1/security/repos/$REPO/fix"   -H "Authorization: Bearer $DTH_TOKEN" -d '{"finding_ids":["…"]}'
```

Scans and fixes are in the audit log (`security.scan`, `security.fix`).
