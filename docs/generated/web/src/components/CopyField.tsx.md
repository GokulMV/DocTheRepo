<!-- dth:generated source="web/src/components/CopyField.tsx" — edit only inside dth:human blocks -->
# `web/src/components/CopyField.tsx`

<!-- dth:chunk b1ebcb50533cf2d9 -->
## `CopyField`

A read-only input field displaying a string value (like a link or ID) with an adjacent copy-to-clipboard button. The input auto-selects on focus for keyboard convenience. The button copies the value to the clipboard and displays a temporary "Copied" confirmation with a checkmark icon for 2 seconds before reverting to a copy icon. Gracefully handles clipboard API unavailability by catching errors silently.
