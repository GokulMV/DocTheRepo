<!-- dth:generated source="internal/ingest/knowledge.go" — edit only inside dth:human blocks -->
# `internal/ingest/knowledge.go`

<!-- dth:chunk 7d3fe16762fdd6b3 -->
## `KnowledgeSync`

A struct that orchestrates syncing of Confluence/Jira documents into the Palace knowledge base. It manages the full pipeline: polling upstream sources for changed documents, chunking and embedding them, linking them into the Palace, and applying rules. It also handles document deletions, converts documents with known-issue labels into draft rules, and flips rules to label-only status when Jira issues move to Done or lose the label so fixed bugs' errors are no longer hidden. Uploads can be enabled via the Uploads field; embedding via the Embed function; and draft rule suggestion via the Propose function. The ReloadRules callback recompiles the matcher after rule changes. Last sync times are tracked per source in the mu-protected last map.
