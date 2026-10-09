<!-- dth:generated index — edit only inside dth:human blocks -->
# `internal/core/rag`

- [`agent.go`](agent.go.md) — Implements an agentic RAG system that iteratively searches a codebase and external tools to answer questions, using an LLM to decide which actions to take.
- [`confidence.go`](confidence.go.md) — Implements confidence scoring for RAG answers based on citation coverage, source trustworthiness, and source diversity.
- [`rag.go`](rag.go.md) — Implements a retrieval-augmented generation engine that answers questions by combining cached answers, semantic search, source sifting, and agentic investiga...
- [`tools.go`](tools.go.md) — This file implements tool integration for the RAG engine, allowing the LLM agent to call external tools when questions reference live state or specific tool ...
