# opencode and DocTheRepo Hub

There are two independent integrations:

1. **opencode writes your docs.** The Hub runs opencode as a documentation engine through the
   `external_cli` provider. Pushes, PRs, the spend guard, and audit stay in the Hub.
2. **opencode (or Claude Code, Cursor, …) asks the Hub.** `dth mcp` is an MCP server that gives the agent
   read-only tools for Q&A, the knowledge graph, the Library, and production issues with their decodes.

## 1. opencode as the documentation engine

`dth engine opencode` speaks the Hub's DocGen contract v2. It turns a task into one prompt, runs
`opencode run --format json` in an empty scratch directory (so the agent cannot change a repository; the
Hub lands docs itself), reads the reply and token usage from the JSON events, and writes the result file.

**Requirements:** `opencode` and `dth` on the hub's `PATH`, with opencode already authenticated for the
model you choose (`opencode auth login`, or the provider's API key in the hub's environment). This fits a
native install (`scripts/quickstart.sh`) or a VM. The stock container image is distroless and has no
opencode; build your own image on top of it if you run the hub in a container.

**Check it before routing to it:**

```sh
dth adapter-test dth engine opencode --model anthropic/claude-sonnet-5-5 {task_file} {result_file}
# → every check PASS and "Conforms to DocGen v2."
```

**Add it** (UI: **Providers & routing → Add provider**, kind `external_cli`; then set the **docgen** route
to it). With the API:

```sh
curl -sS -X POST "$HUB/api/v1/providers" -H "Authorization: Bearer $DTH_TOKEN" -H 'Content-Type: application/json' -d '{
  "kind": "external_cli", "name": "opencode",
  "extra": {
    "command_template": "dth engine opencode --model anthropic/claude-sonnet-5-5 {task_file} {result_file}",
    "timeout": "10m"
  }}'
curl -sS -X PUT "$HUB/api/v1/routes/docgen" -H "Authorization: Bearer $DTH_TOKEN" -H 'Content-Type: application/json' \
  -d '{"provider_id":"<id from the response>","model":"anthropic/claude-sonnet-5-5"}'
```

Flags: `--model provider/model` (default: opencode's configured model), `--agent <name>`,
`--opencode <path>` (or `OPENCODE_BIN`), `--timeout` (default 10m). Exit codes: 0 success; 1 opencode
failed (a result with `status: "error"` is written, so the Hub shows the reason); 3 unsupported contract
version.

**Spend.** When opencode's JSON events include token counts (`tokens.input` / `tokens.output`, as its
step-finish events do), they are reported to the Hub and the spend guard counts them like any other call.
If your opencode version does not emit them, `dth adapter-test` warns "usage not reported"; then add
`"reports_usage": "false"` to `extra` and allow unreported usage in the hub config
(`spend.allow_unreported_usage: true`), knowing the guard can only estimate that route.

**Keys.** If you store an API key on the provider, the Hub passes it to the engine as `DTH_ENGINE_API_KEY`.
Set `OPENCODE_API_KEY_ENV` in `extra.env` to the variable opencode's provider reads
(for example `"env": "OPENCODE_API_KEY_ENV=ANTHROPIC_API_KEY"`) and the engine hands it over under that name.

## 2. The Hub as an MCP server

`dth mcp` runs an MCP server on stdio. It uses your personal access token, so the agent sees exactly what
you can see in the UI.

| Tool | What it does |
|---|---|
| `ask` | Q&A over code, docs, Confluence/Jira, and decoded issues, with citations; optional `repos` / `include` |
| `search_entities` | Find services, endpoints, topics, env vars, datastores, dependencies, owners by name |
| `entity_graph` | What an entity calls, exposes, publishes, subscribes to, reads, depends on, is owned by (1–3 hops) |
| `list_issues` / `get_issue` | Production issues, and one issue with its decode (cause, impact, affected code, next steps) |
| `list_known_issues` | Problems already understood, and why they are suppressed or labelled |
| `library` | Library shelves, or the items on one (`runbooks`, `decisions`, `apis`, …) |
| `read_doc` | One generated or imported doc as Markdown |

All tools are read-only. Sign the CLI in once (`dth login --server https://hub.example.com --token dth_pat_…`,
or `dth up` locally), or pass `DTH_SERVER` and `DTH_TOKEN` in the client's MCP config.

**opencode** — `opencode.json` in your project (or `~/.config/opencode/opencode.json`):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "doctherepo": {
      "type": "local",
      "command": ["dth", "mcp"],
      "enabled": true,
      "environment": { "DTH_SERVER": "https://hub.example.com", "DTH_TOKEN": "dth_pat_…" }
    }
  }
}
```

**Claude Code:**

```sh
claude mcp add doctherepo -e DTH_SERVER=https://hub.example.com -e DTH_TOKEN=dth_pat_… -- dth mcp
```

**Cursor** — `~/.cursor/mcp.json`:

```json
{ "mcpServers": { "doctherepo": { "command": "dth", "args": ["mcp"],
  "env": { "DTH_SERVER": "https://hub.example.com", "DTH_TOKEN": "dth_pat_…" } } } }
```

Then ask the agent things like "why is checkout throwing TimeoutError in prod?" or "what publishes to the
payments topic and who owns it?".

## Verify against your opencode version

This integration was built without access to opencode's documentation site, from its CLI behaviour as
publicly described. Before relying on it:

- `opencode run --help` should list `--format json`, `--model`, and `--agent`; `dth adapter-test` (above)
  exercises the engine end to end.
- The MCP config key and fields (`mcp`, `type: "local"`, `command` array, `environment`) are opencode's
  current format as far as we know; if your version differs, `opencode mcp list` (or its docs) shows the
  expected shape — the server itself is standard MCP over stdio and does not depend on it.
