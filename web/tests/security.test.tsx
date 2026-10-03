import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import Security from '@/pages/Security';
import { me, mockApi, renderAt } from './helpers';

const repo = { id: 'r1', full_name: 'acme/shop', connector_id: 'c1', connector_type: 'github', default_branch: 'main', docs_path: 'docs/generated/', enabled: true };
const finding = (id: string, extra: object = {}) => ({
  id, dimension: 'security-pentest', title: `Finding ${id}`, surface: 'login', file: 'internal/auth/login.go', line_start: 7, line_end: 7,
  repro: ['POST /login'], evidence: 'db.Query("…" + name)', severity: 'critical', exploitability: 'easy', priority: 'P0', status: 'confirmed', fix_status: '', ...extra,
});
const scan = {
  id: 's1', repo_id: 'r1', repo: 'acme/shop', status: 'done', commit_sha: 'abcdef123456', modules: ['security-pentest'], verdict: 'NO-GO',
  summary: { counts: { P0: 2, rejected: 1 }, tokens: 12000 }, created_at: '2026-10-03T10:00:00Z', finished_at: '2026-10-03T10:01:00Z',
  findings: [finding('f1'), finding('f2', { fix_status: 'pr_opened', fix_pr_url: 'https://github.com/acme/shop/pull/7' }), finding('f3', { status: 'rejected', priority: 'P2' })],
};

describe('Security', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('lists repositories with their verdict', async () => {
    mockApi({ 'GET /me': me('editor'), 'GET /repos': { items: [repo] }, 'GET /security': { items: [scan] } });
    renderAt('/security', <Route path="/security" element={<Security />} />);
    expect(await screen.findByText('acme/shop')).toBeInTheDocument();
    expect(screen.getByText('NO-GO')).toBeInTheDocument();
    expect(screen.getByText('P0: 2')).toBeInTheDocument();
    expect(screen.getByText(/kryptonite/)).toBeInTheDocument();
  });

  it('fixes only the findings selected, and only after confirming', async () => {
    const { calls } = mockApi({
      'GET /me': me('editor'), 'GET /repos': { items: [repo] },
      'GET /security/repos/r1/scans': { items: [scan] }, 'GET /security/scans/s1': scan,
      'POST /security/repos/r1/fix': { job_id: 'j1', queued: 1 },
    });
    renderAt('/security/r1', <Route path="/security/:repoId" element={<Security />} />);
    expect(await screen.findByText('Finding f1')).toBeInTheDocument();
    expect(screen.queryByText('Finding f3')).not.toBeInTheDocument(); // rejected hidden by default
    expect(screen.getByRole('link', { name: /Pull request/ })).toHaveAttribute('href', 'https://github.com/acme/shop/pull/7');
    expect(screen.getByLabelText('Select Finding f2')).toBeDisabled(); // already in a pull request
    const fixButton = screen.getByRole('button', { name: /Fix selected/ });
    expect(fixButton).toBeDisabled();

    await userEvent.click(screen.getByLabelText('Select Finding f1'));
    await userEvent.click(screen.getByRole('button', { name: 'Fix selected (1)' }));
    expect(screen.getByText('Fix 1 finding?')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST')).toBe(false); // nothing happens before the confirmation
    await userEvent.click(screen.getByRole('button', { name: 'Open a pull request' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST' && c.url.endsWith('/security/repos/r1/fix'))).toBe(true));
    const post = calls.find((c) => c.method === 'POST')!;
    expect(JSON.parse(String(post.init?.body))).toEqual({ finding_ids: ['f1'] });

    await userEvent.click(screen.getByRole('checkbox', { name: 'Show 1 rejected' }));
    expect(screen.getByText('Finding f3')).toBeInTheDocument();
    expect(screen.getByLabelText('Select Finding f3')).toBeDisabled();
  });
});
