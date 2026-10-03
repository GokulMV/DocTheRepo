<!-- dth:generated source="internal/settings/document.go" — edit only inside dth:human blocks -->
# `internal/settings/document.go`

<!-- dth:chunk 1c450dd11c4e8fec -->
## `Document`

Document is a Hub configuration where every section is optional. It contains authentication settings, user accounts, authentication providers, model routes, connectors, repositories, and spend limits. A version field may track configuration schema changes.

<!-- dth:chunk 3d9a0bb18269df42 -->
## `Auth`

Auth defines the authentication methods for signing into the Hub. The Password field controls email-and-password sign-in (nil means use the deployment default), and SSO enables OpenID Connect authentication. Changes to Auth require the owner role.

<!-- dth:chunk db43c2a17894b984 -->
## `SSO`

SSO configures an OpenID Connect identity provider (such as Google Workspace, Microsoft Entra ID, Okta, or Keycloak). The Issuer and ClientID are required; Provider is an optional label, AllowedDomains restricts access by domain, and GroupsClaim maps group information from the provider.

<!-- dth:chunk bc7120ea20d79e2a -->
## `User`

User represents a person identified by email. They can authenticate via single sign-on with the same email or via a password link created by an admin. A user with the owner role owns the Hub from their first sign-in. The Role field defaults to viewer and accepts viewer, editor, admin, or owner. A user can be disabled with the Disabled flag.

<!-- dth:chunk 3b31deb0365cbc1f -->
## `Document.merge`

The merge method combines a document p into the receiver d, updating Auth settings, and merging Users, Providers, Connectors, and Repos by matching them on a key function (email, name, etc.). Routes are added or replaced by key, and Spend overwrites entirely. It uses the mergeBy helper to deduplicate and combine list items.

<!-- dth:chunk 032d5d37714d6794 -->
## `Document.Validate`

Validate checks required fields and internal consistency: SSO requires Issuer and ClientID; Users must have non-empty email and a valid role (viewer, editor, admin, owner, or unset); Providers and Connectors require name and kind/type; Repos require full_name and connector; Routes require provider and model with the same for fallback; and Spend limits require scope and window. Returns an error joining all violations found.
