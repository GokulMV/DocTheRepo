<!-- dth:generated source="web/src/index.css" — edit only inside dth:human blocks -->
# `web/src/index.css`

Global stylesheet defining theme variables, Tailwind CSS v4 configuration, and base application styles including dark mode support and component defaults.

<!-- dth:chunk f7c357d291025e78 -->
## `web/src/index.css`

Configures the global stylesheet for the application using Tailwind CSS v4 with custom theme variables and compatibility adjustments.

Defines a custom brand color palette (shades 50-950), typography settings for sans-serif (Inter) and monospace fonts, and a subtle card shadow. Implements a `dark` custom variant for dark mode support. Adds Tailwind v4 compatibility styles to preserve v3's default border-color behavior on form elements and other components by explicitly setting them to `--color-gray-200`. Applies base styles to the body element with antialiased text, light/dark mode backgrounds, and branded font features. Includes a decorative fixed radial gradient overlay positioned behind page content (adjusted for dark mode). Configures custom scrollbar styling and adds a delayed-appear animation (150ms, 300ms delay) for loading indicators to reduce visual flashing, with graceful fallback for motion-preference users.
