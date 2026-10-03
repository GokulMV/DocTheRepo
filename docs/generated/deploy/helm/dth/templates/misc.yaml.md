<!-- dth:generated source="deploy/helm/dth/templates/misc.yaml" — edit only inside dth:human blocks -->
# `deploy/helm/dth/templates/misc.yaml`

<!-- dth:chunk 749c46765fc5bf2f -->
## `deploy/helm/dth/templates/misc.yaml`

Helm template that generates optional Kubernetes manifests for ServiceAccount, database Secret, API PodDisruptionBudget, and settings Secret resources, with conditional creation based on chart values and automatic labeling via included helpers.
