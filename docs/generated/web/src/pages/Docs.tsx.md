<!-- dth:generated source="web/src/pages/Docs.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Docs.tsx`

<!-- dth:chunk d716fc71bc748d10 -->
## `NodeIcon`

Returns an icon representing the node type: repository (FolderGit2, brand blue), directory (Folder or FolderOpen, amber, opening/closing based on state), section (Hash, slate), or file (FileText, slate). All icons are marked as decorative with aria-hidden.

<!-- dth:chunk 3b46557f683dc5e2 -->
## `Explorer`

Manages the explorer's open/closed state for tree nodes. Nodes are open when their default state (folders open after Expand all) is flipped by user interaction. `isOpen` checks the current state, `toggle` flips it, and `reveal` (a counter) triggers scroll-into-view when a selected row changes.

<!-- dth:chunk 5d356473e955e3b6 -->
## `isFolder`

Predicate that returns true for repository and directory nodes, used to identify which nodes are expandable folders in the tree.

<!-- dth:chunk bb729cd36a8bad65 -->
## `useExplorer`

Creates explorer state management. Tracks which nodes are manually flipped from default (stored in `flipped` Set), whether all folders are expanded by default (`all`), and a reveal counter. `expandAll` and `collapseAll` reset state; `locate` opens all ancestors and increments reveal to trigger scroll-into-view, enabling navigation to a specific node.

<!-- dth:chunk 5d45fad8f5d35334 -->
## `Branch`

Renders one tree row (repository, directory, section, or file). Loads children on demand when opened and expandable. Highlights the active (selected) row and scrolls it into view when `ex.reveal` changes. Clicking toggles expandable nodes, clicking files calls `onSelect`. Displays chevron, icon, title, and indent guides.

<!-- dth:chunk b382b464ec9fd2a1 -->
## `NoDocs`

Displays messaging and next steps when docs are unavailable. Shows a 3-step setup guide (connect GitHub, track repos, push or import) for new users, or status about ongoing doc generation jobs with error messages and action buttons. Only admins see provider setup; the "Generate docs now" button generates docs for all tracked repos immediately.

<!-- dth:chunk 80c1b944ff972a81 -->
## `Docs`

Entry point for the docs page. Fetches root tree nodes and the selected doc page. Routes to `DocsBrowser` if roots exist, otherwise shows loading or calls `NoDocs` for empty state. Navigates by URL param nodeId.

<!-- dth:chunk 372030faf7106e2f -->
## `DocsBrowser`

Main documentation browser component with two-column layout: left sidebar tree explorer (with expand/collapse and locate buttons) and right content panel. Handles node selection, loading states, and reveals the selected page in the tree on first render. Explorer state persists across selections so folder state survives navigation.

<!-- dth:chunk a6e2be216266ab4d -->
## `__module__`

Module-level constants: `rowCls` for tree row styling, `ExplorerCtx` React context providing explorer state to tree nodes, `DESCRIPTION` page description noting dth:human blocks survive regeneration, and `toolBtn` for toolbar button styling.
