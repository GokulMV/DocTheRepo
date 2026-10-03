<!-- dth:generated source="web/src/api/types.ts" — edit only inside dth:human blocks -->
# `web/src/api/types.ts`

<!-- dth:chunk f37046a4d158a6bf -->
## `User`

Represents a user account with authentication status, role, account state (disabled/invite pending), and login history. The sso and has_password flags indicate available authentication methods, while created_at and last_login_at track account lifecycle.

<!-- dth:chunk 6449d2c01f27f2bb -->
## `AuthConfig`

Configuration returned by the sign-in endpoint (GET /auth/config) that defines the authentication mode and capabilities available. Specifies whether the deployment uses local password authentication or OIDC SSO, whether email is configured for invite/password links, and optionally includes the deployment environment name.

<!-- dth:chunk ff444bb82fa5910b -->
## `SiftSummary`

Metrics describing how Ask's source picker filtered candidate documents before the answering model processed them. Tracks candidates evaluated, kept sources, token savings to the answering model, judge model tokens consumed, costs (both saved and incurred), and optionally the judge model used and files explored during indexing. The saved_usd value is negative when filtering cost more than it saved.

<!-- dth:chunk 21fe2527b88b4ebb -->
## `AskResponse`

Response from Ask containing a complete answer to a user query, including the thread and message identifiers, the answer text, citations, token usage and cost metrics, cache status, and optionally metrics from source picking (sift) that filtered the documents used.

<!-- dth:chunk 5d62db1911f404f0 -->
## `Message`

A single message in an Ask conversation thread with role (user or assistant), content, citations, model used, token usage, and optional metadata including whether the search required investigation (expanded scope), how sources were filtered (sift), and user feedback. The cached flag indicates if results were cached.

<!-- dth:chunk a79570ce51d86d07 -->
## `DocNode`

A documented code node representing a symbol or entity within a repository. Contains the node's identity, documentation (summary and markdown), source location (path, repo, commit), and related chunks with their language and signature. The ancestors field traces the hierarchy from repository root down to parent.

<!-- dth:chunk 439d188beca85c3f -->
## `ShelfEntry`

An entry in a user's document shelf, supporting multiple document types including code and external team sources (Confluence, Jira, Notion, uploaded). Can be pinned, includes an optional user note, and optionally references a repository.

<!-- dth:chunk 4b3279238ef53233 -->
## `Job`

A background job record tracking its execution state, including type, target repository, status, retry information, error messages, result payload, and optional progress for running jobs. The correlation_id links related jobs, while replayed_from indicates job replay history.

<!-- dth:chunk 0385023d0a077254 -->
## `JobProgress`

Progress state for a running job, indicating the current stage, number of items completed, total items, and optionally the current item being processed.

<!-- dth:chunk e827a4dfc7486bf8 -->
## `UsagePoint`

A data point in API usage metrics tracked at timestamp t, recording the number of calls, tokens consumed, cost in USD, and optional cache performance (cached calls and cache read tokens). The blocked field indicates requests that were rate-limited or rejected.
