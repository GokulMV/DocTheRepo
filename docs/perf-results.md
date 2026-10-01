# Performance and quality results (Milestone 1)

Measured values on the hardware below, not guarantees (plan § 10). Re-run with `make perf-push`,
`make perf-qa`, and `make test` (the retrieval eval runs as part of the Go suite).

## Hardware and software

| | |
|---|---|
| Machine | 4 vCPU Intel Xeon @ 2.80 GHz, 15 GB RAM (one shared cloud VM) |
| Topology | One hub process running all three roles (api, worker, scheduler) |
| Database | PostgreSQL 16.15 + pgvector 0.8.6 in Docker on the same VM (default settings, HNSW m=16, ef_construction=64) |
| Model | `test/mocks/stubllm`: deterministic OpenAI-compatible stub; feature-hashed 256-dim embeddings |
| Load tool | k6 v1.3.0 on the same VM |
| Date | 2026-09-30 |

Everything shares four cores, so these numbers are a conservative floor for the recommended deployment
(separate database, api and worker replicas).

## Push burst: 40 commits across 5 repositories, 90 s per docgen call

`test/perf/push-burst.js` against `go run ./test/e2e/stack -auth local -synthetic-repos 5 -docgen-delay 90s`.
Five VUs push 40 commits (one new Go file each) through the GitHub mock. Each push arrives by signed
webhook and becomes one `code_push` job that makes one 90 s docgen call.

| Threshold (plan) | Target | Measured |
|---|---|---|
| Queue drain time | < 30 min | **902.5 s (15.0 min)** |
| Docgen calls in flight (code_push concurrency 4) | never > 4 | **max 4** |
| Jobs completed | 40 | **40** |
| Jobs failed or dead | 0 | **0** |

The theoretical floor is 40 × 90 s ÷ 4 = 900 s. The hub adds about 2.5 s of overhead across the whole
burst: webhook ingress, triage, chunking, docs assembly, direct landing, and indexing. Pushes to one
repository ran in order (per-repo serial key) while five repositories shared four worker slots. A smoke
run with a 2 s delay drained in 22 s (floor 20 s).

## Q&A: 50 users, one question every 10 s, 250,000 chunks

`test/perf/qa.js` against an OIDC stack with 24 repositories (4 fixtures + 20 synthetic).
`test/perf/seed` prepares the stack and writes 250,000 synthetic Go chunks with the stub's embeddings,
giving 250,159 live chunks in total. Seeding took 19 m 39 s (212 chunks/s including HNSW maintenance).
Fifty SSO users each ask a random question from the seed vocabulary every 10 s for 5 minutes, so the
answer cache never hits. Retrieval latency comes from the hub's `dth_retrieval_duration_seconds`
histogram, counting only the run's own samples.

| Threshold (plan) | Target | Measured |
|---|---|---|
| Retrieval stage p95 | < 400 ms | **99.4% of 1,500 retrievals ≤ 400 ms** (p95 in the 300–400 ms bucket) |
| Ask end to end p95 (retrieval + packing + stub model + citation contract) | < 1.5 s | **473 ms** (median 418 ms, max 525 ms) |
| Failed asks | < 1% | **0 of 1,500** |

Per-stage p95 upper bounds (histogram buckets): embed ≤ 200 ms, vector search ≤ 200 ms, full text
≤ 200 ms, load + graph expansion ≤ 100 ms.

The first attempt at this run hit the seeded **2M tokens/day global spend ceiling** after 547 answers:
the remaining 953 asks were refused with HTTP 402 before any model call. That is the spend guard
working as designed. The seeder now raises the ceiling explicitly, as an operator would for a load
test, and the table above is from the rerun.

Headroom notes:
- The 400 ms target is met, but on a shared 4-core VM the typical retrieval sits in the 300–400 ms
  bucket.
- Embed, vector search, and full text each have a p95 of at most 200 ms (the histogram cannot rank them
  more finely), and they run one after another.
- Running the embed + vector search and full-text search concurrently, or a dedicated database host, are
  the obvious levers if a deployment needs more margin.

## Retrieval quality: golden set

`test/eval/golden.json` holds 50 questions over the fixture repositories (`test/testdata/repos`), each
listing the files a correct answer should cite. One question has no answer in the sources. The eval
runs the real pipeline (index code, import docs) and the real retrieval, packing, and citation contract.

| Run | Citation precision | Citation recall | Hit rate |
|---|---|---|---|
| Stub model (CI baseline, `test/eval/baseline.json`) | **0.740** | **0.900** | 0.900 |

The build fails when precision or recall drops more than 10 points below the baseline.

The stub cites every source that shares enough terms with the question, so precision is structurally
below what a real model reaches. Recall is the stronger signal for retrieval. Known misses at the
baseline:
- The two dunning questions (`bil-05`, `bil-06`).
- The "what does the payments service do" README question (`x-01`).
- The negative question, which cites the search ADR because both mention a "cluster".

To score a real model (report only, no gate):

```sh
DTH_EVAL_BASE_URL=https://api.openai.com/v1 DTH_EVAL_API_KEY=… DTH_EVAL_MODEL=… DTH_EVAL_EMBED_MODEL=… \
  DTH_EVAL_REPORT=eval.json DTH_REQUIRE_DOCKER=1 go test ./cmd/hub -run TestRetrievalEval -v
```

## Signal storm (Milestone 2)

`test/perf/signal-storm.js` (`make perf-signals`) against `go run ./test/e2e/stack -auth local` on the same
4-vCPU VM, with k6, PostgreSQL, and the hub sharing the machine. Two ingestion paths: Amazon Data Firehose
deliveries (CloudWatch Logs subscription format) and generic-webhook batches, half the traffic each, with
a fixed set of distinct fingerprints per path. After the run the script reads every issue back from the
API: **accepted − stored** must be exactly 0, the issue count must equal the fingerprint count, and decode
jobs must equal new issues (not events).

| Run | Events | Accepted − stored | Issues (expected) | Decode jobs | Webhook p99 | Firehose p99 (after persistence) | Backpressure retries |
|---|---|---|---|---|---|---|---|
| 2,000 events/s × 60 s, 2 × 500 fingerprints | **120,200** | **0** | **1,000** (1,000) | **1,000** | **40 ms** | **1.44 s** | 0 |
| 10,000 events/s × 30 s, 2 × 1,000 fingerprints | **290,000** (~9,700/s) | **0** | **2,000** (2,000) | **2,000** | 876 ms | 4.65 s | 73 |

- At 2,000 events/s every target holds. Webhooks answer 202 once events are queued (the plan's 300 ms
  budget). Firehose is answered only after its events are persisted, so a crash cannot lose a delivery
  Firehose believes was delivered; that costs up to one 1-second aggregation window, hence the separate
  2.5 s Firehose threshold.
- At ~10,000 events/s on one shared 4-core VM the hub slows down instead of losing data: the aggregator's
  backpressure answers 503, senders retry (73 deliveries here), and every accepted event is counted
  exactly once. The latency thresholds fail at this rate on this hardware.
- The plan's full target (20,000 events/s for 10 minutes on 3 api replicas of 2 vCPU / 4 GB, Postgres on
  its own host) has not been run; this VM cannot host that topology. Pub/Sub was not part of the run (it
  needs an emulator); its consumer acknowledges only after the same durable flush.

A first run of the script had a bug (the run ID was computed per k6 VU, so each VU produced its own
fingerprints: 19,000 issues instead of 1,000); the table is from the corrected script.
