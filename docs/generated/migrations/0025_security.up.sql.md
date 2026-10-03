<!-- dth:generated source="migrations/0025_security.up.sql" — edit only inside dth:human blocks -->
# `migrations/0025_security.up.sql`

<!-- dth:chunk 37d9d4c7639240a2 -->
## `migrations/0025_security.up.sql`

This migration establishes the database schema for security scanning and findings management. It adds a new 'security' LLM feature type and creates three core tables:

**security_scans** tracks security scan operations on repositories, storing status (queued/running/done/failed), commit SHA, analyzed modules, and a verdict ('GO'/'NO-GO') from the security analysis engine. It includes timing data (created_at, finished_at) and metadata about what triggered the scan (started_by) and analysis summaries (counts, tokens used).

**security_findings** stores individual security issues discovered in a scan, with location data (file, line numbers), severity/exploitability/priority ratings, reproduction steps, and fix proposal tracking (status: confirmed/plausible/rejected; fix_status tracks the workflow from proposal to PR). Each finding references both a scan and repository.

**security_module_cache** implements memoization for individual security modules—if the same module is scanned with identical inputs (matched by hash), cached findings are reused rather than re-running analysis. This optimizes repeated scans of unchanged code.
