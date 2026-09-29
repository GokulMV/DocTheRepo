# DocTheRepo Hub — developer entry points. Integration tests need Docker (testcontainers).
GO        ?= go
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -s -w -X main.version=$(VERSION)
SQLC      ?= sqlc

.PHONY: all build test test-unit test-integration cover lint vet fmt generate vuln clean web web-test release

all: lint test build

web: ## Build the React UI and stage it for embedding into the hub binary
	cd web && npm ci --no-audit --no-fund && npm run build
	find internal/webui/dist -mindepth 1 ! -name .gitkeep -delete
	cp -R web/dist/. internal/webui/dist/

web-test: ## Typecheck and unit-test the UI
	cd web && npm ci --no-audit --no-fund && npm run lint && npm test

release: web build ## Hub binary with the UI embedded

build: ## Build the hub and CLI binaries into ./bin
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/dth-hub ./cmd/hub

test: ## Run every test; integration tests fail (not skip) when Docker is missing
	DTH_REQUIRE_DOCKER=1 $(GO) test -race -count=1 ./...

test-unit: ## Run tests that need no Docker (integration tests skip)
	$(GO) test -count=1 ./...

cover: ## Coverage report for the whole module
	DTH_REQUIRE_DOCKER=1 $(GO) test -count=1 -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1

vet:
	$(GO) vet ./...

fmt:
	gofmt -w $$(git ls-files '*.go')

lint: vet ## gofmt check + vet
	@test -z "$$(gofmt -l $$(git ls-files '*.go'))" || (echo "gofmt needed:"; gofmt -l $$(git ls-files '*.go'); exit 1)

generate: ## Regenerate sqlc code from migrations + queries
	$(SQLC) generate

vuln: ## Known-vulnerability scan
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...

clean:
	rm -rf bin coverage.out
