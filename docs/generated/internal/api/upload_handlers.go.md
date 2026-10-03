<!-- dth:generated source="internal/api/upload_handlers.go" — edit only inside dth:human blocks -->
# `internal/api/upload_handlers.go`

<!-- dth:chunk d118cb0fb9f26273 -->
## `UploadDeps`

Struct holding dependencies for upload route handlers. `Auth` is used for audit logging; `Upload` is the business logic for ingesting files; `Store` provides read/write access to persisted uploads with methods to list all uploads, fetch one by ID, and delete by ID.

<!-- dth:chunk 6470174c48103ae0 -->
## `UploadRoutes`

Mounts HTTP routes for `/library/uploads` endpoint. Viewers can list uploads (GET `/library/uploads`) and fetch individual uploads by ID (GET `/library/uploads/{id}`). Editors can create uploads via multipart file submission (POST `/library/uploads`, max 25 MB total, max 20 files, in "files" field) and delete uploads by ID (DELETE `/library/uploads/{id}`). File uploads are parsed, passed to the Upload dependency, and audit-logged with the uploader's identity and file names. Responses are JSON; validation errors return detailed messages.

<!-- dth:chunk 43324cc223ce5236 -->
## `__module__`

Module-level constants defining upload limits: maximum 20 files per request and maximum 25 MB total size (25 << 20 bytes).
