# Team documents: Notion and uploads

Besides code, generated docs, Confluence and Jira ([confluence-jira.md](confluence-jira.md)), the Hub
reads two more kinds of team documents. Like Confluence pages, they are shared: everyone who can sign in
can read them, Ask cites them, they land on Team docs shelves, and Architecture links them to the services they
mention.

| Source | How documents arrive | Updates |
|---|---|---|
| **Notion** | Pages shared with a Notion integration | Changed pages every 15 minutes; unshared or deleted pages leave daily |
| **Upload** | Markdown, text or HTML files added in Team docs | When someone uploads again (same name in the same collection replaces it) |

## Notion

1. In Notion: **Settings → Connections → Develop or manage integrations → New integration**. Give it
   read access only (no insert or update), then copy the **Internal integration secret**.
2. Share the pages the whole team may read with the integration: on a page, **••• → Connections → add
   the integration**. Sharing a page shares its subpages.
3. In the Hub: **Settings → Connections → Notion**, paste the secret. The first sync starts
   within a minute; **Sync now** runs one immediately.

The Hub sees only what is shared with the integration, so access is managed in Notion. Pages are
converted to Markdown: headings, lists, to-dos, quotes, callouts, code blocks, tables and links. Nested
blocks are read three levels deep; subpages are separate documents. With the API:

```sh
curl -sS -X POST "$HUB/api/v1/connectors" -H "Authorization: Bearer $DTH_TOKEN" -H 'Content-Type: application/json' \
  -d '{"type":"notion","name":"Notion","mode":"poll","credentials":"<integration secret>"}'
```

In Ask, `include: ["notion"]` limits a question to Notion pages; `include: ["knowledge"]` covers every team
source (Confluence, Jira, Notion, uploads).

## Upload documents

**Docs → Team docs → Upload documents** (editors and above). Pick a collection, for example *Runbooks* or
*Onboarding*, and up to 20 files of at most 5 MB each:

| Format | Read as |
|---|---|
| `.md`, `.markdown`, `.mdx` | Markdown |
| `.txt`, `.text`, `.rst` | Plain text |
| `.html`, `.htm` | Converted to Markdown (scripts, styles and navigation dropped) |
| `.pdf`, `.docx`, slides, spreadsheets | Refused with a message: export as Markdown or HTML first |

Uploaded documents are listed under **Uploaded documents** in Team docs, open in the Hub, and are
searched like generated docs (`include: ["docs"]` covers them). Uploading a file with the same name to the
same collection replaces it; **Remove** on the document takes it out of Team docs and search. Uploads
and removals are in the audit log.

```sh
curl -sS -X POST "$HUB/api/v1/library/uploads" -H "Authorization: Bearer $DTH_TOKEN" \
  -F collection=Runbooks -F files=@db-failover.md -F files=@refunds.html
```

## Other platforms

| Platform | Today |
|---|---|
| Google Docs, Microsoft Word / SharePoint | Download as HTML, Markdown or plain text and upload |
| GitHub or GitLab wikis, docs folders in repositories | Track the repository; **Repositories → Import docs** brings existing Markdown in |
| Docs sites (Docusaurus, MkDocs, Read the Docs) | Their Markdown source usually lives in a repository: track it, or upload the files |
