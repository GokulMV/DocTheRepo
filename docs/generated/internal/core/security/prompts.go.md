<!-- dth:generated source="internal/core/security/prompts.go" — edit only inside dth:human blocks -->
# `internal/core/security/prompts.go`

<!-- dth:chunk 9708cb12af966c27 -->
## `attackerSystem`

System prompt for the attacker phase that instructs an LLM to find credible security weaknesses within a single module by static code analysis. It enforces strict rules: findings must cite exact file and line numbers from provided code, stay within the module's scope, include concrete reproduction steps, and estimate severity using the module's rubric. The prompt returns an empty list if no weaknesses are found rather than inventing findings.

<!-- dth:chunk 30c0be946602fc94 -->
## `verifierSystem`

System prompt for the verifier phase that instructs an LLM to re-check each candidate finding from the attacker. For each finding, it requires a verdict (confirmed, plausible, or rejected) with evidence quoted from code, re-derived severity assessment, and identification of duplicate root causes. The verifier is skeptical and requires positive evidence of correctness to reject a finding, not mere absence of evidence.

<!-- dth:chunk 625f9fad7cae5240 -->
## `__module__`

File exports a system prompt for the fixer phase and a prompt version constant. The fixer prompt instructs an LLM to design minimal fixes for verified findings with exactly two tests: one reproducing the vulnerability and one testing legitimate feature functionality. It restricts changes to shown files, prohibits refactoring, and requires complete file contents in output.
