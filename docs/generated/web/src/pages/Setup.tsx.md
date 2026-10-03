<!-- dth:generated source="web/src/pages/Setup.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Setup.tsx`

The Setup page provides a guided onboarding checklist with steps to configure sign-in, connect Git repositories, add an LLM provider, and enable documentation generation.

<!-- dth:chunk 45c6b13be4d8d422 -->
## `Step`

Interface describing a setup checklist step with its title, completion status, explanatory text (`why`), requirements (`need`), optional status message, navigation path, action button label, icon, and optional secondary completion method (`skip`) to mark the step done without navigating away.

<!-- dth:chunk 9a5480097419540d -->
## `readFlag`

Safely reads a localStorage flag, returning true if the value is '1' and false if localStorage is unavailable (e.g., in private browsing mode) or the key doesn't exist.

<!-- dth:chunk 298d64c9b0f86b88 -->
## `Setup`

A first-run setup checklist component that guides users through three to four essential steps (sign-in configuration, Git connection, model selection, and repository selection) to enable documentation and Q&A features. It reads live state from APIs to show completion status and current configuration, then displays optional setup items. Owner users see a sign-in setup step; all users see the remaining steps. The page serves dual purposes: onboarding and system health check.

<!-- dth:chunk 8dd0ac6d318a4d59 -->
## `__module__`

The localStorage key used to persist whether the user has accepted email/password sign-in as sufficient (avoiding re-prompting for SSO setup).
