import { expect, test } from '@playwright/test';
import { session } from '../lib/stack';

// Users and sign-in in a real browser: an owner of an SSO-only Hub turns on passwords, adds a teammate,
// and the teammate sets a password from the link and lands in the Hub; then the owner removes them.
test('add a user with a password link, sign in with it, remove them', async ({ browser }) => {
  const owner = await session(browser, 'owner@acme.test');
  const page = owner.page;

  await page.goto('/sign-in');
  await expect(page.getByText('Single sign-on', { exact: true })).toBeVisible();
  await expect(page.getByLabel('callback URL', { exact: true })).toHaveValue(/\/api\/v1\/auth\/callback$/);
  const turnOn = page.getByRole('button', { name: 'Turn on passwords' });
  if (await turnOn.isVisible()) await turnOn.click();
  await expect(page.getByRole('button', { name: 'Turn off passwords' })).toBeVisible();

  await page.goto('/users');
  await expect(page.getByText('People sign in with single sign-on or email and password.')).toBeVisible();
  await page.getByRole('button', { name: 'Add user' }).click();
  await page.getByLabel('Email').fill('pat@acme.test');
  await page.getByLabel('Name (optional)').fill('Pat');
  await page.getByRole('radio', { name: /editor/i }).check();
  await page.getByRole('dialog').getByRole('button', { name: 'Add user' }).click();
  const link = await page.getByLabel('password link', { exact: true }).inputValue();
  expect(link).toMatch(/\/invite\/[A-Za-z0-9_-]+$/);
  await page.getByRole('button', { name: 'Done' }).click();
  await expect(page.getByText('Link sent, not used yet')).toBeVisible();

  // Pat, in their own browser.
  const pat = await browser.newContext();
  const pp = await pat.newPage();
  await pp.goto(link);
  await expect(pp.getByText('Welcome, Pat')).toBeVisible();
  await pp.getByLabel('New password').fill('pats long passphrase');
  await pp.getByLabel('Repeat it').fill('pats long passphrase');
  await pp.getByRole('button', { name: 'Set password and sign in' }).click();
  await expect(pp).toHaveURL(/\/ask/);
  const me = await (await pp.request.get('/api/v1/me')).json();
  expect(me.role).toBe('editor');
  await pp.goto(link);
  await expect(pp.getByText('This link does not work')).toBeVisible();

  // The owner removes Pat; Pat is signed out.
  await page.reload();
  await expect(page.getByRole('row', { name: /pat@acme\.test/ }).getByText('Password')).toBeVisible();
  page.once('dialog', (d) => void d.accept());
  await page.getByRole('button', { name: 'Remove pat@acme.test' }).click();
  await expect(page.getByText('pat@acme.test')).toHaveCount(0);
  expect((await pp.request.get('/api/v1/me')).status()).toBe(401);
  await pat.close();
});
