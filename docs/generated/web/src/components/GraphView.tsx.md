<!-- dth:generated source="web/src/components/GraphView.tsx" — edit only inside dth:human blocks -->
# `web/src/components/GraphView.tsx`

<!-- dth:chunk 259045760b22c8df -->
## `layoutOptions`

Returns Cytoscape layout configuration for the specified layout type. Disables animation to prevent crashes when the component unmounts mid-layout. The `n` parameter scales iteration count for force-directed layouts (capping at 1500 for small graphs, 400 for large ones). Spacing factors account for node labels to prevent overlap. The optional `focus` parameter sets the root node for hierarchy layouts using CSS-escaped IDs.

<!-- dth:chunk 50b417360c9a368f -->
## `GraphView`

Renders an interactive graph visualization of Palace entities using Cytoscape. Nodes are styled by kind (shape and color) and sized by degree. Hovering highlights the node's neighborhood and adjacent edges. Clicking selects nodes, and the query prop highlights matching nodes by label substring (case-insensitive). Supports multiple layout algorithms and auto-fits small graphs. Properly cleans up the layout and Cytoscape instance on unmount to avoid renderer crashes. Manages selection and query highlights through separate effects to handle prop updates independently.
