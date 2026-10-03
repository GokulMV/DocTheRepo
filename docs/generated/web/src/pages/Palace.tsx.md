<!-- dth:generated source="web/src/pages/Palace.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Palace.tsx`

Palace page component for displaying and exploring an interactive knowledge graph of repositories, services, and infrastructure extracted from code.

<!-- dth:chunk a307b19aa3a5e2d5 -->
## `Summary`

Renders a summary card listing the most connected repositories and services in the graph, sorted by activity (number of outgoing edges plus endpoint count). For each entity, describes its interactions using verb phrases (e.g., "publishes to", "calls", "uses") and shows the count of exposed endpoints. Shows up to 12 items; if an entity has no links or endpoints, displays a placeholder message. Accepts nodes, edges, endpoints map, and a selection callback.

<!-- dth:chunk fffc62630c7ba57b -->
## `HowToRead`

A dismissible explanation card that describes how to read the knowledge graph. Persists dismissal state to localStorage (via `EXPLAINER_KEY`) so users see it only once per browser. Only rendered in overview mode when entities exist. Contains three explanatory paragraphs about entity types, edge meanings (relationship types like "publishes to" and "calls"), and interaction methods (click for details, focus mode).

<!-- dth:chunk b91b570402b6ad30 -->
## `Palace`

Main Palace page component displaying an interactive knowledge graph of repositories, services, endpoints, datastores, and topics. Supports two modes: overview (all entities) and focus mode (single entity and its neighbors). Provides controls for filtering by entity kind, changing graph layout (force/concentric/hierarchy/circle/grid), adjusting neighborhood depth (focus mode only), searching entities, and selecting/deselecting nodes. Shows different panels based on mode: details sidebar when a node is selected, or a summary card in overview mode. Renders loading states, error messages, and an empty state when no data exists.

<!-- dth:chunk 97dd4a8f754a9ded -->
## `__module__`

Module-level constants: `LAYOUTS` defines available graph layout algorithms with display names; `VERBS` maps edge kinds to human-readable verb phrases describing relationships (e.g., "publishes to", "calls", "uses"); `EXPLAINER_KEY` is the localStorage key for persisting the dismissal state of the how-to-read explainer card.
