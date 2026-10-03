<!-- dth:generated source="web/src/components/ArchitectureDiagram.tsx" — edit only inside dth:human blocks -->
# `web/src/components/ArchitectureDiagram.tsx`

<!-- dth:chunk 37aac490ac9fbf0e -->
## `ArchNode`

Represents a node in an architecture diagram. Identifies a component or grouped set of components with metadata for layout (layer, position) and visualization (kind, degree). Optional `count` and `items` describe how many entities a group node represents; `degree` tracks connection count for weighting in the diagram.

<!-- dth:chunk 78295d7eefd19217 -->
## `nodeSubtitle`

Returns a subtitle for an architecture diagram node, describing its type and aggregated count. For endpoint or dependency groups, formats a count with appropriate singular/plural wording ("endpoint"/"endpoints", "library"/"libraries"); otherwise returns the provided label.

<!-- dth:chunk 792b3c8760cdba71 -->
## `ArchitectureDiagram`

Renders an interactive architecture diagram displaying nodes organized in layers (columns) with directed links between them. Computes layout geometry including node positions, lane routing for long-distance links, and handles hover/selection interaction to highlight connected nodes. Supports zoom-to-fit or 100% view, and renders SVG with markers, paths, boxes, and labels. The `hidden` parameter allows collapsing layers to show truncated counts.
