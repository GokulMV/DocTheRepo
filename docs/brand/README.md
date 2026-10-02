# DocTheRepo logo

**The idea:** five lines of code whose ends trace a **D**. Each line is split into a highlighted token and the
rest, like syntax-highlighted source. Code that becomes documentation.

| File | Use |
|---|---|
| `dth-symbol-color.svg` | Default, on light or dark backgrounds (also the web favicon) |
| `dth-symbol-black.svg` | One colour: print, stamps, embossing, monochrome contexts |
| `dth-symbol-white.svg` | Reversed, on photos or dark brand colour |
| `dth-app-icon.svg` | App and social icon: colour mark on the dark UI surface, 22 % corner radius |

In the web UI the mark is `web/src/components/Logo.tsx` (`<Logo />` for colour, `<Logo mono />` for currentColor).

## Colours

| Role | HEX | Where |
|---|---|---|
| Code (brand blue) | `#3B6EF6` (`#5B8BFF` on dark backgrounds) | the long part of every line |
| Token: amber | `#F59E0B` | lines 1 and 5 |
| Token: pink | `#EC4899` | lines 2 and 4 |
| Token: teal | `#14B8A6` | line 3 |

The token colours are the reason the mark reads as code rather than as a menu icon. Keep them, or go fully
one colour; don't recolour single lines.

## Construction and use

- **Grid:** 256 × 256. Five lines 28 tall with 12 between them; the line ends follow a circle of radius 96. The
  gap between token and code is 10.
- **Clear space:** one line height (28/256 of the mark) on every side.
- **Minimum size:** 16 px on screens. Below 24 px the tokens merge into coloured stripes; that is expected.
- **Wordmark:** "DocTheRepo" set in the UI's sans (Inter, semibold, tight tracking), to the right of the mark,
  cap height about 45 % of the mark's height.
- **Don't:** rotate, outline, add shadows or gradients, change line lengths, or put the colour mark on busy
  backgrounds (use the white version).

The masters contain no live text, filters or rasters. A trademark search hasn't been done; do one before
registering the mark.
