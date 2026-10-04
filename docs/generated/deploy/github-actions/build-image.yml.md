<!-- dth:generated source="deploy/github-actions/build-image.yml" — edit only inside dth:human blocks -->
# `deploy/github-actions/build-image.yml`

GitHub Actions workflow for building and publishing the DocTheRepo Hub Docker image to ghcr.io based on git tags or manual trigger.

<!-- dth:chunk 9c7df1521f66e542 -->
## `deploy/github-actions/build-image.yml`

GitHub Actions workflow that builds and publishes the DocTheRepo Hub container image to the GitHub Container Registry (ghcr.io). Triggered on version tags (`v*`) or manual dispatch with a custom tag, it checks out the repository, sets up Docker buildx, authenticates with ghcr.io using the workflow token, builds the image from `docker/Dockerfile` with the specified tag, and outputs the published image URI and digest to the job summary. The image is named `ghcr.io/<owner>/doctherepo-hub:<tag>` (with lowercase owner) and is ready for deployment by `deploy-hub.yml`.
