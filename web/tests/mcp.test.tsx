import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import Connectors from '@/pages/Connectors';
import { mockApi, openSealed, renderAt } from './helpers';

const realLocation = window.location;
afterEach(() => {
  vi.unstubAllGlobals();
  Object.defineProperty(window, 'location', { value: realLocation, configurable: true });
});

const server = (over: Record<string, unknown> = {}) => ({
  id: 'm1', name: 'Sentry', url: 'https://mcp.sentry.dev/mcp', catalog_key: 'sentry', auth: 'oauth', config: {}, has_secret: false, signed_in: true,
  min_role: 'editor', enabled: true, status: 'ok', created_at: '', tool_choices: {},
  tools: [{ name: 'search_issues', description: 'Find issues', read_only: true }, { name: 'update_issue', description: 'Change an issue', read_only: false }],
  ...over,
});

describe('Connections: MCP', () => {
  it('connects Sentry by signing in: creates the connection, then opens the sign-in page', async () => {
    const assign = vi.fn();
    Object.defineProperty(window, 'location', { value: { ...realLocation, assign }, configurable: true });
    const { calls } = mockApi({
      'GET /connectors': { items: [] }, 'GET /mcp/servers': { items: [], redirect_uri: 'https://hub/api/v1/mcp/oauth/callback' },
      'POST /mcp/servers': server({ status: 'needs_sign_in', signed_in: false, tools: [] }),
      'POST /mcp/servers/m1/sign-in': { url: 'https://sentry.io/oauth/authorize?x=1' },
    });
    renderAt('/connectors', <Route path="/connectors" element={<Connectors />} />);
    await userEvent.click(await screen.findByRole('button', { name: 'Live lookups in Sentry' }));
    const dialog = await screen.findByRole('dialog', { name: 'Live lookups in Sentry' });
    expect(within(dialog).queryByLabelText('Token')).not.toBeInTheDocument(); // nothing to paste
    await userEvent.click(within(dialog).getByRole('button', { name: 'Connect and sign in' }));
    await waitFor(() => expect(assign).toHaveBeenCalledWith('https://sentry.io/oauth/authorize?x=1'));
    const body = JSON.parse(String(calls.find((c) => c.method === 'POST' && c.url.endsWith('/mcp/servers'))!.init?.body));
    expect(body).toMatchObject({ name: 'Sentry', url: 'https://mcp.sentry.dev/mcp', auth: 'oauth', catalog_key: 'sentry', min_role: 'editor', secret: '' });
  });

  it('connects New Relic with a sealed key in its own header and region', async () => {
    const { calls } = mockApi({
      'GET /connectors': { items: [] }, 'GET /mcp/servers': { items: [], redirect_uri: '' },
      'POST /mcp/servers': server({ name: 'New Relic', auth: 'header', signed_in: false }),
    });
    renderAt('/connectors', <Route path="/connectors" element={<Connectors />} />);
    await userEvent.click(await screen.findByRole('button', { name: /Show all \d+ products/ }));
    await userEvent.click(screen.getByRole('button', { name: 'Live lookups in New Relic' }));
    const dialog = await screen.findByRole('dialog', { name: 'Live lookups in New Relic' });
    await userEvent.selectOptions(within(dialog).getByLabelText('Region or site'), 'EU');
    expect(within(dialog).getByLabelText('How it signs in')).toHaveValue('oauth');
    await userEvent.selectOptions(within(dialog).getByLabelText('How it signs in'), 'header');
    await userEvent.type(within(dialog).getByLabelText('Key'), 'NRAK-secret');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Connect' }));
    expect(await screen.findByRole('dialog', { name: 'New Relic: Connected' })).toHaveTextContent('1 of its 2 tools');
    const body = JSON.parse(String(calls.find((c) => c.method === 'POST')!.init?.body));
    expect(body.url).toBe('https://mcp.eu.newrelic.com/mcp/');
    expect(body.config).toEqual({ header_name: 'api-key' });
    expect(openSealed(body.secret, 'mcp.secret')).toBe('NRAK-secret');
  });

  const app = (over: Record<string, unknown> = {}) => ({
    connector_id: 'c-app', name: 'GitHub (acme)', app_slug: 'docthrepo-acme', owner: 'acme', web: 'https://github.com',
    client_id: 'Iv23liAPP', oauth: true, installed: true, ...over,
  });

  it('GitHub with the Hub’s GitHub App: signs in on GitHub’s page instead of a token', async () => {
    const assign = vi.fn();
    Object.defineProperty(window, 'location', { value: { ...realLocation, assign }, configurable: true });
    const { calls } = mockApi({
      'GET /connectors': { items: [] },
      'GET /mcp/servers': { items: [], redirect_uri: 'https://hub/api/v1/mcp/oauth/callback', github_apps: [app()] },
      'POST /mcp/servers': server({ id: 'g1', name: 'GitHub', catalog_key: 'github', status: 'needs_sign_in', signed_in: false, tools: [] }),
      'POST /mcp/servers/g1/sign-in': { url: 'https://github.com/login/oauth/authorize?client_id=Iv23liAPP' },
    });
    renderAt('/connectors', <Route path="/connectors" element={<Connectors />} />);
    await userEvent.click(await screen.findByRole('button', { name: 'Live lookups in GitHub' }));
    const dialog = await screen.findByRole('dialog', { name: 'Live lookups in GitHub' });
    await waitFor(() => expect(within(dialog).getByLabelText('How it signs in')).toHaveValue('oauth'));
    expect(within(dialog).queryByLabelText('Token')).not.toBeInTheDocument();
    expect(dialog).toHaveTextContent('only in the repositories the App is installed on');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Sign in with GitHub' }));
    await waitFor(() => expect(assign).toHaveBeenCalledWith('https://github.com/login/oauth/authorize?client_id=Iv23liAPP'));
    const body = JSON.parse(String(calls.find((c) => c.method === 'POST' && c.url.endsWith('/mcp/servers'))!.init?.body));
    expect(body).toMatchObject({ name: 'GitHub', url: 'https://api.githubcopilot.com/mcp/', auth: 'oauth', catalog_key: 'github', config: { github_app: 'c-app' }, secret: '' });
  });

  it('GitHub with the Hub’s GitHub App: a token is still an option', async () => {
    const { calls } = mockApi({
      'GET /connectors': { items: [] },
      'GET /mcp/servers': { items: [], redirect_uri: '', github_apps: [app()] },
      'POST /mcp/servers': server({ name: 'GitHub', auth: 'bearer', signed_in: false }),
    });
    renderAt('/connectors', <Route path="/connectors" element={<Connectors />} />);
    await userEvent.click(await screen.findByRole('button', { name: 'Live lookups in GitHub' }));
    const dialog = await screen.findByRole('dialog', { name: 'Live lookups in GitHub' });
    await waitFor(() => expect(within(dialog).getByLabelText('How it signs in')).toHaveValue('oauth'));
    await userEvent.selectOptions(within(dialog).getByLabelText('How it signs in'), 'bearer');
    await userEvent.type(within(dialog).getByLabelText('Token'), 'github_pat_x');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Connect' }));
    await screen.findByRole('dialog', { name: 'GitHub: Connected' });
    const body = JSON.parse(String(calls.find((c) => c.method === 'POST')!.init?.body));
    expect(body.auth).toBe('bearer');
    expect(body.config).toEqual({});
    expect(openSealed(body.secret, 'mcp.secret')).toBe('github_pat_x');
  });

  it('GitHub without a GitHub App: the token form, and a hint to connect an App first', async () => {
    mockApi({ 'GET /connectors': { items: [] }, 'GET /mcp/servers': { items: [], redirect_uri: '', github_apps: [] } });
    renderAt('/connectors', <Route path="/connectors" element={<Connectors />} />);
    await userEvent.click(await screen.findByRole('button', { name: 'Live lookups in GitHub' }));
    const dialog = await screen.findByRole('dialog', { name: 'Live lookups in GitHub' });
    expect(within(dialog).getByLabelText('Token')).toBeInTheDocument();
    expect(within(dialog).queryByLabelText('How it signs in')).not.toBeInTheDocument();
    expect(within(dialog).getByRole('link', { name: 'Connect a GitHub App first' })).toHaveAttribute('href', '/connectors#code');
    expect(dialog).toHaveTextContent('Connect a GitHub App first to sign in with GitHub');
    expect(within(dialog).getByRole('button', { name: 'Connect' })).toBeInTheDocument();
  });

  it('GitHub with an App but no client yet: adds the client ID and secret, then signs in with GitHub', async () => {
    let apps = [app({ oauth: false, client_id: undefined })];
    const { calls } = mockApi({
      'GET /connectors': { items: [] },
      'GET /mcp/servers': () => ({ items: [], redirect_uri: 'https://hub/api/v1/mcp/oauth/callback', github_apps: apps }),
      'PUT /github/connect/c-app/oauth-client': () => { apps = [app()]; return app(); },
    });
    renderAt('/connectors', <Route path="/connectors" element={<Connectors />} />);
    await userEvent.click(await screen.findByRole('button', { name: 'Live lookups in GitHub' }));
    const dialog = await screen.findByRole('dialog', { name: 'Live lookups in GitHub' });
    expect(within(dialog).getByLabelText('Token')).toBeInTheDocument();
    expect(dialog).toHaveTextContent('https://hub/api/v1/mcp/oauth/callback');
    await userEvent.type(within(dialog).getByLabelText('Client ID'), 'Iv23liAPP');
    await userEvent.type(within(dialog).getByLabelText('Client secret'), 'app-secret');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save and use GitHub sign-in' }));
    await waitFor(() => expect(within(dialog).getByRole('button', { name: 'Sign in with GitHub' })).toBeInTheDocument());
    const put = calls.find((c) => c.method === 'PUT')!;
    const body = JSON.parse(String(put.init?.body));
    expect(body.client_id).toBe('Iv23liAPP');
    expect(openSealed(body.client_secret, 'connector.oauth_client_secret')).toBe('app-secret');
  });

  it('lists connections and turns tools on and off', async () => {
    const { calls } = mockApi({
      'GET /connectors': { items: [] }, 'GET /mcp/servers': { items: [server()], redirect_uri: '' }, 'PATCH /mcp/servers/m1': server(),
    });
    renderAt('/connectors', <Route path="/connectors" element={<Connectors />} />);
    const row = (await screen.findByText('https://mcp.sentry.dev/mcp')).closest('tr')!;
    expect(within(row).getByText('1 of 2')).toBeInTheDocument();
    await userEvent.click(within(row).getByRole('button', { name: 'Choose tools for Sentry' }));
    const dialog = await screen.findByRole('dialog', { name: 'Sentry: tools' });
    expect(within(dialog).getByLabelText('search_issues')).toBeChecked();
    expect(within(dialog).getByLabelText('update_issue')).not.toBeChecked();
    await userEvent.click(within(dialog).getByLabelText('update_issue'));
    expect(within(dialog).getByText(/may change things is on/)).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PATCH')).toBe(true));
    expect(JSON.parse(String(calls.find((c) => c.method === 'PATCH')!.init?.body))).toEqual({ tool_choices: { search_issues: true, update_issue: true } });
  });
});
