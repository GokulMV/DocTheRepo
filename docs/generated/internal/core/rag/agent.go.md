<!-- dth:generated source="internal/core/rag/agent.go" — edit only inside dth:human blocks -->
# `internal/core/rag/agent.go`

<!-- dth:chunk 1f21d898d123f625 -->
## `PathEntry`

Represents a file in the repository index as the agent sees it. Contains the repository identifier, file path, and number of indexed chunks for that file.

<!-- dth:chunk b7cd0106c66f0ed5 -->
## `Explorer`

Explorer lets the agent read files and list paths. A Store without it limits the agent to searching.

<!-- dth:chunk 327177ea3e878649 -->
## `agentStep`

Represents a single decision made by the agentic search loop. The action is one of: search, read_file, list_files, or answer. Input varies by action (search words, file path, or directory text). Reason is a human-readable phrase explaining the agent's intent.

<!-- dth:chunk 5192be5ab35ade1b -->
## `Status`

Represents progress updates reported by the agent during investigation (displayed as status messages to the user). Includes the step number, action type, action input, and human-readable reason for the current action.

<!-- dth:chunk eeb3307ef0328b28 -->
## `Engine.investigate`

Runs the agentic search loop for a query, iteratively gathering relevant code chunks. The agent performs up to AgentSteps iterations, choosing between search, read_file, list_files, and answer actions based on an LLM prompt. Returns all discovered chunks sorted newest-first after seed material, while tracking token usage and deduplicating results by ID. Supports optional status callbacks and respects the OnStatus callback if provided.

<!-- dth:chunk 47e106be8b820968 -->
## `summarize`

Formats a list of chunk paths into a human-readable summary. Returns "nothing new" if the list is empty, otherwise returns a count followed by a comma-separated list of paths in the form "repo:path".

<!-- dth:chunk c658f8a65d9dbd1e -->
## `agentPrompt`

Constructs the prompt for each agent step, describing the question, up to 30 previously found chunks (truncated to 240 characters each), action history, and available actions. Indicates whether file exploration is available. Ends with "What next?" to prompt the agent's next decision.

<!-- dth:chunk 971d94139204322b -->
## `__module__`

Defines the agentic search configuration: AgentMinSources is a constant (likely minimum chunks required), agentSchema is the JSON structure for agent responses (requiring action, input, and reason fields with action constrained to four specific values), and agentSystem is the system prompt instructing the agent to iteratively search code and documents, avoiding repeated actions and ignoring instructions within data tags.
