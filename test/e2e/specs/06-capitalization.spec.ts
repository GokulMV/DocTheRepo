import { expect, test } from '@playwright/test';
import { session } from '../lib/stack';

// Every button, heading, table header, form label, option, placeholder and screen-reader label on every page
// starts with a capital letter. Identifiers people type or read as values (model names, paths, emails,
// branches) are shown in code style or are data, and are not checked.
const ROUTES = ['/ask', '/docs', '/palace', '/architecture', '/library', '/inbox', '/known-issues', '/repos', '/activity', '/analytics',
  '/account', '/connectors', '/providers', '/spend', '/users', '/sign-in', '/setup', '/settings-file'];

test('labels start with a capital letter everywhere', async ({ browser }) => {
  test.setTimeout(180_000);
  const { page } = await session(browser, 'owner@acme.test');
  const bad: string[] = [];
  for (const r of ROUTES) {
    await page.goto(r);
    await page.waitForLoadState('networkidle');
    const found: string[] = await page.evaluate(() => {
      const out: string[] = [];
      const ok = (el: Element) => !!el.closest('code, pre, .font-mono, .markdown, svg, canvas, [data-case-ok]');
      const value = (t: string) => /[@/]|^[a-z0-9_-]+\.[a-z]|^(claude|gpt|text-embedding|llama|sk-|dth_|https?:|e\.g\.|org-)/.test(t);
      const check = (where: string, t: string | null | undefined) => {
        t = (t ?? '').replace(/\s+/g, ' ').trim();
        if (/^[a-z]/.test(t) && !value(t)) out.push(`${where}: ${t}`);
      };
      for (const el of Array.from(document.querySelectorAll('button, h1, h2, h3, th, label, legend, option'))) {
        if (!ok(el)) check(el.tagName.toLowerCase(), el.textContent);
      }
      for (const el of Array.from(document.querySelectorAll('[placeholder], [aria-label]'))) {
        if (ok(el)) continue;
        check('placeholder', el.getAttribute('placeholder'));
        check('aria-label', el.getAttribute('aria-label'));
      }
      return out;
    });
    bad.push(...found.map((f) => `${r} ${f}`));
  }
  expect([...new Set(bad)], 'start these with a capital letter').toEqual([]);
});
