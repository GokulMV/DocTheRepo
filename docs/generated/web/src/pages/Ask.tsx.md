<!-- dth:generated source="web/src/pages/Ask.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Ask.tsx`

<!-- dth:chunk e7bf6e4dcabdcb2b -->
## `NotFoundTips`

Displays tips to help users improve their question when Ask returns no answer. Suggests being specific about what they're asking for (file, function, endpoint, etc.), checking that repository docs are available, and enabling embeddings routing for semantic search.

<!-- dth:chunk c0f4169321151078 -->
## `siftLabel`

Formats a concise, human-readable summary of source selection results. Returns an empty string if there's nothing to report; otherwise concatenates up to three metrics (sources read, files explored, tokens saved with optional USD cost) separated by bullets and capitalizes the result.

<!-- dth:chunk 5343232dbae66be4 -->
## `siftDetail`

Generates a detailed multi-line explanation of source selection behavior for a tooltip. Describes how many sources the judge evaluated and kept, tokens saved, files explored, judge model and token cost with calibration status, and net cost or savings.

<!-- dth:chunk cbd2cc37f5ee62f3 -->
## `SiftTotal`

Displays total tokens and USD saved by source selection across all messages in a conversation. Returns null if no tokens were saved. Appears as a tooltip-enabled summary line describing cost savings from using a cheaper judge model.

<!-- dth:chunk a48b030f94a400f2 -->
## `Feedback`

Renders feedback buttons (thumbs up/down) for a message and displays associated badges. Tracks user feedback state locally with optimistic UI updates and sends the choice to the API. Also displays cached status, investigation indicator, source-sift summary, and usage cost.

<!-- dth:chunk 0358ecd8e937e2c2 -->
## `AgentStatus`

Describes one step taken by the agent when looking further—search, file read, or file listing. Includes the step number, action type, input query/filename, and reason explaining why this step was taken.

<!-- dth:chunk 30502cbafc1bd06f -->
## `AgentSteps`

Shows the agent's investigation steps when the first search returned insufficient results. Renders an ordered list of steps with actions (searched for, read, listed files) and their reasons, with a status message indicating whether the search is still in progress.

<!-- dth:chunk 7f3fa9c5a67b383b -->
## `Ask`

Main component for the Ask page. Manages question input, streaming responses, agent investigation steps, and conversation threading. Handles form submission to fetch answers via Server-Sent Events, displays pending/completed messages with citations and feedback, and provides example questions and scope selection. Scrolls to latest message on updates and gracefully handles request abortion.

<!-- dth:chunk d32562df7b326a01 -->
## `__module__`

Defines module-level constants: INCLUDES (searchable content types), NOT_FOUND (fallback message when no sources answer), STEP_VERB (maps agent actions to display verbs), and EXAMPLES (four example questions with icons for the empty state).
