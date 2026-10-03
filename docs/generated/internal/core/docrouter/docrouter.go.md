<!-- dth:generated source="internal/core/docrouter/docrouter.go" — edit only inside dth:human blocks -->
# `internal/core/docrouter/docrouter.go`

<!-- dth:chunk 70720d6ffd3cb9b1 -->
## `Tier`

Tier is how a chunk gets its doc.

<!-- dth:chunk 05a2e36cc5b9b988 -->
## `Mode`

Mode is the cost/thoroughness setting.

<!-- dth:chunk 0fb34bb7215ba310 -->
## `ParseMode`

Parses a string into a Mode, normalizing whitespace and case. Returns Balanced as the default for unrecognized input.

<!-- dth:chunk 777482b38ecb28e8 -->
## `Key`

Key identifies a doc by the code it describes.

<!-- dth:chunk 5f7765810c88b98a -->
## `KeyOf`

KeyOf is a chunk's cache key.

<!-- dth:chunk dcaa7b1b0170b84a -->
## `Decision`

Decision represents the router's choice for documenting one chunk, specifying the generation tier, the reason for that choice, and for no-call tiers (Reuse and Comment), the precomputed doc Body.

<!-- dth:chunk d5959c547d9c341d -->
## `limits`

limits per mode: code lines at or below which a commented chunk uses its comment, and at or below which the fast model writes the doc.

<!-- dth:chunk 35e3505faf62729c -->
## `Decide`

Routes a single chunk to a generation tier based on mode limits and code characteristics. Returns Reuse if cached doc exists for identical code; Comment if the doc comment meets length thresholds; Fast if code is short enough and fastRouted is true; otherwise Full. Relies on modeLimits map to interpret mode.

<!-- dth:chunk 4befa7fa453bfcaf -->
## `countLines`

Counts non-empty, non-brace lines in code. Used to evaluate code brevity for routing decisions. Ignores lines that are only whitespace, braces, or closing parens.

<!-- dth:chunk effa030b891e26a3 -->
## `Split`

Separates a chunk's leading documentation from its code. Handles block comments (/* **...*/) and line-style comments (// # --, etc.); for Python or when triple-quotes appear, also extracts Python docstrings. Returns plain-text comment and remaining code.

<!-- dth:chunk 0a5ccd2c05a565ed -->
## `clean`

Joins comment lines into paragraphs, stripping lint directives (nolint, eslint-, go:, @ts-, noqa), empty lines, and whitespace. Consecutive non-empty lines become a single space-joined paragraph, separated from others by blank lines.

<!-- dth:chunk 7342cf54a0dc44d7 -->
## `__module__`

Module-level constants and variables: Tier values (Reuse, Comment, Fast, Full) and Mode values (Thorough, Balanced, Economy); modeLimits map defines comment-line and fast-line thresholds per mode; regex patterns for Python docstrings, block comments, and line-style comment markers.
