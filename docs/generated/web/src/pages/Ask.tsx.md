<!-- dth:generated source="web/src/pages/Ask.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Ask.tsx`

The Ask.tsx page implements an AI-powered question-answering interface with message threads, streaming responses, citations, and user feedback collection.

<!-- dth:chunk 2a4f3675eaf9dc78 -->
## `Citations`

Renders an ordered list of citations from the AI response. Each citation displays a badge indicating its type (code, tool/live, or other), a link or plain text with the file path or title, and the repository name if available. Returns nothing if the items list is empty. Truncates long citation text to prevent layout overflow.

<!-- dth:chunk e7bf6e4dcabdcb2b -->
## `NotFoundTips`

Renders a card with tips shown when no source answers a user's question. Displays styled guidance suggesting users name specific entities (files, functions, endpoints), check for repository documentation, or switch to a semantic search model for better results.

<!-- dth:chunk a48b030f94a400f2 -->
## `Feedback`

Provides feedback UI for a message with thumbs up/down buttons that send ratings to the API. Displays confidence level (high/medium/low) with explanations, and status badges for cached results, further investigation, SIFT validation, and token cost. The feedback state updates optimistically but reverts if the API call fails. Shows "Thanks for the feedback" once a rating is submitted.
