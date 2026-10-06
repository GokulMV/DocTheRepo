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
    await userEvent.type(within(dialog).getByLabelText('Key'), 'NRAK-secret');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Connect' }));
    expect(await screen.findByRole('dialog', { name: 'New Relic: Connected' })).toHaveTextContent('1 of its 2 tools');
    const body = JSON.parse(String(calls.find((c) => c.method === 'POST')!.init?.body));
    expect(body.url).toBe('https://mcp.eu.newrelic.com/mcp/');
    expect(body.config).toEqual({ header_name: 'api-key' });
    expect(openSealed(body.secret, 'mcp.secret')).toBe('NRAK-secret');
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
