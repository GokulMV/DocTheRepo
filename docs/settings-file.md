# Settings file

Everything you would otherwise click through in the UI can live in one YAML or JSON file, or in several:
- LLM providers and their keys.
- Feature routing.
- Git, signal and knowledge connectors.
- Tracked repositories.
- Spend limits.

Secret values are **references** to where the secret lives, so the file is safe to commit.

```bash
dth apply -f hub.yaml --dry-run     # show what would change
dth apply -f hub.yaml               # apply it
dth settings export > hub.yaml      # start from what the Hub has now
```

The UI has the same: **Administration → Settings file**. Paste or load a file, then click **Preview changes**
and **Apply**. **Current settings** loads the export.

A full example is in [`deploy/settings/hub.example.yaml`](../deploy/settings/hub.example.yaml).

## What apply does

- **Changes:** it creates what is missing and updates what differs. It **never deletes**.
- **How items are matched:**
  - Providers and connectors by `name`.
  - Repositories by `full_name` plus `connector`.
  - Routes by feature.
  - The `spend` section replaces all spend limits, when present.
- **Order:** providers, then connectors, routes, repositories and spend. A route names its provider, and a
  repository its connector, by name.
- **Same path as the UI:** it goes through the Hub's API. Validation, role checks (admin), webhook
  registration and the audit log are the same as in the UI, and the audit entries are under your account.
- **Errors:**
  - An unknown key (a typo) is an error, with its line number.
  - Apply stops at the first failure. Everything before it is applied, and running it again is safe.
- **Secrets on later runs:** secrets are write-only, so a provider's key or a connector's credentials are
  re-applied on every run. They show as `update` with the field name only.

## Several files

Pass `-f` more than once, or a directory. Every `.yaml`, `.yml` and `.json` file in it is read, in name order:

```bash
dth apply -f providers.yaml -f repos.json -f connectors/
```

How files merge:
- **Lists** concatenate. A later item with the same name replaces an earlier one.
- **Routes** merge by feature.
- **Spend:** the last `spend` section wins.

## Secret references

Write a reference anywhere a value goes, either as the whole value or inside a string. `$${…}` is a literal
`${…}`.

| Reference | Reads |
|---|---|
| `${env:NAME}` | An environment variable. In GitHub Actions, map secrets to env (below). |
| `${file:path}` | A whole file, trimmed. `~/` works. |
| `${file:keys.json#github.token}` | A key in a JSON or YAML file (dotted path; `a.0.b` for lists). |
| `${file:application.properties#db.password}` | A key in a `.properties`, `.env` or `key=value` file. |
| `${vault:secret/data/dth#anthropic}` | HashiCorp Vault, KV v2 or v1. Uses `VAULT_ADDR` and `VAULT_TOKEN` (or `~/.vault-token`), plus `VAULT_NAMESPACE` if set. Without `#key` the secret must have one key. |
| `${gopass:dth/github-webhook}` | `gopass show -o`. With `#key`: `gopass show <path> <key>`. |
| `${awssm:prod/dth#token}` | AWS Secrets Manager with the default credential chain. An ARN carries its region. `#key` picks from a JSON secret. |
| `${gcpsm:project/secret}` | Google Secret Manager with application default credentials. Accepts `project/secret/version` or the full `projects/…/versions/…` name. |

**JSON credentials:** a connector whose credentials are JSON (Wiz: `client_id` + `client_secret`) can take a
mapping, and each value can be a reference:

```yaml
credentials:
  client_id: ${awssm:prod/dth/wiz#client_id}
  client_secret: ${awssm:prod/dth/wiz#client_secret}
```

**What never reaches the output:** values. Errors name the reference, and the plan lists field names only.

### Where references are resolved

**With `dth apply`,** on the machine or CI runner that runs it. Every source works there, and only resolved
values travel to the Hub, over its API.

**In the UI,** the Hub process resolves them, with its own environment, files and cloud identity. That is a
different trust boundary, so every source is **off** unless the operator enables it:

| Variable | Effect |
|---|---|
| `DTH_SETTINGS_SECRET_SOURCES` | The enabled sources, comma-separated, e.g. `vault,awssm,gcpsm`. Empty by default: pasted files may only hold literal values. |
| `DTH_SETTINGS_ENV_PREFIX` | `${env:…}` may only read variables with this prefix. Default `DTH_SECRET_`, so `DTH_DATABASE_URL` can never be read. |
| `DTH_SETTINGS_FILE_ROOT` | The only directory `${file:…}` may read. `file` stays off without it. Symlinks and `..` cannot leave it. |

In a config file the same settings are `settings.secret_sources`, `settings.env_prefix` and
`settings.file_root`. Give the Hub's Vault token or cloud role read access to the Hub's own secrets only.

## GitHub Actions

Keep `hub.yaml` in a repository and apply it on merge:

```yaml
name: hub-settings
on:
  push:
    branches: [main]
    paths: [deploy/settings/**]
jobs:
  apply:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.25.11" }
      - run: CGO_ENABLED=0 go install github.com/GokulMV/DocTheRepo/cmd/dth@main
      - run: dth apply -f deploy/settings/
        env:
          DTH_SERVER: ${{ vars.DTH_SERVER }}
          DTH_TOKEN: ${{ secrets.DTH_TOKEN }}                 # an admin's personal access token
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
          GITHUB_APP_TOKEN: ${{ secrets.DTH_GITHUB_TOKEN }}
```

Run the same command with `--dry-run` on pull requests to review the plan before merging.

## Format reference

```yaml
version: 1
providers:
  - name: …            # unique; how routes refer to it
    kind: anthropic | openai | azure_openai | bedrock | vertex | ollama | openai_compat | jev | external_cli
    base_url: …
    api_key: …         # secret
    extra: { key: value }
    redact_pii: false
    enabled: true
routes:
  <feature>:           # docgen, qa, decode, triage, embedding, suggest, decide
    provider: …        # a provider name
    model: …
    effort: low | medium | high | xhigh | max
    max_output_tokens: 8000
    context_token_budget: 16000
    temperature: 0.2
    fallback: { provider: …, model: … }
connectors:
  - name: …
    type: github | gitlab | sentry | datadog | … | confluence | jira   # as in the UI catalogue
    mode: webhook | poll | both
    poll_seconds: 300
    config: { key: value }   # the fields the UI form shows for this type
    credentials: …           # secret; string or mapping
    webhook_secret: …        # secret
    enabled: true
repos:
  - full_name: owner/name
    connector: …             # a connector name
    tracked_branch: main
    docs_path: docs/generated/
    push_mode: direct | pr_auto_merge | pr_with_approver
    approver: …
    on_reject: …
    pr_conflict_strategy: …
    service_name: …
    owners: [team-a]
    enabled: true
spend:
  limits:
    - scope: global | feature | provider | repo
      key: …                 # feature name, provider name or repository full name
      window: day | month
      max_tokens: 2000000
      max_cost_usd: 300
      on_breach: block | block_and_alert
      alert_url: …
```
