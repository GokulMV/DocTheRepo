<!-- dth:generated source="cmd/dth/client.go" — edit only inside dth:human blocks -->
# `cmd/dth/client.go`

<!-- dth:chunk 95f0a465f9d9ad33 -->
## `cliConfig`

Configuration stored in ~/.dth/cli.json containing default server and token credentials, named hub profiles, and which profile is currently active.

<!-- dth:chunk 2d0284f82a354aa9 -->
## `cliProfile`

cliProfile is one named hub.

<!-- dth:chunk 02e21fe05ca34801 -->
## `cliConfig.resolve`

Resolves a profile by name, falling back to the current profile or the unnamed default. Returns the unnamed default (built from `c.Server` and `c.Token`) if name is empty and no current profile is set. Returns an error if a named profile is requested but not found.

<!-- dth:chunk 43d3bbdfc3813f08 -->
## `cliConfig.withProfile`

Stores server and token credentials under a profile name, or updates the unnamed default if name is empty. Creates the profiles map if needed. Automatically sets the named profile as current if no current profile was previously set.
