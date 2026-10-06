<!-- dth:generated source="web/src/pages/SettingsGroups.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/SettingsGroups.tsx`

Settings page sections for managing Hub users, sign-in, spending, and configuration.

<!-- dth:chunk 00216628362e50ef -->
## `People`

Renders the People section of Settings, displaying user management via `Users` and, for owners only, sign-in configuration via `SignIn`. When the pathname is `/sign-in` and the user is an owner, scrolls to the sign-in section on mount. Returns early without the sign-in section for non-owner roles.

<!-- dth:chunk 2161546af390b79d -->
## `Advanced`

Renders the Advanced section of Settings, displaying spending limits via `Spend`, the settings file editor via `SettingsFile`, and a link to the setup checklist for new Hub configurations.
