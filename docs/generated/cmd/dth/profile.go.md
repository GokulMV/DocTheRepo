<!-- dth:generated source="cmd/dth/profile.go" — edit only inside dth:human blocks -->
# `cmd/dth/profile.go`

<!-- dth:chunk 832740064fb8b15a -->
## `app.profileCmd`

Constructs a Cobra command for profile management, enabling users to list, switch between, and remove named hub profiles (e.g., nonlive, production). The parent command lists all profiles with their server URLs, marking the current profile with an asterisk; running without arguments shows available profiles. Two subcommands are included: `use NAME` switches the current profile (returns error if profile doesn't exist), and `remove NAME` deletes a profile and its stored token, clearing the current profile if it was active. All operations load and save the configuration to disk.
