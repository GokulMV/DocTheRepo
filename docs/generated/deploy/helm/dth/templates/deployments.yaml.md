<!-- dth:generated source="deploy/helm/dth/templates/deployments.yaml" — edit only inside dth:human blocks -->
# `deploy/helm/dth/templates/deployments.yaml`

<!-- dth:chunk 9ce6da952fae12cc -->
## `deploy/helm/dth/templates/deployments.yaml`

Helm template that generates Kubernetes Deployments for three application roles: api, worker, and scheduler. Each role gets a separate Deployment with independently configurable replicas and resources. The scheduler uses a Recreate strategy to ensure only one instance runs at a time (since it holds a leader lock). The template validates the chart, applies consistent labels and selectors, mounts a writable tmpdir on the otherwise read-only root filesystem, and configures role-specific probes: api uses http probes on port 8080, while worker and scheduler use metrics probes on port 9090. Pod security is enforced with non-root user (65532), disabled privilege escalation, and dropped capabilities. Database URL changes trigger pod restarts via checksum annotation.
