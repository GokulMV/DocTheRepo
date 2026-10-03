<!-- dth:generated source="internal/bootstrap/compose.yaml" — edit only inside dth:human blocks -->
# `internal/bootstrap/compose.yaml`

<!-- dth:chunk 76b141030cccacc4 -->
## `internal/bootstrap/compose.yaml`

Docker Compose configuration for DocTheRepo Hub local stack. Defines three services: PostgreSQL 16 with pgvector extension for database storage, the Hub web service with environment variables for database connection, authentication, SMTP, and API keys for third-party integrations (OpenAI, Anthropic, GitHub, GitLab, OIDC), and an optional Ollama service for local LLM inference. The Hub service depends on a healthy PostgreSQL instance, exposes port 8080, mounts settings and data volumes, and reads sensitive values from a `.env` file. Volumes persist database, application, and embeddings data across container restarts.
