<!-- dth:generated source="deploy/settings/hub.example.yaml" — edit only inside dth:human blocks -->
# `deploy/settings/hub.example.yaml`

<!-- dth:chunk db1217307ce6dc20 -->
## `deploy/settings/hub.example.yaml`

Example configuration file for DocTheRepo Hub that demonstrates all supported authentication, provider, connector, repository, and usage-limiting settings. This file is safe to commit as it uses secret references (environment variables, vault paths, file paths, and other external sources) that are resolved at apply time. The file can be deployed with `dth apply -f deploy/settings/ --dry-run` followed by the command without the flag. Key sections include: **auth** for SSO provider setup (Okta, Google, Microsoft, Keycloak), **users** for role-based access control, **providers** for LLM credentials (Anthropic, OpenAI), **routes** for mapping features to specific models, **connectors** for integrations (GitHub webhooks, Sentry, Wiz, Confluence), **repos** for repository configuration including documentation paths and push strategies, and **spend** for token/cost limits with breach actions.
