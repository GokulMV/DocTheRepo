<!-- dth:generated source="pkg/contract/contracts.go" — edit only inside dth:human blocks -->
# `pkg/contract/contracts.go`

<!-- dth:chunk 9f99362957f3da48 -->
## `DocGenTask`

DocGenTask represents the task file written by the Hub for an external documentation generation engine. It contains the repository metadata (repo name, commit SHA, job ID), input specification (chunks to generate with their context and code diffs), output configuration (docs and output paths), and model parameters (name, max tokens per chunk). The Context field holds scoped prompt text including changed code and related signatures. RepairErrors is populated only on retry to communicate schema problems from the previous attempt. DiffSummaryPath optionally references a JSON file with triage verdicts for the push.
