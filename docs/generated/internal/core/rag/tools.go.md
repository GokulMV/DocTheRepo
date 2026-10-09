<!-- dth:generated source="internal/core/rag/tools.go" — edit only inside dth:human blocks -->
# `internal/core/rag/tools.go`

This file implements tool integration for the RAG engine, allowing the LLM agent to call external tools when questions reference live state or specific tool names.

<!-- dth:chunk 1f82437cef71fc18 -->
## `AgentTool`

A struct defining a tool available to the LLM agent. Each tool has a unique name (e.g., "sentry.search_issues"), a server/connection name for citation purposes, a human-readable description, and a JSON Schema string describing its arguments.

<!-- dth:chunk 93ca07e0772b6397 -->
## `ToolBox`

ToolBox lists and calls the tools someone may use.

<!-- dth:chunk 19cfa79c0279f617 -->
## `Engine.agentTools`

Returns the list of agent tools available for a given query's role, or nil if tools are not configured or the role is empty.

<!-- dth:chunk 7c1b82a790b38077 -->
## `Engine.wantsTools`

Determines if a question should be routed to connected tools first by checking if the question mentions a tool's server name (≥3 chars) or tool prefix (e.g., "sentry" for "sentry.search_issues"), or contains live-state keywords like "errors", "logs", "deploy", "alerts", etc.

<!-- dth:chunk f4ac94e210949584 -->
## `citesTool`

Returns true if any citation in the list has type "tool", used to detect whether an answer was sourced from tool results.

<!-- dth:chunk 6b9f59e8e8e49830 -->
## `toolCall`

toolCall is the agent's input for use_tool.

<!-- dth:chunk 32dbc1c055c6dabc -->
## `Engine.runTool`

Calls a tool by parsing JSON input containing tool name and arguments, invokes the tool through the engine's Tools interface, and returns a Chunk with the results. The chunk ID is based on SHA256 of the tool name and arguments, and results are trimmed to 6000 characters. Returns an error if the input is invalid JSON, the tool is not found, or the tool call fails.

<!-- dth:chunk e47aff1ede6937e2 -->
## `compactJSON`

Converts a JSON RawMessage to a compact string representation, handling unmarshaling failures gracefully. Truncates to 200 characters if the marshaled output exceeds that length.

<!-- dth:chunk 2ef60bea0d715b6f -->
## `toolList`

Formats a list of tools into a human-readable string for inclusion in the agent's prompt, showing each tool's name, server, description (max 220 chars), and schema (max 500 chars). Caps the output at 40 tools, noting if more exist.

<!-- dth:chunk 2c1ba3dd4a4c0613 -->
## `__module__`

Module-level constants and regex: `maxToolsInPrompt` (40) limits tools shown in prompts, `maxToolResult` (6000) caps tool output size, and `liveWords` regex matches live-state keywords like errors, logs, deployments, alerts, incidents, and time references used to detect if a question warrants tool lookup.
