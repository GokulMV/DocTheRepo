<!-- dth:generated source="internal/store/decodes.go" — edit only inside dth:human blocks -->
# `internal/store/decodes.go`

Decodes.go implements a decode store adapter backed by a database connection, handling persistence and retrieval of decoded documentation chunks.

<!-- dth:chunk 00e6c3c0a6d138e4 -->
## `Decodes.Chunks`

Implements `decode.Store` to retrieve chunks by ID, filtering out any chunks that require multiple repositories. This implements a security/visibility boundary: explanation chunks are only shown to users who can view the associated issue, so pieces sourced from multiple repositories (indicating system-level architecture) are excluded from results. Returns `nil, nil` for empty input IDs and propagates retrieval errors from the underlying chunks store.
