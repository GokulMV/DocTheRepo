<!-- dth:generated source="internal/core/sift/navigate.go" — edit only inside dth:human blocks -->
# `internal/core/sift/navigate.go`

<!-- dth:chunk 1c2b06e846f72b86 -->
## `Tree`

Tree is the indexed content a question may read (already restricted by the repository ACL).

<!-- dth:chunk 1e0e9f8ac5fc50c1 -->
## `File`

File is one indexed file.

<!-- dth:chunk fdb1fc225a8511a8 -->
## `dir`

A directory node in the file tree explored by `Navigate`. The `key` identifies the directory (repo name + path), `label` is used for display to the LLM, `children` holds subdirectories, and `files` lists files directly in this directory. `total` counts all files transitively contained in this subtree, used to inform exploration decisions.

<!-- dth:chunk 1c9d2e31f68a4098 -->
## `Sifter.Navigate`

Explores a repository to find chunks relevant to a question, using a person-like exploration strategy: judges directories by their contents, descends only into promising ones, previews candidate files, then reads and scores the best files piece by piece. Returns relevant chunks sorted by score. Skips exploration if the question is not worth answering, fails to read the indexed file tree, or if no files survive judging. Incomplete reports indicate results were truncated at MaxPaths, MaxDirs, MaxPreviews, or MaxReads limits.

<!-- dth:chunk 65a928b94d6f0841 -->
## `Sifter.walk`

Traverses the directory tree level by level, judging directories and descending into the most promising ones. Accumulates files from directories that meet the relevance threshold, processing breadth-first and returning them sorted by judgment score. Stops early if the directory limit (MaxDirs) is reached and sets the Incomplete flag.

<!-- dth:chunk 5df02acd78765e54 -->
## `dirQuestions`

Generates a single judging question asking whether a directory is worth exploring, based on its entry names and file count. The question instructs the LLM that folder names are clues but not rules, and that a partial listing does not guarantee absent files are not useful.

<!-- dth:chunk 45d7f44160e24b23 -->
## `fileQuestions`

Generates a single judging question asking whether a file is likely to contain material answering the question—implementation, configuration, documentation, or tests—based on its path and preview. Files that only mention the topic without substantive content do not count.

<!-- dth:chunk f155fc8b4d1f4850 -->
## `filePreview`

Converts a file and its initial chunks into a judgment item, extracting symbol declarations from the chunks and prefixing the preview text with them if present. Clips the first chunk's content to previewChars length.

<!-- dth:chunk 10e4a62ea5ce4c79 -->
## `buildTree`

Builds a directory tree structure from a flat list of files, grouping files by directory and storing them in `dir` nodes. Creates a synthetic root and handles multiple repositories by including repo names in directory keys and labels. Each node accumulates a total count of all files transitively below it.

<!-- dth:chunk f2d41dcf05d09c9a -->
## `sortedChildren`

Returns the immediate child directories of a `dir` node in alphabetical order by label.

<!-- dth:chunk 5869cca09c39396c -->
## `entries`

Lists the entries (subdirectories and files) in a directory, sorted alphabetically. Subdirectory names are suffixed with `/`. If the list exceeds listEntries, it is truncated and an ellipsis entry indicating the count of omitted items is appended.

<!-- dth:chunk 9a6ec93e27676e41 -->
## `__module__`

Defines tuning constants for the Navigate exploration strategy: MaxPaths limits indexed files considered, DirectFiles skips directory traversal if the repository is small enough, MaxDirs limits the number of directories judged, MaxPreviews limits files previewed before full reading, MaxReads limits files read in full, and previewChars and listEntries control preview and display truncation.
