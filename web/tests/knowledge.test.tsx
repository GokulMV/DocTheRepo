import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import Connectors from '@/pages/Connectors';
import KnownIssues from '@/pages/KnownIssues';
import Library from '@/pages/Library';
import { me, mockApi, renderAt } from './helpers';
import { openSealed } from './helpers';

afterEach(() => vi.unstubAllGlobals());

const body = (calls: { method: string; url: string; init?: RequestInit }[], method: string, suffix: string) =>
  JSON.parse(String(calls.find((c) => c.method === method && c.url.endsWith(suffix))!.init?.body));

describe('Connectors: knowledge sources', () => {
  it('adds a Jira connector with its projects and token, then offers Sync now', async () => {
    const jira = { id: 'j1', type: 'jira', name: 'Jira', mode: 'poll', enabled: true, health: 'ok', poll_seconds: 900, last_sync_at: null };
    const { calls } = mockApi({ 'GET /connectors': { items: [jira] }, 'POST /connectors': { id: 'j2' }, 'POST /connectors/j1/sync': { job_ids: ['job1'] } });
    renderAt('/connectors', <Route path="/connectors" element={<Connectors />} />);
    expect(await screen.findByText('synced every 15 min')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Sync now' }));
    expect(await screen.findByText('Queued 1 sync job(s).')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: 'Add knowledge source' }));
    const dialog = await screen.findByRole('dialog', { name: 'Add a knowledge source' });
    await userEvent.selectOptions(within(dialog).getByLabelText('Source'), 'jira');
    await userEvent.type(within(dialog).getByLabelText('Site URL'), 'https://acme.atlassian.net');
    await userEvent.type(within(dialog).getByLabelText('Projects'), 'ENG, OPS');
    await userEvent.type(within(dialog).getByLabelText('Account e-mail (optional)'), 'bot@acme.com');
    await userEvent.type(within(dialog).getByLabelText('API token'), 'tok');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Add' }));
    expect(await screen.findByText(/The first Jira sync starts within a minute/)).toBeInTheDocument();
    const b = body(calls, 'POST', '/connectors');
    expect(b).toEqual({ type: 'jira', name: 'Jira', mode: 'poll', credentials: expect.stringMatching(/^dthseal1:/),
      config: { base_url: 'https://acme.atlassian.net', projects: 'ENG, OPS', email: 'bot@acme.com' } });
    expect(openSealed(b.credentials, 'connector.credentials')).toBe('tok'); // sealed in the browser
  });
});

describe('Known issues: from a link', () => {
  it('explains a Jira URL and saves the rule with its origin; shows upstream notes', async () => {
    const { calls } = mockApi({
      'GET /me': me('editor'),
      'GET /known-issues': { items: [{ id: 'k1', title: 'ENG-1: pool exhausted', reason: 'known_bug', match: { services: ['checkout'] }, action: 'label_only',
        enabled: true, source: 'jira', label_managed: true, jira_key: 'ENG-1', ticket_url: 'https://acme.atlassian.net/browse/ENG-1', upstream_status: 'Done',
        upstream_note: 'Fixed upstream (Jira issue is Done) — verify the errors stopped.', hits: 3, created_at: '', updated_at: '', description: '', explanation: '' }] },
      'POST /known-issues/from-link': { explanation: 'ENG-7 describes the checkout timeouts.', reason: 'third_party', confidence: 'medium',
        proposed_match: { fingerprints: ['fp-t'] }, candidates: [{ issue_id: 'i1', fingerprint: 'fp-t', title: 'TimeoutError', service: 'checkout', occurrences: 9 }],
        extracted: {}, matching_issues_last_7d: 1, sample_issue_ids: ['i1'],
        link: { source: 'jira', external_id: 'ENG-7', title: 'ENG-7: gateway timeouts', url: 'https://acme.atlassian.net/browse/ENG-7', status: 'Done', done: true,
          jira_key: 'ENG-7', source_text: 'ENG-7: gateway timeouts\n\nThe payment gateway times out.' } },
      'POST /known-issues/test': { would_match_last_7d: 1, sample_issue_ids: [] },
      'POST /known-issues': { id: 'k2' },
    });
    renderAt('/known-issues', <Route path="/known-issues" element={<KnownIssues />} />);
    expect(await screen.findByText(/Fixed upstream \(Jira issue is Done\)/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'ENG-1' })).toHaveAttribute('href', 'https://acme.atlassian.net/browse/ENG-1');

    await userEvent.click(screen.getByRole('tab', { name: 'From text' }));
    await userEvent.type(screen.getByLabelText('Jira issue or Confluence page URL'), 'https://acme.atlassian.net/browse/ENG-7');
    await userEvent.click(screen.getByRole('button', { name: 'Explain link' }));
    expect(await screen.findByText(/ENG-7 describes the checkout timeouts/)).toBeInTheDocument();
    expect(screen.getByText('Done upstream')).toBeInTheDocument();
    expect(body(calls, 'POST', '/known-issues/from-link')).toEqual({ url: 'https://acme.atlassian.net/browse/ENG-7' });

    await userEvent.click(screen.getByRole('button', { name: 'Review and save as a rule' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByLabelText('Title')).toHaveValue('ENG-7: gateway timeouts');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save rule' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST' && c.url.endsWith('/known-issues'))).toBe(true));
    const saved = body(calls, 'POST', '/known-issues');
    expect(saved).toMatchObject({ source: 'jira', jira_key: 'ENG-7', ticket_url: 'https://acme.atlassian.net/browse/ENG-7', action: 'label_only',
      match: { fingerprints: ['fp-t'] }, explanation: 'ENG-7 describes the checkout timeouts.' });
  });
});

describe('Library: knowledge items', () => {
  it('links Confluence pages and Jira issues to their source', async () => {
    mockApi({
      'GET /me': me('viewer'),
      'GET /library/shelves/runbooks': { id: 's1', slug: 'runbooks', title: 'Runbooks', description: '', rules: [], curated: false, order: 80, item_count: 1,
        items: [{ type: 'confluence_page', id: 'kd1', title: 'Checkout runbook', path: 'https://acme.atlassian.net/wiki/spaces/ENG/pages/123', summary: 'Scale the pool.', pinned: false }] },
    });
    renderAt('/library/runbooks', <Route path="/library/:slug" element={<Library />} />);
    const link = await screen.findByRole('link', { name: 'Checkout runbook' });
    expect(link).toHaveAttribute('href', 'https://acme.atlassian.net/wiki/spaces/ENG/pages/123');
    expect(screen.getByText('Confluence')).toBeInTheDocument();
    expect(screen.getByText('Scale the pool.')).toBeInTheDocument();
  });
});
