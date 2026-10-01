import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import Connectors from '@/pages/Connectors';
import Inbox from '@/pages/Inbox';
import IssueDetail from '@/pages/IssueDetail';
import KnownIssues from '@/pages/KnownIssues';
import { me, mockApi, renderAt } from './helpers';

afterEach(() => vi.unstubAllGlobals());

const issue = (over: Record<string, unknown> = {}) => ({
  id: '11111111-1111-1111-1111-111111111111', fingerprint: 'fp-npe', kind: 'error', title: 'NullPointerException in OrderService.refund',
  service: 'orders', environment: 'prod', status: 'decoded', severity: 'error', occurrences: 42, suppressed_count: 0, sources: ['sentry'],
  first_seen: new Date(Date.now() - 3600_000).toISOString(), last_seen: new Date().toISOString(), decode_summary: 'Refund dereferences a missing payment',
  sparkline: Array.from({ length: 24 }, (_, i) => i % 3), ...over,
});

describe('Inbox', () => {
  it('lists issues with summaries and marks a selection as known in one rule', async () => {
    const second = issue({ id: '22222222-2222-2222-2222-222222222222', fingerprint: 'fp-npe-2', title: 'NPE in RefundJob' });
    const { calls } = mockApi({
      'GET /me': me('editor'),
      'GET /issues': { items: [issue(), second], next_cursor: null },
      'POST /known-issues': { id: 'k1' },
      'POST /known-issues/test': { would_match_last_7d: 2, sample_issue_ids: [] },
    });
    renderAt('/inbox', <Route path="/inbox" element={<Inbox />} />);
    expect(await screen.findByText('NullPointerException in OrderService.refund')).toBeInTheDocument();
    expect(screen.getAllByText('Refund dereferences a missing payment')).toHaveLength(2);
    expect(screen.getAllByRole('img', { name: /occurrences in the last 24 hours/ })).toHaveLength(2);
    await userEvent.click(screen.getByLabelText('Select NullPointerException in OrderService.refund'));
    await userEvent.click(screen.getByLabelText('Select NPE in RefundJob'));
    await userEvent.click(screen.getByRole('button', { name: 'Mark 2 as known' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Test against the last 7 days' }));
    expect(await within(dialog).findByRole('status')).toHaveTextContent('Would match 2 issues');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save rule' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST' && c.url.endsWith('/known-issues'))).toBe(true));
    const body = JSON.parse(String(calls.find((c) => c.method === 'POST' && c.url.endsWith('/known-issues'))!.init?.body));
    expect(body.match).toEqual({ fingerprints: ['fp-npe', 'fp-npe-2'], services: ['orders'] });
  });

  it('passes filters to the API and shows the empty state', async () => {
    const { calls } = mockApi({ 'GET /me': me('viewer'), 'GET /issues': { items: [], next_cursor: null } });
    renderAt('/inbox', <Route path="/inbox" element={<Inbox />} />);
    expect(await screen.findByText('Nothing here')).toBeInTheDocument();
    await userEvent.selectOptions(screen.getByLabelText('Severity'), 'error');
    await waitFor(() => expect(calls.some((c) => c.url.includes('severity=error'))).toBe(true));
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument();
  });
});

const detail = {
  ...issue(),
  decode: {
    id: 'd1', summary: 'Refund dereferences a missing payment', probable_cause: 'payment is null for partial refunds', impact: 'refunds fail',
    affected_code: [{ chunk_id: 'c1', path: 'src/orders/RefundService.java', symbol: 'RefundService.refund', reason: 'calls payment.getId()' }],
    related_commits: [{ sha: 'abcdef1234', author: 'dev', message: 'Allow partial refunds', at: new Date().toISOString() }],
    related_docs: [], next_steps: ['guard null payments'], confidence: 'high', is_actionable: true, suggest_known_issue: false,
    provider: 'anthropic', model: 'm', tokens: 5000, cost_usd: 0.02, created_at: new Date().toISOString(),
  },
  events: [{ source: 'sentry', external_id: 'e1', occurred_at: new Date().toISOString(), severity: 'error', kind: 'error', service: 'orders', environment: 'prod',
    title: 'NullPointerException in OrderService.refund', message: 'payment is null', stack: [{ module: 'orders', function: 'refund', file: 'RefundService.java', line: 42, in_app: true }], attrs: { order: '[REDACTED]' } }],
  hourly: [], similar_issues: [{ id: '33333333-3333-3333-3333-333333333333', title: 'NPE in RefundJob', status: 'resolved' }],
};

describe('IssueDetail', () => {
  it('shows the explanation, events, and acts on the issue', async () => {
    const { calls } = mockApi({ 'GET /me': me('editor'), [`GET /issues/${detail.id}`]: detail, [`PATCH /issues/${detail.id}`]: detail });
    renderAt(`/inbox/${detail.id}`, <Route path="/inbox/:id" element={<IssueDetail />} />);
    expect(await screen.findByText('payment is null for partial refunds')).toBeInTheDocument();
    expect(screen.getByText(/RefundService.refund/)).toBeInTheDocument();
    expect(screen.getByText(/Allow partial refunds/)).toBeInTheDocument();
    expect(screen.getByText('high confidence')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: /NullPointerException in OrderService.refund/ }));
    expect(screen.getByText('[REDACTED]')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Resolve' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PATCH')).toBe(true));
    expect(JSON.parse(String(calls.find((c) => c.method === 'PATCH')!.init?.body))).toEqual({ status: 'resolved' });
  });

  it('hides actions from viewers', async () => {
    mockApi({ 'GET /me': me('viewer'), [`GET /issues/${detail.id}`]: detail });
    renderAt(`/inbox/${detail.id}`, <Route path="/inbox/:id" element={<IssueDetail />} />);
    expect(await screen.findByText('payment is null for partial refunds')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Resolve' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Mark as known' })).not.toBeInTheDocument();
  });
});

describe('KnownIssues', () => {
  it('lists rules and proposes one from pasted text', async () => {
    mockApi({
      'GET /me': me('editor'),
      'GET /known-issues': { items: [{ id: 'k1', title: 'LB health checks', reason: 'expected_noise', match: { services: ['lb'], fingerprints: ['a'] }, action: 'suppress',
        enabled: true, source: 'manual', hits: 12, created_at: '', updated_at: '', description: '', explanation: '' }] },
      'POST /known-issues/from-text': { explanation: 'The note describes the refund NPE.', reason: 'known_bug', confidence: 'high', proposed_match: { fingerprints: ['fp-npe'] },
        candidates: [{ issue_id: detail.id, fingerprint: 'fp-npe', title: 'NullPointerException in OrderService.refund', service: 'orders', occurrences: 42 }],
        extracted: {}, matching_issues_last_7d: 1, sample_issue_ids: [detail.id] },
    });
    renderAt('/known-issues', <Route path="/known-issues" element={<KnownIssues />} />);
    expect(await screen.findByText('LB health checks')).toBeInTheDocument();
    expect(screen.getByText(/service lb/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('tab', { name: 'From text' }));
    await userEvent.type(screen.getByLabelText('Text'), 'refund NPE is ORD-12');
    await userEvent.click(screen.getByRole('button', { name: 'Find matching issues' }));
    expect(await screen.findByText(/The note describes the refund NPE/)).toBeInTheDocument();
    expect(screen.getByText(/would match 1 issue/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Review and save as a rule' })).toBeInTheDocument();
  });
});

describe('Connectors: signal sources', () => {
  it('creates a source and shows its webhook URL and secret once', async () => {
    const { calls } = mockApi({ 'GET /connectors': { items: [] }, 'POST /connectors': { id: 'c9', webhook_path: '/hooks/sentry/c9' } });
    renderAt('/connectors', <Route path="/connectors" element={<Connectors />} />);
    await userEvent.click(await screen.findByRole('button', { name: 'Add signal source' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Add' }));
    expect(await screen.findByText(/\/hooks\/sentry\/c9/)).toBeInTheDocument();
    const body = JSON.parse(String(calls.find((c) => c.method === 'POST')!.init?.body));
    expect(body.type).toBe('sentry');
    expect(body.webhook_secret).toMatch(/^[0-9a-f]{48}$/);
    expect(screen.getByText(body.webhook_secret)).toBeInTheDocument();
  });
});
