<!-- dth:generated source="web/src/lib/labels.ts" — edit only inside dth:human blocks -->
# `web/src/lib/labels.ts`

Defines label mappings for features, jobs, push modes, and integrations to display human-readable names in the UI.

<!-- dth:chunk 64e62c77b5f600dc -->
## `__module__`

Exports three dictionaries that map feature, job, and push-mode identifiers to human-readable labels for UI display. `FEATURE_LABELS` maps core platform capabilities like documentation generation, Q&A, and security scanning. `JOB_LABELS` maps background job types. `PUSH_MODE_LABELS` categorizes code deployment methods. Also defines an internal `NAMES` dictionary mapping integration provider keys (GitHub, GitLab, Sentry, cloud services, messaging platforms, LLMs, documentation tools, and webhook methods) to their display names.
