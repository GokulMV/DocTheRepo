import { expect, test } from '@playwright/test';
import { eventually, prs, push, signIn, stack } from '../lib/stack';

// Plan § 10 cold start: an empty hub → first SSO sign-in (becomes owner) → setup checklist → connect the
// git host and the team's own LLM → track a repo → the repository's documents are written in the Hub → a
// developer push arrives by webhook and the affected documents are rewritten → a cited answer.
test('cold start: connect, push, documents written, cited answer', async ({ page }) => {
  const s = await stack();
  const repo = 'acme/payments';
  const t0 = Date.now();
  const mark = (what: string) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${what}`);

  await signIn(page, 'owner@acme.test');
  await page.goto('/setup');
  const progress = page.getByText(/\d+ of \d+ done\.$/);
  const [done, total] = ((await progress.textContent()) ?? '').match(/(\d+) of (\d+)/)!.slice(1).map(Number);
  expect(done, 'a fresh hub is not set up').toBeLessThan(total);

  // Git host (webhook mode: the hub registers the webhook when a repo is tracked).
  mark(`Git host (webhook mode: the hub registers the webhook when a repo is tracked).`);
  await page.goto('/connectors');
  await expect(page.getByRole('button', { name: 'Connect with GitHub' })).toBeVisible(); // nothing connected: one click first
  await page.getByRole('button', { name: /GitLab, a token/ }).click();
  const connect = page.getByRole('dialog', { name: 'Connect GitLab or GitHub by hand' });
  await connect.getByLabel('API base URL').fill(s.github_api_url);
  await connect.getByLabel('Access token').fill('ghp_e2e_token');
  await connect.getByRole('button', { name: 'Connect', exact: true }).click();
  await expect(page.getByText('Finish webhook setup')).toBeVisible();
  await page.getByRole('button', { name: 'Done' }).click();
  await page.getByRole('button', { name: 'Test' }).click();
  const testDialog = page.getByRole('dialog', { name: 'Connector test' });
  await expect(testDialog.getByText(`signed in as ${s.github_bot}`)).toBeVisible();
  await expect(testDialog.getByText(repo)).toBeVisible();
  await page.keyboard.press('Escape');

  // Bring your own LLM: the stub speaks the OpenAI protocol.
  mark(`Bring your own LLM: the stub speaks the OpenAI protocol.`);
  await page.goto('/providers');
  await page.getByRole('button', { name: 'Add provider' }).click();
  const add = page.getByRole('dialog', { name: 'Add an LLM provider' });
  await add.getByLabel('Provider').selectOption('openai_compat');
  await add.getByLabel('Base URL').fill(s.llm_url);
  await add.getByLabel(/API key/).fill('sk-team');
  // "Use it for everything" is on for the first provider: one model for writing and answering, one for search.
  await add.getByLabel('Model', { exact: true }).fill('stub');
  await add.getByLabel(/Embedding model/).fill('stub-embed');
  await add.getByRole('button', { name: 'Advanced' }).click();
  await add.getByLabel('Display name').fill('Team LLM');
  await add.getByRole('button', { name: 'Add provider' }).click();
  await expect(add.getByText('Team LLM is ready')).toBeVisible();
  await expect(add.getByText(/It now handles: Docs generation, Docs \(short code\), Ask \(Q&A\), Error explanations, Change triage, Known-issue suggestions, Decisions, Security scans, Embeddings/)).toBeVisible();
  await add.getByRole('button', { name: 'Done' }).click();
  await expect(page.getByRole('cell', { name: 'Team LLM' }).first()).toBeVisible();
  await page.getByLabel('Model to test').fill('stub');
  await page.getByRole('button', { name: 'Test' }).click();
  await expect(page.getByText(/ok|reachable|\d+ ms/i).first()).toBeVisible();
  await page.getByText('Advanced: a model per feature').click();
  for (const feature of ['Docs generation', 'Ask (Q&A)', 'Change triage', 'Embeddings']) {
    const row = page.getByRole('row').filter({ has: page.getByLabel(`${feature}: provider`) });
    await expect(row.getByLabel(`${feature}: provider`)).toHaveValue(/.+/);
    await expect(row.getByRole('status')).toHaveText('Active');
  }

  // Track the repository: the webhook is registered on the git host.
  mark(`Track the repository: the webhook is registered on the git host.`);
  await page.goto('/repos');
  await page.getByRole('button', { name: 'Track repository' }).click();
  const track = page.getByRole('dialog', { name: 'Track a repository' });
  await track.getByLabel('Repository').fill(repo);
  await track.getByRole('button', { name: 'Track' }).click();
  const tracked = page.getByRole('status').filter({ hasText: `Tracking ${repo}.` });
  await expect(tracked).toContainText('docs are being written now');
  await expect(tracked).toContainText('Webhook: registered');
  await expect(page.getByRole('cell', { name: repo })).toBeVisible();

  // Tracking started the first sync: the code is read, then the repository's documents are written in the Hub.
  mark(`Tracking started the first sync; the documents are written in the Hub.`);
  const repos = await (await page.request.get('/api/v1/repos')).json();
  const repoId: string = repos.items.find((r: { full_name: string }) => r.full_name === repo).id;
  type Doc = { type: string; key: string; status: string; source_sha: string; updated_at: string };
  const docs = async (): Promise<Doc[]> => (await (await page.request.get(`/api/v1/repo-docs?repo_id=${repoId}`)).json()).items ?? [];
  await eventually(async () => {
    const ds = await docs();
    return ds.some((d) => d.type === 'overview' && d.status === 'ok') && ds.some((d) => d.type === 'architecture') && ds.some((d) => d.type === 'module') ? ds : undefined;
  }, 'the overview, architecture and module guides', 120_000);
  expect(await prs(repo), 'documents live in the Hub: no docs PR').toHaveLength(0);
  // A developer pushes new code; the webhook delivers it and the affected documents are rewritten.
  mark(`A developer pushes new code; the affected documents are rewritten.`);
  const pushed = await push(repo, {
    'refunds/chargeback.go': `package refunds

// HandleChargeback reverses the ledger entries of a disputed payment and freezes further refunds on it
// until the chargeback dispute is resolved.
func HandleChargeback(paymentID string, amountMinor int64) error {
	return nil
}
`,
  });
  // The module guide covering refunds/ is rewritten from the pushed commit. (Compare commits, not
  // updated_at: the list reports whole seconds, and the rewrite can land in the same second as the first write.)
  await eventually(async () => (await docs()).some((d) => d.type === 'module' && d.source_sha === pushed.sha), 'a module guide rewritten from the pushed commit', 120_000);

  // The Docs page opens on the overview, with every document in its navigation.
  mark(`The Docs page has the documents.`);
  await page.goto('/docs');
  await expect(page.getByRole('heading', { level: 1, name: 'Overview' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'At a glance' })).toBeVisible();
  const nav = page.getByRole('navigation', { name: 'Documents' });
  await nav.getByRole('link', { name: 'Architecture' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Architecture' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Architectural style' })).toBeVisible();
  await expect(page.getByRole('button', { name: /confidence/ }).first()).toBeVisible();

  // Ask, and get a cited answer that points at the new code.
  mark(`Ask, and get a cited answer.`);
  await page.goto('/ask');
  await page.getByLabel('Question').fill('How are chargebacks handled?');
  await page.getByRole('button', { name: 'Ask', exact: true }).click();
  const citations = page.locator('ol li');
  await expect(citations.first()).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText(/refunds\/chargeback\.go/).first()).toBeVisible();
  await expect(page.getByText(/\[1\]/).first()).toBeVisible();

  // The checklist reflects the finished setup, and Activity shows the work.
  mark(`The checklist reflects the finished setup, and Activity shows the work.`);
  await page.goto('/setup');
  await expect(page.getByText('Everything is set up. This page stays as a health check.')).toBeVisible();
  await page.goto('/activity');
  await expect(page.getByRole('cell', { name: /Docs writing/ }).first()).toBeVisible();
});
