<!-- dth:generated source="internal/settings/signin.go" — edit only inside dth:human blocks -->
# `internal/settings/signin.go`

<!-- dth:chunk fa08f28de81c28c7 -->
## `remoteSignIn`

struct `remoteSignIn` mirrors the Hub's authentication settings endpoint response, holding current sign-in configuration state including password enablement, SSO provider details (provider, issuer, client ID, allowed domains, groups claim), and whether a client secret is configured.

<!-- dth:chunk 8b00d6da83d85c30 -->
## `applier.auth`

Updates sign-in authentication settings (password on/off and SSO configuration) in a single request to ensure consistency—the Hub enforces that at least one authentication method remains available. It fetches current settings, builds a request body with only changed fields, and handles write-only client secrets by encrypting them. Returns an error if the caller lacks owner permissions.

<!-- dth:chunk eb356a39d3eae95a -->
## `normDomains`

Normalizes domain strings by lowercasing, trimming whitespace and leading '@' symbols, removing duplicates and empty values, and returning a deduplicated list. Used to ensure consistent comparison of allowed domain lists in SSO configuration.

<!-- dth:chunk a771c27e6b7e77ec -->
## `remoteUser`

struct `remoteUser` models a user record from the Hub API, containing the user's unique ID, email, display name, role, and disabled status.

<!-- dth:chunk b9907449e4120efc -->
## `applier.listUsers`

Paginates through all users on the Hub using cursor-based pagination with a limit of 200 per page, accumulating results until no more pages remain. Returns all users or an error if any API request fails.

<!-- dth:chunk c4d86b048bd16e2e -->
## `applier.users`

Applies user records by creating new users or updating existing ones (by email, case-insensitive) with changed name, role, or disabled status. Never removes users. Defaults role to "viewer" if unspecified. Logs changes with specific fields modified and reports user creation/updates to the applier's change recorder.

<!-- dth:chunk aa4dc61ec0de6555 -->
## `exportAuth`

Fetches and exports current sign-in settings and users into a Document for export when readable. Populates password and SSO configuration (marking client secrets as environment-based), and includes all users with their email, name, role, and disabled status. Silently skips if the caller lacks permissions (when ErrAPI is returned).
