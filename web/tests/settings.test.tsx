import { fireEvent, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import SettingsFile from '@/pages/SettingsFile';
import { mockApi, renderAt } from './helpers';

afterEach(() => vi.unstubAllGlobals());

const plan = (dry: boolean) => ({
  dry_run: dry,
  sources: ['env'],
  changes: [
    { kind: 'provider', name: 'anthropic', action: 'create', fields: ['kind', 'api_key'] },
    { kind: 'route', name: 'qa', action: 'unchanged' },
  ],
});

describe('Settings file', () => {
  it('previews, then applies, and never shows secret values', async () => {
    const { calls } = mockApi({
      'GET /settings/sources': { enabled: ['env'], env_prefix: 'DTH_SECRET_' },
      'POST /settings/apply': (_u: string, init?: RequestInit) => plan(JSON.parse(String(init?.body)).dry_run),
    });
    renderAt('/settings-file', <Route path="/settings-file" element={<SettingsFile />} />);
    expect(await screen.findByText(/Here, the Hub resolves:/)).toBeInTheDocument();

    const apply = screen.getByRole('button', { name: /Apply/ });
    expect(apply).toBeDisabled();
    fireEvent.change(screen.getByLabelText('Settings YAML or JSON'), { target: { value: 'providers:\n  - { name: anthropic, kind: anthropic, api_key: ${env:DTH_SECRET_A} }\n' } });
    expect(apply).toBeDisabled(); // preview first

    await userEvent.click(screen.getByRole('button', { name: /Preview changes/ }));
    expect(await screen.findByText('Preview')).toBeInTheDocument();
    expect(screen.getByText('1 create')).toBeInTheDocument();
    expect(screen.getByText('kind, api_key')).toBeInTheDocument();
    expect(screen.getByText(/Secrets read from: env/)).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: /Apply/ }));
    expect(await screen.findByText('Applied')).toBeInTheDocument();
    const posts = calls.filter((c) => c.method === 'POST');
    expect(posts.map((c) => JSON.parse(String(c.init?.body)).dry_run)).toEqual([true, false]);
  });

  it('loads the current settings for editing', async () => {
    mockApi({
      'GET /settings/sources': { enabled: [], env_prefix: 'DTH_SECRET_' },
      'GET /settings/export': { yaml: 'version: 1\nproviders: []\n' },
    });
    renderAt('/settings-file', <Route path="/settings-file" element={<SettingsFile />} />);
    expect(await screen.findByText(/resolves no references/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: /Current settings/ }));
    await waitFor(() => expect(screen.getByLabelText('Settings YAML or JSON')).toHaveValue('version: 1\nproviders: []\n'));
  });
});
