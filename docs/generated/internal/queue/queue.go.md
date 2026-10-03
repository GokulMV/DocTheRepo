<!-- dth:generated source="internal/queue/queue.go" — edit only inside dth:human blocks -->
# `internal/queue/queue.go`

<!-- dth:chunk a74f05d7e589d4f2 -->
## `toJob`

Converts a database job record (`gen.Job`) to a ports domain job (`ports.Job`) by mapping all fields, dereferencing nullable string pointers using the `deref` helper, and casting job type and status enums. Called by `Enqueue`, `Claim`, `Get`, and `List` to transform database models into the public API representation.

<!-- dth:chunk 410d4e087b7de9d7 -->
## `Queue.SetProgress`

Records progress for a running job by marshaling the provided `JobProgress` to JSON and storing it in the database. Returns an error if JSON marshaling fails or if the database operation fails. Once a job has finished, this method is a no-op (the database layer handles ignoring updates for completed jobs).
