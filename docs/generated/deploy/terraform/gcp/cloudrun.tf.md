<!-- dth:generated source="deploy/terraform/gcp/cloudrun.tf" — edit only inside dth:human blocks -->
# `deploy/terraform/gcp/cloudrun.tf`

<!-- dth:chunk c0e0c185333eb1d9 -->
## `deploy/terraform/gcp/cloudrun.tf`

This file defines the Cloud Run services that host the DocTheRepo application on Google Cloud Platform. It configures three services (api, worker, and scheduler) with environment variables, resource limits, health probes, and IAM settings.

`locals.common_env` merges base configuration with optional OIDC settings: includes the public URL, environment name, KMS key for secret decryption, auth mode, logging, and conditionally adds OIDC issuer, client ID, and allowed domains when OIDC is enabled.

`locals.services` defines three services with different characteristics: the api service listens on port 8080 with a readiness probe, accepts variable scaling, and allows CPU idle; worker and scheduler services use port 9090 with health probes, have fixed or minimal scaling, and disable CPU idle to keep jobs running between requests.

The `google_cloud_run_v2_service` resource creates each service with a lifecycle precondition that enforces OIDC configuration completeness—requiring issuer, client ID, client secret, and at least one allowed domain when OIDC mode is selected, preventing unauthorized sign-ins. It configures 3600-second timeout for SSE streams, concurrency limits (80 for api, 1 for others), private VPC egress for Cloud SQL, environment variables from merged config, and secret references. Startup probes allow 24 failures over 2-minute windows for migrations; liveness probes differ between api (/healthz) and other services.

The `google_cloud_run_v2_service_iam_member` resource grants `allUsers` the `run.invoker` role on the api service, allowing unauthenticated access from the load balancer since the Hub authenticates all requests internally.
