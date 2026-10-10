import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import Connectors from '@/pages/Connectors';
import { mockApi, openSealed, renderAt } from './helpers';

afterEach(() => vi.unstubAllGlobals());

const route = <Route path="/connectors" element={<Connectors />} />;
const scopes = { confluence: ['read:confluence-content.all', 'read:confluence-space.summary', 'search:confluence', 'offline_access'], jira: ['read:jira-work', 'read:jira-user', 'offline_access'] };
const app = (configured: boolean) => ({ configured, client_id: configured ? 'AbCdEf123456' : undefined, callback_url: 'https://hub.acme.example/api/v1/atlassian/connect/callback',
  console_url: 'https://developer.atlassian.com/console/myapps/', scopes });
const body = (calls: { method: string; url: string; init?: RequestInit }[], method: string, suffix: string) =>
  JSON.parse(String(calls.find((c) => c.method === method && c.url.endsWith(suffix))!.init?.body));

describe('Connect with Atlassian', () => {
  it('without the one-time setup, explains it and saves the app (secret sealed), then enables the button', async () => {
    let configured = false;
    const { calls } = mockApi({
      'GET /connectors': { items: [] },
      'GET /atlassian/oauth-app': () => app(configured),
      'PUT /atlassian/oauth-app': () => { configured = true; return app(true); },
    });
    renderAt('/connectors', route);
    await userEvent.click(await screen.findByRole('button', { name: 'Connect Confluence' }));
    const dialog = await screen.findByRole('dialog', { name: 'Connect Confluence' });
    const button = await within(dialog).findByRole('button', { name: /Connect with Atlassian/ });
    await waitFor(() => expect(within(dialog).getByText(/registers this Hub with Atlassian once/)).toBeInTheDocument());
    expect(button).toBeDisabled();
    expect(within(dialog).getByLabelText('API token')).toBeInTheDocument(); // the alternative stays

    await userEvent.click(within(dialog).getByRole('button', { name: 'One-time setup' }));
    expect(within(dialog).getByLabelText('Callback URL')).toHaveValue('https://hub.acme.example/api/v1/atlassian/connect/callback');
    expect(within(dialog).getByText(/read:confluence-content.all, read:confluence-space.summary, search:confluence/)).toBeInTheDocument();
    expect(within(dialog).getByRole('link', { name: 'Atlassian developer console' })).toHaveAttribute('href', 'https://developer.atlassian.com/console/myapps/');
    await userEvent.type(within(dialog).getByLabelText('Client ID'), 'AbCdEf123456');
    await userEvent.type(within(dialog).getByLabelText('Secret'), 'console-secret');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(within(dialog).getByRole('button', { name: /Connect with Atlassian/ })).toBeEnabled());
    const saved = body(calls, 'PUT', '/atlassian/oauth-app');
    expect(saved.client_id).toBe('AbCdEf123456');
    expect(openSealed(saved.client_secret, 'atlassian.client_secret')).toBe('console-secret');
    expect(within(dialog).queryByText(/registers this Hub with Atlassian once/)).not.toBeInTheDocument();
  });

  it('goes to Atlassian’s page with what was filled in', async () => {
    const { calls } = mockApi({
      'GET /connectors': { items: [] },
      'GET /atlassian/oauth-app': app(true),
      'POST /atlassian/connect': { url: 'https://auth.atlassian.com/authorize?state=s' },
    });
    const assign = vi.fn();
    vi.stubGlobal('location', { ...window.location, assign });
    renderAt('/connectors', route);
    await userEvent.click(await screen.findByRole('button', { name: 'Connect Jira' }));
    const dialog = await screen.findByRole('dialog', { name: 'Connect Jira' });
    await userEvent.type(within(dialog).getByLabelText('Projects'), 'ENG');
    await userEvent.type(within(dialog).getByLabelText('Site URL'), 'https://acme.atlassian.net');
    const button = within(dialog).getByRole('button', { name: /Connect with Atlassian/ });
    await waitFor(() => expect(button).toBeEnabled());
    await userEvent.click(button);
    await waitFor(() => expect(assign).toHaveBeenCalledWith('https://auth.atlassian.com/authorize?state=s'));
    expect(body(calls, 'POST', '/atlassian/connect')).toEqual({ type: 'jira', keys: 'ENG', site: 'https://acme.atlassian.net' });
  });

  it('back from Atlassian: asks which spaces, keeping the sign-in settings', async () => {
    const c1 = { id: 'c1', type: 'confluence', name: 'Confluence (acme)', mode: 'poll', enabled: true, health: 'unknown', has_credentials: true,
      config: { auth: 'oauth', cloud_id: 'cl-1', base_url: 'https://acme.atlassian.net/wiki', site_name: 'acme' } };
    const { calls } = mockApi({ 'GET /connectors': { items: [c1] }, 'PATCH /connectors/c1': { id: 'c1' } });
    renderAt('/connectors?atlassian=connected&connector=c1', route);
    expect(await screen.findByText('Confluence connected to acme')).toBeInTheDocument();
    expect(screen.getByText(/Atlassian sign-in \(acme\)/)).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText('Spaces'), 'ENG, OPS');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PATCH')).toBe(true));
    expect(body(calls, 'PATCH', '/connectors/c1')).toEqual({ config: { ...c1.config, spaces: 'ENG, OPS' } });
  });

  it('lets the admin choose the site when the sign-in covers several', async () => {
    const c1 = { id: 'c1', type: 'jira', name: 'Jira', mode: 'poll', enabled: true, health: 'unknown', has_credentials: true,
      config: { auth: 'oauth', oauth_status: 'choose_site', oauth_sites: JSON.stringify([{ id: 'a', url: 'https://acme.atlassian.net', name: 'acme' }, { id: 'b', url: 'https://beta.atlassian.net', name: 'beta' }]) } };
    const { calls } = mockApi({ 'GET /connectors': { items: [c1] }, 'POST /atlassian/connectors/c1/site': { id: 'c1', site: 'https://beta.atlassian.net' } });
    renderAt('/connectors?atlassian=choose_site&connector=c1', route);
    expect(await screen.findByText('Which Jira site?')).toBeInTheDocument();
    await userEvent.selectOptions(screen.getByLabelText('Atlassian site'), 'b');
    await userEvent.click(screen.getByRole('button', { name: 'Use this site' }));
    await waitFor(() => expect(calls.some((c) => c.url.endsWith('/site'))).toBe(true));
    expect(body(calls, 'POST', '/atlassian/connectors/c1/site')).toEqual({ cloud_id: 'b' });
  });

  it('shows an expired sign-in and signs in again on the same connector', async () => {
    const c1 = { id: 'c1', type: 'jira', name: 'Jira (acme)', mode: 'poll', enabled: true, health: 'failing', has_credentials: true,
      last_error: 'ATLASSIAN_SIGN_IN_REQUIRED: the Atlassian sign-in expired or was revoked', config: { auth: 'oauth', cloud_id: 'cl-1', oauth_status: 'needs_sign_in' } };
    const { calls } = mockApi({ 'GET /connectors': { items: [c1] }, 'POST /atlassian/connect': { url: 'https://auth.atlassian.com/authorize?state=r' } });
    const assign = vi.fn();
    vi.stubGlobal('location', { ...window.location, assign });
    renderAt('/connectors', route);
    expect(await screen.findByText('Needs sign-in again')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Sign in again' }));
    await waitFor(() => expect(assign).toHaveBeenCalledWith('https://auth.atlassian.com/authorize?state=r'));
    expect(body(calls, 'POST', '/atlassian/connect')).toEqual({ type: 'jira', connector_id: 'c1' });
  });

  it('shows why Atlassian was not connected', async () => {
    mockApi({ 'GET /connectors': { items: [] } });
    renderAt('/connectors?atlassian_error=access+was+not+granted+on+Atlassian%E2%80%99s+page', route);
    expect(await screen.findByText('access was not granted on Atlassian’s page')).toBeInTheDocument();
  });
});
