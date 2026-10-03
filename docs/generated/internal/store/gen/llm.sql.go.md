<!-- dth:generated source="internal/store/gen/llm.sql.go" — edit only inside dth:human blocks -->
# `internal/store/gen/llm.sql.go`

<!-- dth:chunk ba153cc706248994 -->
## `Queries.GetProvider`

Retrieves a single LLM provider by ID from the database. Executes a query that fetches all provider columns (ID, kind, name, base URL, encrypted key, extra config, PII redaction flag, enabled status, timestamps, and key metadata) and scans them into an `LlmProvider` struct. Returns the provider or a database error.

<!-- dth:chunk 256346360a163b62 -->
## `InsertUsageEventParams`

Parameters for inserting a usage event record. Captures LLM API usage including request identity (ID, timestamp), feature and model information, token counts (input, output, cache), cost in USD, latency, execution outcome, caching flags, and optional references to repo, user, job, and issue. Some fields like provider ID, repo, user, job, and issue are nullable.

<!-- dth:chunk ee634a2daf5fdbd6 -->
## `Queries.InsertUsageEvent`

Inserts a usage event record into the database with all tracking metrics and contextual information from the provided parameters. Executes a parameterized insert statement with 19 values covering event identity, timing, feature, model, tokens, cost, latency, outcome, and optional resource associations.

<!-- dth:chunk a4f156d6d0347912 -->
## `Queries.ListCostTable`

Retrieves all cost table entries from the database, returning a slice of `CostTable` records sorted by provider kind and model. For each row, scans provider kind, model name, pricing per million tokens for input/output/embedding, cache read/write pricing, and metadata (who updated it, when, source, and verification status).

<!-- dth:chunk 2ebdf7e6deb7c530 -->
## `Queries.ListProviders`

Fetches all LLM provider configurations from the database, returning them sorted by name. Iterates through result rows, scanning each provider's 12 columns (ID, kind, name, endpoints, encrypted credentials, extra config, flags, timestamps, and key hints) into `LlmProvider` structs.

<!-- dth:chunk 1e0599cb7bc6e80f -->
## `__module__`

SQL query constant definitions for LLM provider and usage tracking operations. Includes queries for CRUD operations on providers and routes, listing cost pricing tables, and recording usage events with token counts and costs. Generated code mapping SQL statements to prepared query strings.
