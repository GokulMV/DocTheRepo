<!-- dth:generated source="internal/ingest/uploads.go" — edit only inside dth:human blocks -->
# `internal/ingest/uploads.go`

<!-- dth:chunk 99e13264af29e08b -->
## `UploadStore`

UploadStore holds uploaded documents (store.Knowledge).

<!-- dth:chunk aadf34e7fcd13207 -->
## `UploadFile`

UploadFile is one file as uploaded.

<!-- dth:chunk 6ceb853568da84f5 -->
## `UploadedDoc`

A record of a successfully stored file from an upload, containing the generated ID, original filename, extracted title, and URL path for accessing it in the library.

<!-- dth:chunk 74767a2e2c47be65 -->
## `UploadResult`

The response from an upload operation, reporting accepted documents with their metadata, files that were refused and why, the count of embedded chunks, and optional notes (e.g., about spend guards or missing embedding models).

<!-- dth:chunk e4da6da6d5d40387 -->
## `UploadID`

UploadID is an uploaded document's stable ID: uploading the same file name to the same collection replaces the document.

<!-- dth:chunk 93434cd589e576b6 -->
## `KnowledgeSync.Upload`

Ingests files by converting them to Markdown, storing them as team documents organized into collections, chunking and embedding them for search, and placing them on library shelves. Returns details of accepted/refused files and embedding counts. Refuses files that fail Markdown conversion or contain no text, validates collection name length (max 80 chars), and records embedding attempts even if blocked by spend guard or unavailable embedding model; conversions are deduplicated per collection and filename via `UploadID`. Returns validation or connector errors immediately; embedding errors surface as notes in the result rather than stopping the operation.

<!-- dth:chunk 1d0d9aef6697e193 -->
## `__module__`

Default collection name used when none is specified in an upload request.
