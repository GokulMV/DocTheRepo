<!-- dth:generated source="internal/settings/export.go" — edit only inside dth:human blocks -->
# `internal/settings/export.go`

<!-- dth:chunk 5143fbad691113da -->
## `Export`

Exports the Hub's current settings from the API as a structured Document for serialization. Fetches all providers, connectors, routes, repositories, and spend limits from the API state, transforming them into document form. Secrets are not readable; instead, set secrets become `${env:...}` references with suggested variable names for the caller to populate from their secret store. Returns an error if the API call fails.
