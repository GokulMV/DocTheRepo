// Q&A load (plan § 10): 50 concurrent users, one question every 10 s each, stub model, 250k chunks →
// retrieval p95 under 400 ms. Prepare the stack first (the seeder configures, indexes, and writes chunks):
//
//   go run ./test/e2e/stack -hub ./bin/dth-hub -listen 127.0.0.1:18190 -control 127.0.0.1:18199 \
//     -llm 127.0.0.1:18198 -metrics 127.0.0.1:18191 -synthetic-repos 20 -hub-env DTH_ALL_USERS_READ_ALL_REPOS=true
//   go run ./test/perf/seed -control http://127.0.0.1:18199 -chunks 250000
//   DTH_CONTROL=http://127.0.0.1:18199 k6 run test/perf/qa.js
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Gauge } from 'k6/metrics';
import { call, ssoLogin, stackState } from './lib.js';

const USERS = Number(__ENV.USERS || 50);
const PERIOD = Number(__ENV.PERIOD || 10);
const vocab = JSON.parse(open('./seed/vocab.json'));

const p95 = new Gauge('retrieval_p95_seconds');
const within = new Gauge('retrieval_within_400ms');
const stageP95 = {};
for (const s of ['embed', 'vector_search', 'full_text', 'load_expand']) stageP95[s] = new Gauge(`retrieval_${s}_p95_seconds`);

export const options = {
  scenarios: { qa: { executor: 'constant-vus', vus: USERS, duration: __ENV.DURATION || '5m' } },
  setupTimeout: '10m',
  thresholds: {
    retrieval_within_400ms: ['value>=0.95'],
    'http_req_duration{name:ask}': ['p(95)<1500'],
    'http_req_failed{name:ask}': ['rate<0.01'],
    checks: ['rate>0.99'],
  },
};

export function setup() {
  const st = stackState();
  const sessions = [];
  for (let i = 1; i <= USERS; i++) sessions.push(ssoLogin(st.hub_url, `user${String(i).padStart(2, '0')}@acme.test`));
  return { st, sessions };
}

function question() {
  const w = () => vocab[Math.floor(Math.random() * vocab.length)];
  const forms = [
    () => `How does ${w()} ${w()} work when the ${w()} is ${w()}?`,
    () => `Where do we ${w()} the ${w()} ${w()}?`,
    () => `What happens to a ${w()} ${w()} after ${w()} ${w()}?`,
    () => `Which function handles ${w()} ${w()} for ${w()}?`,
  ];
  return forms[Math.floor(Math.random() * forms.length)]();
}

export default function (data) {
  const s = data.sessions[(__VU - 1) % data.sessions.length];
  const t0 = Date.now();
  const r = call(s, 'POST', '/ask', { question: question() }, { tags: { name: 'ask' }, timeout: '60s' });
  check(r, { 'answered 200': (x) => x.status === 200, 'has an answer': (x) => x.status === 200 && !!x.json('answer') });
  sleep(Math.max(0, PERIOD - (Date.now() - t0) / 1000));
}

// p95 upper bound and the share within 400 ms from the hub's dth_retrieval_duration_seconds histogram.
function histogram(text, stage) {
  const buckets = [];
  let count = 0;
  for (const line of text.split('\n')) {
    if (line.startsWith(`dth_retrieval_duration_seconds_bucket{stage="${stage}"`)) {
      const le = line.match(/le="([^"]+)"/)[1];
      buckets.push([le === '+Inf' ? Infinity : Number(le), Number(line.split(' ').pop())]);
    } else if (line.startsWith(`dth_retrieval_duration_seconds_count{stage="${stage}"}`)) {
      count = Number(line.split(' ').pop());
    }
  }
  buckets.sort((a, b) => a[0] - b[0]);
  const q = buckets.find(([, c]) => c >= 0.95 * count);
  const at400 = buckets.find(([le]) => le === 0.4);
  return { p95: q ? q[0] : Infinity, within: count ? (at400 ? at400[1] : 0) / count : 0, count };
}

export function teardown(data) {
  const text = http.get(data.st.metrics_url).body;
  const total = histogram(text, 'total');
  p95.add(total.p95);
  within.add(total.within);
  for (const s of Object.keys(stageP95)) stageP95[s].add(histogram(text, s).p95);
  console.log(`retrieval: ${total.count} samples, p95 ≤ ${total.p95}s, ${(total.within * 100).toFixed(1)}% within 400 ms`);
}
