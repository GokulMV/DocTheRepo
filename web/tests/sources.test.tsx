import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import Connectors from '@/pages/Connectors';
import Inbox from '@/pages/Inbox';
import IssueDetail from '@/pages/IssueDetail';
import { me, mockApi, renderAt } from './helpers';

afterEach(() => vi.unstubAllGlobals());

describe('Inbox: source and kind filters', () => {
  it('filters by Wiz security findings', async () => {
    const { calls } = mockApi({ 'GET /me': me('viewer'), 'GET /issues': { items: [], next_cursor: null } });
    renderAt('/inbox', <Route path="/inbox" element={<Inbox />} />);
    await screen.findByText('Nothing here');
    await userEvent.selectOptions(screen.getByLabelText('Source'), 'wiz');
    await userEvent.selectOptions(screen.getByLabelText('Kind'), 'security_finding');
    await waitFor(() => expect(calls.some((c) => c.url.includes('source=wiz') && c.url.includes('kind=security_finding'))).toBe(true));
    expect(within(screen.getByLabelText('Source')).getByRole('option', { name: 'Splunk' })).toBeInTheDocument();
  });
});

describe('Connectors: Wiz and Splunk', () => {
  it('adds Wiz with "never send to a model" on by default, and gives Splunk one link with the key in it', async () => {
    const { calls } = mockApi({ 'GET /connectors': { items: [] }, 'POST /connectors': { id: 'w1', webhook_path: '/hooks/wiz/w1' } });
    renderAt('/connectors', <Route path="/connectors" element={<Connectors />} />);
    await userEvent.click(await screen.findByRole('button', { name: /Show all \d+ tools/ }));
    await userEvent.click(screen.getByRole('button', { name: 'Connect Wiz' }));
    const dialog = await screen.findByRole('dialog', { name: 'Connect Wiz' });
    await userEvent.click(within(dialog).getByText('More options'));
    expect(within(dialog).getByRole('checkbox', { name: /Never send this tool’s data to an AI model/ })).toBeChecked();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Create link' }));
    const done = await screen.findByRole('dialog', { name: 'Finish in Wiz' });
    expect((within(done).getByLabelText('Link') as HTMLInputElement).value).toMatch(/\/hooks\/wiz\/w1\?token=[0-9a-f]{48}$/);
    const body = JSON.parse(String(calls.find((c) => c.method === 'POST')!.init?.body));
    expect(body).toMatchObject({ type: 'wiz', name: 'Wiz', mode: 'webhook', config: { never_send_to_llm: 'true' } });
    await userEvent.click(within(done).getByRole('button', { name: 'Done' }));

    await userEvent.click(screen.getByRole('button', { name: 'Connect Splunk' }));
    const d2 = await screen.findByRole('dialog', { name: 'Connect Splunk' });
    await userEvent.click(within(d2).getByText('More options'));
    expect(within(d2).getByRole('checkbox', { name: /Never send/ })).not.toBeChecked();
  });
});

describe('IssueDetail: never-send sources', () => {
  it('explains why a finding has no explanation', async () => {
    const id = '33333333-3333-3333-3333-333333333333';
    mockApi({ 'GET /me': me('viewer'), [`GET /issues/${id}`]: {
      id, fingerprint: 'fp-w', kind: 'security_finding', title: 'Public bucket on exports', service: 'payments', environment: 'prod', status: 'new',
      severity: 'critical', occurrences: 1, suppressed_count: 0, sources: ['wiz'], first_seen: new Date().toISOString(), last_seen: new Date().toISOString(),
      sparkline: [], events: [{ source: 'wiz', external_id: 'wiz-1', occurred_at: new Date().toISOString(), severity: 'critical', kind: 'security_finding',
        service: 'payments', title: 'Public bucket on exports', message: '', attrs: { 'dth.no_llm': 'true' } }], hourly: [], similar_issues: [] } });
    renderAt(`/inbox/${id}`, <Route path="/inbox/:id" element={<IssueDetail />} />);
    expect(await screen.findByText(/this source is set to never send its data to a model/)).toBeInTheDocument();
  });
});
