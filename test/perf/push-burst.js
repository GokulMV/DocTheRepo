// Push burst (plan § 10): 40 commits across 5 repositories, each documented by a slow model (90 s per
// docgen call by default), must drain within 30 minutes at code_push/docgen concurrency 4 without ever
// exceeding that cap. Start the stack first:
//
//   go run ./test/e2e/stack -hub ./bin/dth-hub -auth local -synthetic-repos 5 -docgen-delay 90s
//   k6 run test/perf/push-burst.js
import http from 'k6/http';
import exec from 'k6/execution';
import { Counter, Gauge, Trend } from 'k6/metrics';
import { CONTROL, call, configure, countJobs, localLogin, stackState, stub, waitFor } from './lib.js';

const REPOS = ['acme/svc-01', 'acme/svc-02', 'acme/svc-03', 'acme/svc-04', 'acme/svc-05'];
const COMMITS = Number(__ENV.COMMITS || 40);
const CAP = Number(__ENV.CAP || 4);

const drain = new Trend('queue_drain_seconds');
const maxInFlight = new Gauge('docgen_max_in_flight');
const docgenCalls = new Counter('docgen_calls');
const failedJobs = new Counter('push_jobs_failed');
const doneJobs = new Counter('push_jobs_done');

export const options = {
  scenarios: { burst: { executor: 'shared-iterations', vus: REPOS.length, iterations: COMMITS, maxDuration: '5m' } },
  setupTimeout: '20m',
  teardownTimeout: '45m',
  thresholds: {
    queue_drain_seconds: ['max<1800'],
    docgen_max_in_flight: [`value<=${CAP}`],
    push_jobs_failed: ['count==0'],
    push_jobs_done: [`count>=${COMMITS}`],
  },
};

export function setup() {
  const st = stackState();
  const s = localLogin(st.hub_url, 'owner@acme.test', 'correct horse battery staple');
  // Index first without docgen (setup is not measured), then route docgen for the burst.
  const hub = configure(s, st, REPOS, { docgen: false });
  const r = call(s, 'PUT', '/routes/docgen', { provider_id: hub.providerId, model: 'stub' });
  if (r.status !== 204) throw new Error(`route docgen: ${r.status} ${r.body}`);
  const baseline = countJobs(s, 'code_push', 'done');
  stub(st, '/_stub/reset', 'POST');
  return { st, s, baseline, start: Date.now() };
}

export default function () {
  const i = exec.scenario.iterationInTest;
  const repo = REPOS[i % REPOS.length];
  const body = `package feature

// Feature${i} is change number ${i} of the push burst: it applies discount rule ${i} to an order total.
func Feature${i}(total int64) int64 { return total - ${i} }
`;
  const r = http.post(`${CONTROL}/github/push`, JSON.stringify({ repo, author: 'dev', files: { [`feature/feature_${i}.go`]: body } }), { headers: { 'Content-Type': 'application/json' } });
  if (r.status !== 200) throw new Error(`push ${i}: ${r.status} ${r.body}`);
}

export function teardown(data) {
  const { s, st, baseline, start } = data;
  waitFor(() => countJobs(s, 'code_push', 'queued') + countJobs(s, 'code_push', 'processing') === 0 && countJobs(s, 'code_push', 'done') - baseline >= COMMITS, 'push jobs drained', 2700);
  drain.add((Date.now() - start) / 1000);
  const stats = stub(st, '/_stub/stats').json();
  maxInFlight.add(stats.docgen ? stats.docgen.max_in_flight : 0);
  docgenCalls.add(stats.docgen ? stats.docgen.calls : 0);
  doneJobs.add(countJobs(s, 'code_push', 'done') - baseline);
  failedJobs.add(countJobs(s, 'code_push', 'failed') + countJobs(s, 'code_push', 'dead'));
}
