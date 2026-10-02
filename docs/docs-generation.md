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
