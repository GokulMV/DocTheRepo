# How docs are generated

## When

| Event | What happens |
|---|---|
| A repository is tracked | The Hub indexes all of its code and writes docs for it right away. A large repository takes a few minutes; files appear under **Docs** as they finish. |
| A commit lands on the tracked branch | Only what changed is triaged. Cosmetic changes cost nothing; changed functions and types get their docs rewritten. GitHub tells the Hub by webhook, or the Hub polls when GitHub cannot reach it (for example on `localhost`). |
| A model is first set up for `docgen` | Repositories that were synced before any model was set up get their docs written then. |
| **Generate docs** (Repositories page) | Every file is documented again from the latest commit. |

Docs need a model routed to `docgen` (**Providers & routing**; "Use it for everything" does this when you add a
provider). Without one, the code is still indexed and searchable.

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

## What it costs, and how to spend less

Before any model call, plain code (no model) decides how each piece of code gets its doc. The decisions
appear in each job's result under **Activity → Jobs** (`router`):

| Step | When | Cost |
|---|---|---|
| **Unchanged** | A full run ("Generate docs now", a new model route) meets code that has not changed since its doc was written | none: the doc is kept |
| **Reuse** | Exactly this code was documented before: a retried job, a reverted change, moved code, or the same code in another repository | none: the earlier doc is used |
| **Comment** | Tiny code with a doc comment that already explains it (economy mode: most commented code) | none: the comment is the doc |
| **Fast** | Short code, when the **Docs (short code)** route (`docgen_fast`) is set | the cheaper model |
| **Full** | Everything else | the **Docs generation** route |

Pick the mode under **Providers & routing → Docs generation cost**, or with `docs.generation_mode` /
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
  declarations as signatures, the same idea as code-graph tools: the Hub's own knowledge graph (Palace)
  and syntax trees provide it.
- **Only changed code** on a push, after syntax-tree triage that skips cosmetic changes. Tests and
  generated or vendored files are skipped, and so is anything in `.dthignore`.
- **Prompt caching** (Claude) marks the system prompt for caching. It mostly helps follow-up questions in
  Ask: docs prompts differ per file and are usually below the minimum cacheable size.
- **Spend limits** stop a job before it exceeds a ceiling; a dry run (**Repositories → Dry run**) shows
  the estimate and the routing first.

Recommended routing: **Docs generation** on a strong model, **Docs (short code)** on a fast one (for
example Claude Haiku), and the balanced mode.
