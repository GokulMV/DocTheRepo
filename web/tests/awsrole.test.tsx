import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import Connectors from '@/pages/Connectors';
import { mockApi, renderAt } from './helpers';

afterEach(() => vi.unstubAllGlobals());

const EXT = 'dth-abcdefghijklmnopqrstuvwxyz234567';
const ROLE = 'arn:aws:iam::222222222222:role/DocTheRepoHubReadOnly-abcdefghij';
const setup = (over: Record<string, unknown> = {}) => ({
  available: true, template_path: `/api/v1/aws/role/template?external_id=${EXT}&access=hub`,
  setup: {
    hub_principal: 'arn:aws:iam::111111111111:role/dth-hub', external_id: EXT, access: 'hub', role_name: 'DocTheRepoHubReadOnly-abcdefghij',
    stack_name: 'doctherepo-hub-readonly-abcdefghij', permissions: ['sqs:ListQueues', 'sqs:GetQueueAttributes'],
    template_file: 'doctherepo-hub-readonly-abcdefghij.yaml', deploy_command: `aws cloudformation deploy --stack-name doctherepo-hub-readonly-abcdefghij … ExternalId=${EXT}`,
    ...over,
  },
});
const body = (calls: { method: string; url: string; init?: RequestInit }[], path: string) =>
  JSON.parse(String(calls.find((c) => c.method === 'POST' && c.url.endsWith(path))!.init?.body));

async function openSQS() {
  renderAt('/connectors', <Route path="/connectors" element={<Connectors />} />);
  await userEvent.click(await screen.findByRole('button', { name: /Show all \d+ tools/ }));
  await userEvent.click(screen.getByRole('button', { name: 'Connect Amazon SQS' }));
  return screen.findByRole('dialog', { name: 'Connect Amazon SQS' });
}

describe('AWS: read-only role', { timeout: 20000 }, () => {
  it('creates the role from the AWS console, checks the account, and saves role ARN and External ID', async () => {
    const open = vi.fn();
    vi.stubGlobal('open', open);
    const { calls } = mockApi({
      'GET /connectors': { items: [] },
      'POST /aws/role/setup': setup({ quick_create_url: 'https://eu-west-1.console.aws.amazon.com/cloudformation/home?region=eu-west-1#/stacks/create/review?x=1' }),
      'POST /aws/role/check': { ok: true, role_arn: ROLE, account_id: '222222222222', message: 'the Hub can use this role' },
      'POST /connectors': { id: 'c1', webhook_path: '' },
    });
    const dialog = await openSQS();
    await userEvent.type(within(dialog).getByLabelText('Region'), 'eu-west-1');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Create a read-only role in AWS' }));
    await waitFor(() => expect(open).toHaveBeenCalledWith(expect.stringContaining('/stacks/create/review'), '_blank', 'noopener'));
    expect(body(calls, '/aws/role/setup')).toEqual({ access: 'hub', region: 'eu-west-1' });
    expect(within(dialog).getByText(/Create stack/)).toBeInTheDocument();
    await userEvent.type(within(dialog).getByLabelText('AWS account ID'), '222222222222');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Check access' }));
    expect(await within(dialog).findByText(/Access works/)).toBeInTheDocument();
    expect(body(calls, '/aws/role/check')).toEqual({ account_id: '222222222222', external_id: EXT, uses: 'sqs', region: 'eu-west-1' });

    await userEvent.click(within(dialog).getByRole('button', { name: 'Connect' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST' && c.url.endsWith('/connectors'))).toBe(true));
    expect(body(calls, '/connectors').config).toMatchObject({ region: 'eu-west-1', role_arn: ROLE, external_id: EXT });
  });

  it('without an S3 template, shows the template and command; reports why a check failed', async () => {
    vi.stubGlobal('open', vi.fn());
    mockApi({
      'GET /connectors': { items: [] }, 'POST /aws/role/setup': setup(),
      'POST /aws/role/check': { ok: false, problem: 'trust_denied', message: 'the role exists but its trust policy does not let the Hub in with this External ID' },
    });
    const dialog = await openSQS();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Create a read-only role in AWS' }));
    const link = await within(dialog).findByRole('link', { name: 'Download the template' });
    expect(link).toHaveAttribute('href', `/api/v1/aws/role/template?external_id=${EXT}&access=hub`);
    expect((within(dialog).getByLabelText('Command') as HTMLInputElement).value).toContain('aws cloudformation deploy');
    await userEvent.type(within(dialog).getByLabelText('AWS account ID'), '222222222222');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Check access' }));
    expect(await within(dialog).findByText(/The role exists but its trust policy/)).toBeInTheDocument();
  });

  it('on a Hub without an AWS identity, says so and keeps keys or sign-in', async () => {
    mockApi({ 'GET /connectors': { items: [] }, 'GET /mcp/servers': { items: [], redirect_uri: '' },
      'POST /aws/role/setup': { available: false, reason: 'this Hub has no AWS identity of its own. Use access keys instead' } });
    renderAt('/connectors', <Route path="/connectors" element={<Connectors />} />);
    await userEvent.click(await screen.findByRole('button', { name: 'Live lookups in AWS' }));
    const dialog = await screen.findByRole('dialog', { name: 'Live lookups in AWS' });
    await userEvent.selectOptions(within(dialog).getByLabelText('How it signs in'), 'aws');
    expect(within(dialog).getByLabelText('AWS credentials')).toHaveValue('role');
    expect(within(dialog).getByLabelText('Permissions')).toHaveValue('readonly');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Create a read-only role in AWS' }));
    expect(await within(dialog).findByText(/no AWS identity of its own/)).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Sign in with AWS in the browser instead' }));
    expect(within(dialog).getByLabelText('How it signs in')).toHaveValue('oauth');
  });

  it('AWS MCP: signs with the read-only role (no keys stored)', async () => {
    vi.stubGlobal('open', vi.fn());
    const { calls } = mockApi({
      'GET /connectors': { items: [] }, 'GET /mcp/servers': { items: [], redirect_uri: '' },
      'POST /aws/role/setup': setup({ access: 'readonly', permissions: ['AWS managed policy ReadOnlyAccess'], quick_create_url: 'https://console/x' }),
      'POST /aws/role/check': { ok: true, role_arn: ROLE, message: 'ok' },
      'POST /mcp/servers': { id: 'm1', name: 'AWS', url: 'https://aws-mcp.us-east-1.api.aws/mcp', auth: 'aws', config: {}, status: 'ok', tools: [], tool_choices: {}, min_role: 'editor', enabled: true },
    });
    renderAt('/connectors', <Route path="/connectors" element={<Connectors />} />);
    await userEvent.click(await screen.findByRole('button', { name: 'Live lookups in AWS' }));
    const dialog = await screen.findByRole('dialog', { name: 'Live lookups in AWS' });
    await userEvent.selectOptions(within(dialog).getByLabelText('How it signs in'), 'aws');
    expect(within(dialog).getByRole('button', { name: 'Connect' })).toBeDisabled();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Create a read-only role in AWS' }));
    await userEvent.type(await within(dialog).findByLabelText('AWS account ID'), '222222222222');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Check access' }));
    expect(body(calls, '/aws/role/check')).toMatchObject({ uses: 'mcp', external_id: EXT });
    expect(body(calls, '/aws/role/setup')).toMatchObject({ access: 'readonly' });
    expect(await within(dialog).findByText(/The Hub will assume/)).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Connect' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST' && c.url.endsWith('/mcp/servers'))).toBe(true));
    const b = body(calls, '/mcp/servers');
    expect(b).toMatchObject({ auth: 'aws', secret: '', config: { role_arn: ROLE, external_id: EXT } });
  });
});
