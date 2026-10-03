<!-- dth:generated source="web/src/pages/Library.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Library.tsx`

Page component for browsing and managing curated shelves of documentation organized by topic.

<!-- dth:chunk 53edeafc5ff37c53 -->
## `NewShelf`

Modal dialog to create a new curated shelf. Manages form state for title, slug, and description fields, auto-generating a URL-safe slug from the title. On submit, posts the new shelf to the API and closes the dialog. Displays API errors and disables the create button while the request is pending.

<!-- dth:chunk 05a9576549867e6b -->
## `Library`

Main library page displaying curated shelves or a single shelf's contents. When no slug is provided, shows a grid of all shelves (filterable by empty status) with item counts and curation badges; includes upload and new shelf actions for editors. When a slug is provided, displays the shelf's items as a linked list with type-specific badges (Confluence, Jira, etc.), pinned indicators, summaries, and notes. Renders appropriate links based on item type: internal docs/uploads, external URLs, or palace entities.
