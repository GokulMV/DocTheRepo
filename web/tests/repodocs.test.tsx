import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import Docs from '@/pages/Docs';
import { withCitations } from '@/pages/RepoDocs';
import { me, mockApi, renderAt } from './helpers';

const repo = { id: 'r1', connector_id: 'c1', connector_type: 'github', full_name: 'acme/shop', default_branch: 'main', docs_path: 'docs/generated/', last_processed_sha: 'abc1234def', enabled: true,
  push: { mode: 'pr', on_reject: 'pr', conflict_strategy: 'rebase', stale_after_ns: 0 } };
const v2 = (role: string) => ({ ...me(role), features: { issues: true, docs_v2: true } });
const summary = (type: string, key: string, title: string, group: string, label = 'high', confidence = 0.9) =>
  ({ id: `${type}-${key}`, type, key, title, group, order: 1, at_a_glance: '', confidence, label, status: 'ok', updated_at: new Date().toISOString() });
const list = {
  repo_id: 'r1',
  items: [summary('overview', 'overview', 'Overview', 'Basics'), summary('architecture', 'architecture', 'Architecture', 'Basics'),
    summary('module', 'internal-payments', 'internal/payments', 'Modules', 'medium', 0.7), summary('api', 'api', 'API reference', 'Interfaces')],
  budget: { cap_usd: 20, spent_usd: 17 },
};
const doc = {
  id: 'overview-overview', repo_id: 'r1', repo: 'acme/shop', type: 'overview', key: 'overview', title: 'Overview', group: 'Basics',
  at_a_glance: 'The shop sells things and takes payments.', confidence: 0.72, label: 'medium', why: ['Key concepts: 1 of 4 citations do not point at code'],
  calibrated: true, changed: 0.25, source_sha: 'abc1234def', status: 'ok', updated_at: new Date().toISOString(), gaps: ['How refunds are approved'],
  sections: [
    { key: 'what', title: 'What it does', markdown: 'It takes orders [internal/orders/place.go:12].', score: 0.95, label: 'high' },
    { key: 'concepts', title: 'Key concepts', markdown: '`Refund` reverses a charge.', score: 0.7, label: 'medium', why: ['1 of 4 citations do not point at code'] },
  ],
};

describe('Docs v2', () => {
  it('opens the overview: summary, sections with code links, confidence and why', async () => {
    mockApi({ 'GET /me': v2('viewer'), 'GET /repos': { items: [repo] }, 'GET /repo-docs': list, 'GET /repo-docs/find': doc });
    renderAt('/docs/r/r1/overview/overview', <Route path="/docs/r/:repoId/:type/:key" element={<Docs />} />);
    expect(await screen.findByRole('heading', { level: 1, name: 'Overview' })).toBeInTheDocument();
    expect(screen.getByRole('region', { name: 'At a glance' })).toHaveTextContent('The shop sells things');
    const link = screen.getByRole('link', { name: 'internal/orders/place.go:12' });
    expect(link).toHaveAttribute('href', 'https://github.com/acme/shop/blob/abc1234def/internal/orders/place.go#L12');
    expect(screen.getByText(/25% of its code changed/)).toBeInTheDocument();
    expect(screen.getByText(/Check against the code: 1 of 4 citations/)).toBeInTheDocument();
    expect(screen.getByRole('region', { name: 'Not determined from the code' })).toHaveTextContent('How refunds are approved');
    await userEvent.click(screen.getByRole('button', { name: /Medium confidence: 72%/ }));
    expect(screen.getByRole('dialog', { name: 'Why this confidence' })).toHaveTextContent('judged by Jev');
    // Navigation groups documents; the current one is marked.
    const navEl = screen.getByRole('navigation', { name: 'Documents' });
    expect(within(navEl).getByRole('link', { name: 'Overview' })).toHaveAttribute('aria-current', 'page');
    expect(within(navEl).getByText('Modules')).toBeInTheDocument();
    expect(within(navEl).getByRole('link', { name: 'internal/payments' })).toHaveAttribute('href', '/docs/r/r1/module/internal-payments');
    expect(screen.getByText(/Docs budget: \$17\.00 of \$20\.00 this month \(80% used\)/)).toBeInTheDocument();
  });

  it('offers to write the documents with an estimate, for admins', async () => {
    const { calls } = mockApi({
      'GET /me': v2('admin'), 'GET /repos': { items: [repo] }, 'GET /routes': { items: [{ feature: 'docgen', provider_id: 'p1', model: 'm' }] },
      'GET /repo-docs': { repo_id: 'r1', items: [] }, 'POST /repos/r1/docs/estimate': { documents: 14, modules: 6, estimated_usd: 3.2, estimated_tokens: 1 },
      'POST /repos/r1/docs/write': { job_id: 'j1' },
    });
    renderAt('/docs', <Route path="/docs" element={<Docs />} />);
    expect(await screen.findByText(/About 14 documents \(6 modules\), roughly \$3\.20/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Write the documents' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST' && c.url.endsWith('/docs/write'))).toBe(true));
    expect(JSON.parse(String(calls.find((c) => c.url.endsWith('/docs/write'))!.init?.body))).toEqual({ full: true });
  });

  it('turns citations into links, or code when the host is unknown', () => {
    expect(withCitations('see [a/b.go:3] and [Dockerfile:2]', (p, l) => `https://x/${p}#L${l}`)).toBe('see [`a/b.go:3`](https://x/a/b.go#L3) and [`Dockerfile:2`](https://x/Dockerfile#L2)');
    expect(withCitations('see [a/b.go:3-9]', () => undefined)).toBe('see `a/b.go:3`');
  });
});
