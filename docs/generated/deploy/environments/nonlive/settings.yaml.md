<!-- dth:generated source="deploy/environments/nonlive/settings.yaml" — edit only inside dth:human blocks -->
# `deploy/environments/nonlive/settings.yaml`

<!-- dth:chunk 8bbb72263504e74a -->
## `deploy/environments/nonlive/settings.yaml`

Configuration file for the nonlive Hub environment that defines authentication, LLM providers, routing, GitHub integration, and spending limits. Uses AWS Secrets Manager references (${awssm:...}) for sensitive values like API keys, ensuring credentials never appear in version control and are resolved server-side by the Hub using its cloud role. The configuration enables testing with lower-cost Anthropic models (claude-haiku-4-5), allows broader user access than production (owner and admin roles), tracks the `develop` branch of the payments repository, and enforces a 500k token daily spend limit.
