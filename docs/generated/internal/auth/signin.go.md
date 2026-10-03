<!-- dth:generated source="internal/auth/signin.go" — edit only inside dth:human blocks -->
# `internal/auth/signin.go`

<!-- dth:chunk 2586d61829403266 -->
## `Sealer`

Sealer encrypts the stored client secret (the Hub's secrets.Box).

<!-- dth:chunk 48bc9dff1af384e1 -->
## `liveOIDC`

Holds the active OIDC client and the email domains it admits. The `fromSettings` field distinguishes whether this configuration came from the UI settings (taking precedence) or from the deployment's config.

<!-- dth:chunk e6575e455f20ec7b -->
## `Service.OIDC`

Returns the active OIDC client, or nil if single sign-on is disabled. Loads from the atomic value safely.

<!-- dth:chunk b4fb1ef14e500b65 -->
## `Service.UseConfigOIDC`

UseConfigOIDC installs the client built from the deployment's config (its allowed domains apply).

<!-- dth:chunk ce5b15b48e4a8b44 -->
## `Service.allowedDomains`

Returns the email domains allowed for OIDC login: first from UI settings if active, otherwise from deployment config. Falls back to an empty list if no OIDC is configured.

<!-- dth:chunk fa4ecd57c57ac075 -->
## `SSOSettings`

Represents single sign-on settings as persisted in the UI. The client secret is intentionally omitted from serialization for security; `HasSecret` indicates whether one exists.

<!-- dth:chunk 3780948771e61a37 -->
## `SignInState`

Reports the current sign-in configuration state shown to users. It includes password sign-in status (actual, default, and whether explicitly set), active SSO with its source ("settings" or "config"), the corresponding SSO settings, and the issuer from deployment config if any.

<!-- dth:chunk dc4548306d58e7db -->
## `Service.settingsRow`

Fetches the stored authentication settings row from the database. Returns the row, a boolean indicating existence, and any error; returns (empty, false, nil) if no row exists.

<!-- dth:chunk ad11021b56a948e6 -->
## `Service.SignIn`

Returns the current sign-in settings state. Builds the response from both the stored database row and the active OIDC client, reflecting whether SSO is enabled via settings or deployment config. Includes password settings and OIDC configuration if present.

<!-- dth:chunk d6b77ea2d699a01f -->
## `SaveSignIn`

Describes a change to sign-in settings. A nil SSO field leaves it unchanged, `ClearSSO` removes saved settings, and an empty `ClientSecret` preserves the stored secret. `Password` explicitly sets password sign-in on or off.

<!-- dth:chunk a2e17e53b8ee242c -->
## `Service.UpdateSignIn`

Persists sign-in setting changes (caller must verify ownership). Validates and tests new OIDC settings before saving (ensuring the issuer's discovery document loads) to prevent typos from breaking a working setup. Enforces that at least one sign-in method remains available. Updates the active OIDC client if settings change.

<!-- dth:chunk a15d198333af0650 -->
## `Service.LoadSignIn`

Loads and activates single sign-on from saved UI settings at startup. If settings exist and are valid, builds an OIDC client from them and stores it as active; otherwise falls back to the deployment's config OIDC client if provided.

<!-- dth:chunk 60e4308fe0e675e9 -->
## `Service.openSecret`

Decrypts the OIDC client secret from its ciphertext. Returns an empty string if ciphertext is empty, and fails if no decryption box is configured.

<!-- dth:chunk 042a7e357a304baa -->
## `cleanDomains`

Normalizes and deduplicates email domains: lowercases, strips whitespace and optional leading '@', filters empty values, and preserves order. Used to clean user-supplied domain lists.

<!-- dth:chunk 2cf235b8544d8035 -->
## `isLocalHost`

Identifies localhost for validation purposes, accepting the hostnames "localhost", "127.0.0.1", and "::1".

<!-- dth:chunk 5c1a22cbf3251f70 -->
## `__module__`

Defines the additional authentication data for encrypting the OIDC client secret, and the validation error returned when a settings change would disable all sign-in methods.
