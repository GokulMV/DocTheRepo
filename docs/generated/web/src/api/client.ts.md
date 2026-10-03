<!-- dth:generated source="web/src/api/client.ts" — edit only inside dth:human blocks -->
# `web/src/api/client.ts`

<!-- dth:chunk b1db041118ad4e3e -->
## `postForm`

Sends multipart form data via POST request, typically for file uploads. Automatically adds the CSRF token header if available, uses same-origin credentials, and delegates boundary generation to the browser. Parses and returns the JSON response, or throws an `ApiError` if the response status is not ok.
