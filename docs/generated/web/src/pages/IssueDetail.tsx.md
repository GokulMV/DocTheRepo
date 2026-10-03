<!-- dth:generated source="web/src/pages/IssueDetail.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/IssueDetail.tsx`

<!-- dth:chunk e2a496f96e2bcfcb -->
## `MarkKnownDialog`

A dialog that creates a rule to mark an issue as known. Takes editable title and reason fields, with optional expiration in days; the reason defaults to 'expected_noise' if the issue suggests a known issue, otherwise 'known_bug'. On submit, posts to `/issues/{id}/mark-known` and invalidates related query caches. The dialog description indicates the scope (service and environment) of the rule being created.
