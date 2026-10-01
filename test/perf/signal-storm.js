// Signal storm (plan § 10): sustained error events across ingestion paths with a fixed set of distinct
// fingerprints. Asserts that ingress stays fast (p99), that no accepted event is lost (stored occurrences
// equal accepted events), that each fingerprint becomes exactly one issue, and that decode work equals the
// number of new fingerprints — not the number of events.
//
// The plan's full target is 20,000 events/s for 10 minutes on 3 api replicas (2 vCPU / 4 GB each). On one
// machine, scale it down with env vars (defaults below). Start the stack first:
//
//   go run ./test/e2e/stack -hub ./bin/dth-hub -auth local
//   k6 run -e EVENTS_PER_SEC=2000 -e DURATION=60s -e FINGERPRINTS=500 test/perf/signal-storm.js
//
// Paths: Amazon Data Firehose deliveries (CloudWatch Logs subscription format, BATCH log events per request)
// and the generic webhook (BATCH events per request). Pub/Sub needs an emulator and is not part of this run.
import http from 'k6/http';
import encoding from 'k6/encoding';
import exec from 'k6/execution';
import { check, sleep } from 'k6';
import { Counter, Trend } from 'k6/metrics';
import { call, localLogin, stackState, waitFor } from './lib.js';

const RATE = Number(__ENV.EVENTS_PER_SEC || 2000);
const DURATION = __ENV.DURATION || '60s';
const FPS = Number(__ENV.FINGERPRINTS || 500);
const BATCH = Number(__ENV.BATCH || 200);
// The run ID must come from setup(): module code runs once per VU, so a Date.now() here would differ per VU.
const RUN_ENV = __ENV.RUN_ID || '';

const accepted = new Counter('events_accepted');
const retried = new Counter('deliveries_retried');
// events_balance: +events accepted by the hub (2xx) during the run, −occurrences stored at the end. Zero = no loss.
const balance = new Counter('events_balance');
const issuesSeen = new Counter('issues_created');
const decodeJobs = new Counter('decode_jobs');
const ingress = new Trend('ingress_ms', true);

const perPathRate = Math.max(1, Math.round(RATE / BATCH / 2)); // requests per second per path

export const options = {
  scenarios: {
    firehose: { executor: 'constant-arrival-rate', exec: 'firehose', rate: perPathRate, timeUnit: '1s', duration: DURATION, preAllocatedVUs: 20, maxVUs: 200 },
    webhook: { executor: 'constant-arrival-rate', exec: 'webhook', rate: perPathRate, timeUnit: '1s', duration: DURATION, preAllocatedVUs: 20, maxVUs: 200 },
  },
  summaryTrendStats: ['avg', 'med', 'p(95)', 'p(99)', 'max'],
  setupTimeout: '2m',
  teardownTimeout: '10m',
  thresholds: {
    // Webhooks answer 202 as soon as events are queued: the plan's 300 ms. Firehose is answered only once its
    // events are persisted (no loss on a crash), which costs up to one 1-second aggregation window.
    'ingress_ms{path:webhook}': ['p(99)<300'],
    'ingress_ms{path:firehose}': ['p(99)<2500'],
    events_balance: ['count==0'],
    'checks{check:delivered}': ['rate>0.999'],
  },
};

// Fingerprint k → a letter code: numbers are normalised away by fingerprinting, letters are not.
function code(k) {
  let s = '';
  for (let i = 0; i < 4; i++) {
    s += String.fromCharCode(97 + (k % 26));
    k = Math.floor(k / 26);
  }
  return s;
}

export function setup() {
  const RUN = RUN_ENV || `${Date.now()}`.slice(-6);
  const st = stackState();
  const s = localLogin(st.hub_url, 'owner@acme.test', 'correct horse battery staple');
  const mk = (type, name) => {
    const secret = `storm-${type}-${RUN}-secret-0123456789`;
    const r = call(s, 'POST', '/connectors', { type, name: `${name} ${RUN}`, mode: 'webhook', webhook_secret: secret });
    if (r.status !== 201) throw new Error(`create ${type}: ${r.status} ${r.body}`);
    return { url: st.hub_url + r.json('webhook_path'), secret };
  };
  return { run: RUN, hub: st.hub_url, cookie: s.cookie, csrf: s.csrf, firehose: mk('firehose', 'Storm Firehose'), webhook: mk('generic', 'Storm webhook'), start: Date.now() };
}

// deliver posts with Firehose-style retries on 429/503 (the sender's contract) and counts accepted events.
function deliver(url, body, headers, n) {
  for (let attempt = 0; attempt < 5; attempt++) {
    const path = url.includes('/firehose/') ? 'firehose' : 'webhook';
    const res = http.post(url, body, { headers, tags: { path } });
    ingress.add(res.timings.duration, { path });
    if (res.status === 200 || res.status === 202) {
      accepted.add(n);
      balance.add(n);
      check(res, { delivered: () => true });
      return;
    }
    if (res.status !== 429 && res.status !== 503) {
      check(res, { delivered: () => false });
      console.error(`delivery failed: ${res.status} ${res.body}`);
      return;
    }
    retried.add(1);
    sleep(0.5 * (attempt + 1));
  }
  check(null, { delivered: () => false });
}

function pick() {
  return Math.floor(Math.random() * FPS);
}

export function firehose(data) {
  const RUN = data.run;
  const id = `${exec.scenario.name}-${exec.vu.idInTest}-${exec.scenario.iterationInTest}`;
  const now = Date.now();
  const logEvents = [];
  for (let i = 0; i < BATCH; i++) {
    const c = code(pick());
    logEvents.push({ id: `${RUN}-${id}-${i}`, timestamp: now, message: `ERROR storm-${RUN} handler ${c} failed: upstream ${c} rejected request` });
  }
  const rec = JSON.stringify({ messageType: 'DATA_MESSAGE', owner: '123456789012', logGroup: `/ecs/storm-${RUN}`, logStream: 's', logEvents });
  const body = JSON.stringify({ requestId: id, timestamp: now, records: [{ data: encoding.b64encode(rec) }] });
  deliver(data.firehose.url, body, { 'Content-Type': 'application/json', 'X-Amz-Firehose-Access-Key': data.firehose.secret }, BATCH);
}

export function webhook(data) {
  const RUN = data.run;
  const id = `${exec.scenario.name}-${exec.vu.idInTest}-${exec.scenario.iterationInTest}`;
  const events = [];
  for (let i = 0; i < BATCH; i++) {
    const c = code(pick());
    events.push({ id: `${RUN}-${id}-${i}`, title: `storm-${RUN} job ${c} crashed`, message: `job ${c} crashed`, severity: 'error', service: `storm-${RUN}`, kind: 'error' });
  }
  deliver(data.webhook.url, JSON.stringify({ events }), { 'Content-Type': 'application/json', Authorization: `Bearer ${data.webhook.secret}` }, BATCH);
}

// sumIssues walks the Inbox for this run's issues and returns [issues, occurrences].
function sumIssues(s, RUN) {
  let issues = 0;
  let occ = 0;
  let cursor = '';
  for (;;) {
    const r = call(s, 'GET', `/issues?q=storm-${RUN}&since=2h&limit=200${cursor ? `&cursor=${cursor}` : ''}`);
    for (const it of r.json('items')) {
      issues++;
      occ += it.occurrences + it.suppressed_count;
    }
    cursor = r.json('next_cursor');
    if (!cursor) return [issues, occ];
  }
}

export function teardown(data) {
  const s = { hub: data.hub, cookie: data.cookie, csrf: data.csrf };
  // Read stored occurrences until two reads agree (the last 1-second windows flush within a few seconds).
  let issues = 0;
  let occ = 0;
  let last = -1;
  waitFor(() => {
    [issues, occ] = sumIssues(s, data.run);
    const stable = occ === last && occ > 0;
    last = occ;
    return stable;
  }, 'aggregated occurrences to settle', 120);
  balance.add(-occ);
  issuesSeen.add(issues);
  // Each path has its own fingerprints (log lines vs. webhook titles): at most 2 × FINGERPRINTS issues.
  check(issues, { 'one issue per fingerprint (≤ 2 × FINGERPRINTS)': (n) => n <= 2 * FPS && n >= Math.min(FPS, 2 * FPS) * 0.9 });
  let jobs = 0;
  let cursor = '';
  for (;;) {
    const r = call(s, 'GET', `/jobs?type=decode_issue&limit=200${cursor ? `&cursor=${cursor}` : ''}`);
    for (const j of r.json('items')) if (new Date(j.created_at).getTime() >= data.start) jobs++;
    cursor = r.json('next_cursor');
    if (!cursor) break;
  }
  decodeJobs.add(jobs);
  check(jobs, { 'decode jobs = new fingerprints, not events': (n) => n === issues });
  console.log(`storm ${data.run}: ${issues} issues, ${occ} stored occurrences, ${jobs} decode jobs`);
}

function p99(sum, path) {
  const m = sum.metrics[`ingress_ms{path:${path}}`];
  return m && m.values['p(99)'] !== undefined ? m.values['p(99)'].toFixed(1) : '?';
}

export function handleSummary(sum) {
  const acc = sum.metrics.events_accepted ? sum.metrics.events_accepted.values.count : 0;
  const lines = [
    `events accepted: ${acc}`,
    `accepted − stored: ${sum.metrics.events_balance ? sum.metrics.events_balance.values.count : '?'} (0 = no event lost)`,
    `ingress p99: webhook ${p99(sum, 'webhook')} ms, firehose (after persistence) ${p99(sum, 'firehose')} ms`,
    `deliveries retried (backpressure): ${sum.metrics.deliveries_retried ? sum.metrics.deliveries_retried.values.count : 0}`,
    `issues: ${sum.metrics.issues_created ? sum.metrics.issues_created.values.count : '?'}`,
    `decode jobs: ${sum.metrics.decode_jobs ? sum.metrics.decode_jobs.values.count : '?'}`,
  ];
  return { stdout: lines.join('\n') + '\n', 'signal-storm-summary.json': JSON.stringify(sum, null, 2) };
}
