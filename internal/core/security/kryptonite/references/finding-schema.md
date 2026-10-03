# Finding Schema

The **Finding** object is the canonical data structure used across all kryptonite phases to represent a single discovered vulnerability, weakness, or concern. Attacker, verifier, and fixer agents produce, consume, and update Findings in this format.

## Field Specification

| Field | Type | Allowed Values | Required | Purpose |
|-------|------|----------------|----------|---------|
| `id` | str | UUID or unique identifier | Yes | Stable identifier for the finding across phases; enables deduplication and tracking |
| `dimension` | str | Attack module name (e.g., `security-pentest`, `api-abuse-and-limits`, `data-logic-integrity`) | Yes | Attack category; links to the module that discovered it |
| `title` | str | Free text, concise (< 100 chars) | Yes | Human-readable summary of the issue |
| `surface` | str | Affected app surface (route, endpoint, function, data store, auth boundary) | Yes | Where in the app the issue manifests |
| `repro` | list[str] | Ordered steps to reproduce | Yes | Actionable reproduction path for verifier and fixer agents |
| `evidence` | str | Output / logs / captured state proving the issue exists | Yes | Proof that the issue was observed; quotes or traces from the actual app |
| `severity` | str | `critical` \| `high` \| `medium` \| `low` \| `info` | Yes | Impact ranking; assigned during verification phase |
| `exploitability` | str | `trivial` \| `easy` \| `moderate` \| `hard` | Yes | Effort required to exploit; assigned during verification phase |
| `status` | str | `candidate` \| `confirmed` \| `plausible` \| `rejected` | Yes | Finding state: newly discovered, verified, uncertain, or ruled out |

All nine fields are mandatory in every Finding object.

## Example Finding

```json
{
  "id": "finding-20260914-001",
  "dimension": "security-pentest",
  "title": "SQL injection in user search endpoint",
  "surface": "/api/users/search",
  "repro": [
    "POST /api/users/search with body: {\"query\": \"admin' OR '1'='1\"}",
    "Observe that the response returns all users, not just matches for the injected query"
  ],
  "evidence": "Response included users with no match to input; raw SQL query from logs: SELECT * FROM users WHERE name LIKE '%admin' OR '1'='1%'",
  "severity": "critical",
  "exploitability": "trivial",
  "status": "confirmed"
}
```
