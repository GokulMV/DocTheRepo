# Development


Requirements: Go (version in `go.mod`), Docker (integration tests start PostgreSQL + pgvector with
testcontainers), and `sqlc` if you change SQL.

```sh
make test        # all tests; integration tests require Docker
make test-unit   # tests that need no Docker (integration tests are skipped)
make build       # ./bin/dth-hub (API only unless the UI was staged) and ./bin/dth
make image       # container image with the UI embedded (docker/Dockerfile)
make deploy-lint # terraform fmt/validate + helm lint
make e2e         # Playwright E2E (real hub + Postgres + GitHub/OIDC mocks + stub model)
make stack       # that stack on its own, for manual testing (URLs printed as JSON)
make perf-push   # k6 push burst; make perf-qa for the 250k-chunk Q&A run (needs k6)
make web         # build the React UI and stage it for embedding (then `make build`)
make web-test    # UI typecheck + unit tests
```

Run the hub against a local database:

```sh
docker run -d --name dth-pg -e POSTGRES_PASSWORD=pw -e POSTGRES_DB=dth -p 5432:5432 pgvector/pgvector:pg16
export DTH_DATABASE_URL='postgres://postgres:pw@localhost:5432/dth?sslmode=disable'
./bin/dth-hub            # all roles; no config file needed
curl localhost:8080/readyz
```
