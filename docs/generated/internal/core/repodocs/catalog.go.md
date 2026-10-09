<!-- dth:generated source="internal/core/repodocs/catalog.go" — edit only inside dth:human blocks -->
# `internal/core/repodocs/catalog.go`

Defines the document type catalog and specification schema that guides documentation generation, including predicates for detecting repository features and metadata for each document type (sections, audience, content guidelines).

<!-- dth:chunk 4805470cc68e2541 -->
## `IsDecisionCommit`

IsDecisionCommit reports a commit whose message records a decision.

<!-- dth:chunk efb54759c9e74b5d -->
## `IsADR`

IsADR reports architecture decision record files.

<!-- dth:chunk 9471d51cd422822d -->
## `hasDecisions`

Checks whether the repository contains decision commits or ADR (architecture decision record) files. Returns true if any commit message matches decision keywords or if the codebase contains files with names suggesting ADRs (detected via `IsADR` path matcher).

<!-- dth:chunk 8cbeb33aa8abc754 -->
## `weekOf`

Extracts the ISO year-week (e.g., "2024-W15") of the newest commit from a slice, used to track when recent changes were last updated; returns an empty string if no commits exist. Recent changes are considered stable and rewritten at most once per week.

<!-- dth:chunk 7f7845e90eb06d63 -->
## `Section`

Defines one part of a document specification. Key identifies the section, Title is the heading, Guide instructs the content writer what belongs there, Words is the target length (replies over 1.6× longer fail validation), Required indicates whether this section must be present, and Cite specifies whether claims must reference file paths and line numbers.

<!-- dth:chunk 97aff5fc7730ad31 -->
## `Spec`

Defines a document type in the specification catalog. Type is the identifier, Title the display name, Group categorizes it (Basics, Interfaces, Engineering, Operations, or People), Audience and Purpose guide content generation, Sections lists required sections, PerModule generates one document per module when true, Needs lists required input data types, Applies is a predicate determining whether this document type applies to a repository, and Order determines navigation sequence.

<!-- dth:chunk 1d2447e58dbb0b1f -->
## `s`

Helper function that constructs a Section with the given parameters, reducing boilerplate when defining document sections in the catalog.

<!-- dth:chunk e1519c356a8f9072 -->
## `hasFact`

Returns a predicate function that checks whether the repository facts contain any of the specified fact kinds (e.g., "endpoint", "datastore"). Used to conditionally enable document types based on what the code analysis discovered.

<!-- dth:chunk bed01cf48c964e01 -->
## `hasPath`

Returns a predicate function that checks whether any path in the repository's discovered paths matches the provided matcher function. Used to detect presence of files matching patterns like migrations, CI pipelines, or deployment files.

<!-- dth:chunk 36eb7b5600c972a6 -->
## `always`

A predicate that always returns true, used as the Applies condition for document types that should be generated for every repository.

<!-- dth:chunk c1220f8aa44bdedd -->
## `IsMigration`

Detects database migration and schema files by checking for "migration" in the path with source file extensions, or by matching schema-related filenames like schema.sql, schema.prisma, or schema.rb files.

<!-- dth:chunk fd63550a7f5fc64f -->
## `IsCI`

Detects CI/CD and deployment pipeline configuration files across common platforms: GitHub Actions (.github/workflows/), GitLab CI (.gitlab-ci.yml), Jenkins, CircleCI, Azure Pipelines, Bitbucket Pipelines, Google Cloud Build, and AWS CodeBuild.

<!-- dth:chunk 82375eb1375423c0 -->
## `IsDeploy`

Detects infrastructure and deployment files: Docker (Dockerfile, docker-compose), Terraform (.tf), Helm charts (helm/ directory, chart.yaml), Kubernetes configurations (k8s/, kubernetes/), and platform deployment configs (fly.toml, app.yaml, serverless.yml, railway.json, Procfile).

<!-- dth:chunk 2c32d7accbd513a5 -->
## `init`

Initializes the Catalog by appending document specifications for time-based reports (historySpecs), ensuring recent-changes and decision-records documents are registered in the global catalog.

<!-- dth:chunk 0d322bf89d7acbb8 -->
## `SpecByType`

Searches the Catalog for a document type matching the provided type identifier string. Returns the Spec and true if found, or an empty Spec and false otherwise.

<!-- dth:chunk 795f9d272ca3fd72 -->
## `__module__`

Module-level declarations: decisionRE is a regex pattern matching decision-related keywords (ADR, migrated, deprecated, etc.) in commit messages; SpecVersion tracks the specification schema version; Catalog is the slice of all document type specifications, initialized with base specs and extended by init() with history specs; historySpecs defines time-based documents (recent changes and decisions).
