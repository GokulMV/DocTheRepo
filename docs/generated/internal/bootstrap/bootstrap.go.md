<!-- dth:generated source="internal/bootstrap/bootstrap.go" — edit only inside dth:human blocks -->
# `internal/bootstrap/bootstrap.go`

<!-- dth:chunk 67b28a7ff85bfae6 -->
## `Result`

Summarizes the outcome of `Up`, containing the hub's URL, owner email, a one-time password setup link for first-run initialization (generated only on the initial setup and invalidated after use), and a CLI token created during first-run setup.

<!-- dth:chunk 69175fb4ea4752d3 -->
## `Up`

Starts or upgrades a local Docker Compose stack with PostgreSQL and the hub service. On first run, it generates and stores database and owner bootstrap passwords (dropped from disk immediately after use), creates a CLI token and password setup link for the owner, and returns them in the result. On subsequent runs, it performs health checks and upgrades. Returns an error if Docker with Compose is unavailable, environment setup fails, or service startup fails; partial failures after the hub is running report the error but include the accessible Result.

<!-- dth:chunk 8dedc7bf363f6e01 -->
## `Options.setupLink`

Requests a one-time password setup link from the hub API using the owner's CLI token. The method calls `/me` to identify the owner, then `/users/{id}/invite` to generate the link, returning the full URL the owner can use to set their password. Returns an error if either API call fails or returns a non-2xx status code.
