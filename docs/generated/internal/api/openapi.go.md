<!-- dth:generated source="internal/api/openapi.go" — edit only inside dth:human blocks -->
# `internal/api/openapi.go`

Defines the OpenAPI specification and catalog of REST API operations exposed by the Hub.

<!-- dth:chunk 8d7cd432e67ea5ec -->
## `__module__`

Declares the API operations catalog as a slice of `op` structs, each containing HTTP method, route, category, description, required role, and flags indicating if the operation accepts request bodies and streams responses. Also declares a cached OpenAPI specification (`spec`) and synchronization primitive (`specOnce`) for thread-safe lazy initialization of the full specification document. This catalog serves as the source of truth for all REST endpoints exposed by the Hub, from webhook ingestion and authentication to documentation generation, security scanning, and administrative functions.
