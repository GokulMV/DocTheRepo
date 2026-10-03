<!-- dth:generated source="internal/auth/oidc.go" — edit only inside dth:human blocks -->
# `internal/auth/oidc.go`

<!-- dth:chunk ff54ee47210a4a44 -->
## `LoginState`

Stores OpenID Connect state between the login redirect and callback, kept in a short-lived HttpOnly cookie. Contains the OAuth state, nonce for token validation, PKCE verifier, and optionally a return URL to redirect to after login succeeds. The `Redirect` field holds a callback URL for single sign-on scenarios where the client has no pre-configured redirect URL; the same URL must be sent during token exchange.

<!-- dth:chunk b4f57da1f5fc77cc -->
## `OIDC.HasRedirect`

HasRedirect reports whether the client has a configured callback URL.

<!-- dth:chunk 24e7dcfbfc31c691 -->
## `OIDC.config`

Returns an `oauth2.Config` for the given login state, optionally overriding the redirect URL if one was stored in the state (used when single sign-on is configured in the UI rather than the OAuth provider). This allows the token exchange to use the same callback URL that initiated the login.

<!-- dth:chunk e9155fb8c38405e4 -->
## `OIDC.AuthURL`

AuthURL is the IdP authorization URL for ls.

<!-- dth:chunk 1267f624f03a7c41 -->
## `OIDC.Exchange`

Redeems an authorization code for an ID token, verifies the token's signature, issuer, audience, expiry, and nonce, then extracts and returns the token claims. Also populates the Groups field from a configured groups claim in the token if present. Returns an error if code exchange fails, the ID token is missing or invalid, the nonce does not match, or claims extraction fails.
