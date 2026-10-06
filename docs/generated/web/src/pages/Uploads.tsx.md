<!-- dth:generated source="web/src/pages/Uploads.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Uploads.tsx`

Page component for viewing a single uploaded document with deletion capability.

<!-- dth:chunk 066a188fc6382554 -->
## `UploadView`

### UploadView

Displays a single uploaded document with metadata and allows authorized users to delete it. Fetches the document via its route parameter `id`, shows its title, collection, upload timestamp, and uploader. The markdown body renders in a card if present, otherwise shows an empty state. Deletion requires editor role or higher; the delete operation removes the document from uploads, invalidates the upload list cache, and navigates back to `/library`. Errors during deletion are displayed inline.
