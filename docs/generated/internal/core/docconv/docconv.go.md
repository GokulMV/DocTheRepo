<!-- dth:generated source="internal/core/docconv/docconv.go" — edit only inside dth:human blocks -->
# `internal/core/docconv/docconv.go`

<!-- dth:chunk 7d39a1bd5b608285 -->
## `Convert`

Converts document files to Markdown and extracts a title. Accepts `.md`, `.markdown`, `.mdx`, `.txt`, `.text`, `.rst`, `.html`, and `.htm` files, rejecting binary formats (`.pdf`, `.docx`, etc.) and files larger than 5 MB or containing invalid UTF-8. For Markdown-like formats, returns the first heading (or sanitized filename) as title. For HTML, extracts title from the `<title>` tag if present, otherwise uses the first heading from converted content. Returns an error for unsupported file types or invalid input.

<!-- dth:chunk 461505704b0b5d66 -->
## `firstHeading`

Extracts the first H1 or H2 heading from Markdown using a regex pattern, returning it trimmed. If no heading is found, returns the fallback string with hyphens and underscores replaced by spaces and trimmed, allowing filenames to serve as fallback titles.

<!-- dth:chunk b3887a328e20888d -->
## `fromHTML`

Parses HTML to Markdown, extracting a title from the `<title>` tag and converting page content. Drops `<script>`, `<style>`, `<nav>`, `<footer>`, `<noscript>`, and `<svg>` elements entirely. Converts headings, paragraphs, code blocks (preserving raw text), lists (unordered and ordered, nested), blockquotes, tables, and horizontal rules to their Markdown equivalents. Collapses multiple newlines into double newlines for clean output. Falls back to raw text as-is if HTML parsing fails.

<!-- dth:chunk ddf04bc8cd43f8a2 -->
## `inline`

Recursively renders an HTML node's inline content as Markdown. Handles text nodes directly, skips `<script>` and `<style>`, converts `<br>` to newlines, wraps `<strong>`/`<b>` in `**`, `<em>`/`<i>` in `*`, and `<code>` in backticks. For `<a>` tags, creates `[label](href)` links only if href is non-empty, non-JavaScript, and has non-empty label text; otherwise returns just the label. Other elements recursively process their children.

<!-- dth:chunk c6536d08c91a35bb -->
## `wrap`

Wraps a string with Markdown markers (e.g., `**text**` for bold) only if the trimmed string is non-empty; returns the original string unchanged if empty to avoid empty markup like `****`.

<!-- dth:chunk 424a93f1deec2b02 -->
## `attr`

Retrieves an HTML attribute value by key from a node, returning the value if found or an empty string if not present.

<!-- dth:chunk 6ed97ec791d5005a -->
## `__module__`

Module constants and variables: `MaxBytes` limits input to 5 MB, `Supported` lists all accepted file extensions, and `headingRE` is a compiled regex matching H1/H2 Markdown headings for title extraction.
