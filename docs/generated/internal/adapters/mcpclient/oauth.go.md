<!-- dth:generated source="internal/adapters/mcpclient/oauth.go" — edit only inside dth:human blocks -->
# `internal/adapters/mcpclient/oauth.go`

Implements OAuth 2.0 discovery, client registration, and authorization-code flow with PKCE for MCP server authentication.

<!-- dth:chunk 0941b964ff778ef4 -->
## `OAuth`

Struct storing OAuth configuration and state for one protected resource. Holds server endpoints (`AuthEndpoint`, `TokenEndpoint`), client credentials (`ClientID`, `ClientSecret`), authentication method (`TokenAuth`), current tokens (`AccessToken`, `RefreshToken`, `Expiry`), and in-progress sign-in state (`PendingState`, `Verifier`, `RedirectURI`, `PendingUntil`). The `Registered` flag indicates whether the Hub itself registered this client.

<!-- dth:chunk fb7f39f89683c3d9 -->
## `OAuth.Valid`

Valid reports whether the access token can be used without refreshing.

<!-- dth:chunk 946836eab19aeca3 -->
## `protectedResource`

Struct representing the OAuth Protected Resource Metadata document, which describes where a resource is located, which authorization servers protect it, and which scopes it supports.

<!-- dth:chunk f4f139a1f03cc358 -->
## `asMetadata`

Struct representing OAuth Authorization Server Metadata, containing endpoints for authorization, token exchange, and client registration, along with supported authentication methods and PKCE challenges.

<!-- dth:chunk d45ec789cc5e457f -->
## `challengeParam`

Extracts a named parameter from a WWW-Authenticate Bearer challenge header using regex pattern matching on `name="value"` pairs.

<!-- dth:chunk 07046f2f1b476221 -->
## `Discover`

Discovers OAuth configuration for a server by probing protected-resource metadata, querying authorization server metadata endpoints, and determining supported authentication methods. Returns an `OAuth` struct with endpoints and configuration, the registration endpoint URL, and an error if discovery fails. Falls back to older spec defaults (authorization_endpoint `/authorize`, token_endpoint `/token`) if modern metadata is unavailable. Requires PKCE (S256) support if server declares challenge methods.

<!-- dth:chunk 4dcee75b6bb49413 -->
## `authServerMetadata`

Fetches authorization server metadata from standard discovery paths (`.well-known/oauth-authorization-server` or `.well-known/openid-configuration`) for a given issuer. Returns the metadata or an error if no valid metadata is found.

<!-- dth:chunk 2a55e443702f3ce8 -->
## `Register`

Registers the Hub as a public client with an authorization server's registration endpoint. Sends client name, redirect URI, and grant/response types, then updates the OAuth struct with the server's returned `client_id`, `client_secret` (if any), and authentication method. Returns an error if the endpoint is empty, the request fails, or no client ID is returned.

<!-- dth:chunk a0eb37190a0adc7e -->
## `OAuth.Begin`

Initiates a sign-in flow by generating a PKCE verifier, constructing the authorization request URL with code challenge and state, and storing verifier, state hash, redirect URI, and a 15-minute expiry. Returns the authorization URL to redirect the browser to.

<!-- dth:chunk 6204a0101263a0e9 -->
## `HashState`

HashState is how a pending state is stored (the state itself only travels in the browser).

<!-- dth:chunk 3797acd0f6db8de5 -->
## `OAuth.Finish`

Exchanges an authorization code for tokens by calling the token endpoint with the code and PKCE verifier. Clears pending sign-in state after completion (whether successful or not) and returns any token error. Fails if the verifier is missing or the 15-minute sign-in window has expired.

<!-- dth:chunk c5e22ff1e3b8350c -->
## `OAuth.Refresh`

Attempts to refresh an access token using the stored refresh token. Returns `ErrSignInAgain` if no refresh token exists or if the server returns `invalid_grant` or 401, indicating the refresh token is no longer valid.

<!-- dth:chunk d0c5422adc6d45cd -->
## `tokenError`

Struct representing an error response from the token endpoint, with HTTP status code and OAuth error code and description.

<!-- dth:chunk 40df59998ad32b78 -->
## `tokenError.Error`

Returns a human-readable error message: either the OAuth error code and description if available, or the HTTP status code.

<!-- dth:chunk 59a054980c168d0a -->
## `OAuth.token`

Exchanges an authorization code or refresh token for access tokens by POSTing to the token endpoint. Authenticates using one of three methods: client ID in body, client secret in body (client_secret_post), or HTTP Basic auth (client_secret_basic). Parses JSON or form-encoded responses, stores access token, refresh token (if provided), and expiry time. Returns a `tokenError` if the response status is 300+, no access token is returned, or parsing fails.

<!-- dth:chunk 5c716aae03fcf38e -->
## `client`

Returns the provided HTTP client if non-nil, otherwise returns `http.DefaultClient`.

<!-- dth:chunk 189ec840783ce83e -->
## `getJSON`

Performs an HTTP GET request for JSON, decoding the response body into `out`. Limits response size to 1 MB. Returns an error if the request fails or status is not 200 OK.

<!-- dth:chunk a37858e3e622e9f6 -->
## `postJSON`

Performs an HTTP POST with JSON body, encoding `in` as JSON and decoding the response into `out`. Limits response size to 1 MB. Returns an error if the request fails or status is 300+.

<!-- dth:chunk 6b8d6195c01a4acb -->
## `contains`

Simple helper returning true if string slice contains the given value.

<!-- dth:chunk 62b07117861d251b -->
## `__module__`

Package-level regex for parsing OAuth challenge parameters and error constant for sign-in expiry.
