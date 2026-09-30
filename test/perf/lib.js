// Shared helpers for the k6 runs against the E2E stack (go run ./test/e2e/stack …).
import http from 'k6/http';
import { check, fail, sleep } from 'k6';

export const CONTROL = __ENV.DTH_CONTROL || 'http://127.0.0.1:18099';

export function stackState() {
  const res = http.get(`${CONTROL}/state`);
  if (res.status !== 200) fail(`stack control API not reachable at ${CONTROL}: start the stack first`);
  return res.json();
}

/** A session: cookie + CSRF token for hub API calls from any VU. */
export function session(hub, cookie) {
  const headers = { Cookie: `dth_session=${cookie}`, 'Content-Type': 'application/json' };
  const me = http.get(`${hub}/api/v1/me`, { headers });
  if (me.status !== 200) fail(`session check failed: ${me.status} ${me.body}`);
  return { hub, cookie, csrf: me.json('csrf_token') };
}

export function call(s, method, path, body, params = {}) {
  const headers = Object.assign({ Cookie: `dth_session=${s.cookie}`, 'X-CSRF-Token': s.csrf, 'Content-Type': 'application/json' }, params.headers || {});
  return http.request(method, `${s.hub}/api/v1${path}`, body === undefined ? null : JSON.stringify(body), Object.assign({}, params, { headers }));
}

/** Local-mode owner sign-in (stack -auth local). */
export function localLogin(hub, email, password) {
  const jar = new http.CookieJar();
  const res = http.post(`${hub}/api/v1/auth/local/login`, JSON.stringify({ email, password }), { headers: { 'Content-Type': 'application/json' }, jar });
  if (res.status !== 200) fail(`local login: ${res.status} ${res.body}`);
  return session(hub, jar.cookiesForURL(hub).dth_session[0]);
}

/** SSO sign-in through the OIDC mock (stack -auth oidc): the control API picks who signs in. */
export function ssoLogin(hub, email) {
  const set = http.post(`${CONTROL}/oidc/user`, JSON.stringify({ email }), { headers: { 'Content-Type': 'application/json' } });
  if (set.status !== 204) fail(`set oidc user: ${set.status}`);
  const jar = new http.CookieJar();
  const res = http.get(`${hub}/api/v1/auth/login?return=/`, { jar, redirects: 10 });
  const c = jar.cookiesForURL(hub).dth_session;
  if (!c) fail(`sso login for ${email} failed: ${res.status} ${res.url}`);
  return session(hub, c[0]);
}

/**
 * configure makes the hub ready: a webhook GitHub connector on the mock, the stub model routed, and repos
 * tracked (direct landing) and indexed. Idempotent.
 */
export function configure(s, st, repos, { docgen = true } = {}) {
  let conns = call(s, 'GET', '/connectors').json('items');
  let conn = conns.find((c) => c.type === 'github');
  if (!conn) {
    const r = call(s, 'POST', '/connectors', { type: 'github', name: 'GitHub', credentials: 'ghp_perf', webhook_secret: 'perf-secret', mode: 'webhook', config: { base_url: st.github_api_url, auth: 'token' } });
    check(r, { 'connector created': (x) => x.status === 201 }) || fail(r.body);
    conn = r.json();
  }
  let prov = call(s, 'GET', '/providers').json('items').find((p) => p.kind === 'openai_compat');
  if (!prov) {
    const r = call(s, 'POST', '/providers', { kind: 'openai_compat', name: 'Stub', base_url: st.llm_url, api_key: 'sk-perf' });
    check(r, { 'provider created': (x) => x.status === 201 }) || fail(r.body);
    prov = r.json();
  }
  const features = docgen ? ['docgen', 'qa', 'triage', 'embedding'] : ['qa', 'embedding'];
  for (const f of features) {
    const r = call(s, 'PUT', `/routes/${f}`, { provider_id: prov.id, model: f === 'embedding' ? 'stub-embed' : 'stub' });
    check(r, { [`route ${f}`]: (x) => x.status === 204 }) || fail(r.body);
  }
  const tracked = call(s, 'GET', '/repos?limit=200').json('items');
  const ids = {};
  for (const name of repos) {
    let r = tracked.find((x) => x.full_name === name);
    if (!r) {
      const res = call(s, 'POST', '/repos', { connector_id: conn.id, full_name: name, push_mode: 'direct' });
      check(res, { 'repo tracked': (x) => x.status === 201 }) || fail(res.body);
      r = res.json();
    }
    ids[name] = r.id;
  }
  call(s, 'POST', `/connectors/${conn.id}/sync`);
  waitFor(() => call(s, 'GET', '/repos?limit=200').json('items').filter((r) => Object.values(ids).includes(r.id) && !r.last_processed_sha).length === 0, 'repositories indexed', 900);
  return { connectorId: conn.id, providerId: prov.id, repoIds: ids };
}

export function waitFor(fn, what, timeoutSec) {
  const end = Date.now() + timeoutSec * 1000;
  while (Date.now() < end) {
    if (fn()) return;
    sleep(2);
  }
  fail(`timed out waiting for ${what}`);
}

export function stub(st, path, method = 'GET') {
  return http.request(method, st.llm_url.replace(/\/v1$/, '') + path);
}

/** Counts jobs of a type in a status (walks pages). */
export function countJobs(s, type, status) {
  let n = 0;
  let cursor = '';
  for (;;) {
    const r = call(s, 'GET', `/jobs?type=${type}&status=${status}&limit=200${cursor ? `&cursor=${cursor}` : ''}`);
    n += r.json('items').length;
    cursor = r.json('next_cursor');
    if (!cursor) return n;
  }
}
