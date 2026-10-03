<!-- dth:generated source="web/src/api/hooks.ts" — edit only inside dth:human blocks -->
# `web/src/api/hooks.ts`

<!-- dth:chunk a210e5749b994544 -->
## `useAuthConfig`

Fetches the authentication configuration from the server.

Uses React Query to manage the request state and caching with the query key `'auth-config'`. Returns the configuration data typed as `AuthConfig`.
