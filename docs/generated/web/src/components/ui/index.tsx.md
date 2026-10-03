<!-- dth:generated source="web/src/components/ui/index.tsx" — edit only inside dth:human blocks -->
# `web/src/components/ui/index.tsx`

<!-- dth:chunk 2b9089528a20b3d1 -->
## `Badge`

Renders a labeled badge with customizable tone (color). Transforms string content to sentence case or capitalizes the first element if content is an array, so identifiers like "spend_blocked" display as "Spend blocked" while remaining as identifiers elsewhere in the app.

<!-- dth:chunk 08e63345e030ddfa -->
## `Spinner`

Displays a spinning loading indicator with a label, delayed to appear after 300 ms to avoid flashing during quick loads. Uses a spinning circular border animation and includes an accessible status role.

<!-- dth:chunk 61492abe617ae6e8 -->
## `DialogFooter`

Provides a footer section for dialog forms, positioned at the bottom with a top border and light background. Content is right-aligned and separated from dialog fields; an optional `note` appears left-aligned. Automatically adjusts margins and uses dark mode styling.
