<!-- dth:generated source="internal/core/repodocs/modules.go" — edit only inside dth:human blocks -->
# `internal/core/repodocs/modules.go`

Partitions a repository's indexed files into logical documentation modules based on directory structure and code size.

<!-- dth:chunk 7183a8e5393018cb -->
## `Module`

Represents a logical unit of a repository, corresponding to either a complete directory subtree or just the direct files in a directory (with subfolders as separate modules). It groups indexed code and test files together with metadata for documentation purposes.

<!-- dth:chunk 61bf269931d86408 -->
## `SplitOptions`

Configuration for dividing indexed code into modules, specifying acceptable line count ranges per module (MinLines, MaxLines) and a preferred maximum number of modules (Target).

<!-- dth:chunk 823481d3bf9767d4 -->
## `dirNode`

Internal tree node representing a directory during module construction, tracking the files directly in that directory, their line counts, cumulative totals, and child directory nodes.

<!-- dth:chunk 36f1f30b72f91b0a -->
## `dirNode.child`

Lazily initializes and returns a child node by name, creating it if it doesn't already exist in the directory tree.

<!-- dth:chunk 7356e969e68841d4 -->
## `dirNode.sum`

Recursively computes and caches the total line count for a directory node and all its descendants, returning the sum.

<!-- dth:chunk e8a212dda8c122e1 -->
## `dirNode.path`

Constructs the directory path string for a node by joining its name with a prefix, handling the repository root case specially.

<!-- dth:chunk 7d8d12d7da9aceb7 -->
## `Split`

Partitions indexed files into modules based on size constraints, excluding test files which are attached separately. Iteratively adjusts size thresholds if the initial split produces too many modules, then merges undersized modules and assigns stable slug-based keys.

<!-- dth:chunk cfd39a21abfd0cdb -->
## `walk`

Recursively traverses the directory tree to create modules, either emitting a whole subtree as one module if it fits the size limit, or keeping local files with small subdirectories together and recursing into larger subdirectories.

<!-- dth:chunk befb777ff5f88228 -->
## `joinDir`

Joins a directory path with a name component, handling the root directory (".") as a special case.

<!-- dth:chunk c26c189c7884abce -->
## `allFiles`

Collects all files from a directory node and its descendants into a flat list.

<!-- dth:chunk 421ea9de3f96dec1 -->
## `mergeSmall`

Iteratively merges undersized modules into their closest containing ancestor module, improving the overall module structure by eliminating orphaned small fragments.

<!-- dth:chunk b2fa9e74239411ba -->
## `isAncestor`

Checks if directory `a` is an ancestor of directory `b` (either equal, ".", or `b` under `a`).

<!-- dth:chunk 0696ad33d58ff2c5 -->
## `commonDepth`

Counts the number of matching path components between two directory paths from the root.

<!-- dth:chunk 9286f13caa868415 -->
## `attachTests`

Assigns test files to modules by matching them to their nearest code module via directory path or by mirroring test directories (e.g., `tests/foo` matches `foo`).

<!-- dth:chunk 7968504dcf279137 -->
## `ModuleOf`

Creates a map from file paths to their assigned module keys for quick lookups.

<!-- dth:chunk 523edbde8dd49679 -->
## `slug`

Converts a directory path to a lowercase URL-safe slug, replacing non-alphanumeric characters with hyphens and handling the root directory specially.

<!-- dth:chunk 5d924cbb2c5eab24 -->
## `__module__`

Default module splitting configuration: modules should be at least 300 lines, at most 15000 lines, aiming for about 25 modules total.
