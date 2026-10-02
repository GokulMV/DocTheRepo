import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import Architecture from '@/pages/Architecture';
import { me, mockApi, renderAt } from './helpers';

afterEach(() => vi.unstubAllGlobals());

const routes = (
  <>
    <Route path="/architecture" element={<Architecture />} />
    <Route path="/architecture/:repoId" element={<Architecture />} />
  </>
);

const arch = {
  repo: { id: 'r1', full_name: 'acme/payments', service_name: 'payments' },
  nodes: [
    { id: 'repo:r2', kind: 'repo', name: 'acme/checkout', layer: 'upstream', repo_id: 'r2', degree: 0 },
    { id: 'e1', kind: 'endpoint', name: 'POST /charges', layer: 'interface', entity_id: 'e1', degree: 2 },
    { id: 's1', kind: 'service', name: 'payments', layer: 'core', entity_id: 's1', degree: 5 },
    { id: 't1', kind: 'queue_topic', name: 'payments.settled', layer: 'messaging', entity_id: 't1', degree: 3 },
  ],
  links: [
    { src: 'repo:r2', dst: 'e1', kind: 'calls', weight: 1 },
    { src: 's1', dst: 'e1', kind: 'exposes', weight: 1 },
    { src: 's1', dst: 't1', kind: 'publishes', weight: 2 },
  ],
  hidden: { interface: 4 },
  restricted: 1,
  env: ['STRIPE_SECRET_KEY'],
  docs: [{ entity_id: 'p1', kind: 'confluence_page', name: 'Payments runbook', key: '9001', relation: 'runbook_for' }],
  owners: ['team-payments'],
  diagrams: [{ id: 'd1', path: 'docs/architecture/system.html', title: 'System architecture', generator: 'archify 3.0.1', size_bytes: 10, updated_at: '2026-10-02T00:00:00Z' }],
};

describe('Architecture', () => {
  it('lists every repository with what its architecture holds', async () => {
    mockApi({
      'GET /architecture': { items: [
        { repo_id: 'r1', full_name: 'acme/payments', service_name: 'payments', endpoints: 3, modules: 4, topics: 2, datastores: 2, diagrams: 2 },
        { repo_id: 'r2', full_name: 'acme/checkout', endpoints: 3, modules: 0, topics: 1, datastores: 0, diagrams: 0 },
      ] },
    });
    renderAt('/architecture', routes);
    expect(await screen.findByText('acme/payments')).toBeInTheDocument();
    expect(screen.getByText('2 diagrams')).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText('Filter repositories'), 'check');
    expect(screen.queryByText('acme/payments')).not.toBeInTheDocument();
    expect(screen.getByText('acme/checkout')).toBeInTheDocument();
  });

  it('draws the generated architecture, explains a component, and shows authored diagrams sandboxed', async () => {
    mockApi({ 'GET /me': me('viewer'), 'GET /architecture/repos/r1': arch });
    renderAt('/architecture/r1', routes);
    expect(await screen.findByRole('img', { name: 'Architecture diagram' })).toBeInTheDocument();
    expect(screen.getByText('+ 4 more')).toBeInTheDocument();
    expect(screen.getByText(/1 link\(s\) to repositories you don’t have access to/)).toBeInTheDocument();
    expect(screen.getByText('STRIPE_SECRET_KEY')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Payments runbook' })).toHaveAttribute('href', '/palace/p1');
    expect(screen.queryByRole('button', { name: /Find diagrams/ })).not.toBeInTheDocument(); // viewers cannot scan

    await userEvent.click(screen.getByRole('button', { name: 'Service payments' }));
    expect(await screen.findByText('×2')).toBeInTheDocument(); // publishes payments.settled twice
    expect(screen.getByRole('button', { name: 'Open in Palace' })).toBeInTheDocument();

    await userEvent.click(screen.getByRole('tab', { name: /System architecture/ }));
    const frame = await screen.findByTitle('System architecture');
    expect(frame).toHaveAttribute('src', '/api/v1/architecture/diagrams/d1');
    expect(frame.getAttribute('sandbox')).toContain('allow-scripts');
    expect(frame.getAttribute('sandbox')).not.toContain('allow-same-origin');
  });

  it('lets editors look for diagrams', async () => {
    const { calls } = mockApi({
      'GET /me': me('editor'),
      'GET /architecture/repos/r1': { ...arch, diagrams: [] },
      'POST /architecture/repos/r1/scan': { found: 1, removed: 0, checked: 3 },
    });
    renderAt('/architecture/r1', routes);
    await userEvent.click(await screen.findByRole('button', { name: /Find diagrams/ }));
    expect(await screen.findByText(/Checked 3 file\(s\): 1 diagram\(s\) found/)).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST' && c.url.endsWith('/architecture/repos/r1/scan'))).toBe(true);
  });
});
