<!-- dth:generated source="internal/adapters/mcpclient/auth.go" — edit only inside dth:human blocks -->
# `internal/adapters/mcpclient/auth.go`

Provides authentication strategies for MCP server requests: Bearer tokens, token-based functions (OAuth), AWS SigV4 signing, and Google service account authentication.

<!-- dth:chunk ff89486f4da82e20 -->
## `Header`

Header sets one header, e.g. Authorization: Bearer <token>, or api-key: <key>.

<!-- dth:chunk eddd6ec958b156e9 -->
## `Header.Authorize`

Authorize implements Authorizer.

<!-- dth:chunk 6246432639bdb4be -->
## `Bearer`

Constructs an Authorization header with Bearer token, or passes through values that already specify a scheme (detected by a space before the first period). If the token contains a scheme, it's sent as-is; otherwise, it's prefixed with "Bearer ".

<!-- dth:chunk ecd82825ee12582d -->
## `TokenFunc`

TokenFunc fetches a bearer token for each request (OAuth access tokens, refreshed as they expire).

<!-- dth:chunk 560a5caacb1035b6 -->
## `TokenFunc.Authorize`

Fetches a token from the TokenFunc and sets it in the request's Authorization header as a Bearer token. Returns any error from token generation.

<!-- dth:chunk 9c90c2ad59d265d6 -->
## `AWSKeys`

Holds static AWS credentials for SigV4 signing: AccessKeyID and SecretAccessKey are required; SessionToken is optional. Empty struct means credentials will come from the default chain (IAM role or environment variables).

<!-- dth:chunk 067bd1547755a4d8 -->
## `SigV4`

Performs AWS SigV4 request signing for MCP servers. Holds the AWS region, service name, credential provider, and v4 signer instance needed to sign HTTP requests.

<!-- dth:chunk 771e5f2eeac1f5c7 -->
## `NewSigV4`

Creates a SigV4 signer: requires region and service name. If keys (JSON) is provided, parses it as AWSKeys with required AccessKeyID and SecretAccessKey; otherwise uses the default credential chain (IAM role, environment). Returns error if required fields are missing or if default credentials cannot be loaded.

<!-- dth:chunk 6949d9d468531b0e -->
## `SigV4.Authorize`

Signs the HTTP request using AWS SigV4. Retrieves current credentials, computes SHA256 hash of the body, and signs the request with the region and service. Returns error if credentials cannot be retrieved.

<!-- dth:chunk 41efa798d1b04650 -->
## `NewGoogle`

Creates a TokenFunc for Google authentication: uses provided service account JSON key if non-empty, otherwise falls back to Application Default Credentials (attached service account on GKE, Cloud Run, or Compute Engine). Tokens are cached and refreshed automatically.

<!-- dth:chunk 7f590419d2d45fd5 -->
## `__module__`

OAuth scope for Google Cloud Platform authentication, used when obtaining credentials for cloud resources.
