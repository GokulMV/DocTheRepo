<!-- dth:generated source="docker/Dockerfile" — edit only inside dth:human blocks -->
# `docker/Dockerfile`

Dockerfile defining a multistage build for a containerized DocTheRepo Hub service with embedded UI, compiled Go binaries, and a minimal distroless runtime.

<!-- dth:chunk 331cb0443ce9dcd3 -->
## `docker/Dockerfile`

Multistage Dockerfile that produces a minimal, secure container image for the DocTheRepo Hub service compatible with Compose, ECS Fargate, Cloud Run, and Helm.

The build has three stages: (1) Node.js stage builds React UI assets, (2) Go stage compiles two binaries (`dth-hub` and `dth` tools) with CGO enabled for Tree-sitter grammar support and embeds the UI assets into the hub binary, (3) distroless runtime stage with glibc runs as non-root user 65532, exposing ports 8080 (main) and 9090 (metrics), with `/data` volume for persistent secrets and optional runtime grammars. A health check runs every 10s. The `VERSION` build argument defaults to "dev". The image uses BuildKit mount caches for faster rebuilds.
