<!-- dth:generated source="deploy/helm/dth/templates/_helpers.tpl" — edit only inside dth:human blocks -->
# `deploy/helm/dth/templates/_helpers.tpl`

<!-- dth:chunk 0baef52b2c6e2537 -->
## `deploy/helm/dth/templates/_helpers.tpl`

This Helm template helper file provides reusable named templates for the DocTheRepo (DTH) Helm chart. It defines:

**dth.fullname**: Generates the deployment's full name by combining release and chart names (max 63 chars), avoiding duplication if the release name already contains the chart name.

**dth.labels**: Standard Kubernetes labels including app name, instance, version, managed-by, and chart information for resource identification and management.

**dth.selector**: Creates label selectors for pod matching, with support for role-based component selection via the `.role` parameter.

**dth.serviceAccountName**: Returns the service account name, using a generated default based on fullname if creation is enabled, otherwise defaults to "default".

**dth.databaseSecret**: References the database credentials secret, either a user-provided existing secret or a default named secret derived from fullname.

**dth.validate**: Performs comprehensive configuration validation, failing deployment if database credentials, secrets provider, authentication mode, or OIDC settings are misconfigured. Ensures localfile provider has a shared key and OIDC has required issuer, client ID, secret, and allowed domains.

**dth.env**: Generates container environment variables for DTH application configuration, including URLs, database credentials, secrets encryption, authentication (OIDC or local), email settings, and inline/external settings, with conditional inclusion based on enabled features.
