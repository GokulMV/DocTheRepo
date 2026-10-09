<!-- dth:generated index — edit only inside dth:human blocks -->
# `internal/core/repodocs`

- [`catalog.go`](catalog.go.md) — Defines the document type catalog and specification schema that guides documentation generation, including predicates for detecting repository features and m...
- [`check.go`](check.go.md) — Validates generated documentation against specifications, fact sources, and code material, checking sections, word limits, citations, and identifier references.
- [`facts.go`](facts.go.md) — Defines core data structures and utility functions for representing repository facts: code symbols, files, facts, and inter-repository links.
- [`generator.go`](generator.go.md) — Generator orchestrates LLM-powered creation and updating of repository documentation, managing costs, batching, and API routing.
- [`index.go`](index.go.md) — This file provides functions to convert repository and system documentation into searchable chunks with metadata and access controls.
- [`inputs.go`](inputs.go.md) — Defines the data types and functions for assembling documentation input material (symbols, modules, facts, dependencies) into prioritized, budget-aware text ...
- [`modules.go`](modules.go.md) — Partitions a repository's indexed files into logical documentation modules based on directory structure and code size.
- [`report.go`](report.go.md) — Defines Report structures and aggregation logic to summarize documentation generation results and quality metrics.
- [`system.go`](system.go.md) — Generates system architecture documentation for repositories by orchestrating LLM-based generation, validation, and scoring.
- [`wiring.go`](wiring.go.md) — Extracts and matches wiring facts—served hosts, external calls, published images, and inter-repository references—from repository configuration, deployme...
