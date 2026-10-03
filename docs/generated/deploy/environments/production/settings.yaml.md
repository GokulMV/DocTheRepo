<!-- dth:generated source="deploy/environments/production/settings.yaml" — edit only inside dth:human blocks -->
# `deploy/environments/production/settings.yaml`

<!-- dth:chunk 55ce463ffb6af7c1 -->
## `deploy/environments/production/settings.yaml`

Production configuration for the Hub, specifying authentication, API providers, routes, connectors, and tracked repositories. Defines a single Anthropic provider with routed models for document generation and QA tasks. Integrates GitHub webhook-based connectivity to auto-merge PRs for the `acme/payments` repository, with credentials sourced from AWS Secrets Manager. Enforces a global daily token spending limit of 3,000,000 to control AI model usage costs.
