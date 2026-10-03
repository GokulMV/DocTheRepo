<!-- dth:generated source="web/src/pages/SignIn.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/SignIn.tsx`

Sign-in configuration page where owners choose between password authentication and single sign-on (or both) for the Hub.

<!-- dth:chunk 04ed57c0dda8934a -->
## `SSOSettings`

Represents the single sign-on configuration stored in the system. Includes identity provider details (provider type, issuer URL, client ID), security settings (whether a client secret is set), and optional domain and group restrictions. This structure is returned by the `/auth/settings` API endpoint.

<!-- dth:chunk 4ebb79e187d5ca43 -->
## `SignInState`

Represents the current sign-in authentication state including whether password authentication and SSO are enabled, whether they are set locally or use defaults, and the SSO settings if configured. The `sso_source` field distinguishes between settings configured in the app versus inherited from a deployment config file.

<!-- dth:chunk 93c4096453c2ea25 -->
## `Preset`

Describes a preconfigured single sign-on provider (Google, Microsoft, Okta, Keycloak, or generic OpenID Connect) with setup instructions. The `steps` array contains user-facing setup directions, where `{callback}` placeholders are replaced with the actual callback URL. Hints guide users on domain and group configuration specific to each provider.

<!-- dth:chunk 52e2c99302ef4a51 -->
## `PasswordCard`

UI component for toggling password-based authentication on or off. Sends a PUT request to `/auth/settings` to persist changes and prevents disabling passwords unless SSO is already enabled (to avoid locking out all users). Displays the current password status and relevant warnings about sign-in fallbacks.

<!-- dth:chunk 90aaf9f261a13a17 -->
## `SSOCard`

Form component for configuring single sign-on with OpenID Connect. Allows selection from preset providers or custom configuration, collects issuer URL, client ID/secret, allowed email domains, and optional group claims. Encrypts the client secret before sending and validates the issuer by loading the provider's OpenID configuration. Supports removal of existing SSO settings with a confirmation dialog.

<!-- dth:chunk 6516f7c5c1c69bba -->
## `SignIn`

Page for owners to configure sign-in methods. Fetches current authentication state and settings from the API, then renders UI cards to manage both SSO and password authentication options. Refreshes related queries after successful settings changes.

<!-- dth:chunk a62b2a8a93b55cf3 -->
## `__module__`

Array of preconfigured OpenID Connect providers with their setup instructions. Includes Google Workspace, Microsoft Entra ID, Okta, Keycloak, and a generic OpenID Connect option. Each preset provides provider-specific hints for domains, groups, and step-by-step directions to create the required OAuth application. The "Other" entry serves as a fallback for unsupported providers.
