<!-- dth:generated source="web/src/pages/ModelSetup.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/ModelSetup.tsx`

Configuration page for selecting LLM providers and models for different features, and setting a monthly budget cap on model API costs.

<!-- dth:chunk 9b87d2c3d2172ac0 -->
## `Choice`

Represents a model configuration consisting of a provider ID and model name. Used to store user selections across provider and model picker components.

<!-- dth:chunk e83a12aeb41ffbd7 -->
## `pick`

Selects a model configuration from routes matching any of the provided feature names. Returns the first matching route's provider and model, or empty strings if no match is found. Used to initialize model pickers with existing route configurations.

<!-- dth:chunk 961213034a2eb9e2 -->
## `ModelPicker`

A React component rendering a side-by-side provider dropdown and model text input. The dropdown is filtered by an optional `kinds` predicate and displays provider names. The model input shows a provider-specific default placeholder if available. Passes changes to `onChange` as a `Choice` object.

<!-- dth:chunk a91065d3395797a5 -->
## `SimpleModels`

A React component allowing users to configure three model assignments: a main model for documentation and analysis, a fast (optional) model for quick tasks and source selection, and an embedding model for semantic search. Compares current state to initial routes and saves changes via API, returning a count of queued document rewrites. The fast model defaults to the main model if unset.

<!-- dth:chunk 7ae2ed47758daeee -->
## `isBudget`

Predicate that identifies a spend limit as a global monthly budget by checking scope, window, and presence of a cost cap.

<!-- dth:chunk 71380e1f19eb60a5 -->
## `monthStart`

Returns the ISO 8601 date string for the first day of the current month (or provided date's month) in UTC, with milliseconds removed. Used to establish the billing period start for usage calculations.

<!-- dth:chunk 1d800f68632160df -->
## `MonthlyBudget`

A React component for setting a monthly USD budget cap on model API costs. Displays current month's spending and a progress bar (green <70%, amber 70–89%, red ≥90%). Saves the budget via API, filtering out non-monthly spend limits. Empty input removes the cap entirely. Shows saved confirmation and disables the save button when there are no changes or the input is invalid.

<!-- dth:chunk f0a641a6c2953ace -->
## `__module__`

Two exported constants defining which feature names map to the main model (`MAIN_FEATURES`: docgen, QA, code decoding, suggestions, security) and fast model (`FAST_FEATURES`: fast docgen, triage, source filtering).
