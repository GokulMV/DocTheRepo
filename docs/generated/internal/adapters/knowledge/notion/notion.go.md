<!-- dth:generated source="internal/adapters/knowledge/notion/notion.go" — edit only inside dth:human blocks -->
# `internal/adapters/knowledge/notion/notion.go`

<!-- dth:chunk 61e89aeb4c76f08d -->
## `Source`

Source is the Notion adapter. Base is the API root (tests point it at a fake server).

<!-- dth:chunk a0e1eeb3c1cd7684 -->
## `New`

New returns the adapter.

<!-- dth:chunk bfec255969997dd7 -->
## `Source.Type`

Type implements ports.KnowledgeSource.

<!-- dth:chunk c2b6c2b6da8fdc27 -->
## `Source.Streams`

Streams implements ports.KnowledgeSource: one stream, every page shared with the integration.

<!-- dth:chunk 689336dc53e174b1 -->
## `Source.headers`

Constructs HTTP headers for Notion API requests, including Bearer token authorization and API version specification. Returns a validation error if the credentials token is missing, indicating where to obtain it from Notion's settings.

<!-- dth:chunk 2f74a482f1e9c4b5 -->
## `Source.call`

Executes JSON HTTP requests to the Notion API using configured headers and base URL. Wraps httpx.JSON to provide Notion-specific error handling: returns a validation error for HTTP 401 (invalid token) and enhances 404 errors with guidance to share the page with the integration.

<!-- dth:chunk fe9f0650449e7792 -->
## `page`

Represents a Notion page retrieved from the API, containing metadata (ID, URL, edit time, archived/trash status) and a map of properties including title and other attributes.

<!-- dth:chunk 4a329b474dc1738b -->
## `property`

Represents a property object within a Notion page, with a type indicator (e.g., "title") and rich text content.

<!-- dth:chunk 09c1f474f4d2cad3 -->
## `richText`

Represents a rich text element with plain text content, optional hyperlink, and inline formatting annotations (bold, italic, code, strikethrough).

<!-- dth:chunk 6fcf7d0eb668a27c -->
## `page.title`

Extracts the page title from its properties by finding the property with type "title" and converting its rich text to plain text, returning "Untitled" if no title property exists.

<!-- dth:chunk aa4020116bc05831 -->
## `searchResp`

Represents a paginated response from Notion's search endpoint, containing page results, a flag indicating more results exist, and a cursor for pagination.

<!-- dth:chunk bd51465bf2c3bc9a -->
## `Source.pages`

Retrieves all shared Notion pages sorted by most recent edit first, excluding archived and deleted pages. Paginates through results using cursors, invoking the stop callback on each page to allow early termination. Returns pages that pass the stop condition.

<!-- dth:chunk 2c7a2bb7b23fc6a2 -->
## `Source.Changed`

Implements the KnowledgeSource interface to emit changed pages since a given cursor (RFC3339Nano timestamp). Fetches pages edited after the cursor time, sorts them oldest-first, batches them by 20 to limit crash recovery overhead, and emits each batch with the last page's edit time as the new cursor.

<!-- dth:chunk 95f47e08c0f3bbe8 -->
## `Source.IDs`

Implements the KnowledgeReconciler interface to return all currently shared page IDs, enabling removal of pages that are no longer shared or have been deleted from the knowledge base.

<!-- dth:chunk edfecc778c061e65 -->
## `Source.Labeled`

Labeled implements ports.KnowledgeSource. Notion pages carry no labels the Hub imports.

<!-- dth:chunk 0ad99576ea4e29b9 -->
## `Source.Owns`

Determines if a URL belongs to Notion by checking for notion.so, www.notion.so, or *.notion.site hostnames. Returns false if the URL cannot be parsed.

<!-- dth:chunk 9a4c9e4c97a89357 -->
## `Source.Fetch`

Fetches a single Notion page from a browser link URL by extracting the page ID via regex and retrieving it from the API. Returns a validation error if the link is unparseable or lacks a page ID, and delegates to doc() to convert the page to a KnowledgeDoc.

<!-- dth:chunk c21bd744b56886a7 -->
## `Source.doc`

Converts a Notion page to a KnowledgeDoc by recursively fetching all child blocks and rendering them as Markdown, then combining with the page's metadata (ID, title, URL, edit time).

<!-- dth:chunk e9f3a65eee675fd6 -->
## `blocksResp`

Represents a paginated response from Notion's blocks endpoint, containing raw block data, a flag for additional results, and a pagination cursor.

<!-- dth:chunk 593722646b2c35f2 -->
## `Source.blocks`

Recursively writes a block's children as Markdown with indentation based on depth. Processes up to maxBlocks total blocks and up to maxDepth nesting levels, tracking numbered list item positions. Fetches subsequent pages of blocks using pagination cursors.

<!-- dth:chunk 6f520ab2ef37261d -->
## `writeBlock`

Renders a single Notion block type as Markdown, handling headings, lists, to-dos, quotes, code blocks, tables, and paragraphs. Applies indentation for nested blocks and formats numbered list items with their position. Preserves plain text for code blocks and pipes in table cells.

<!-- dth:chunk cced994b5e2c83fc -->
## `richTexts`

Converts a raw interface value (from JSON) into a slice of richText objects by extracting plain text, href, and annotation properties from each item, safely handling type assertions.

<!-- dth:chunk 711049b925d596c8 -->
## `plain`

Concatenates the plain text content of multiple rich text elements into a single string, stripping all formatting.

<!-- dth:chunk 8490ce0d58480cbd -->
## `markdown`

Converts rich text elements to Markdown by applying inline formatting (bold, italic, code, strikethrough) and wrapping hyperlinks in Markdown link syntax. Preserves whitespace-only segments as-is.

<!-- dth:chunk ac4f57c2c3bd45ed -->
## `__module__`

Module-level constants: Notion API version (2022-06-28), knowledge base space name ("notion"), maximum recursion depth (3 levels), maximum block count per page (2000), and a regex pattern to extract Notion page IDs from URLs in either UUID or UUID-with-hyphens format.
