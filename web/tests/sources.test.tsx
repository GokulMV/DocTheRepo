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
  it('adds Wiz with "never send to a model" on by default', async () => {
    const { calls } = mockApi({ 'GET /connectors': { items: [] }, 'POST /connectors': { id: 'w1', webhook_path: '/hooks/wiz/w1' } });
    renderAt('/connectors', <Route path="/connectors" element={<Connectors />} />);
    await userEvent.click(await screen.findByRole('button', { name: 'Add signal source' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.selectOptions(within(dialog).getByLabelText('Source'), 'wiz');
    expect(within(dialog).getByRole('checkbox', { name: /Never send this source’s data to a model/ })).toBeChecked();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Add' }));
    expect(await screen.findByText(/\/hooks\/wiz\/w1/)).toBeInTheDocument();
    const body = JSON.parse(String(calls.find((c) => c.method === 'POST')!.init?.body));
    expect(body).toMatchObject({ type: 'wiz', mode: 'webhook', config: { never_send_to_llm: 'true' } });

    await userEvent.click(screen.getByRole('button', { name: 'Add signal source' }));
    const d2 = await screen.findByRole('dialog');
    await userEvent.selectOptions(within(d2).getByLabelText('Source'), 'splunk');
    expect(within(d2).getByRole('checkbox', { name: /Never send/ })).not.toBeChecked();
    expect(within(d2).getByText(/add \?token=<secret> to the URL/)).toBeInTheDocument();
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
