<!-- dth:generated source="internal/ports/llm.go" — edit only inside dth:human blocks -->
# `internal/ports/llm.go`

<!-- dth:chunk b9bc020bb04e2258 -->
## `JudgeQuestion`

JudgeQuestion is one yes/no question about a judgment's shared state.

<!-- dth:chunk 6543f3875a2ac76d -->
## `JudgeRequest`

Batches multiple yes/no questions about a single state into one LLM request. The Task describes what the LLM should evaluate, State provides the context being judged, and Questions contains the list of individual yes/no queries. This design amortizes the cost of setting up the shared state across multiple questions, which is how the Ask evidence sifter operates (asking two questions per retrieved source).

<!-- dth:chunk 4f7ab2c1391ff9be -->
## `Judgment`

Response containing probability estimates for yes/no questions, keyed by question ID in the P map. Calibrated indicates whether the model has been calibrated (meaning its confidence estimates have been adjusted to match actual accuracy). Model names the LLM used (when provided), and Usage records token consumption for the request.

<!-- dth:chunk 29a8c5b28fcd0536 -->
## `Judger`

Judger is implemented by providers with a native yes/no model (TypeSafe Jev). Chat providers answer judgments through a JSON contract instead.
