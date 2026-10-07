# How docs are written

The Hub writes **readable documents per repository**. You get:

- an Overview;
- one Architecture document for the whole application;
- a guide for each part (module) of the code;
- flows, API, data model, tests, runbooks and the other documents that apply.

There is no longer one doc per source file. Documents live in the Hub under **Docs**. **Export to repository** opens a pull request with them as Markdown whenever you want them in git.

Every claim cites the code as `[path:line]`, which the Hub turns into a link to that line. Every document and section has a **confidence** score that says how far to trust it.

## Which documents

Each document is written only when the repository has what it describes. For example, there is no Events document without queues or topics, and no API reference without endpoints. Every document opens with **At a glance**: 3–5 plain sentences anyone can read.

| Group | Document | What is in it |
|---|---|---|
| Basics | **Overview** | What it does and for whom, key concepts, module map, entry points, how to run it, where to go next |
| Basics | **Architecture** | See the outline below this table |
| Modules | **Module guide** (one per module) | Purpose, responsibilities, how it works, public interface, data it owns, errors and edge cases, configuration, depends on / used by, files, where to change things, do's and don'ts, tests |
| Interfaces | **Flows** | The key paths step by step, with sequence diagrams and failure paths |
| Interfaces | **API reference** | Every endpoint: auth, request, response, errors, handler |
| Interfaces | **Events and messages** | Topics and queues: producers, consumers, payloads, delivery and retries |
| Interfaces | **Data model** | Stores, tables, fields, relations (diagram), schema history |
| Engineering | **Configuration** | Every setting and environment variable |
| Engineering | **Test architecture** | Test layers, how to run them, fixtures, what is and is not covered |
| Engineering | **Build, CI and deploy** | Building, pipelines, environments |
| Engineering | **Error catalogue** | Error types and codes, where raised, what to do |
| Operations | **Getting started** | From a fresh checkout to a running app |
| Operations | **Runbooks and observability** | Logs, metrics, common incidents, rollback |
| Operations | **Security and access** | Authentication, authorization, data protection, attack surface |
| Operations | **Integrations** | External systems and what breaks if each is down |
| People | **Glossary** | Domain and technical terms in plain words |
| People | **Ownership and contributing** | Owners, conventions, adding a feature |
| People | **Recent changes** | What changed recently, by area, in plain words (from the last 90 days of commits; rewritten at most once a week) |
| People | **Decision records** | Decisions visible in the history (migrations, replacements, adoptions) and in `adr/` or `decisions/` files, with context and consequences, marked "inferred" when only a commit message supports them |

The Architecture document covers:

- **Architectural style:** which style the code follows, how we can tell, its benefits here, and an example from the code.
- **Layers:** what belongs in each layer, what must not, and examples.
- **Components:** with a diagram drawn from the code's graph.
- **End-to-end flow:** with a sequence diagram.
- **What is stored where.**
- **Cross-cutting concerns.**
- **External systems.**
- **Do's and don'ts.**
- **Trade-offs and limits.**

## System architecture (across repositories)

When tracked repositories talk to each other, a **System** item appears in the sidebar. Repositories count as talking when:
- one publishes events another consumes;
- one's code calls another's endpoints or code;
- one depends on a package built from another (for example `github.com/acme/lib` or `@acme/lib`).

The page shows:
- **A map** of the repositories and their links, drawn from the code.
- **The links** themselves, each with where it is in the code.
- **A write-up** covering:
  - each repository's role;
  - how the parts talk;
  - end-to-end flows across services, with sequence diagrams;
  - shared contracts;
  - coupling and risks;
  - rules for changes across services.

The write-up is rewritten when the links or the repositories' own documents change. It spans every linked repository, so only someone who can read all of them sees it. Others see the map and the links between the repositories they can read.

## How a repository is split into modules

**The split:**

1. A module is a directory with what is under it.
2. A directory over about 15,000 lines is split into its subdirectories. Its own files and small subdirectories stay together as "*dir* (other files)".
3. Parts under about 300 lines join the module above them.

A repository ends up with roughly 5–25 modules. Each test file is attached to the module whose code it sits next to (or mirrors, as in `tests/orders`).

## How a document is written

1. **Facts, no model.** The Hub collects what the code declares from its index and knowledge graph: declarations with line numbers, calls between files, endpoints, environment variables, datastores, topics, dependencies and owners. It also reads the README, build, deploy and CI files whole.
2. **File notes.** Each file gets a short note (purpose, main declarations, side effects). Notes use the fast model when one is routed (**Docs (short code)**). A note is written again only when the file's *structure* changes (its declarations and signatures), not on every edit inside a function.
3. **The document.** The docs model (**Docs generation**) writes each document from its own material: notes, declarations, the most-called code with line numbers, facts, tests and the files above. Module guides come first; the Overview and Architecture are written from them.
4. **Checks.** Every reply is checked:
   - required sections are present;
   - each section stays within its length;
   - citations point at real files and lines;
   - the names in backticks exist in the code;
   - diagrams are valid.

   A failed check gets one repair round. The component diagram is drawn from the graph, never by the model.

## Confidence

Each section gets a score from 0 to 1:

- **Grounding:** the share of its citations that point at real code.
- **Names:** the share of the names it mentions that exist in the code.
- **Support** (when a model is routed to **Decisions**): the decision model's probability that the cited code supports what the section says. With [Jev](jev.md) this probability is calibrated. Otherwise it is the chat model's own estimate.

A document's confidence is its weakest required section. It is lowered when a module guide leaves out the module's most-used declarations, and for each thing the model reported it could not determine.

| Score | Label |
|---|---|
| 0.8 and above | High |
| 0.6 to 0.8 | Medium |
| Below 0.6 | Low |

Click the badge to see why.

**In Ask:** each section is a search piece that carries its confidence, so the answering model knows when to prefer the code. Broad questions ("how is this built?") start from the Overview and Architecture.

**Answer confidence.** Every cited answer gets its own High / Medium / Low badge. Hover over it to see why. It is lowered when:
- few of its statements cite a source;
- the sources behind it are weak (code counts most, then pages, then generated documents by their own confidence);
- it rests on a single source;
- the source picker found nothing clearly relevant.

**Coding agents.** The Hub's MCP server returns the same answer confidence in `ask`. Its `read_document` tool returns a document's confidence, the reasons, and which sections to check against the code.

## When documents are rewritten

A push reads the changed code first; documents follow in a separate **Docs writing** job. A document is rewritten only when its inputs change materially:

- **Module guides:** the module's files or their structure change, what the module declares changes (endpoints, settings, stores, topics), its tests change, or its links to other modules change.
- **Repository documents:** the facts they are written from change.
- **Any document:** at least 30% of its source lines changed in edits inside functions. Below that, the document shows "*N*% of its code changed since it was written".

A document that failed is tried again when its inputs change, or with **Write changed docs now**. **Rewrite all** rewrites everything.

## What it costs, and the monthly budget

The first run writes every document, and it is the expensive one: roughly one docs-model call per module, plus a dozen for the repository documents, plus the file notes. After that, a typical push rewrites nothing or one module guide.

**Before the first run:** the **Docs** page shows an estimate (`POST /api/v1/repos/{id}/docs/estimate`).

**Each repository has a monthly docs budget:**

- The first run sets it to twice the estimate, at least $10, unless `docs.repo_monthly_cap_usd` (in config) sets one for every repository.
- The Docs page shows what this month cost and turns amber at 80%.
- When the budget is used up, documents pause until next month or until an admin raises it with **Change budget**. What was written before the cap is kept.

The global spend limits still apply on top.

## Leaving files out: `.dthignore`

Add a `.dthignore` file to the root of the repository. It uses `.gitignore` syntax, and matching files are
neither documented nor indexed:

```gitignore
# generated code and fixtures
generated/
internal/legacy/**
*.sql

# but keep this one
!db/schema.sql

# only at the repository root
/scripts
```

| Pattern | Matches |
|---|---|
| `name` | A file or directory with that name, at any depth. |
| `dir/` | Only directories, and everything in them. |
| `/path` or `a/b` | Anchored to the repository root. |
| `*`, `?`, `**` | As in `.gitignore`: `*` stays within a folder, `**` crosses folders. |
| `!pattern` | Re-includes a path that an earlier line, or the built-in list, left out. |

The file is read from the commit being processed, so a change to it applies from that commit on. Run
**Generate docs** to apply it to everything at once.

The Hub already skips lockfiles, vendored and built output (`vendor/`, `node_modules/`, `dist/`, …),
minified and generated files, and licences. Tests are indexed for search but not documented.


## Docs v1 (one doc per source file)

Set `docs.version: 1` (or `DTH_DOCS_VERSION=1`) to keep the earlier behaviour: one generated Markdown file per source file, landed in the repository as a docs PR or a direct commit, browsed as a file tree. The rest of this section applies only to v1.

### What it costs, and how to spend less

Before any model call, plain code (no model) decides how each piece of code gets its doc. The decisions
appear in each job's result under **Activity → Jobs** (`router`):

| Step | When | Cost |
|---|---|---|
| **Unchanged** | A full run ("Generate docs now", a new model route) meets code that has not changed since its doc was written | none: the doc is kept |
| **Reuse** | Exactly this code was documented before: a retried job, a reverted change, moved code, or the same code in another repository | none: the earlier doc is used |
| **Comment** | Tiny code with a doc comment that already explains it (economy mode: most commented code) | none: the comment is the doc |
| **Fast** | Short code, when the **Docs (short code)** route (`docgen_fast`) is set | the cheaper model |
| **Full** | Everything else | the **Docs generation** route |

Pick the mode under **Settings → AI models → Advanced → Docs generation cost**, or with `docs.generation_mode` /
`DTH_DOCS_MODE`:

| Mode | Comment used when | Fast model writes |
|---|---|---|
| Thorough | never | nothing |
| Balanced (default) | the code is at most 3 lines and the comment explains it | code up to 25 lines |
| Economy | the code is at most 60 lines and the comment is substantial | code up to 80 lines |

Measured on this repository (255 files, about 2,700 pieces of code):

| Mode | Comment (no call) | Fast model | Main model |
|---|---|---|---|
| Thorough | 0 | 0 | 2,713 |
| Balanced | 427 | 1,948 | 338 |
| Economy | 920 | 1,762 | 31 |

Other things that keep cost down:
- **Output sized to the code.** One or two sentences for simple code, at most about 150 words. Output
  room is reserved per call for what is asked; output tokens cost several times input tokens.
- **Context is signatures, not files.** Each call gets the code being documented plus one hop of related
  declarations as signatures, the same idea as code-graph tools: the Hub's own knowledge graph
  and syntax trees provide it.
- **Only changed code** on a push, after syntax-tree triage that skips cosmetic changes. Tests and
  generated or vendored files are skipped, and so is anything in `.dthignore`.
- **Prompt caching** (Claude) marks the system prompt for caching. It mostly helps follow-up questions in
  Ask: docs prompts differ per file and are usually below the minimum cacheable size.
- **The monthly budget** (**Settings → AI models**) and detailed spend limits (**Settings → Advanced**) stop a job before it exceeds a ceiling; a dry run (**Repositories → Dry run**) shows
  the estimate and the routing first.

Recommended routing: **Docs generation** on a strong model, **Docs (short code)** on a fast one (for
example Claude Haiku), and the balanced mode.
