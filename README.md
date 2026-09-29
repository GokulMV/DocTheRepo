# DocTheRepo Hub

A self-hosted engineering intelligence hub: it connects your repositories, cloud consoles (AWS, GCP),
monitoring and alerting tools, event platforms, security tools, and knowledge bases; keeps generated
documentation and a searchable index in sync with every push; answers questions with citations; and decodes
errors and alerts into one inbox, while a known-issues registry keeps LLM spend off problems you already
understand. You bring your own LLM provider and keys.

The full design is in [`docs/plan-doctherepo-hub.md`](docs/plan-doctherepo-hub.md).

## Status

Under construction, milestone by milestone (plan § 14). Implemented so far:

- **Phase 1 — Foundation**: configuration with complete defaults, PostgreSQL schema and migrations,
  Postgres job queue (exactly-once claims, per-repo ordering, retries, dead letter, lease recovery),
  worker pool, leader-elected scheduler, envelope encryption for secrets, health/readiness endpoints,
  structured logging and Prometheus metrics.

## Develop

Requirements: Go (version in `go.mod`), Docker (integration tests start PostgreSQL + pgvector with
testcontainers), and `sqlc` if you change SQL.

```sh
make test        # all tests; integration tests require Docker
make test-unit   # tests that need no Docker (integration tests are skipped)
make build       # ./bin/dth-hub
```

Run the hub against a local database:

```sh
docker run -d --name dth-pg -e POSTGRES_PASSWORD=pw -e POSTGRES_DB=dth -p 5432:5432 pgvector/pgvector:pg16
export DTH_DATABASE_URL='postgres://postgres:pw@localhost:5432/dth?sslmode=disable'
./bin/dth-hub            # all roles; no config file needed
curl localhost:8080/readyz
```

## License

MIT — see [LICENSE](LICENSE).
