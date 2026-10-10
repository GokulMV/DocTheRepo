# Confluence and Jira

The Hub syncs Confluence spaces and Jira projects **read-only**. Synced pages and issues are used for:

- **Ask**: answers can cite them (`include: confluence` covers both).
- **Decodes**: runbooks are used for explanations.
- **Team docs**: shelves for Confluence, Jira, Runbooks and Decisions, plus the shelves your labels match.
- **Architecture**: each service lists the pages that document it.
- **Known issues**: labelled issues and pages are imported as draft rules.

Cloud and Data Center are both supported. Notion pages and uploaded documents work the same way: see
[team-docs.md](team-docs.md).

## Connect

In the UI, go to **Settings → Connections** and click **Confluence** or **Jira** under *Team documents*.

- **Atlassian Cloud (recommended): Connect with Atlassian.** Click the button and approve read-only access on
  Atlassian's own page, where you also pick the site. The browser comes back to Connections, where you enter
  the spaces or projects to sync. There is no token to create or paste. This needs a [one-time
  setup](#one-time-setup-for-connect-with-atlassian) by an admin.
- **API token (Cloud) or personal access token (Data Center).** This is the alternative, in the same dialog:
  the site URL, the spaces or projects, and the token.

### One-time setup for Connect with Atlassian

The Hub is self-hosted, so it signs in to Atlassian with an OAuth 2.0 (3LO) app that you register once. The
dialog's **One-time setup** link shows the same steps, with this Hub's callback URL ready to copy.

1. Open the [Atlassian developer console](https://developer.atlassian.com/console/myapps/) and choose
   **Create → OAuth 2.0 integration**. Name it, for example, "DocTheRepo Hub".
2. Under **Authorization**, add **OAuth 2.0 (3LO)** with this callback URL:
   `<server.public_url>/api/v1/atlassian/connect/callback`, for example
   `https://hub.acme.example/api/v1/atlassian/connect/callback`. It must match exactly. Without
   `server.public_url` (`DTH_PUBLIC_URL`) the Hub uses the address you opened it at, which must be `https`
   (or `localhost`).
3. Under **Permissions**, add these read-only classic scopes:

   | API | Scopes | Used for |
   |---|---|---|
   | Confluence API | `read:confluence-content.all`, `read:confluence-space.summary`, `search:confluence` | CQL search for changed pages with their bodies and labels, and each page's space |
   | Jira API | `read:jira-work`, `read:jira-user` | JQL search and issues; the account's time zone (`/myself`) |

   The Hub also asks for `offline_access`, which Atlassian grants without configuration, so that the
   sign-in renews on its own.
4. Under **Distribution**, make the app available to the users of your organization, so admins other than
   the app's owner can connect too. A private app works only for its owner.
5. Under **Settings**, copy the **Client ID** and **Secret** into the setup form, or use the API:

   ```sh
   curl -sS -X PUT "$HUB/api/v1/atlassian/oauth-app" -H "Authorization: Bearer $DTH_TOKEN" -H 'Content-Type: application/json' \
     -d '{"client_id":"<client id>","client_secret":"<secret>"}'
   # GET /api/v1/atlassian/oauth-app → {configured, client_id, callback_url, console_url, scopes}; never the secret
   ```

The secret is stored sealed and is covered by `dth-hub rotate-key`. Only admins can see or change the setup
and connect.

### How the sign-in behaves

- **What is stored.** The Hub keeps the access token (valid about an hour) and the refresh token, sealed in
  the connector's credentials. Neither is ever returned by the API or written to logs.
- **Renewal.** Atlassian rotates refresh tokens: each renewal returns a new refresh token that replaces the
  old one, and the Hub stores it before using the new access token. Atlassian ends a refresh token that is
  not used for 90 days. A connector that syncs every 15 minutes never reaches that.
- **When the sign-in ends.** If Atlassian refuses a renewal (the app's access was revoked, the account left
  the site, or the token lapsed), the connector shows **Needs sign-in again** with the reason under Health,
  and syncs stop. Click **Sign in again** on its row: the same connector, its spaces and its synced pages are
  kept. The new sign-in must cover the same site.
- **Several sites.** The site you choose on Atlassian's page is used. If the sign-in covers several sites,
  the Hub uses the one whose URL you typed in **Site URL**. Otherwise it asks you to choose on the
  Connections page.
- **Whose access.** The connector reads what the account that signed in can read. Use a dedicated
  read-only account if possible.
- **API calls.** Requests go through Atlassian's API gateway (`https://api.atlassian.com/ex/confluence/<cloud
  id>/…` and `…/ex/jira/<cloud id>/…`) with a bearer token. Links in answers still point at your site.
- **Changing the app.** If you replace the client secret, existing connectors keep working. If you delete
  the app or switch to a different one, their renewals fail and each needs **Sign in again**.

With the API, start a sign-in with `POST /api/v1/atlassian/connect` (`{"type":"confluence","keys":"ENG"}`; add
`"connector_id"` to sign an existing connector in again). It returns `{url}` for the browser to open. Choose a
site with `POST /api/v1/atlassian/connectors/{id}/site` (`{"cloud_id":…}`).

### With an API token or personal access token

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
| `api_version` | | optional | `3` for Cloud (the default when `email` is set or the connector signs in with Atlassian), `2` for Data Center. |

Connectors made with Connect with Atlassian also carry `auth: oauth`, `cloud_id`, `site_name` and, while
needed, `oauth_status` (`choose_site` or `needs_sign_in`). The Hub manages these keys and `base_url`, and
edits keep them.

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
- **Service links.**
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
| The Atlassian sign-in expired or was revoked | 400 | `ATLASSIAN_SIGN_IN_REQUIRED` |

## Verify on first use

This was built and tested against recorded API shapes, not a live site. Before relying on it, check the
following:

- The connector's **Health** turns `ok` after the first sync, and its job result
  (`GET /api/v1/jobs?type=knowledge_sync`) shows documents and chunks.
- Confluence Cloud: the account can run CQL searches (`/wiki/rest/api/content/search`).
- Jira Cloud: the account can use the new search endpoint (`/rest/api/3/search/jql`).
- Data Center: set `api_version: 2` for Jira.
- Connect with Atlassian was tested against a fake Atlassian only. On first use, check that the consent
  page lists the scopes above and that the connector syncs. If Atlassian answers 401 or 403 through the API
  gateway, compare the app's scopes with the table above.
