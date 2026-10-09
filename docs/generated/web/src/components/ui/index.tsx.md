<!-- dth:generated source="web/src/components/ui/index.tsx" — edit only inside dth:human blocks -->
# `web/src/components/ui/index.tsx`

This file exports reusable UI components including layout containers (Card, Table, Dialog), feedback elements (IconTile, Empty), and form controls (Button, Input, Textarea, Select) with consistent styling and dark mode support.

<!-- dth:chunk 72ab768a34c7631b -->
## `Card`

Renders a styled container section with optional title and action elements. The header displays the title and actions side-by-side only if at least one is provided. Uses rounded borders, subtle shadows, and responsive dark mode styling.

<!-- dth:chunk a2dd21465fb43771 -->
## `IconTile`

Displays a Lucide icon centered in a tinted rounded square with configurable color tone and size. Maps tone values (brand, green, amber, slate, violet) to background and text colors that adapt for dark mode. Size (sm, md, lg) controls both the container dimensions and icon dimensions.

<!-- dth:chunk 091b605d637528d2 -->
## `Empty`

Shows a centered empty state with an icon, title text, optional descriptive content, and optional action element. Defaults to CircleDashed icon if not provided. Styled with dashed border and semi-transparent background to indicate a blank state.

<!-- dth:chunk 58c9bed9f031fdc5 -->
## `Table`

Renders a responsive horizontal-scrolling table with a header row and tbody. Header cells are uppercase, small text with even spacing; body rows are divided by subtle borders. Accepts an array of header strings and children (typically table rows).

<!-- dth:chunk 9644bfb670e91479 -->
## `Dialog`

Wraps Radix UI Dialog primitives with consistent styling: centered modal overlay with blur, fixed positioning, configurable max height with scroll, and borders. Renders title and optional description; description defaults to title if not provided (for accessibility). Controlled via `open` and `onOpenChange` props.

<!-- dth:chunk 61492abe617ae6e8 -->
## `DialogFooter`

Styles a dialog footer section with action buttons right-aligned and an optional left note. Uses negative margins to extend to dialog edges, rounded bottom corners, and a subtle top border to separate from content.

<!-- dth:chunk 7e7ccab637b0f942 -->
## `__module__`

Re-exports `cx` utility and defines styled form components (Button, Input, Textarea, Select) using Tailwind classes. Button supports four variants (primary, secondary, ghost, danger) and two sizes. Input/Textarea/Select share base field styling with borders and focus states. Also defines color tone mappings (gray, green, amber, red, blue) for badge-like components.
