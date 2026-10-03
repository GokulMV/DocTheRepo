<!-- dth:generated source="web/src/pages/Uploads.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Uploads.tsx`

Provides UI components for uploading documents to a shared team library and viewing them grouped by collection.

<!-- dth:chunk 7ef58577a35a2eb0 -->
## `UploadDoc`

Data structure representing an uploaded document in the library. Includes metadata (id, collection, title, uploaded_by) and content (summary, body) along with an updated_at timestamp. The body field is optional and may not be populated in all contexts.

<!-- dth:chunk 9c984fd85caa466b -->
## `UploadResult`

Response type for document upload requests. Contains arrays of successfully uploaded documents and optionally refused documents (with error reasons), plus an embedded count and optional notes. Used internally to communicate upload operation results.

<!-- dth:chunk 73b1547f1db787b0 -->
## `UploadDocs`

Modal dialog component for uploading Markdown, text, or HTML files to a named collection. Manages file selection, collection assignment, and submission via FormData. On successful upload, displays results (accepted documents, refused files with errors, and notes) and invalidates uploads and shelves queries. Supports multiple file selection and collection autocomplete.

<!-- dth:chunk f106c8e07f4f9acd -->
## `UploadedList`

Displays uploaded documents grouped by collection in a card, fetched via the uploads query. Each document shows title (linked to detail view), relative timestamp, uploader name if available, and a summary. Returns null if no documents exist; shows spinner while loading.

<!-- dth:chunk 066a188fc6382554 -->
## `UploadView`

Page component showing a single uploaded document's full content rendered as Markdown. Displays metadata (collection, upload time, uploader). Editors and above can delete the document via a confirmation dialog. Includes a back link to the library and handles loading and error states.

<!-- dth:chunk 06764ce38d08ede8 -->
## `__module__`

Module-level constants: ACCEPT defines the file type whitelist for uploads (.md, .markdown, .mdx, .txt, .text, .rst, .html, .htm), and uploadsKey is the React Query key for uploads queries.
