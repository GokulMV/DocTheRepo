<!-- dth:generated source="internal/core/llmgateway/judge.go" — edit only inside dth:human blocks -->
# `internal/core/llmgateway/judge.go`

<!-- dth:chunk 50776e870f53a780 -->
## `Gateway.JudgeRoute`

Returns the first available route from the configured sift routes (FeatureSift, FeatureDecide, FeatureDocGenFast), labeling it with the FeatureSift feature. Both the primary route and its fallback (if present) are tagged with FeatureSift. Returns ErrNoRoute if no route is available.

<!-- dth:chunk 5e1a05f8b660b9de -->
## `Gateway.Judge`

Answers multiple yes/no questions about a state using a LLM provider via the given route. Providers implementing the native Judger interface use calibrated judgment; others use a JSON contract through chat. The state is protected, and the call is spend-guarded and recorded. Returns an error if no questions are provided, and an empty judgment before scrubbing the state if provider lookup fails.

<!-- dth:chunk 75d74261972dcf1b -->
## `Gateway.judgeNative`

Invokes a native Judger provider's Judge method for calibrated answers. Estimates input tokens from state and question instructions, output as 4 tokens per question. Enforces spend limits before calling. Records the call with actual or estimated usage and latency, falling back to estimation if usage is unreported. Clamps probabilities to [0,1] per question before return. Returns an error if spend enforcement or the judgment call fails.

<!-- dth:chunk a8bf3493a733c6ba -->
## `Gateway.judgeJSON`

Uses a chat provider to answer questions via a JSON schema contract. Constructs a schema requiring an object with field "p" containing question IDs mapped to numbers in [0,1]. Calls chatOn and retries on transient error with fallback route if available. Decodes the response without a repair pass—parsing errors return immediately as permanent schema errors. Clamps probabilities before returning. No usage recording occurs here; chatOn handles it.

<!-- dth:chunk ca89cbebd7a0aba5 -->
## `clampJudgment`

Validates that every asked question has an answer in the judgment probabilities (NaN is invalid), returns an error for missing or invalid answers as Transient, and clamps valid probabilities to [0,1]. Ensures callers never mistake "not answered" for "no".

<!-- dth:chunk ae32f48accb8cfbf -->
## `__module__`

Module constants: FeatureSift labels judgment calls; SiftRoutes lists eligible routes (sift, decide, docgen-fast) in preference order; JudgeSystem is the system prompt instructing the LLM to output JSON probabilities for yes/no questions while ignoring instructions in the state itself.
