<!-- dth:generated source="internal/mcp/tools.go" — edit only inside dth:human blocks -->
# `internal/mcp/tools.go`

This file defines MCP tools that expose Hub API endpoints for code search, documentation, entity relationships, and production issue management.

<!-- dth:chunk 4650e85cec602bfb -->
## `HubTools`

Returns a slice of read-only tools exposing Hub API endpoints: `ask` (question-answering with citations), `search_entities` (knowledge graph lookup), `entity_graph` (entity relationship visualization), `list_issues`/`get_issue` (production issue management), `list_known_issues` (known-issue rules), `library` (documentation shelves), `list_docs`/`read_document` (repository documentation), and `read_doc` (individual doc nodes). Each tool defines its name, description, input schema with validation, and a handler that unmarshals parameters, calls the Hub API via the provided `Caller`, and formats the response as Markdown text, handling pagination, citations, confidence scores, and edge cases like missing optional parameters.

<!-- dth:chunk 0b1b2a8aaf2f5d91 -->
## `shortSHA`

Returns a truncated SHA string, keeping only the first 7 characters if longer, otherwise returning the input unchanged. Used to display abbreviated commit hashes in formatted output.

<!-- dth:chunk d0d4a7e99c320726 -->
## `whyText`

Formats a slice of reason strings into a single string prefixed with ": " and joined by "; ". Returns an empty string if the slice is empty. Used to append explanatory text to confidence labels.
