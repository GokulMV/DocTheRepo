<!-- dth:generated source="internal/core/security/kryptonite.go" — edit only inside dth:human blocks -->
# `internal/core/security/kryptonite.go`

<!-- dth:chunk 1d1680c258531714 -->
## `Module`

Module represents a kryptonite security audit dimension with metadata about its capabilities and usage. It includes the module's name, description, whether it can run statically (on code alone without a live application), and whether it's selected by default when users start a scan. The Reason field explains why a module requires a live application if Static is false. Playbook is unmarshaled separately and contains the full module definition.

<!-- dth:chunk a8c02c4f97f68925 -->
## `Modules`

Modules loads all vendored security modules from the embedded filesystem, parsing their front matter metadata and enriching them with configuration from needsLiveApp and defaults maps. It sorts the result with default modules first, then alphabetically by name. Errors reading individual module files are silently skipped.

<!-- dth:chunk 31f1be1ab5f41ba3 -->
## `ModuleByName`

ModuleByName retrieves a module by exact name match, returning the module and a boolean indicating whether it was found. It searches the current module list in order.

<!-- dth:chunk 4df087676a16bae9 -->
## `parseModule`

parseModule extracts name and description fields from a module file's YAML front matter (between `---` delimiters) and preserves the full file text as the playbook. It returns a partial Module if front matter is missing or malformed, with only the Playbook field populated.

<!-- dth:chunk 74efdf6cc2c79a5c -->
## `Reference`

Reference returns a vendored reference document (severity, finding-schema, agent-prompts).

<!-- dth:chunk 44a2646a29a7ee66 -->
## `Priority`

Priority maps severity and exploitability levels to a priority ranking (P0–P3) using a lookup matrix. Unknown severity values default to "info" (least urgent), and unknown exploitability values use the rightmost column of their severity row (also least urgent).

<!-- dth:chunk defdf458f2aac22d -->
## `Verdict`

Verdict returns "NO-GO" if any finding is both confirmed or plausible and has priority P0 or P1, otherwise "GO". It provides a readiness assessment based on the severity of open security findings.

<!-- dth:chunk 4333afb7a774ae47 -->
## `__module__`

Module-level configuration and lookup tables for kryptonite security scanning. Includes an embedded filesystem (vendored) for module files, maps of modules requiring live applications (needsLiveApp), default modules to run by default, severity levels, exploitability levels, and a priority matrix that combines severity and exploitability into P0–P3 ratings.
