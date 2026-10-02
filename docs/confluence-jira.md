# Confluence and Jira

The Hub syncs Confluence spaces and Jira projects **read-only**. Synced pages and issues are used for:

- **Ask**: answers can cite them (`include: confluence` covers both).
- **Decodes**: runbooks are used for explanations.
- **The Library**: shelves for Confluence, Jira, Runbooks and Decisions, plus the shelves your labels match.
- **The Palace**: links from services to the pages that document them.
- **Known issues**: labelled issues and pages are imported as draft rules.

Cloud and Data Center are both supported.

## Connect

In the UI, go to **Connectors → Add knowledge source** and pick Confluence or Jira. With the API:

```sh
# Confluence Cloud: e-mail + API token. Data Center: omit email and use a personal access token.
curl -sS -X POST "$HUB/api/v1/connectors" -H "Authorization: Bearer $DTH_TOKEN" -H 'Content-Type: application/json' -d '{
  "type": "confluence", "name": "Wiki", "credentials": "<api token>",
  "config": {"base_url": "https://acme.atlassian.net/wiki", "spaces": "ENG, OPS", "email": "hub-bot@acme.com"}}'

curl -sS -X POST "$HUB/api/v1/connectors" -H "Authorization: Bearer $DTH_TOKEN" -H 'Content-Type: application/json' -d '{
  "type": "jira", "name": "Jira", "credentials": "<api token>",
  "config": {"base_url": "https://acme.atlassian.net", "projects": "ENG, OPS", "email": "hub-bot@acme.com"}}'
```

| Key | Confluence | Jira | Notes |
|---|---|---|---|
| `base_url` | ✔ | ✔ | Confluence Cloud: `https://<site>.atlassian.net/wiki`. Jira Cloud: `https://<site>.atlassian.net`. |
| `spaces` / `projects` | ✔ | ✔ | Comma-separated keys. |
| `email` | optional | optional | Cloud only. Without it, the credential is sent as a bearer personal access token (Data Center). |
| `known_issue_label` | optional | optional | Defaults to `known-issue`. Set it to `-` to turn off known-issue import. |
| `timezone` | optional | optional | The account's time zone (IANA name). Jira reads it from the account profile if you leave it out. Without it, Confluence re-reads the last 14 hours on each sync to be safe. |
| `jql` | | optional | An extra filter combined with every query using AND, e.g. `issuetype in (Bug, Incident)`. |
| `lookback_days` | | optional | How far back the first sync reads. Defaults to 365. |
| `api_version` | | optional | `3` for Cloud (the default when `email` is set), `2` for Data Center. |

Use a dedicated read-only account and sync only spaces and projects that **every Hub user may read**. Synced
content belongs to no repository, so every Hub viewer can see it. The Hub does not mirror per-page
Confluence or Jira permissions.

## How sync works

- **Schedule.** Each connector syncs every 15 minutes (`poll_seconds`, default 900). The **Sync now**
  button on the Connectors page queues one sync immediately.
- **What it reads.** Confluence uses a CQL search per space for pages modified since the cursor, with the
  `storage` body converted to Markdown. Jira uses a JQL search per project for issues updated since the
  cursor, reading the description and the last 20 comments (rich text converted to Markdown).
  - The cursor is stored after each page of results is committed. A crash repeats at most one page of
    results, and writes are idempotent.
- **Indexing.** Unchanged content is not re-embedded (the same manifest diff as code). Headings become chunk
  boundaries.
- **Palace links.**
  - A service, repository, or endpoint named in a page gets a `documented_in` link to it. Names must match
    exactly: whole words, at least 4 characters, endpoints written as `POST /orders`.
  - Pages labelled `runbook`, `playbook`, or `oncall` (or titled "…runbook…") also link `runbook_for` back.
- **Deletions.** Once a day the Hub lists each space's page IDs and removes pages that disappeared. If an
  upstream glitch returns an empty list, nothing is removed. Jira deletions are not reconciled.
- **Spend.** Embeddings and rule proposals go through the spend guard. If either is blocked, the sync still
  finishes: pages stay searchable by keyword, and the job result notes what was skipped.

## Known issues from labels

Each sync also queries issues and pages carrying the known-issue label:

- **New labelled document.** It becomes a **disabled draft rule**. The suggest route proposes a match from
  recent Inbox issues, and the draft carries the ticket link, the upstream status, and the upstream text.
  Nothing is suppressed until a person reviews and enables it.
- **Jira issue moves to Done.** A suppressing rule tied to it flips to **label only**, with the note "Fixed
  upstream … verify". This also applies to rules saved from a link. The errors show up in the Inbox again,
  so you can confirm the fix worked.
- **Label removed.** A label-imported rule flips to label only in the same way.
- **After you change it back.** If someone turns suppression back on after a flip, later syncs leave it
  alone.

## Explain a link

On Known Issues, open **From text** and paste a Jira issue or Confluence page URL. The Hub fetches it
through the connector whose site owns the URL and returns the same proposal as for pasted text, plus the
document itself. Saving the rule keeps its origin (Jira key or page ID, ticket URL, upstream text).

```sh
curl -sS -X POST "$HUB/api/v1/known-issues/from-link" -H "Authorization: Bearer $DTH_TOKEN" -H 'Content-Type: application/json' \
  -d '{"url":"https://acme.atlassian.net/browse/ENG-412"}'
# → {explanation, reason, proposed_match, confidence, candidates, matching_issues_last_7d, sample_issue_ids,
#    link: {source, external_id, title, url, status, done, jira_key | confluence_page_id, source_text}}
```

| Error | Status | Code |
|---|---|---|
| No Confluence/Jira connector matches the URL | 400 | `NO_CONNECTOR` |
| Page or issue not found, or the connector's account cannot see it | 404 | `NOT_FOUND` |
| No model on the `suggest` route | 409 | `NO_ROUTE` |
| Bad credentials (the connector's health shows the same) | 400 | `INVALID_CONNECTOR` |

## Verify on first use

This was built and tested against recorded API shapes, not a live site. Before relying on it, check the
following:

- The connector's **Health** turns `ok` after the first sync, and its job result
  (`GET /api/v1/jobs?type=knowledge_sync`) shows documents and chunks.
- Confluence Cloud: the account can run CQL searches (`/wiki/rest/api/content/search`).
- Jira Cloud: the account can use the new search endpoint (`/rest/api/3/search/jql`).
- Data Center: set `api_version: 2` for Jira.
