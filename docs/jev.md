# TypeSafe Jev in DocTheRepo Hub

Jev is TypeSafe AI's "System One" model. It does not write text: it reads a state (text or JSON) and
answers typed questions — a choice, a score, or a yes/no — with a probability per answer. TypeSafe trains
it for calibration, so a 0.9 should be right about nine times in ten. That makes it a good fit for the
cheap, frequent yes/no calls the Hub makes before it decides to spend on a full model call.

Jev is optional. Without it, the same decisions run on any chat provider you already configured, with
self-reported probabilities that the Hub labels as such.

## What the Hub uses it for

| Decision | Question | When it is acted on | Saves |
|---|---|---|---|
| Ask source picking (`sift` route) | For each retrieved piece: does it answer the question, and is it about the asked-about component? Many pieces per call | Pieces at relevance ≥ 0.5 are kept, the rest not sent | Most of the answering model's input on most questions ([ask-sources.md](ask-sources.md)) |
| Issue actionability (Phase 11.5) | Does this new issue need engineering attention, or is it recurring noise? | Only "known noise" at p ≥ `decide.gate_threshold` (default 0.9) | The full decode (~8k tokens on average) for that issue |

Everything below the threshold, every "actionable" answer, and every error goes down the normal path, so a
wrong or unavailable decision can only cost a little extra, never hide an explanation. A gated issue stays
in the Inbox with a short note ("Recurring noise … p=0.97, calibrated probability") and a full decode is
one click away (`POST /api/v1/issues/{id}/decode {"force": true}`).

Planned (same port, not wired yet): LLM triage fallback for file changes, likely-cause selection before a
decode and issue → service routing.

## Set it up

1. Get an API key from TypeSafe (Jev is in early access).
2. Make sure the Hub can reach `api.typesafe.ai` (egress allow-list / proxy).
3. Add Jev as a provider and point the `decide` route at it. In the UI: **Settings → AI models → Add
   provider**, kind `jev`, then set the **decide** route on the same page. With the API:

   ```sh
   # as an admin (session cookie + CSRF header, or a personal access token)
   curl -sS -X POST "$HUB/api/v1/providers" -H "Authorization: Bearer $DTH_TOKEN" -H 'Content-Type: application/json' \
     -d '{"kind":"jev","name":"TypeSafe Jev","api_key":"'"$TYPESAFE_API_KEY"'"}'
   # → {"id":"<provider id>", ...}

   curl -sS -X POST "$HUB/api/v1/providers/<provider id>/test" -H "Authorization: Bearer $DTH_TOKEN" \
     -H 'Content-Type: application/json' -d '{"model":"jev-latest"}'
   # → {"ok":true,"latency_ms":…}   (one minimal decision)

   curl -sS -X PUT "$HUB/api/v1/routes/decide" -H "Authorization: Bearer $DTH_TOKEN" -H 'Content-Type: application/json' \
     -d '{"provider_id":"<provider id>","model":"jev-latest"}'
   ```

   `base_url` is optional (default `https://api.typesafe.ai/v1`); set it to go through a gateway that
   proxies the System One API. The Hub refuses to route any feature other than `decide` and `sift` to a Jev provider,
   because Jev cannot write text.
4. Optional: tune the gate in the Hub config:

   ```yaml
   decide:
     gate_threshold: 0.9   # 0.5–1; higher = fewer skipped decodes, fewer mistakes
   ```

Every Jev call goes through the same guard rails as every other paid call: secrets are scrubbed from the
state (and PII redacted if the provider is set to), the spend guard checks the limits first, and the
reported usage lands on the usage ledger (Usage shows it under the `decide` feature; gated decodes show
up as `decision_gate` savings).

## Measure it before trusting it

The repository ships 100 labelled issues (`test/eval/decide_issues.json`, 50 actionable / 50 noise, with
the hard cases annotated). Run them through Jev — and, for comparison, through your chat provider — and
read accuracy, calibration, and what the gate would have done:

```sh
# Jev
DTH_REQUIRE_DOCKER=1 DTH_DECIDE_EVAL_KIND=jev DTH_DECIDE_EVAL_API_KEY=$TYPESAFE_API_KEY \
  DTH_EVAL_REPORT=jev.json go test ./cmd/hub -run TestDecisionEval -v

# The same questions on a chat model (self-reported probabilities)
DTH_REQUIRE_DOCKER=1 DTH_DECIDE_EVAL_KIND=anthropic DTH_DECIDE_EVAL_API_KEY=$ANTHROPIC_API_KEY \
  DTH_DECIDE_EVAL_MODEL=<model> DTH_EVAL_REPORT=chat.json go test ./cmd/hub -run TestDecisionEval -v
```

What the report means:

| Field | Meaning | What you want |
|---|---|---|
| `accuracy` | Share of the 100 answered correctly | High |
| `ece` | Expected calibration error: average gap between stated confidence and actual accuracy (10 bins) | Close to 0 |
| `reliability` | Per confidence bin: mean confidence vs. accuracy | The two numbers close in every bin |
| `brier` | Squared error of the probabilities | Low |
| `coverage` | Share decided at p ≥ threshold | As high as possible without hurting the next two |
| `gated_accuracy` | Accuracy of those confident decisions | ≥ the threshold (0.9 means ≥ 90%) |
| `missed_defects` | Real defects the gate would have skipped | 0 |
| `tokens_per_item`, `net_tokens_saved_per_issue` | Decision cost vs. the full decodes it avoids (`DTH_EVAL_DECODE_TOKENS`, default 8000) | Positive net saving |

Adopt the gate in production when `gated_accuracy` is at or above the threshold and `missed_defects` is 0
on this set and, ideally, on a sample of your own issues. The stub numbers in CI (`decide_baseline.json`)
only prove the harness works; they say nothing about any model.

## How the integration works

- `ports.Decider` is the port: a typed question (task, plain-language question, context, options) in, a
  probability per option out, plus whether those probabilities are calibrated.
- `internal/adapters/llm/jev` implements it against the System One API and refuses chat. It is registered
  as provider kind `jev`.
- `llmgateway.Decide` calls a native decider directly when the `decide` route's provider has one, and
  otherwise asks the chat provider for JSON probabilities. It normalises probabilities over the question's
  options and picks the most likely one.
- `core/decode` asks the actionability question before a full decode and acts only on a confident "known
  noise" (see the table above).

### Wire format used

```text
POST {base_url}/systemone
Authorization: Bearer <api key>

{"model": "jev-latest",
 "state": "<issue kind, title, service, exception, message, occurrences…>",
 "questions": {"decision": {"type": "choice",
                            "instructions": "Does this production issue need engineering attention, or is it recurring noise …?",
                            "criteria": {"actionable": "…", "known_noise": "…"}}}}

200 {"model": "jev-…",
     "answers": {"decision": {"type": "choice", "choice": "known_noise", "confidence": 0.95,
                              "probabilities": {"known_noise": 0.97, "actionable": 0.03}}},
     "usage": {"input_tokens": 80, "output_tokens": 6}}
```

Errors: 401 (bad key) and 422 (invalid request) fail the call permanently; 429 (rate limit) and 529
(overloaded) are retried with backoff and `Retry-After`, then handed back to the job queue. A choice takes
up to 255 options. Only input tokens are billed.

### Where these details come from — verify on the first live call

The build environment for this work could not reach `typesafe.ai` (blocked egress), so the
[TypeSafe docs](https://docs.typesafe.ai/agent-skill) themselves were not read. The wire format above
was assembled from several independent descriptions that agree with each other, including the
[LiteLLM pass-through docs](https://docs.litellm.ai/docs/pass_through/typesafe), the
[Vercel guide](https://vercel.com/kb/guide/typesafe-jev-and-ai-sdk), the
[Cloudflare model page](https://developers.cloudflare.com/ai/models/typesafe/jev/), and
[Flavio Copes' deep dive](https://flaviocopes.com/jev/). The adapter's tests pin exactly this format
against a fake server. Before relying on it:

1. Run the provider test above (`/providers/<id>/test`) — it makes one real decision.
2. Run `TestDecisionEval` with `DTH_DECIDE_EVAL_KIND=jev`; a format mismatch shows up as failed decisions.
3. If anything differs from the official API reference, the only file to change is
   `internal/adapters/llm/jev/jev.go` (and its test).

Rate limits reported by those sources (1,200 requests/minute, 250,000 input tokens/second per account,
subject to change) are far above what the Hub needs: one decision per new issue.
