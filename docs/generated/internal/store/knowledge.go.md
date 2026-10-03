<!-- dth:generated source="internal/store/knowledge.go" — edit only inside dth:human blocks -->
# `internal/store/knowledge.go`

<!-- dth:chunk e223cca4f3d8c567 -->
## `entityKind`

Maps a chunk source to the corresponding Palace entity kind: Jira issues, Notion pages, and uploads become Document kind; Confluence defaults to ConfluencePage.

<!-- dth:chunk d7e96e17615990c7 -->
## `shelfType`

Maps a chunk source to the corresponding shelf item type for Library storage: Jira and uploads use knowledge_doc type, Notion also maps to knowledge_doc, and Confluence defaults to ConfluencePage.

<!-- dth:chunk 74a51b713e0d1f32 -->
## `Knowledge.Apply`

Writes documents from a connector by upserting chunks (unchanged content skipped), knowledge_docs rows, Palace entities with mention-based edges (documented_in links plus runbook_for for runbooks), and Library placements. Each document is processed in its own transaction. Returns counts of added/changed/removed/unchanged documents and links created. Uploads store their Markdown body; synced sources only link externally.

<!-- dth:chunk e36f1cfc16e14dc3 -->
## `Knowledge.RemoveMissing`

Removes documents from a connector's space that no longer exist upstream (deleted or moved). Ignores empty live lists to prevent data loss from upstream glitches. Each removed document is soft-deleted, its links retired, and its entity retired.

<!-- dth:chunk 5b9794e7679cc045 -->
## `Knowledge.removeDoc`

Permanently removes an uploaded document by soft-deleting its chunks, retiring associated entity and graph edges, removing Library placements, and deleting the database row. All operations execute within a single transaction to maintain consistency.

<!-- dth:chunk 9198d497585e6495 -->
## `Knowledge.UploadConnector`

Returns the internal connector ID for uploaded documents, creating one named "Uploaded documents" if none exists. Uses an upsert with conflict handling to ensure only one such connector is created across concurrent requests.

<!-- dth:chunk 65890a4b795fe483 -->
## `Upload`

Represents an uploaded document as returned by the Library. The ID is stable per collection and file name. Body is populated only when explicitly retrieved via GetUpload.

<!-- dth:chunk 07b6cae0bbccf584 -->
## `Knowledge.Uploads`

Lists uploaded documents ordered by most recent first, up to 1000 items. Joins with the users table to include uploader email addresses, and does not populate the Body field.

<!-- dth:chunk c54c77310f5efd53 -->
## `Knowledge.GetUpload`

Retrieves a single uploaded document by external ID with its full Markdown body. Returns ports.ErrNotFound if the document does not exist.

<!-- dth:chunk f5635b210b604048 -->
## `Knowledge.DeleteUpload`

Removes an uploaded document from search and the Library by looking up its database ID and path, then delegating to removeDoc. Returns ports.ErrNotFound if the document does not exist.

<!-- dth:chunk c62126c32fd23df7 -->
## `__module__`

Defines the type alias for knowledge application results and the constant name for the internal upload connector.
