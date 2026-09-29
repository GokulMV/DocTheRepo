# DocTheRepo Hub — developer entry points. Integration tests need Docker (testcontainers).
GO        ?= go
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -s -w -X main.version=$(VERSION)
SQLC      ?= sqlc

.PHONY: all build test test-unit test-integration cover lint vet fmt generate vuln clean web web-test release image cli-release deploy-lint

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
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/dth ./cmd/dth

IMAGE ?= ghcr.io/gokulmv/doctherepo-hub:$(VERSION)
image: ## Container image (UI + hub + CLI on distroless)
	docker build -f docker/Dockerfile --build-arg VERSION=$(VERSION) -t $(IMAGE) .

CLI_PLATFORMS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64
cli-release: ## Static dth CLI binaries for every platform into ./dist
	@for p in $(CLI_PLATFORMS); do os=$${p%/*}; arch=$${p#*/}; ext=; [ $$os = windows ] && ext=.exe; \
	  echo "dth $$os/$$arch"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/dth_$${os}_$${arch}$$ext ./cmd/dth || exit 1; \
	done

deploy-lint: ## terraform fmt/validate and helm lint for the deploy/ tree (needs terraform + helm)
	terraform fmt -recursive -check deploy/terraform
	for d in deploy/terraform/aws deploy/terraform/gcp deploy/terraform/modules/readonly-roles/aws deploy/terraform/modules/readonly-roles/gcp; do \
	  terraform -chdir=$$d init -backend=false -input=false >/dev/null && terraform -chdir=$$d validate || exit 1; \
	done
	helm lint deploy/helm/dth --strict --set database.existingSecret=db --set secrets.provider=awskms \
	  --set secrets.kmsKeyId=k --set auth.oidc.issuer=https://idp --set auth.oidc.clientId=c \
	  --set auth.oidc.existingSecret=s --set 'auth.oidc.allowedDomains={example.com}'

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
	rm -rf bin dist coverage.out
