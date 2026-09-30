import { expect, type Browser, type BrowserContext, type Page } from '@playwright/test';

export const CONTROL = process.env.DTH_E2E_CONTROL ?? 'http://127.0.0.1:18099';

export interface StackState {
  hub_url: string;
  github_api_url: string;
  github_bot: string;
  llm_url: string;
  repos: string[];
}

export interface PR {
  number: number;
  head: string;
  state: string;
  merged: boolean;
  title: string;
}

async function ctl<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(CONTROL + path, { method, headers: { 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
  if (!res.ok) throw new Error(`control ${method} ${path}: ${res.status} ${await res.text()}`);
  return (res.status === 204 ? undefined : await res.json()) as T;
}

/** The stack's URLs and fixture repositories. */
export const stack = () => ctl<StackState>('GET', '/state');

/** Commits files to a fixture repo on the GitHub mock (a signed push webhook follows for tracked repos). */
export const push = (repo: string, files: Record<string, string | null>, author = 'ann') =>
  ctl<{ sha: string }>('POST', '/github/push', { repo, files, author });

export const prs = (repo: string) => ctl<PR[]>('GET', `/github/prs?repo=${encodeURIComponent(repo)}`);
export const setChecks = (repo: string, number: number, state: 'success' | 'failure' | 'pending') =>
  ctl<void>('POST', '/github/checks', { repo, number, state });
export const file = (repo: string, path: string) =>
  ctl<{ exists: boolean; content: string }>('GET', `/github/file?repo=${encodeURIComponent(repo)}&path=${encodeURIComponent(path)}`);

/** Stub LLM counters per feature (docgen, qa, triage, embedding). */
export async function llmStats(): Promise<Record<string, { calls: number; max_in_flight: number }>> {
  const s = await stack();
  const res = await fetch(s.llm_url.replace(/\/v1$/, '') + '/_stub/stats');
  return res.json();
}

/** Polls fn until it returns a truthy value. */
export async function eventually<T>(fn: () => Promise<T | undefined | null | false>, what: string, timeoutMs = 90_000): Promise<T> {
  const end = Date.now() + timeoutMs;
  let last: unknown;
  while (Date.now() < end) {
    try {
      const v = await fn();
      if (v) return v;
    } catch (e) {
      last = e;
    }
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error(`timed out waiting for ${what}${last ? `: ${last}` : ''}`);
}

/** Signs in through the OIDC mock as email (the first account ever becomes owner). */
export async function signIn(page: Page, email: string, groups: string[] = []) {
  await ctl('POST', '/oidc/user', { email, groups });
  await page.goto('/login');
  await page.getByRole('button', { name: 'Sign in with single sign-on' }).click();
  await page.waitForURL((u) => !u.pathname.startsWith('/login'));
}

/** A fresh browser context signed in as email. */
export async function session(browser: Browser, email: string): Promise<{ ctx: BrowserContext; page: Page; api: Api }> {
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  await signIn(page, email);
  return { ctx, page, api: await Api.for(page) };
}

/** Api calls the hub with the page's session cookie and CSRF token (for setup a spec is not about). */
export class Api {
  private constructor(private page: Page, private csrf: string) {}

  static async for(page: Page): Promise<Api> {
    const res = await page.request.get('/api/v1/me');
    expect(res.ok(), 'signed in').toBeTruthy();
    const me = await res.json();
    return new Api(page, me.csrf_token);
  }

  async call<T = any>(method: string, path: string, body?: unknown): Promise<{ status: number; data: T }> {
    const res = await this.page.request.fetch('/api/v1' + path, {
      method,
      headers: { 'X-CSRF-Token': this.csrf, 'Content-Type': 'application/json' },
      data: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await res.text();
    return { status: res.status(), data: text ? JSON.parse(text) : undefined };
  }

  get = <T = any>(path: string) => this.call<T>('GET', path);
  post = <T = any>(path: string, body?: unknown) => this.call<T>('POST', path, body ?? {});
  put = <T = any>(path: string, body?: unknown) => this.call<T>('PUT', path, body);
}

export interface Hub {
  connectorId: string;
  providerId: string;
  repoIds: Record<string, string>;
}

/**
 * ensureConfigured makes the hub ready for a spec that is not about setup: a webhook GitHub connector on
 * the mock, the stub LLM routed for every feature, and the given repos tracked and indexed. It reuses
 * what the cold-start spec (or an earlier run) created.
 */
export async function ensureConfigured(api: Api, repos: string[]): Promise<Hub> {
  const s = await stack();
  let conns = (await api.get('/connectors')).data.items as any[];
  let conn = conns.find((c) => c.type === 'github');
  if (!conn) {
    const r = await api.post('/connectors', { type: 'github', name: 'GitHub', credentials: 'ghp_e2e', webhook_secret: 'e2e-webhook-secret', mode: 'webhook', config: { base_url: s.github_api_url, auth: 'token' } });
    expect(r.status).toBe(201);
    conn = { id: r.data.id };
  }
  let provs = (await api.get('/providers')).data.items as any[];
  let prov = provs.find((p) => p.kind === 'openai_compat');
  if (!prov) {
    const r = await api.post('/providers', { kind: 'openai_compat', name: 'Stub', base_url: s.llm_url, api_key: 'sk-e2e' });
    expect(r.status).toBe(201);
    prov = { id: r.data.id };
  }
  for (const f of ['docgen', 'qa', 'triage', 'embedding']) {
    const r = await api.put(`/routes/${f}`, { provider_id: prov.id, model: f === 'embedding' ? 'stub-embed' : 'stub' });
    expect(r.status).toBe(204);
  }
  const tracked = (await api.get('/repos')).data.items as any[];
  const repoIds: Record<string, string> = {};
  const added: string[] = [];
  for (const name of repos) {
    let r = tracked.find((x) => x.full_name === name);
    if (!r) {
      const res = await api.post('/repos', { connector_id: conn.id, full_name: name, push_mode: 'direct' });
      expect(res.status).toBe(201);
      r = { id: res.data.id, last_processed_sha: '' };
      added.push(r.id);
    }
    repoIds[name] = r.id;
  }
  await indexed(api, repoIds);
  // Existing Markdown (ADRs, READMEs) enters the index through a docs import, as an admin would run it.
  for (const id of added) {
    const imp = await api.post(`/repos/${id}/import`, {});
    expect(imp.status).toBe(202);
    await jobDone(api, imp.data.job_id);
  }
  return { connectorId: conn.id, providerId: prov.id, repoIds };
}

/** Waits until a job reaches done (fails fast on failed/dead). */
export async function jobDone(api: Api, jobId: string) {
  await eventually(async () => {
    const j = (await api.get(`/jobs/${jobId}`)).data;
    if (j.status === 'failed' || j.status === 'dead') throw new Error(`job ${jobId} ${j.status}: ${j.error}`);
    return j.status === 'done';
  }, `job ${jobId} done`, 120_000);
}

/** Syncs the connector until every repo has been processed. */
export async function indexed(api: Api, repoIds: Record<string, string>) {
  const pending = async () => ((await api.get('/repos')).data.items as any[]).filter((r) => Object.values(repoIds).includes(r.id) && !r.last_processed_sha);
  if ((await pending()).length === 0) return;
  const conns = (await api.get('/connectors')).data.items as any[];
  await api.post(`/connectors/${conns.find((c) => c.type === 'github').id}/sync`);
  await eventually(async () => (await pending()).length === 0, 'repositories indexed', 120_000);
}
