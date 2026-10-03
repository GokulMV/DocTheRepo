import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Shell } from '@/layouts/Shell';
import Login from '@/pages/Login';
import Setup from '@/pages/Setup';
import Spend from '@/pages/Spend';
import { Citations } from '@/pages/Ask';
import { render } from '@testing-library/react';
import { me, mockApi, renderAt } from './helpers';

afterEach(() => vi.unstubAllGlobals());

describe('Shell', () => {
  it('shows administration only to admins', async () => {
    mockApi({ 'GET /me': me('viewer') });
    renderAt('/ask', <Route element={<Shell />}><Route path="/ask" element={<p>ask page</p>} /></Route>);
    expect(await screen.findByText('ask page')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Docs' })).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'More' }));
    expect(screen.getByRole('link', { name: 'Palace' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Connectors' })).not.toBeInTheDocument();
  });

  it('shows administration to admins', async () => {
    mockApi({ 'GET /me': me('admin') });
    renderAt('/ask', <Route element={<Shell />}><Route path="/ask" element={<p>ask page</p>} /></Route>);
    await userEvent.click(await screen.findByRole('button', { name: 'More' }));
    expect(screen.getByRole('link', { name: 'Connectors' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Spend limits' })).toBeInTheDocument();
  });

  it('redirects to login when unauthenticated', async () => {
    mockApi({
      'GET /me': () => new Response(JSON.stringify({ error: { code: 'UNAUTHENTICATED', message: 'x', correlation_id: 'c' } }), { status: 401 }),
      'GET /auth/config': { mode: 'local', sso: false, password: true },
    });
    renderAt('/docs', <><Route element={<Shell />}><Route path="/docs" element={<p>docs</p>} /></Route><Route path="/login" element={<Login />} /></>);
    expect(await screen.findByText('Sign in to DocTheRepo Hub')).toBeInTheDocument();
  });
});

describe('Login', () => {
  it('submits local credentials and returns to the page', async () => {
    const { calls } = mockApi({
      'GET /auth/config': { mode: 'local', sso: false, password: true },
      'POST /auth/local/login': { csrf_token: 'tok', user: {} },
    });
    renderAt('/login?return=/docs', <><Route path="/login" element={<Login />} /><Route path="/docs" element={<p>docs page</p>} /></>);
    await userEvent.type(await screen.findByLabelText('Email'), 'ann@acme.com');
    await userEvent.type(screen.getByLabelText('Password'), 'correct horse battery');
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(await screen.findByText('docs page')).toBeInTheDocument();
    const login = calls.find((c) => c.method === 'POST')!;
    expect(JSON.parse(String(login.init?.body))).toEqual({ email: 'ann@acme.com', password: 'correct horse battery' });
  });

  it('offers SSO when configured', async () => {
    mockApi({ 'GET /auth/config': { mode: 'oidc', sso: true, password: false } });
    renderAt('/login', <Route path="/login" element={<Login />} />);
    expect(await screen.findByRole('button', { name: 'Sign in with single sign-on' })).toBeInTheDocument();
    expect(screen.queryByLabelText('Password')).not.toBeInTheDocument();
  });

  it('shows the server error', async () => {
    mockApi({
      'GET /auth/config': { mode: 'local', sso: false, password: true },
      'POST /auth/local/login': () => new Response(JSON.stringify({ error: { code: 'INVALID_CREDENTIALS', message: 'invalid email or password', correlation_id: 'c1' } }), { status: 401 }),
    });
    renderAt('/login', <Route path="/login" element={<Login />} />);
    await userEvent.type(await screen.findByLabelText('Email'), 'a@b.c');
    await userEvent.type(screen.getByLabelText('Password'), 'nope');
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('INVALID_CREDENTIALS');
  });
});

describe('Setup', () => {
  it('derives the checklist from live state', async () => {
    mockApi({
      'GET /connectors': { items: [{ id: 'c', type: 'github', name: 'GitHub' }] },
      'GET /providers': { items: [{ id: 'p' }], kinds: [] },
      'GET /routes': { items: [{ feature: 'qa' }], features: ['qa', 'docgen', 'embedding'] },
      'GET /repos': { items: [] },
      'GET /spend/limits': { items: [{ scope: 'global' }] },
      'GET /docs/tree': { nodes: [] },
    });
    renderAt('/setup', <Route path="/setup" element={<Setup />} />);
    // GitHub is connected; a model is added but not yet used for docs; no repositories yet.
    expect(await screen.findByText('A few steps to docs and answers. 1 of 4 done.')).toBeInTheDocument();
    expect(screen.getByText('Connected: GitHub')).toBeInTheDocument();
    expect(screen.getByText('Used for: Ask (Q&A)')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /Add a model/ })).toHaveAttribute('href', '/providers');
  });

  it('starts an owner with sign-in, which can be done before anything else', async () => {
    mockApi({
      'GET /me': me('owner'),
      'GET /auth/config': { mode: 'local', sso: false, password: true },
      'GET /connectors': { items: [] },
      'GET /providers': { items: [], kinds: [] },
      'GET /routes': { items: [], features: [] },
      'GET /repos': { items: [] },
      'GET /spend/limits': { items: [] },
      'GET /docs/tree': { nodes: [] },
    });
    renderAt('/setup', <Route path="/setup" element={<Setup />} />);
    expect(await screen.findByText('A few steps to docs and answers. 0 of 5 done.')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /Set up sign-in/ })).toHaveAttribute('href', '/sign-in');
    await userEvent.click(screen.getByRole('button', { name: 'Passwords are enough' }));
    expect(await screen.findByText('A few steps to docs and answers. 1 of 5 done.')).toBeInTheDocument();
  });
});

describe('Spend', () => {
  it('edits and saves the whole set', async () => {
    const { calls } = mockApi({
      'GET /spend/limits': { items: [{ scope: 'global', scope_key: '', window: 'day', max_tokens: 2000000, max_cost_usd: null, on_breach: 'block' }] },
      'PUT /spend/limits': () => new Response(null, { status: 204 }),
    });
    renderAt('/spend', <Route path="/spend" element={<Spend />} />);
    const tokens = await screen.findByLabelText('Max tokens');
    await userEvent.clear(tokens);
    await userEvent.type(tokens, '5000000');
    await userEvent.click(screen.getByRole('button', { name: 'Add limit' }));
    await userEvent.click(screen.getByRole('button', { name: 'Save all' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true));
    const body = JSON.parse(String(calls.find((c) => c.method === 'PUT')!.init?.body));
    expect(body.items).toHaveLength(2);
    expect(body.items[0].max_tokens).toBe(5000000);
    expect(body.items[1]).toMatchObject({ scope: 'feature', scope_key: 'qa' });
  });
});

describe('Citations', () => {
  it('renders numbered sources with links when available', () => {
    render(
      <Citations
        items={[
          { n: 1, type: 'code', title: 'Refund', repo: 'acme/pay', path: 'refund.go', chunk_id: 'a' },
          { n: 2, type: 'doc', title: 'x', path: 'docs/a.md', url: 'https://example.com/a', chunk_id: 'b' },
        ]}
      />,
    );
    expect(screen.getByText('[1]')).toBeInTheDocument();
    expect(screen.getByText('refund.go')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'docs/a.md' })).toHaveAttribute('href', 'https://example.com/a');
  });
});
