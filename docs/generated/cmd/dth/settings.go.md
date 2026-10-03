<!-- dth:generated source="cmd/dth/settings.go" — edit only inside dth:human blocks -->
# `cmd/dth/settings.go`

<!-- dth:chunk 4d11f0bb2cd2f170 -->
## `app.settingsCmd`

Creates a `settings` command group with three subcommands: `export` (writes Hub's current configuration as YAML with secrets as references), `resolve` (fills in secret references from environment/vaults and prints the result), and `check` (validates settings files). The export subcommand supports an optional output file via `-o` flag, while export and resolve accept settings files via `-f`.

<!-- dth:chunk da4c0baa7f00e0b6 -->
## `app.settingsCheckCmd`

Creates a `settings check` subcommand that validates settings files for syntax, unknown keys, missing fields, and SSO domain compliance without contacting a Hub. Accepts one or more files/directories via `-f` flag, optionally resolves secret references with `--resolve`, and can require at least one owner with `--require-owner`. Exits non-zero if any problems are found, making it suitable for deployment pipeline validation.

<!-- dth:chunk cdeb2520e43e8874 -->
## `app.printCheck`

Formats and prints a settings check report to stdout, displaying owners, admins, SSO configuration, password settings, and secret references. Shows whether secrets were resolved and lists any validation problems found. Outputs "No problems found." on success or lists problems with dashes on failure.
