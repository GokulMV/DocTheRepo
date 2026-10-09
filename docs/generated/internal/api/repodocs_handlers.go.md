<!-- dth:generated source="internal/api/repodocs_handlers.go" — edit only inside dth:human blocks -->
# `internal/api/repodocs_handlers.go`

HTTP request handlers and routes for repository documentation operations, including retrieval, reporting, and administrative management.

<!-- dth:chunk a28323e6e9854d38 -->
## `RepoDocsDeps`

RepoDocsDeps provides dependencies for repository documentation handlers. It includes storage and authentication services, queue access for asynchronous jobs, and callbacks for budget management, cost estimation, and pull-request export of generated documentation.

<!-- dth:chunk aa40e74c845d6f3b -->
## `systemLinksFor`

systemLinksFor filters a list of system links to include only those where the caller can read both endpoints. It takes a permission check function and returns links with accessible source and target repositories.

<!-- dth:chunk 51a90f02c35a0150 -->
## `repoDocsHandlers`

repoDocsHandlers wraps RepoDocsDeps to implement HTTP handlers for documentation endpoints.

<!-- dth:chunk 15a26b26303cee89 -->
## `RepoDocsRoutes`

Mounts repository documentation routes organized by permission level. The viewer-level routes expose public docs queries (`/repo-docs`, `/repo-docs/find`, `/repo-docs/{id}`, `/system`). Editor-level routes enable cost estimation and report generation for a specific repository. Admin-level routes permit writing docs, managing budgets, exporting data, and system-wide updates. Returns a chi router group configurator that applies role-based middleware to each route cluster.

<!-- dth:chunk fb4d6742833ae735 -->
## `repoDocsHandlers.readable`

readable checks if the caller has permission to access a repository by verifying authentication and scope. It returns false and writes an error response if access is denied or the repository ID is empty, treating missing authorization as a not-found error.

<!-- dth:chunk b20c809d59811800 -->
## `docSummary`

docSummary is a JSON-serializable overview of a document without its section details, including metadata like title, confidence level, status, and generation timestamp.

<!-- dth:chunk c1d11a8397e76ae9 -->
## `repoDocsHandlers.list`

list responds with a repository's generated documents and their summaries. It optionally includes budget information and the status of any queued generation job if those services are available.

<!-- dth:chunk 15e486dea417c671 -->
## `repoDocsHandlers.send`

send sends a document and its sections as JSON to the client. It includes repository name, confidence labels for each section, and related metadata. It checks read permission before responding.

<!-- dth:chunk b2504aca4bcfbb64 -->
## `repoDocsHandlers.get`

get retrieves a document by ID from storage and sends it to the client via send.

<!-- dth:chunk a2a7578255ee25ec -->
## `repoDocsHandlers.find`

find retrieves a document by repository ID and document key or type, supporting optional type/key query parameters. It returns the first match or 404 if not found.

<!-- dth:chunk b5b055fe10400b99 -->
## `repoDocsHandlers.estimate`

estimate performs a dry-run cost calculation for generating missing documentation, responding with token count, USD estimate, and lists of documents that would be written or remain unchanged. The full parameter controls whether all docs are estimated or just missing ones.

<!-- dth:chunk 329039c38bd7b064 -->
## `repoDocsHandlers.report`

HTTP handler that generates a summary report of repository documents for review and tuning. It retrieves the repository and associated documents, then builds a report via `BuildReport`; if a budget function is configured, it includes cap and spent USD values. Query parameters control output: `?text=true` preserves prose in the report, and `?format=md` renders the response as Markdown (with appropriate Content-Type header) instead of the default JSON. Requires editor-level access to the repository.

<!-- dth:chunk 8933160f22c3eb52 -->
## `repoDocsHandlers.write`

write enqueues an asynchronous job to generate repository documentation, accepting optional parameters for full generation and specific document keys. It validates the repository exists, logs an audit event, and returns the job ID.

<!-- dth:chunk beee9eac99d2bc09 -->
## `repoDocsHandlers.setBudget`

setBudget updates a repository's monthly documentation generation cost cap, validating that the cap is non-negative and logging an audit event.

<!-- dth:chunk ba752eb513591900 -->
## `repoDocsHandlers.export`

export triggers generation of a pull request containing the repository's documentation as Markdown files, returning the pull request number and URL, and logging an audit event.

<!-- dth:chunk 3d0fae285158af47 -->
## `ExportMarkdown`

ExportMarkdown renders documentation as Markdown files in a specified directory, organizing modules in a subdirectory and creating a README index. It links [path:line] citations in the content to code via a provided URL function and includes confidence labels and source commit info in each file.

<!-- dth:chunk 34d87768fa6d7eb3 -->
## `linkCitations`

linkCitations replaces [path:line] citation patterns in Markdown with hyperlinked versions using a provided URL function. If no URL function is given, the Markdown is returned unchanged.

<!-- dth:chunk 1102523357c8bd0b -->
## `firstSentence`

firstSentence extracts the first sentence from a string by finding the first period followed by a space, or returns the entire string if no sentence boundary exists.

<!-- dth:chunk 5fbfacbcefcc0496 -->
## `LinkCitationsRelative`

LinkCitationsRelative replaces [path:line] citations with relative links pointing to code files, using the provided up string to navigate from the document directory to the repository root, compatible with standard git hosting platforms.

<!-- dth:chunk ea1d7810845c3314 -->
## `repoDocsHandlers.system`

system returns the system architecture view showing links between readable repositories and their diagram. It includes the written system documentation only if the caller can read all linked repositories, and the status of any queued system generation job if available.

<!-- dth:chunk c888766f033e8dda -->
## `repoDocsHandlers.writeSystem`

writeSystem enqueues an asynchronous job to generate system-wide documentation covering relationships between repositories, logging an audit event and returning the job ID.

<!-- dth:chunk e1ba5d25c7f42bab -->
## `__module__`

citeLinkRE matches citation patterns in Markdown of the form [path:line] or [path:line-line], where path contains file path characters and line is a line number.
