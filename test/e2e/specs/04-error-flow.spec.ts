import { createHmac } from 'node:crypto';
import { expect, test } from '@playwright/test';
import { Api, ensureConfigured, llmStats, signIn, stack } from '../lib/stack';

// Plan § 10 error flow: a Sentry error arrives by signed webhook → it appears in the Inbox, explained by
// the team's own model → "Mark as known" → the next identical error is counted as known, not decoded again,
// and the Inbox stays clean.
test('error flow: Sentry webhook → decoded in the Inbox → mark as known → next one suppressed', async ({ page }) => {
  await signIn(page, 'owner@acme.test');
  const api = await Api.for(page);
  const hub = await ensureConfigured(api, []);
  expect((await api.put('/routes/decode', { provider_id: hub.providerId, model: 'stub' })).status).toBe(200);

  // Connect Sentry in the UI: the Hub gives the link for Sentry, and Sentry's own signing secret is pasted back.
  await page.goto('/connectors');
  await page.getByRole('button', { name: 'Connect Sentry' }).click();
  await page.getByRole('dialog', { name: 'Connect Sentry' }).getByRole('button', { name: 'Create link' }).click();
  const finish = page.getByRole('dialog', { name: 'Finish in Sentry' });
  const url = await finish.getByLabel('Link', { exact: true }).inputValue();
  expect(url).toMatch(/\/hooks\/sentry\/[0-9a-f-]{36}$/);
  const secret = 'sentry-e2e-client-secret';
  await finish.getByLabel('Sentry Client Secret').fill(secret);
  await finish.getByRole('button', { name: 'Save' }).click();
  await expect(finish).toBeHidden();

  const s = await stack();
  const deliver = async (eventId: string) => {
    const body = JSON.stringify({
      action: 'created',
      data: { error: { event_id: eventId, issue_id: '4242', level: 'error', title: 'ZeroDivisionError: division by zero',
        exception: { values: [{ type: 'ZeroDivisionError', value: 'division by zero', stacktrace: { frames: [{ module: 'billing.invoice', function: 'render', in_app: true }] } }] },
        tags: [['service', 'billing'], ['environment', 'prod']] } },
    });
    const res = await page.request.post(url, {
      headers: { 'Content-Type': 'application/json', 'Sentry-Hook-Resource': 'error', 'Sentry-Hook-Signature': createHmac('sha256', secret).update(body).digest('hex') },
      data: body,
    });
    expect(res.status(), await res.text()).toBe(202);
  };
  const forged = await page.request.post(url, { headers: { 'Content-Type': 'application/json', 'Sentry-Hook-Signature': '00' }, data: '{}' });
  expect(forged.status(), 'unsigned deliveries are rejected').toBe(401);

  const decodesBefore = (await llmStats()).decode?.calls ?? 0;
  await deliver('e2e-evt-1');

  // The issue appears in the Inbox and gets explained by the stub model.
  await page.goto('/inbox');
  const row = page.getByRole('link', { name: 'ZeroDivisionError: division by zero' });
  await expect(async () => {
    await page.reload();
    await expect(row).toBeVisible({ timeout: 2_000 });
  }).toPass({ timeout: 60_000 });
  await row.click();
  await expect(async () => {
    await page.reload();
    await expect(page.getByText('stub decode of ZeroDivisionError: division by zero')).toBeVisible({ timeout: 2_000 });
  }).toPass({ timeout: 90_000 });
  expect((await llmStats()).decode?.calls ?? 0).toBe(decodesBefore + 1);

  // Mark as known.
  await page.getByRole('button', { name: 'Mark as known' }).click();
  const mark = page.getByRole('dialog', { name: 'Mark as known' });
  await mark.getByLabel('Reason').selectOption('known_bug');
  await mark.getByRole('button', { name: 'Save rule' }).click();
  await expect(page.getByText('Manage rules')).toBeVisible();

  // The same error again: counted as known, not decoded, not in the Inbox.
  await deliver('e2e-evt-2');
  await page.goto('/inbox');
  await expect(page.getByText('Nothing here').or(page.getByRole('link', { name: 'ZeroDivisionError: division by zero' }))).toBeVisible();
  await expect(page.getByRole('link', { name: 'ZeroDivisionError: division by zero' })).toHaveCount(0);
  await page.getByLabel('Status').selectOption('suppressed');
  await expect(async () => {
    await page.reload();
    await page.getByLabel('Status').selectOption('suppressed');
    await expect(page.getByText('(+1 known)')).toBeVisible({ timeout: 2_000 });
  }).toPass({ timeout: 30_000 });
  expect((await llmStats()).decode?.calls ?? 0, 'the known issue was not decoded again').toBe(decodesBefore + 1);

  await page.goto('/known-issues');
  await expect(page.getByText('ZeroDivisionError: division by zero')).toBeVisible();
  void s;
});
