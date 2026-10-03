import { expect, test } from '@playwright/test';
import { eventually, file, prs, push, setChecks, signIn, stack } from '../lib/stack';

// docsPRMerged waits for a docs PR not in seen and for the hub's lifecycle sweep to merge it (reporting CI
// green if the PR is still waiting).
async function docsPRMerged(repo: string, seen: number[]): Promise<number> {
  const pr = await eventually(async () => (await prs(repo)).find((p) => !seen.includes(p.number) && p.head.startsWith('dth/docs-')), 'a docs PR', 120_000);
  if (!pr.merged) await setChecks(repo, pr.number, 'success');
  await eventually(async () => (await prs(repo)).find((p) => p.number === pr.number && p.merged), `docs PR #${pr.number} merged`);
  return pr.number;
}

// Plan § 10 cold start: an empty hub → first SSO sign-in (becomes owner) → setup checklist → connect the
// git host and the team's own LLM → track a repo → a developer push arrives by webhook → a docs PR is
// opened and merged when CI is green → the question is answered with a citation to the new doc.
test('cold start: connect, push, docs PR merged, cited answer', async ({ page }) => {
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
  await expect(page.getByText('No connectors yet')).toBeVisible();
  await page.getByRole('button', { name: 'Connect git host' }).click();
  const connect = page.getByRole('dialog', { name: 'Connect GitHub or GitLab' });
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
  await expect(add.getByText(/It now handles: Docs generation, Docs \(short code\), Ask \(Q&A\), Error explanations, Change triage, Known-issue suggestions, Decisions, Embeddings/)).toBeVisible();
  await add.getByRole('button', { name: 'Done' }).click();
  await expect(page.getByRole('cell', { name: 'Team LLM' }).first()).toBeVisible();
  await page.getByLabel('Model to test').fill('stub');
  await page.getByRole('button', { name: 'Test' }).click();
  await expect(page.getByText(/ok|reachable|\d+ ms/i).first()).toBeVisible();
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

  // Tracking started the first sync: it documents the existing code; its docs PR merges once CI is green.
  mark(`Tracking started the first sync; its docs PR merges once CI is green.`);
  const first = await docsPRMerged(repo, []);

  // A developer pushes new code; the webhook delivers it and a new docs PR follows.
  mark(`A developer pushes new code; the webhook delivers it and a new docs PR follows.`);
  await push(repo, {
    'refunds/chargeback.go': `package refunds

// HandleChargeback reverses the ledger entries of a disputed payment and freezes further refunds on it
// until the chargeback dispute is resolved.
func HandleChargeback(paymentID string, amountMinor int64) error {
	return nil
}
`,
  });
  await docsPRMerged(repo, [first]);
  const doc = await file(repo, 'docs/generated/refunds/chargeback.go.md');
  expect(doc.exists, 'the generated doc is on main').toBe(true);
  expect(doc.content).toContain('HandleChargeback');

  // The Docs tree has the new page.
  mark(`The Docs tree has the new page.`);
  await page.goto('/docs');
  const tree = page.getByRole('navigation', { name: 'Documentation' });
  const open = async (name: string) => {
    const b = tree.getByRole('button', { name, exact: true });
    if ((await b.getAttribute('aria-expanded')) !== 'true') await b.click();
  };
  await open('acme/payments'); // the only repository: open already
  for (const dir of ['docs', 'generated', 'refunds']) await open(dir);
  await tree.getByRole('button', { name: /chargeback/ }).click();
  await expect(page.getByRole('heading', { name: /HandleChargeback/ }).first()).toBeVisible();

  // Ask, and get a cited answer that points at the new doc.
  mark(`Ask, and get a cited answer that points at the new doc.`);
  await page.goto('/ask');
  await page.getByLabel('Question').fill('How are chargebacks handled?');
  await page.getByRole('button', { name: 'Ask', exact: true }).click();
  const citations = page.locator('ol li');
  await expect(citations.first()).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText('docs/generated/refunds/chargeback.go.md').first()).toBeVisible();
  await expect(page.getByText(/\[1\]/).first()).toBeVisible();

  // The checklist reflects the finished setup, and Activity shows the work.
  mark(`The checklist reflects the finished setup, and Activity shows the work.`);
  await page.goto('/setup');
  await expect(page.getByText('Everything is set up. This page stays as a health check.')).toBeVisible();
  await page.goto('/activity');
  await expect(page.getByRole('cell', { name: /Docs update/ }).first()).toBeVisible();
});
