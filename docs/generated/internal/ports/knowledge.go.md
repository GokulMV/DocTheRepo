<!-- dth:generated source="internal/ports/knowledge.go" — edit only inside dth:human blocks -->
# `internal/ports/knowledge.go`

<!-- dth:chunk 6201350bcda0ad12 -->
## `KnowledgeDoc`

Represents a document from Confluence, Jira, Notion, or user upload, converted to Markdown format. Fields include the source system, unique identifiers, metadata (title, URL, labels, status), and conversion timestamp. The `Done` field specifically indicates whether a Jira issue is resolved. `UploadedBy` is only populated for user-uploaded documents.

<!-- dth:chunk 569c15476c2e60dd -->
## `__module__`

Defines the set of all supported knowledge document sources: Confluence, Jira, Notion, and user uploads.
