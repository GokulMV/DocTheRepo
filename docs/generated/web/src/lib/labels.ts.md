<!-- dth:generated source="web/src/lib/labels.ts" — edit only inside dth:human blocks -->
# `web/src/lib/labels.ts`

<!-- dth:chunk eaaa9a0703d329df -->
## `sentence`

sentence turns an identifier or lowercase phrase into a label: "spend_blocked" → "Spend blocked".

<!-- dth:chunk 71fe618603a99a7f -->
## `capFirst`

capFirst capitalizes the first letter and leaves the rest alone (names, phrases with identifiers).

<!-- dth:chunk efd0c1818a6d9ee1 -->
## `featureLabel`

Returns a human-readable label for a feature identifier. Looks up the feature in `FEATURE_LABELS`; if not found, converts the identifier to sentence case using `sentence()`.

<!-- dth:chunk fb4b01a091de3f24 -->
## `jobLabel`

Returns a human-readable label for a job type identifier. Looks up the job type in `JOB_LABELS`; if not found, converts the identifier to sentence case using `sentence()`.

<!-- dth:chunk 43332f79c66e59cc -->
## `pushModeLabel`

Returns a human-readable label for a push mode identifier. Looks up the mode in `PUSH_MODE_LABELS`; if not found, converts the identifier to sentence case using `sentence()`.

<!-- dth:chunk 8761f72c76d6a2f2 -->
## `nameOf`

Connector, provider and mode identifiers as product names ("openai_compat" → "OpenAI-compatible").

<!-- dth:chunk 64e62c77b5f600dc -->
## `__module__`

Defines mappings from internal identifiers to user-facing labels for features (docgen, Q&A, security scanning, etc.), job types (documentation updates, error explanations, reviews), push modes (PR auto-merge, direct commits), and integrations (GitHub, Slack, cloud platforms, databases, and AI providers). These mappings enable consistent display of system concepts across the UI.
