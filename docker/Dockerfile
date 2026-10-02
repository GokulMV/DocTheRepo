# syntax=docker/dockerfile:1.7
# DocTheRepo Hub image: one image for Compose, ECS Fargate, Cloud Run, and Helm. Build from the repo root:
#   docker build -f docker/Dockerfile -t ghcr.io/gokulmv/doctherepo-hub:dev .

# 1. React UI → static assets embedded into the hub binary.
FROM node:22-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# 2. Go binaries. CGO stays on: the built-in Tree-sitter grammars are C, and runtime grammars are dlopen'd.
FROM golang:1.25.13-bookworm AS build
WORKDIR /src
ENV GOTOOLCHAIN=local CGO_ENABLED=1
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/web/dist/ internal/webui/dist/
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/dth-hub ./cmd/hub && \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/dth ./cmd/dth && \
    mkdir -p /out/data/grammars

# 3. Runtime: distroless with glibc + libstdc++ (no shell, no package manager), non-root.
FROM gcr.io/distroless/cc-debian12:nonroot
COPY --from=build /out/dth-hub /out/dth /
# /data holds the local master key (localfile secrets) and optional runtime grammars; owned by nonroot so a
# fresh named volume inherits a writable directory.
COPY --from=build --chown=65532:65532 /out/data /data
ENV DTH_LISTEN=0.0.0.0:8080 \
    DTH_METRICS_LISTEN=0.0.0.0:9090 \
    DTH_LOCAL_KEY_FILE=/data/master.key \
    DTH_GRAMMARS_DIR=/data/grammars \
    DTH_LOG_FORMAT=json
USER 65532:65532
EXPOSE 8080 9090
HEALTHCHECK --interval=10s --timeout=3s --start-period=20s --retries=5 CMD ["/dth-hub", "healthcheck"]
ENTRYPOINT ["/dth-hub"]
