import { expect, test } from '@playwright/test';
import { ensureConfigured, jobDone, llmStats, push, session, stack } from '../lib/stack';

// Plan § 10 spend: a limit below the job's estimate → the push job is spend_blocked and the provider is
// never called; an admin can run it once above the ceiling (audited).
test('spend limit below the estimate blocks before any provider call', async ({ browser }) => {
  const owner = await session(browser, 'owner@acme.test');
  const hub = await ensureConfigured(owner.api, ['acme/catalog']);
  const catalog = hub.repoIds['acme/catalog'];
  const page = owner.page;
  const before = (await owner.api.get('/spend/limits')).data.items as any[];
  try {
    // A 100-token daily ceiling on this repository, set in the UI.
    await page.goto('/spend');
    await page.getByRole('button', { name: 'Add limit' }).click();
    const row = page.getByRole('row').filter({ has: page.getByLabel('Scope') }).last();
    await row.getByLabel('Scope', { exact: true }).selectOption('repo');
    await row.getByLabel('Scope key').fill(catalog);
    await row.getByLabel('Window').selectOption('day');
    await row.getByLabel('Max tokens').fill('100');
    await page.getByRole('button', { name: 'Save all' }).click();
    await expect.poll(async () => ((await owner.api.get('/spend/limits')).data.items as any[]).some((l) => l.scope === 'repo' && l.scope_key === catalog)).toBe(true);

    // A developer push: the pipeline estimates the docgen cost and the guard blocks it.
    const s = await stack();
    await fetch(s.llm_url.replace(/\/v1$/, '') + '/_stub/reset', { method: 'POST' });
    await push('acme/catalog', {
      'catalog/shipping.py': `"""Shipping cost rules."""


def shipping_cost_cents(weight_grams: int, express: bool = False) -> int:
    """Flat 499 cents up to 1 kg, plus 150 cents per extra kg; express doubles it."""
    base = 499 + max(0, (weight_grams - 1000 + 999) // 1000) * 150
    return base * 2 if express else base
`,
    });
    await page.goto('/activity');
    await page.getByLabel('Job status').selectOption('spend_blocked');
    const blocked = page.getByRole('row').filter({ hasText: 'code_push' }).filter({ hasText: 'spend_blocked' }).first();
    await expect(async () => {
      await page.reload();
      await page.getByLabel('Job status').selectOption('spend_blocked');
      await expect(blocked).toBeVisible({ timeout: 2_000 });
    }).toPass({ timeout: 60_000 });

    const stats = await llmStats();
    expect(stats.docgen?.calls ?? 0, 'no docgen call reached the provider').toBe(0);
    expect(stats.triage?.calls ?? 0, 'no triage call either').toBe(0);

    // The job explains itself; an admin runs it once above the ceiling.
    await blocked.click();
    const dialog = page.getByRole('dialog', { name: 'code_push · spend_blocked' });
    await expect(dialog.getByText(/spend|ceiling|limit/i).first()).toBeVisible();
    page.once('dialog', (d) => d.accept());
    await dialog.getByRole('button', { name: 'Retry above ceiling' }).click();
    const queued = await dialog.getByText(/^Queued job /).textContent();
    const retryId = queued!.replace('Queued job ', '').replace(/\.$/, '');
    await jobDone(owner.api, retryId);
    expect((await llmStats()).docgen?.calls ?? 0, 'the override run documented the change').toBeGreaterThan(0);
    const audit = await owner.api.get('/audit');
    expect(JSON.stringify(audit.data)).toMatch(/override/i);
  } finally {
    await owner.api.put('/spend/limits', { items: before });
    await owner.ctx.close();
  }
});
