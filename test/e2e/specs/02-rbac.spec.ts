import { expect, test, type Page } from '@playwright/test';
import { ensureConfigured, session } from '../lib/stack';

const SECRET_Q = 'What is the Nightingale enterprise discount tier?';

async function ask(page: Page, question: string) {
  await page.goto('/ask');
  await page.getByLabel('Question').fill(question);
  await page.getByRole('button', { name: 'Ask', exact: true }).click();
}

// Plan § 10 RBAC: a viewer without access to repo B cannot see B's chunks in answers, graph, or docs; an
// owner can. Access is granted through the Users & access page.
test('viewer without access to acme/billing never sees it', async ({ browser }) => {
  const owner = await session(browser, 'owner@acme.test');
  const hub = await ensureConfigured(owner.api, ['acme/payments', 'acme/billing']);
  const billing = hub.repoIds['acme/billing'];

  // The owner (reads everything) gets a cited answer from the restricted repo.
  await ask(owner.page, SECRET_Q);
  await expect(owner.page.getByText('acme/billing ·').first()).toBeVisible({ timeout: 30_000 });
  await expect(owner.page.getByText(/nightingale/i).first()).toBeVisible();
  const billingDocs = (await owner.api.get(`/repo-docs?repo_id=${billing}`)).data.items as { id: string }[];
  expect(billingDocs.length, 'billing has documents').toBeGreaterThan(0);

  // A new teammate signs in: SSO users start as viewers with no repository access.
  const viewer = await session(browser, 'vera@acme.test');
  await expect(viewer.page.getByText('viewer', { exact: true })).toBeVisible();

  // The owner grants read access to acme/payments only, in the UI.
  await owner.page.goto('/users');
  const row = owner.page.getByRole('row').filter({ hasText: 'vera@acme.test' });
  await expect(row.getByLabel('Role for vera@acme.test')).toHaveValue('viewer');
  await row.getByRole('button', { name: 'Repositories' }).click();
  const dialog = owner.page.getByRole('dialog', { name: 'Repository access: vera@acme.test' });
  await dialog.getByLabel('acme/payments').check();
  await expect(dialog.getByLabel('acme/billing')).not.toBeChecked();
  await dialog.getByRole('button', { name: 'Save' }).click();
  await expect(dialog).toBeHidden();
  // Reopening shows the saved grant (the editor loads existing grants).
  await row.getByRole('button', { name: 'Repositories' }).click();
  await expect(dialog.getByLabel('acme/payments')).toBeChecked();
  await owner.page.keyboard.press('Escape');

  // Answers: the restricted repo is never retrieved for the viewer.
  await ask(viewer.page, SECRET_Q);
  await expect(viewer.page.getByText('I could not find this in the connected sources.').first()).toBeVisible({ timeout: 30_000 });
  await expect(viewer.page.getByText('acme/billing')).toHaveCount(0);
  const direct = await viewer.api.post('/ask', { question: SECRET_Q });
  expect(direct.status).toBe(200);
  expect(JSON.stringify(direct.data.citations)).not.toContain('acme/billing');
  const scoped = await viewer.api.post('/ask', { question: SECRET_Q, scope: { repo_ids: [billing] } });
  expect(scoped.status, 'scoping a question to a forbidden repo is refused').toBe(403);
  // The viewer still gets answers from what they can read.
  const allowed = await viewer.api.post('/ask', { question: 'How are failed refunds retried?' });
  expect(JSON.stringify(allowed.data.citations)).toContain('acme/payments');

  // Docs: the viewer reads acme/payments' documents; acme/billing's cannot be listed or opened.
  await viewer.page.goto('/docs');
  await expect(viewer.page.getByRole('heading', { level: 1, name: 'Overview' })).toBeVisible();
  await expect(viewer.page.getByText('acme/billing')).toHaveCount(0);
  const listed = await viewer.api.get(`/repo-docs?repo_id=${billing}`);
  expect(listed.status, 'a forbidden repository looks missing').toBe(404);
  const node = await viewer.api.get(`/repo-docs/${billingDocs[0].id}`);
  expect(node.status, 'a forbidden document is indistinguishable from a missing one').toBe(404);

  // Graph: no billing entities, and a billing entity's neighbourhood is not reachable.
  const ownerEnts = (await owner.api.get('/palace/entities?kind=repo')).data.items as any[];
  const billingEnt = ownerEnts.find((e) => JSON.stringify(e).includes('acme/billing'));
  expect(billingEnt, 'the owner sees the billing repo entity').toBeTruthy();
  const viewerEnts = (await viewer.api.get('/palace/entities?kind=repo')).data.items as any[];
  expect(JSON.stringify(viewerEnts)).not.toContain('acme/billing');
  expect(JSON.stringify(viewerEnts)).toContain('acme/payments');
  const graph = await viewer.api.get(`/palace/entities/${billingEnt.id}/graph?depth=2`);
  expect(graph.status, 'a forbidden entity is indistinguishable from a missing one').toBe(404);

  // Administration stays closed to viewers.
  expect((await viewer.api.get('/connectors')).status).toBe(403);
  expect((await viewer.api.put(`/users/x/repo-access`, { repo_ids: [billing] })).status).toBe(403);

  await owner.ctx.close();
  await viewer.ctx.close();
});
