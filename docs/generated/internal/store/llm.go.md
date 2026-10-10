<!-- dth:generated source="internal/store/llm.go" — edit only inside dth:human blocks -->
# `internal/store/llm.go`

Provides functions to load spend limits and cost tables from the database and construct a spend guard for tracking LLM usage.

<!-- dth:chunk d10e2a51045c271d -->
## `LoadGuard`

Assembles a spend guard from database spend_limits and cost_table rows. Loads spending limits and transforms them into `spendguard.Limit` objects, converting nullable token and cost caps to their values and mapping breach actions to alert flags. Loads the cost table and builds a map of model-to-price entries using `toPrice` for each model. Returns an initialized `spendguard.Guard` configured with these limits and prices, or an error if database queries fail.

<!-- dth:chunk 73583e1c190aadb9 -->
## `toPrice`

Converts a database cost_table row into a `spendguard.Price` struct. Maps standard pricing fields (input, output, embed, cache read/write per million tokens) directly. If a long-prompt tier threshold is configured (positive value), creates a separate pricing tier for long prompts, using long-specific prices where provided and falling back to base prices otherwise. Returns the base price if no threshold is set.
