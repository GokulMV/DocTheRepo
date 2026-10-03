<!-- dth:generated source="web/src/pages/Providers.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Providers.tsx`

Providers page for configuring LLM providers, feature routing, and documentation generation cost modes.

<!-- dth:chunk 5e97e5cf8bc260d6 -->
## `RouteOut`

Optional return type for route API responses, indicating the count of repositories queued for documentation generation after routing a provider or model.

<!-- dth:chunk c1d51bbe58ff63b2 -->
## `AddProvider`

Dialog form to add a new LLM provider with fields dynamically shown based on provider kind: API key (if required), base URL, and kind-specific extras. When "use for all" is toggled, also routes the provider to all applicable features (chat, embeddings) in a single step. Returns a confirmation showing created provider name, routed features, and count of repositories queued for doc generation.

<!-- dth:chunk d8f98ad6a890ecb1 -->
## `ReplaceKey`

Dialog to replace a provider's API key. Since keys are write-only (cannot be read after storing), this allows updating without revealing the existing key. Submits via `PATCH` to `/providers/{id}` with the sealed new key.

<!-- dth:chunk 54af66269bd41354 -->
## `ProviderRow`

Table row for a provider showing its name, kind, key status, and actions: test a model on the provider (with latency), disable/enable it, or remove it entirely. Test results display success/error and latency in milliseconds.

<!-- dth:chunk f59b192b817f0c3e -->
## `RouteRow`

Table row for routing a feature, allowing selection of primary provider and model, effort level (low/high/max for non-embedding), and optional fallback. Tracks dirty state and shows "Active" when saved, or "Unsaved changes" if modified. Saves via `PUT /routes/{feature}` and displays count of repositories queued for doc writing if applicable.

<!-- dth:chunk 1c9f0fff32b12a54 -->
## `DocsCost`

Presents radio buttons for selecting documentation generation cost mode (thorough, balanced, or economy), which controls how aggressively code is documented. Fetches the current mode from `/docs/mode` and saves changes via `PUT`. Shows explanatory text and tips about routing different doc modes to differently-capable models.

<!-- dth:chunk 20a0c40bfc469fc9 -->
## `Providers`

Main providers and routing page. Displays a table of configured LLM providers with actions, and a routing table mapping features (documentation, Q&A, search, etc.) to providers and models. Includes the doc generation cost selector. Allows adding new providers and configuring routing all in one place.

<!-- dth:chunk 77e9791c8561dd77 -->
## `__module__`

Constants defining feature descriptions and documentation generation modes (thorough/balanced/economy), used throughout the page to label and explain each routing feature and cost option.
