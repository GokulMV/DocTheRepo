<!-- dth:generated source="deploy/templates/hub.yaml" — edit only inside dth:human blocks -->
# `deploy/templates/hub.yaml`

Configuration template for DocTheRepo Hub settings including SSO authentication, user access control, AI model routing, and spending limits.

<!-- dth:chunk 4b6a2bf82636add7 -->
## `deploy/templates/hub.yaml`

Configuration file for DocTheRepo Hub that defines authentication, user roles, AI model providers, and spending limits. This is a template file where users fill in provider details (SSO issuer, client ID), allowed email domains, owner/admin emails, and API keys via environment variables. The file maps AI tasks (docgen, qa, embedding, etc.) to specific Claude and OpenAI models and enforces a daily spending limit to control costs. It contains no secrets themselves, only references to environment variables where actual secret values are set at runtime.
