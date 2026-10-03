<!-- dth:generated source="web/src/pages/signalSources.ts" — edit only inside dth:human blocks -->
# `web/src/pages/signalSources.ts`

Defines TypeScript interfaces and configurations for signal sources and knowledge sources that integrate external error, alert, and documentation platforms with the DocTheRepo Hub.

<!-- dth:chunk ddc001b79e269bed -->
## `KnowledgeSpec`

Interface specifying the structure of a knowledge source (Confluence, Jira, or Notion) for Q&A, runbook decoding, library queries, and known-issue imports. The `config` array defines user-configurable fields with their UI hints and validation rules. The optional `token` field provides custom labeling for authentication tokens, which may be stored encrypted.

<!-- dth:chunk 5ddca1ac43e4b562 -->
## `__module__`

Defines `SOURCES` and `KNOWLEDGE` arrays that catalog all supported event and knowledge sources. `SOURCES` lists webhook and polling integrations grouped by category (Errors & alerts, Security & log platforms, Cloud logs & alarms, Event platforms), each with mode support, authentication requirements, and configuration fields. `KNOWLEDGE` defines Confluence, Jira, and Notion sources for syncing documentation and known-issue data. Both arrays configure the Hub's supported integrations, their UI presentation, and required credentials.
