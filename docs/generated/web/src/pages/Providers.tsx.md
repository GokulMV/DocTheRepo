<!-- dth:generated source="web/src/pages/Providers.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Providers.tsx`

The Providers page for managing AI model provider accounts, their configurations, and feature-to-model routing in a Hub application.

<!-- dth:chunk 20a0c40bfc469fc9 -->
## `Providers`

### Providers

Displays a page for managing AI providers with two main sections: a table of configured provider accounts and an advanced configuration area.

Fetches provider and route data, showing a loading spinner and any errors. The main section lists providers with add/edit/test options, or displays empty state guidance if none exist. The advanced section (collapsible) allows configuring which model provider handles each feature, with fallback routing and documentation cost tracking.

Key dependencies: `useProviders()` and `useRoutes()` hooks for data, `ProviderRow` and `RouteRow` for rendering individual items, and `SimpleModels`, `MonthlyBudget`, and `DocsCost` for supplementary UI.
