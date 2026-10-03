<!-- dth:generated source="internal/api/github_existing.go" — edit only inside dth:human blocks -->
# `internal/api/github_existing.go`

<!-- dth:chunk f4456f434c408005 -->
## `ghExistingApp`

Represents a GitHub App as returned by the GitHub API, containing the app's slug, display name, and owner information (login and account type).

<!-- dth:chunk 9ba45fc442970764 -->
## `ghInstallation`

Represents an installation of a GitHub App, including its numeric ID, the account it's installed on (login and type), and whether it's currently suspended. The `SuspendedAt` field is non-nil if suspended.

<!-- dth:chunk bd0e81523b8f4edd -->
## `parseAppKey`

Parses a GitHub App private key from PEM format, supporting both PKCS#1 and PKCS#8 encodings. Returns a validation error if the key is invalid, not a PEM file, or not an RSA key. Whitespace is trimmed before parsing.

<!-- dth:chunk 54000988be39e9c2 -->
## `appJWT`

Creates an RS256 JWT that authenticates as the GitHub App itself, valid for 9 minutes with a 60-second clock skew buffer. The token includes the app ID as the issuer claim and is used for API calls as the app.

<!-- dth:chunk e8e1b0f4ece190b7 -->
## `ghAppGet`

Makes a GET request to the GitHub API authenticated as the App using a Bearer JWT token. Unmarshals the JSON response into `out`. Returns specific validation errors for 401 (bad app credentials) and 404 (app not found), and generic errors for other non-2xx responses.

<!-- dth:chunk af2b7a10fe814244 -->
## `inspectApp`

Authenticates as a GitHub App using its ID and private key, then retrieves the app details and lists its installations (up to 100). Wraps private key parsing errors as validation errors with a helpful message.

<!-- dth:chunk c27e7d50684e0f46 -->
## `installURL`

Builds the URL for installing a GitHub App. Uses a different path structure for GitHub Enterprise (`/github-apps/`) versus public GitHub (`/apps/`).

<!-- dth:chunk abae492678d650ed -->
## `pickInstallation`

Selects an installation from a list, filtering out suspended installations. If an account name is provided, returns the matching installation; if no account is given and exactly one installation exists, returns it automatically. Otherwise returns the list of active installation account names for the caller to choose from.

<!-- dth:chunk c5f265f1adc0e581 -->
## `validAppID`

Validates that a string is a positive integer by parsing it as a base-10 int64, after trimming whitespace.
