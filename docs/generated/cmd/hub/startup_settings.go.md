<!-- dth:generated source="cmd/hub/startup_settings.go" — edit only inside dth:human blocks -->
# `cmd/hub/startup_settings.go`

<!-- dth:chunk 9323ebef3567b136 -->
## `applyStartupSettings`

Applies settings files specified in config (DTH_SETTINGS_FILE, DTH_SETTINGS) through the Hub's API as a system principal. Runs idempotently on startup in a goroutine, retrying up to 20 times with 30-second intervals on failure; failures are logged but never stop the Hub. Combines settings from specified file paths with optional inline config, loads them, and applies changes via the Hub's internal API, logging the counts of created, updated, and unchanged settings when changes occur.

<!-- dth:chunk 96dd7d2edd28f2c9 -->
## `withFiles`

Filters a list of paths to include only those that are files or contain at least one settings file (with .yaml, .yml, or .json extension). Passes through paths that fail to read as directories, letting the caller's error handling report missing files or permission issues.

<!-- dth:chunk 315ef951933cf661 -->
## `__module__`

Interval duration (30 seconds) between retry attempts when applying startup settings fails.
