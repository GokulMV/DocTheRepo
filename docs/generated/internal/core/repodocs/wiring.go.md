<!-- dth:generated source="internal/core/repodocs/wiring.go" — edit only inside dth:human blocks -->
# `internal/core/repodocs/wiring.go`

Extracts and matches wiring facts—served hosts, external calls, published images, and inter-repository references—from repository configuration, deployment, and CI pipeline files.

<!-- dth:chunk 405d6df8fe31ab38 -->
## `Wire`

A Wire represents one fact about how a repository is wired to the outside world, extracted from configuration and CI files. The Kind field categorizes the fact: "host" (Kubernetes Service or ingress hostname it serves), "calls" (external host it calls), "image_pub" (image it publishes), "image_use" (image it runs or builds on), or "ci_ref" (other repository its CI references). Each wire includes the source location (Path and Line) and an optional Note for display.

<!-- dth:chunk 2c7cd37166a54347 -->
## `WiringFile`

WiringFile identifies configuration, deployment, and CI files worth scanning for wiring facts. It returns true for known CI platforms (.github/workflows, .gitlab-ci.yml, etc.), Docker files, deployment manifests (Kubernetes, Compose, Helm), infrastructure-as-code files (Terraform, Kustomize), and configuration files in recognized directories or with recognized names (application*, values*, config*, settings*).

<!-- dth:chunk cff750b768f0dc33 -->
## `CIFile`

CIFile identifies pipeline definition files across common CI systems: GitHub Actions, GitLab CI, Jenkins, CircleCI, Azure Pipelines, Bitbucket Pipelines, Google Cloud Build, CodeBuild, Buildkite, Tekton, and Drone.

<!-- dth:chunk ee2487645d242399 -->
## `ExtractWiring`

ExtractWiring scans a single file for wiring facts about its repository (given as owner/name). It detects URLs and host environment variables as external calls, extracts FROM statements in Dockerfiles as image dependencies, identifies published images in docker build/push commands, and finds CI references. For YAML and fly.toml files, it delegates to yamlWiring for structured parsing. Returns deduplicated wires with line numbers.

<!-- dth:chunk 3a044add914c8962 -->
## `yamlWiring`

yamlWiring extracts wiring from YAML configuration: Kubernetes Service and Ingress names as served hosts, image references in container specs, published images in CI workflows (docker/build-push-action tags field), and CI references to other repositories in workflow/job definitions. For fly.toml it extracts the app name as a fly.dev hostname. Handles up to 50 YAML documents per file.

<!-- dth:chunk 77c5a9f5ca69a7b7 -->
## `get`

Helper that retrieves a YAML mapping node's value by key, returning nil if the node is nil or not a mapping.

<!-- dth:chunk efddad2eb5a9b104 -->
## `scalar`

Helper that extracts a YAML node's scalar value as a string, returning empty string if the node is nil.

<!-- dth:chunk 725745893cf762e8 -->
## `seq`

Helper that extracts a YAML node's sequence (array) content as a slice of nodes, returning nil if the node is nil or not a sequence.

<!-- dth:chunk 5209a14b75eaf169 -->
## `walkYAML`

Walks a YAML tree depth-first, invoking the callback for each mapping entry with its key, value node, and parent mapping. Recursively descends through sequences and document nodes.

<!-- dth:chunk 516f02214f93bd62 -->
## `splitTags`

Parses a YAML tags field (from docker/build-push-action): if a sequence, returns its content; if a scalar, splits by newlines or commas and returns nodes with adjusted line numbers for block or folded styles.

<!-- dth:chunk bf07d91d7a51b171 -->
## `normImage`

Normalizes a container image reference by removing tags and digests, replacing repository variable references (${{ github.repository }}), dropping references with unresolved variables or "scratch", and lowercasing the result. Strips registry prefixes (docker.io, index.docker.io, library/) to get the canonical form. Returns empty string for references that cannot be normalized.

<!-- dth:chunk 7b71da696995d2a3 -->
## `isNumberish`

Returns true if the string contains only digits and dots (IP address-like, used to filter out port numbers and numeric values from host detection).

<!-- dth:chunk ab077d5a12ff8d08 -->
## `clipNote`

Truncates a string to 160 characters, appending "…" if truncated, after trimming whitespace.

<!-- dth:chunk 208156f8f29aa32a -->
## `dedupeWires`

Removes duplicate wires, keeping the first occurrence of each unique (Kind, Value, Path) combination. Uses an in-place slice operation to reduce allocations.

<!-- dth:chunk d94709bda01904b6 -->
## `InternalHost`

Identifies hosts that resolve only within a deployment cluster or private network: bare service names (no dot), or names ending with internal DNS suffixes (.svc, .cluster.local, .local, .internal, .consul, .lan, .intranet, .private) or containing .svc. in the middle.

<!-- dth:chunk 46253c1f2566f9fd -->
## `WiringRepo`

WiringRepo is one tracked repository with its wiring facts.

<!-- dth:chunk 5c3d8cf96d93894d -->
## `MatchWiring`

Links repositories by matching their wiring facts across a system. Creates three kinds of links: "api" when one repository calls a host served by another (matching Kubernetes Services, ingress hosts, or internal cluster names), "image" when one runs or publishes an image from another, and "pipeline" when CI references another repository. Matches hosts by full name or (for internal hosts) by service name or repository base name.

<!-- dth:chunk 4370160e33c748d4 -->
## `__module__`

Regular expressions and skip list for extracting wiring facts: urlRE matches protocol schemes and URLs; hostVarRE matches environment variable names and assignments to hosts; ciRefRE extracts owner/name references; fromRE parses Docker FROM statements; buildTagRE and pushRE find docker build/push image tags; ghRepoVarRE replaces GitHub repository variables. skipHosts excludes localhost, example.com, and literal strings like "true" and "false" from being treated as real hosts.
