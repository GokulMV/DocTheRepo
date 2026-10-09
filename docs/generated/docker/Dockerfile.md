<!-- dth:generated source="docker/Dockerfile" — edit only inside dth:human blocks -->
# `docker/Dockerfile`

Multistage Dockerfile that builds and packages the DocTheRepo Hub application for containerized deployment across multiple platforms.

<!-- dth:chunk 331cb0443ce9dcd3 -->
## `docker/Dockerfile`

Multi-stage Docker image for DocTheRepo Hub, supporting Compose, ECS Fargate, Cloud Run, and Helm deployment. Stage 1 builds React UI static assets from `web/` using Node 22. Stage 2 compiles two Go binaries (`dth-hub` and `dth`) with CGO enabled for Tree-sitter C grammar support, embedding the web assets and initializing a `data/grammars` directory. Stage 3 uses `distroless/cc-debian12:nonroot` runtime with glibc and libstdc++ for size and security, running as uid 65532 with `/data` mounted for secrets and optional runtime grammars. Exposes ports 8080 (main) and 9090 (metrics) with a 10-second health check interval.
