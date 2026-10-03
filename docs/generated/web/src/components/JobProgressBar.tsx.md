<!-- dth:generated source="web/src/components/JobProgressBar.tsx" — edit only inside dth:human blocks -->
# `web/src/components/JobProgressBar.tsx`

<!-- dth:chunk 3f7903c75ee0f848 -->
## `JobProgressBar`

Renders a progress bar for a running job showing current stage and completion percentage. Returns `null` if the job is not processing or lacks progress data. Displays "Documenting X of Y files" during processing and "Writing docs Z files" during the writing stage, with the percentage shown only during processing. The bar animates with a pulsing effect while writing, otherwise shows a static width proportional to completion percentage.

<!-- dth:chunk 3cca3baa668aef30 -->
## `__module__`

Maps job stage identifiers to human-readable display strings: `documenting` → "Documenting" and `writing` → "Writing docs".
