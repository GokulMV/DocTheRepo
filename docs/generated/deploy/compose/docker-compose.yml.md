<!-- dth:generated source="deploy/compose/docker-compose.yml" — edit only inside dth:human blocks -->
# `deploy/compose/docker-compose.yml`

<!-- dth:chunk 267e7c7cf135ba98 -->
## `deploy/compose/docker-compose.yml`

Defines the local Docker Compose stack for DocTheRepo Hub, with three services:

- **postgres**: pgvector-enabled PostgreSQL 16 database with persistent storage, health checks every 3s, required by the hub service
- **hub**: The main application container (image from `DTH_IMAGE` environment variable or latest ghcr.io build) that listens on `DTH_LISTEN` and depends on a healthy postgres instance; accepts database credentials, auth mode (default local), SMTP configuration for email invites, and optional API keys for OIDC, Anthropic, OpenAI variants, GitHub, and GitLab via secret environment variables; mounts persistent data volume and read-only settings directory for initialization files created by `dth init`
- **ollama**: Optional local LLM service (enabled via `--ollama` profile) for embeddings and chat without cloud keys, mounted at base URL `http://ollama:11434/v1`

Both postgres and hub include health checks; all services auto-restart unless stopped. Environment variables from `.env` override defaults like port (8080) and public URL (http://localhost:8080).
