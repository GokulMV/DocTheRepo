<!-- dth:generated source="internal/store/llm.go" — edit only inside dth:human blocks -->
# `internal/store/llm.go`

<!-- dth:chunk 8e1c56fa4237b0c7 -->
## `Ledger.Record`

Records a single LLM usage event to the database via the store's query interface. Converts the `ports.UsageRecord` input into database-specific parameters, defaulting the outcome to "ok" if not provided, and returns any error from the insert operation.

<!-- dth:chunk d10e2a51045c271d -->
## `LoadGuard`

Constructs a spend guard from database spend limits and cost table entries. Queries spend_limits to build scope-based limit rules (supporting token and cost constraints with optional breach alerts) and cost_table to populate per-model pricing. Returns an initialized `spendguard.Guard` or an error if database queries fail. The `allowUnlimited` parameter is passed through to the Guard constructor to control whether unlimited operations are permitted.
