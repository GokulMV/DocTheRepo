#!/usr/bin/env bash
# docs-real-run.sh writes a repository's documents with a real model and prints the run report, for
# reviewing output and tuning the prompts. It starts the hub against an empty database, signs in as a
# local owner, adds the provider and a GitHub connector, tracks the repository with a hard monthly cap,
# waits for the documents, and writes docs-report.md (with the text), docs-report-summary.md and docs-report.json.
#
# Needs: bin/dth-hub and bin/dth (make release), curl, jq, psql, and:
#   DTH_DATABASE_URL     an empty PostgreSQL (pgvector) database
#   RUN_PROVIDER_KIND    anthropic | openai | openai_compat | github_models | ...
#   RUN_MODEL            the docs model, e.g. claude-sonnet-5-5
#   RUN_API_KEY          the provider key (never printed)
#   RUN_GITHUB_TOKEN     a token that can read RUN_REPO
# Optional: RUN_PRICE_IN / RUN_PRICE_OUT (USD per million tokens; required for a model the hub has no price
# for, so the cap can be enforced), RUN_LONG_PROMPT_THRESHOLD with RUN_PRICE_LONG_IN / RUN_PRICE_LONG_OUT (the
# whole request's prices once a prompt is over that many tokens; without them a stored tier is kept), RUN_BASE_URL, RUN_GITHUB_API_URL (default https://api.github.com/), RUN_FAST_MODEL (short code docs; defaults to RUN_MODEL), RUN_REPO
# (default GokulMV/DocTheRepo), RUN_CAP_USD (default 3), RUN_TIMEOUT_MIN (default 40), RUN_OUT (default .)
set -euo pipefail

: "${DTH_DATABASE_URL:?}" "${RUN_PROVIDER_KIND:?}" "${RUN_MODEL:?}" "${RUN_API_KEY:?}" "${RUN_GITHUB_TOKEN:?}"
REPO=${RUN_REPO:-GokulMV/DocTheRepo}
CAP=${RUN_CAP_USD:-3}
TIMEOUT_MIN=${RUN_TIMEOUT_MIN:-40}
OUT=${RUN_OUT:-.}
FAST=${RUN_FAST_MODEL:-$RUN_MODEL}
HUB=http://127.0.0.1:18190
WORK=$(mktemp -d)
OWNER=owner@docs-run.local
OWNER_PW=$(openssl rand -hex 24)

log() { printf '%s  %s\n' "$(date -u +%H:%M:%S)" "$*"; }

DTH_LISTEN=127.0.0.1:18190 DTH_PUBLIC_URL=$HUB DTH_AUTH_MODE=local DTH_OWNER_EMAIL=$OWNER DTH_OWNER_PASSWORD=$OWNER_PW \
  DTH_LOCAL_KEY_FILE=$WORK/master.key DTH_GRAMMARS_DIR=$WORK/grammars DTH_LOG_FORMAT=json DTH_LOG_LEVEL=info \
  ./bin/dth-hub > "$OUT/hub.log" 2>&1 &
HUB_PID=$!
trap 'kill $HUB_PID 2>/dev/null || true; rm -rf "$WORK"' EXIT

for _ in $(seq 1 120); do curl -fsS "$HUB/readyz" >/dev/null 2>&1 && break; sleep 1; done
curl -fsS "$HUB/readyz" >/dev/null || { log "hub did not start"; tail -50 "$OUT/hub.log"; exit 1; }
log "hub ready"

# Sign in, then make a token for the API and the CLI.
JAR=$WORK/cookies
CSRF=$(curl -fsS -c "$JAR" -H 'Content-Type: application/json' -d "{\"email\":\"$OWNER\",\"password\":\"$OWNER_PW\"}" \
  "$HUB/api/v1/auth/local/login" | jq -r .csrf_token)
TOKEN=$(curl -fsS -b "$JAR" -H "X-CSRF-Token: $CSRF" -H 'Content-Type: application/json' \
  -d '{"name":"docs-run","expires_in_days":1}' "$HUB/api/v1/tokens" | jq -r .token)
api() { # api METHOD PATH [JSON]
  curl -fsS -X "$1" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' ${3:+-d "$3"} "$HUB/api/v1$2"
}

# The cap only holds when the model has a price: set one, or refuse to spend blind.
sql() { psql "$DTH_DATABASE_URL" -v ON_ERROR_STOP=1 -tAq "$@"; }
if [ -n "${RUN_PRICE_IN:-}" ] && [ -n "${RUN_PRICE_OUT:-}" ]; then
  sql -v k="$RUN_PROVIDER_KIND" -v m="$RUN_MODEL" -v i="$RUN_PRICE_IN" -v o="$RUN_PRICE_OUT" \
    -v t="${RUN_LONG_PROMPT_THRESHOLD:-}" -v li="${RUN_PRICE_LONG_IN:-}" -v lo="${RUN_PRICE_LONG_OUT:-}" <<'SQL'
INSERT INTO cost_table (provider_kind, model, input_per_mtok_usd, output_per_mtok_usd, source,
  long_prompt_threshold_tokens, long_input_per_mtok_usd, long_output_per_mtok_usd)
VALUES (:'k', :'m', :'i', :'o', 'operator', nullif(:'t', '')::integer, nullif(:'li', '')::numeric, nullif(:'lo', '')::numeric)
ON CONFLICT (provider_kind, model) DO UPDATE SET input_per_mtok_usd = EXCLUDED.input_per_mtok_usd,
  output_per_mtok_usd = EXCLUDED.output_per_mtok_usd, source = 'operator',
  long_prompt_threshold_tokens = coalesce(EXCLUDED.long_prompt_threshold_tokens, cost_table.long_prompt_threshold_tokens),
  long_input_per_mtok_usd = coalesce(EXCLUDED.long_input_per_mtok_usd, cost_table.long_input_per_mtok_usd),
  long_output_per_mtok_usd = coalesce(EXCLUDED.long_output_per_mtok_usd, cost_table.long_output_per_mtok_usd);
SQL
fi
for m in "$RUN_MODEL" "$FAST"; do
  n=$(sql -v k="$RUN_PROVIDER_KIND" -v m="$m" <<'SQL'
SELECT count(*) FROM cost_table WHERE provider_kind = :'k' AND model = :'m';
SQL
)
  if [ "$n" = 0 ] && [ "$RUN_PROVIDER_KIND" != github_models ]; then
    log "no price for $RUN_PROVIDER_KIND/$m: set RUN_PRICE_IN and RUN_PRICE_OUT (USD per million tokens) so the cap holds"
    exit 1
  fi
done
# A global ceiling as well, so every paid call counts (not only the documents' own budget).
api PUT /spend/limits "{\"items\":[{\"scope\":\"global\",\"scope_key\":\"\",\"window\":\"month\",\"max_cost_usd\":$CAP,\"on_breach\":\"block\"}]}" >/dev/null

provider=$(jq -n --arg k "$RUN_PROVIDER_KIND" --arg key "$RUN_API_KEY" --arg url "${RUN_BASE_URL:-}" \
  '{kind:$k, name:"docs run", api_key:$key} + (if $url == "" then {} else {base_url:$url} end)')
PROV=$(api POST /providers "$provider" | jq -r .id)
api PUT /routes/docgen "{\"provider_id\":\"$PROV\",\"model\":\"$RUN_MODEL\"}" >/dev/null
api PUT /routes/docgen_fast "{\"provider_id\":\"$PROV\",\"model\":\"$FAST\"}" >/dev/null
log "provider $RUN_PROVIDER_KIND, model $RUN_MODEL (short code: $FAST)"

conn=$(jq -n --arg t "$RUN_GITHUB_TOKEN" --arg url "${RUN_GITHUB_API_URL:-https://api.github.com/}" \
  '{type:"github", name:"GitHub", credentials:$t, mode:"poll", config:{base_url:$url, bot_login:"github-actions[bot]"}}')
CONN=$(api POST /connectors "$conn" | jq -r .id)
REPO_ID=$(api POST /repos "{\"connector_id\":\"$CONN\",\"full_name\":\"$REPO\"}" | jq -r .id)
api PUT "/repos/$REPO_ID/docs/budget" "{\"cap_usd\":$CAP}" >/dev/null
log "tracking $REPO with a \$$CAP cap"

# Wait for the first docs job to start and then for it to stop.
deadline=$(( $(date +%s) + TIMEOUT_MIN * 60 ))
last=""
while :; do
  job=$(api GET "/repo-docs?repo_id=$REPO_ID" | jq -c '.job // empty')
  if [ -n "$job" ]; then
    line=$(jq -r '"\(.status) \(.progress // {} | tostring)"' <<<"$job")
    [ "$line" != "$last" ] && log "docs job: $line" && last=$line
    case $(jq -r .status <<<"$job") in
      done|failed|dead|aborted|spend_blocked|needs_human) break ;;
    esac
  else
    pushes=$(api GET "/jobs?type=code_push&limit=5" | jq -r '[.items[] | "\(.status)\(if .error then ": " + .error else "" end)"] | join(", ")')
    [ "code_push: $pushes" != "$last" ] && log "code_push: $pushes" && last="code_push: $pushes"
    case $pushes in *failed*|*dead*) log "indexing the code failed"; break ;; esac
  fi
  [ "$(date +%s)" -ge "$deadline" ] && { log "timed out after $TIMEOUT_MIN minutes"; break; }
  sleep 10
done

./bin/dth --server "$HUB" --token "$TOKEN" docs report "$REPO" --text -o "$OUT/docs-report.md"
./bin/dth --server "$HUB" --token "$TOKEN" --json docs report "$REPO" > "$OUT/docs-report.json"
./bin/dth --server "$HUB" --token "$TOKEN" docs report "$REPO" -o "$OUT/docs-report-summary.md"
grep -E '"level":"(WARN|ERROR)"' "$OUT/hub.log" | tail -40 > "$OUT/hub-warnings.log" || true
log "report written: $OUT/docs-report.md"
