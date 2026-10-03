<!-- dth:generated source="deploy/helm/dth/templates/NOTES.txt" — edit only inside dth:human blocks -->
# `deploy/helm/dth/templates/NOTES.txt`

<!-- dth:chunk e15c8ca9d627776d -->
## `deploy/helm/dth/templates/NOTES.txt`

Helm post-installation notes template that displays startup information for DocTheRepo Hub after deployment. It shows the replica counts, public URL, and provides authentication setup instructions that differ based on the configured auth mode (OIDC or local). For OIDC, it directs users to register a redirect URI with their identity provider and explains owner assignment either via settings file or first-user-wins. For local auth, it displays the owner email and references the secret containing the password. The template also provides helpful CLI commands for monitoring the rollout and logging in.
