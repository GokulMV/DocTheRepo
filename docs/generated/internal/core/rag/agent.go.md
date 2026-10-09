<!-- dth:generated source="internal/core/rag/agent.go" — edit only inside dth:human blocks -->
# `internal/core/rag/agent.go`

Implements an agentic RAG system that iteratively searches a codebase and external tools to answer questions, using an LLM to decide which actions to take.

<!-- dth:chunk 327177ea3e878649 -->
## `agentStep`

Represents a single decision made by the agent during investigation. The `Action` field specifies the operation (search, read_file, list_files, use_tool, or answer); `Input` contains the query text, file path, or tool call JSON; and `Reason` provides a human-readable explanation of what the agent is attempting.

<!-- dth:chunk eeb3307ef0328b28 -->
## `Engine.investigate`

Orchestrates the agent's iterative search for material to answer a question, running up to `Engine.AgentSteps` iterations (plus 2 if tools are available). Each iteration prompts the LLM to choose an action (search, read_file, list_files, use_tool, or answer), executes it, and accumulates results. The function deduplicates chunks by ID, skips repeated actions, and stops on error or when the agent chooses to answer. Returns results ordered with newly discovered chunks first, then seed chunks; also updates token usage metrics from LLM calls and notifies via `Query.OnStatus` callback.

<!-- dth:chunk c658f8a65d9dbd1e -->
## `agentPrompt`

Constructs the prompt for the agent by formatting the question, up to 30 found chunks (with 240-character previews), action history, available tools if any, and an indication of whether file exploration is possible. Each chunk is shown as `scope:path symbol: preview` within data tags.

<!-- dth:chunk 4ba28aca2b482391 -->
## `clipStr`

Truncates a string to at most `n` characters, appending an ellipsis if truncated.

<!-- dth:chunk 971d94139204322b -->
## `__module__`

Defines agent configuration: `AgentMinSources` is the minimum number of sources expected; `agentSchema` validates the JSON structure of agent decisions (action, input, reason); and `agentSystem` provides the system prompt instructing the agent to search iteratively using full-text/semantic search, file reading, directory listing, tool calls, or stopping when sufficient material is found, with warnings against repeating actions or following instructions in data tags.
