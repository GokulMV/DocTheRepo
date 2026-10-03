<!-- dth:generated source="internal/core/security/scan.go" — edit only inside dth:human blocks -->
# `internal/core/security/scan.go`

<!-- dth:chunk fde79f01c2fe00c4 -->
## `Finding`

A Finding represents a security issue identified in code, combining kryptonite's finding schema with location details and optional fix information. It includes the vulnerability's location (file and line range), risk assessment (severity, exploitability, priority), and reproduction steps; once the verifier confirms it or a user requests a fix, it includes the proposed remedy or any error encountered applying it.

<!-- dth:chunk a1348acff6a34d9a -->
## `Plan`

Plan describes what a scan will read and its estimated cost, presented before execution as a gate mechanism. It tracks the commit, modules to scan, files each module attacks, token consumption estimate, and which modules are cached (unchanged inputs since last scan) or skipped, letting the user approve the operation's scope and cost.

<!-- dth:chunk d7caf75aeb86f965 -->
## `Result`

Result reports a completed scan's findings, counts by severity tier (P0–P3) or rejection status, token consumption, and any notes (skipped modules or modules with no applicable files). Findings are omitted from JSON output but retained in memory.

<!-- dth:chunk 08075f276a8cbbfc -->
## `Cache`

Cache keeps a module's verified findings per exact inputs.

<!-- dth:chunk 9fdbe6896d7c1f7d -->
## `Scanner`

Scanner orchestrates security scans using an LLM gateway and optional cache. It runs the attack-then-verify pipeline per module, reusing cached findings for unchanged inputs, and invokes an optional progress callback to report module completion.

<!-- dth:chunk a3fb67d3f1b48764 -->
## `Target`

Target specifies a repository, code host, and commit to scan, with an optional overrides map for file contents already modified by earlier fixes in the same pull request.

<!-- dth:chunk 56679e502fe86326 -->
## `read`

read loads candidate files for the given modules from the repository at the target commit. It skips binary files, truncates files exceeding MaxFileBytes (noting them as skipped), and halts on retrieval errors, returning the loaded files and list of truncated paths.

<!-- dth:chunk cb66d7665a343b14 -->
## `Scanner.PlanScan`

PlanScan determines which files each module would attack and estimates token cost without calling the model. It reads files, maps modules to their targets, checks cache for unchanged inputs (marking them as cached), and estimates tokens as three-halves the code size (attacker reads once, verifier rereads excerpts) plus overhead.

<!-- dth:chunk c093d2656c4cd49b -->
## `inputsHash`

inputsHash creates a deterministic identifier for a module run by hashing the module name, prompt version, reference severity config, and all selected files' contents. Used to detect unchanged inputs and enable caching.

<!-- dth:chunk d2258d11e44acad0 -->
## `Scanner.feature`

feature returns the route the scanner uses: FeatureSecurity if available, else FeatureQA (fallback when security routing is not available).

<!-- dth:chunk 6e7f8db20e17c6a3 -->
## `Scanner.Run`

Run executes scans: for each module, it attacks (LLM-generated findings), then verifies (LLM re-evaluates), caching results for unchanged inputs, and compiles a final result with sorted findings, counts by priority, and an overall verdict. Returns error if any module fails to attack or verify.

<!-- dth:chunk fa36304e7a31e274 -->
## `numbered`

numbered renders a file's content with 1-indexed line numbers in XML tags, used by the attacker model to reference lines of code.

<!-- dth:chunk 4da65dbec1ae2d4c -->
## `excerpt`

excerpt extracts lines around a finding with context (±30 lines) and line numbers in XML tags, used by the verifier to review the specific code region cited in each candidate finding.

<!-- dth:chunk d9425acdb0485579 -->
## `attackOut`

attackOut is the JSON structure the attacker model returns, containing a list of findings with title, surface description, file location, line range, repro steps, evidence, severity, and exploitability.

<!-- dth:chunk 2bc5ab1b34edd6a7 -->
## `Scanner.attack`

attack invokes the attacker model to generate security findings from selected code files. It formats code with line numbers, constructs a prompt with repository and module context, validates that cited files are in the selection, and returns findings with clipped strings to avoid overflow; on success returns findings, token count, and nil error.

<!-- dth:chunk 6f200ce6804159f3 -->
## `verifyOut`

verifyOut is the JSON structure the verifier model returns, containing verdicts on each candidate finding with index, status, refined severity and exploitability, additional evidence, and optional duplicate-of index to merge findings.

<!-- dth:chunk 6cab4b37a54866b4 -->
## `Scanner.verify`

verify invokes the verifier model to evaluate candidates by presenting each with its code context (title, surface, file, severity, exploitability, repro, evidence, and ±30-line excerpt). It validates verdicts cover all candidates, skips out-of-range or merged findings, refines severity and exploitability, appends verifier evidence to the finding, and computes priority; returns verified findings, token count, and nil error.

<!-- dth:chunk 0de40cd074478706 -->
## `contains`

contains checks whether a string appears in a slice of strings using linear search.

<!-- dth:chunk 12908ed933302763 -->
## `clip`

clip truncates a string to n characters if needed, trimming whitespace and appending an ellipsis if truncated.

<!-- dth:chunk d06bada35e05d6ba -->
## `short`

short returns the first 12 characters of a commit SHA, or the full SHA if shorter.

<!-- dth:chunk ce898dbff6c73bbe -->
## `__module__`

Defines constants and schemas for the scanning module: status enum values (confirmed, plausible, rejected), the security feature flag, and JSON schemas for the attacker and verifier model outputs (finding fields with required enums for severity and exploitability).
