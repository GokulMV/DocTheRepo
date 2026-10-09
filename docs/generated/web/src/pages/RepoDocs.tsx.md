<!-- dth:generated source="web/src/pages/RepoDocs.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/RepoDocs.tsx`

Renders the repository documentation viewer and editor, displaying auto-generated documents with confidence scoring, citations linked to source code, and admin controls for generation and budgeting.

<!-- dth:chunk a02e24187786331e -->
## `listKey`

Creates a stable query key for the repository documents list using the repository ID.

<!-- dth:chunk a5d4558fd4e72bdb -->
## `useRepoDocs`

Fetches the list of generated documents for a repository. Auto-refetches every 3 seconds while documents are being written (job status is 'queued' or 'processing'), otherwise relies on manual invalidation.

<!-- dth:chunk b8d6371c2e01731c -->
## `useRepoDoc`

Fetches a single document by repository, document type, and key. Requires both `repoId` and `type` to be defined before querying.

<!-- dth:chunk cc02be04fa486bf9 -->
## `ConfidenceBadge`

Displays a clickable badge showing document confidence level (high/medium/low) with percentage, opening a dialog on click that explains how the score is calculated, including specific concerns if any, and whether the calibration is AI-validated.

<!-- dth:chunk daf8dda983516d60 -->
## `codeURL`

Generates a direct link to cited code on GitHub or GitLab based on repository connector type, file path, and line number; returns undefined if the repository is not found or its host is not supported.

<!-- dth:chunk b40890ab0f8c4037 -->
## `withCitations`

Transforms citation syntax `[path:line]` in markdown into clickable links to source code, falling back to inline code if no URL is available.

<!-- dth:chunk 34e48bd279ccb55b -->
## `DocView`

Renders the full document view with title, confidence badge, sections with citations, gaps, and a sticky on-page navigation. Shows warnings if code changed significantly since writing or if generation failed.

<!-- dth:chunk 3ff5b36b3b0fcfee -->
## `DocNav`

Renders a sidebar navigation of document summaries grouped by category (Basics, Modules, Interfaces, etc.), with visual indicators for confidence level or error status and highlighting for the currently active document.

<!-- dth:chunk 0fe6a9e1163332f5 -->
## `Writing`

Shows a dismissable status banner indicating document generation progress—either queued, reading code, or writing documents with stage and completion count when available.

<!-- dth:chunk 7e3cb697039bfa77 -->
## `Actions`

Displays document budget tracking (spent vs. monthly cap), and admin-only controls to adjust budget, write changed docs, rewrite all, or export documents to a pull request.

<!-- dth:chunk d18f79aa080d298a -->
## `Empty`

Shows an empty state when no documents exist, with contextual messaging and actions: prompts code sync if not yet done, points to model setup if docgen is not configured, or offers to estimate and generate documents if prerequisites are met.

<!-- dth:chunk a147f46a2d207d91 -->
## `RepoDocsPage`

The main Docs page component for the v2 document system (per-repository). Displays document navigation, the selected document view, generation status, and admin controls; auto-redirects to the first document on initial load.

<!-- dth:chunk d31a7694ae587651 -->
## `__module__`

Module constants: `GROUPS` defines document category order; `TONE` maps confidence levels to badge colors; `DOT` maps confidence to indicator dot colors; `CITE` regex matches citation syntax `[path:line]` to extract file paths and line numbers.
