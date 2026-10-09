<!-- dth:generated index — edit only inside dth:human blocks -->
# `internal/core/repodocs`

- [`catalog.go`](catalog.go.md) — Defines the document type catalog and specification schema that guides documentation generation, including predicates for detecting repository features and m...
- [`check.go`](check.go.md) — This file defines data structures and logic for managing generated documentation about code repositories, including validation and quality assessment.
- [`facts.go`](facts.go.md) — Defines core data structures and utility functions for representing repository facts: code symbols, files, facts, and inter-repository links.
- [`generator.go`](generator.go.md) — Generator orchestrates the creation of repository documentation by calling an LLM, validating outputs, and persisting results.
- [`index.go`](index.go.md) — This file provides functions to convert repository and system documentation into searchable chunks with metadata and access controls.
- [`inputs.go`](inputs.go.md) — Defines the data types and functions for assembling documentation input material (symbols, modules, facts, dependencies) into prioritized, budget-aware text ...
- [`modules.go`](modules.go.md) — Partitions a repository's indexed files into logical documentation modules based on directory structure and code size.
- [`report.go`](report.go.md) — Defines data structures and functions to summarize and report on document generation runs, including problem aggregation, quality metrics, and Markdown rende...
- [`system.go`](system.go.md) — Provides system-level documentation generation for multi-repository architectures.
