# Architecture tab

**Knowledge → Architecture** lists every repository you can see. Open one to get its architecture.

## Generated

Every tracked repository gets an architecture diagram built from what the Hub already extracts from its code.
It costs no model calls and refreshes with every processed push.

| Layer | What is in it |
|---|---|
| Upstream | Other repositories that call this one, and that publish the topics it reads |
| Interface | The endpoints it exposes |
| Core | The service it runs as, and its main modules |
| Messaging | The topics and queues it publishes or subscribes to |
| Data | The datastores it uses |
| Downstream | Repositories and external APIs it calls, and the repositories that consume its topics |

**What a box stands for:** code-level entities are lifted to keep the picture readable. Functions and files
become the service, and anything in another repository becomes that repository's box. Arrow width grows with
the number of code-level links an arrow stands for.

**Large repositories:** each layer shows its most connected items ("+ N more" counts the rest).

**Reading it:** hover to trace a component's links; click one for its connections or
**Ask about it**. **Fit** scales the diagram to the page; **100%** shows it at full size.

**Below the diagram:** the environment variables the code reads, the Confluence and Jira pages linked to
the service (runbooks first), and owners.

**Access:** the diagram respects repository access. A repository you cannot see never appears, not even as a
dependency. The page says how many links were left out.

## Authored diagrams (archify)

Diagrams made with [archify](https://github.com/tt-a1i/archify) and committed to the repository appear as
extra tabs next to **Generated**.

**Which files:** HTML files whose path mentions `architecture`, `archify`, `diagram`, `arch/` or `design/`
(e.g. `docs/architecture/*.html`). A file only counts if archify generated it, i.e. it has
`<meta name="generator" content="archify …">`.

**When they sync:**
- **Every push:** added, changed, renamed and deleted diagrams are updated as part of the push.
- **Find diagrams** (editors and up) scans the whole tracked branch, for diagrams that were there before the
  repository was tracked. A scan reads at most 60 candidate files, each up to 8 MB.

**How they are shown:** in a sandbox. The HTML is served with
`Content-Security-Policy: sandbox allow-scripts …; connect-src 'none'` and framed only by the Hub. The
diagram's own scripts work (zoom, themes, export), but it cannot read your session, call the Hub's API or
load anything from the network. **Open full screen** opens it in its own tab, under the same policy.

To add one, run archify on the repository (it is an agent skill for Claude Code, Codex and others). Then
commit the HTML it writes, e.g. `docs/architecture/system-architecture.html`. It appears after the next
push, or right away with **Find diagrams**.

## API

| Method | Path | Role |
|---|---|---|
| GET | `/api/v1/architecture` | viewer: repositories with counts per layer and diagrams |
| GET | `/api/v1/architecture/repos/{id}` | viewer with access: layers, links, env, docs, owners, diagrams |
| POST | `/api/v1/architecture/repos/{id}/scan` | editor: rescan authored diagrams |
| GET | `/api/v1/architecture/diagrams/{id}` | viewer with access: the diagram's HTML, sandboxed |
