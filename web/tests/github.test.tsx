import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import Connectors from '@/pages/Connectors';
import { mockApi, renderAt } from './helpers';

afterEach(() => vi.unstubAllGlobals());

const route = <Route path="/connectors" element={<Connectors />} />;

describe('Connect with GitHub', () => {
  it('offers the one-click flow first, and the manual form for GitLab and tokens', async () => {
    mockApi({ 'GET /connectors': { items: [] } });
    const assign = vi.fn();
    vi.stubGlobal('location', { ...window.location, assign });
    renderAt('/connectors', route);
    await userEvent.click(await screen.findByRole('button', { name: /Organization or GitHub Enterprise/ }));
    await userEvent.type(screen.getByLabelText('Organization (optional)'), 'acme');
    await userEvent.click(screen.getByRole('button', { name: 'Connect with GitHub' }));
    expect(assign).toHaveBeenCalledWith('/api/v1/github/connect/start?org=acme');
    await userEvent.click(screen.getByRole('button', { name: /GitLab, a token/ }));
    expect(screen.getByLabelText('Host')).toBeInTheDocument(); // the manual form
  });

  it('after installing, offers the repositories to track', async () => {
    const { calls } = mockApi({
      'GET /connectors': { items: [] },
      'GET /connectors/c1/available-repos': { items: [{ full_name: 'acme/shop', tracked: false }, { full_name: 'acme/old', tracked: true }, { full_name: 'acme/api', tracked: false }] },
      'POST /repos': { id: 'r1' },
    });
    renderAt('/connectors?github=connected&connector=c1', route);
    expect(await screen.findByText(/GitHub connected/)).toBeInTheDocument();
    expect(screen.queryByText('acme/old')).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: /Select all \(2\)/ }));
    await userEvent.click(screen.getByRole('button', { name: 'Track 2 repositories' }));
    await waitFor(() => expect(calls.filter((c) => c.method === 'POST' && c.url.endsWith('/repos'))).toHaveLength(2));
    expect(JSON.parse(String(calls.find((c) => c.method === 'POST')!.init!.body))).toEqual({ connector_id: 'c1', full_name: 'acme/shop' });
  });

  it('shows why GitHub was not connected', async () => {
    mockApi({ 'GET /connectors': { items: [] } });
    renderAt('/connectors?github_error=the+link+expired', route);
    expect(await screen.findByText('the link expired')).toBeInTheDocument();
  });
});
