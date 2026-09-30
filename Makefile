# DocTheRepo Hub — developer entry points. Integration tests need Docker (testcontainers).
GO        ?= go
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -s -w -X main.version=$(VERSION)
SQLC      ?= sqlc

.PHONY: all build test test-unit test-integration cover lint vet fmt generate vuln clean web web-test release image cli-release deploy-lint e2e stack perf-push perf-qa

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

e2e: release ## Playwright E2E against the real hub + mocks (needs Docker and Chromium)
	cd test/e2e && npm ci --no-audit --no-fund && CI=1 npx playwright test

stack: ## Run the E2E stack (hub + Postgres + GitHub/OIDC mocks + stub LLM) for manual testing or k6
	$(GO) run ./test/e2e/stack -hub ./bin/dth-hub

K6 ?= k6
perf-push: build ## Push burst: 40 commits / 5 repos / 90 s docgen must drain < 30 min at concurrency 4 (needs k6)
	$(GO) run ./test/e2e/stack -hub ./bin/dth-hub -auth local -synthetic-repos 5 -docgen-delay 90s -work-dir .perf/push -state-file .perf/push.json & \
	  until [ -s .perf/push.json ]; do sleep 1; done; $(K6) run test/perf/push-burst.js; s=$$?; kill %1; rm -f .perf/push.json; exit $$s

perf-qa: build ## Q&A: 50 users, 1 ask/10 s, 250k chunks, retrieval p95 < 400 ms (needs k6)
	$(GO) run ./test/e2e/stack -hub ./bin/dth-hub -listen 127.0.0.1:18190 -control 127.0.0.1:18199 -llm 127.0.0.1:18198 \
	  -metrics 127.0.0.1:18191 -synthetic-repos 20 -hub-env DTH_ALL_USERS_READ_ALL_REPOS=true -work-dir .perf/qa -state-file .perf/qa.json & \
	  until [ -s .perf/qa.json ]; do sleep 1; done; $(GO) run ./test/perf/seed -control http://127.0.0.1:18199 -chunks 250000 && \
	  DTH_CONTROL=http://127.0.0.1:18199 $(K6) run test/perf/qa.js; s=$$?; kill %1; rm -f .perf/qa.json; exit $$s

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
	rm -rf bin dist coverage.out .perf
