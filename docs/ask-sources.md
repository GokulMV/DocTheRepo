# How Ask picks its sources

Every Ask answer is written by the model on the `qa` route, which is usually the most expensive model the
Hub calls. Most of what it is paid to read is the source material put in front of it. The source picker
("sift") cuts that material down to what the question needs, using a much cheaper model to choose.

## What it does

1. **Retrieval stays generous.** Vector search, full-text search and the code graph return up to about 30
   pieces of code and documents, so nothing relevant is missed.
2. **A cheap judge reads them.** For every piece it answers two yes/no questions, each with a probability:
   - *relevance:* does this piece answer the question, or implement, configure, document or test what is
     asked? The current code counts even if it is wrong, because the question may be about a bug.
   - *scope:* does it belong to the component the question is about, rather than something similar?

   Ten pieces go into one request, and several requests run at once.
3. **Only what passes is sent.** A piece is kept when relevance is at least 0.5 and scope at least 0.25
   (`ask.sift_keep_at`). There is no fixed number: a narrow question may keep one piece and a broad one
   twenty. Kept pieces are ordered by relevance.
4. **When search finds too little, the picker explores before the agent does.** It walks the indexed files
   like a person would. It judges each top-level directory from the names inside it, opens only the
   promising ones, judges files there from a short preview, then reads the best few and keeps the pieces
   that pass. Only if that finds nothing does the agent spend model calls searching. See the
   [budgets](#limits).

The answering model then gets fewer, better sources. How much is cut depends on your questions and index;
the `sift` details on each answer and Usage show the real numbers. The judge reads the same material,
but at a fraction of the price.

## It never makes an answer worse by failing

- If the judge errors, times out or returns something unreadable, every retrieved piece is used, as before.
- If nothing passes, the best three by relevance are still sent, and Ask also explores, so the answer can
  either find more or say plainly that the sources do not cover it.
- Judgments are cached in memory by question and piece content. Asking again, or asking in different case
  or spacing, judges nothing twice.
- Retrieval and exploration read only what the asker may read: the repository ACL is applied in SQL first.

## It only runs when it saves money

The picker skips itself, and the answer is built exactly as before, when:

- no judge route is configured. It uses the `sift` route, else `decide`, else `docgen_fast`;
- the judge is the same model as the answering model;
- the judge's input price is more than half the answering model's (when both are priced);
- the retrieved material is under about 2,500 tokens, too little for trimming to pay.

## Set it up

The best judge is **TypeSafe Jev**. It answers yes/no questions natively with calibrated probabilities and
bills input only. Add it as a provider and route `sift` to it (see [jev.md](jev.md)). Any small fast chat
model also works (Claude Haiku, GPT-mini, Gemini Flash). Its probabilities are self-reported, so they are
rougher, but it still cuts the answering model's input.

```yaml
# settings file
routes:
  qa:   { provider: anthropic, model: claude-sonnet-5-5 }
  sift: { provider: jev,       model: jev-latest }        # or { provider: anthropic, model: claude-haiku-4-5 }
```

`dth init` sets `sift` to your fast model when you give one. In the UI the route is **Settings → AI models → Advanced →
Ask source picking**.

| Setting | Env | Default | Meaning |
|---|---|---|---|
| `ask.sift` | `DTH_ASK_SIFT` | `on` | `off` sends every retrieved piece, as before |
| `ask.sift_keep_at` | `DTH_ASK_SIFT_KEEP_AT` | `0.5` | Relevance a piece needs (0.1–0.95). Lower keeps more |

## See what it did

- **On the Ask page**, each answer the picker trimmed carries a green note such as *Read 3 of 20 sources ·
  9.4k tokens saved ($0.05)*. Hover it for the judge's model, tokens and cost and the net saving. A
  conversation's title shows the total saved so far. These are kept with the conversation.
- The Ask API's `done` event carries `sift`: candidates, kept, tokens not sent to the answering model, the
  judge's tokens and cost, and files explored.
- **Usage** shows the judge's usage under *Ask source picking*. The net saving (answering-model input
  not sent, minus the judge's cost) appears under *Ask sources trimmed before answering*.

## Limits

Exploration is bounded so a monorepo cannot run up a bill. It considers 5,000 indexed files, judges at most
120 directories and previews 60 files, and reads at most 8 files in full. A directory with a misleading name
can hide the answer. When that happens, the agent's own search (`DTH_ASK_AGENT_STEPS`) is still the next
step.

## Where the idea comes from

The design follows the approach described by [jevgrep](https://github.com/dzhng/jevgrep) (MIT), a code
search tool for coding agents. Its method: a calibrated yes/no model judges relevance across folders, files
and declarations; every unit that passes is kept rather than a fixed top-N; relevance and scope are judged
separately; navigation is bounded and reports when it stops early; and judgments are cached by content.
jevgrep's authors reported 25.8% lower total cost, judge included, with the same tasks solved, in a single
run on ten SWE-bench tasks. That is their measurement for coding agents, not a promise for Ask. The Hub's version is written
from scratch for its own index: it judges indexed chunks rather than files on disk, runs inside the LLM
gateway (spend limits, secret scrubbing, ledger), and works with any chat model as well as Jev. Measure it
on your own questions in Usage before relying on a particular saving.
