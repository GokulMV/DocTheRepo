import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import Invite from '@/pages/Invite';
import Users from '@/pages/Users';
import { me, mockApi, renderAt } from './helpers';

const owner = { ...me('owner'), id: 'u1' };
const people = [
  { id: 'u1', email: 'ann@acme.com', name: 'Ann', role: 'owner', disabled: false, sso: false, has_password: true, invite_pending: false, created_at: '2026-01-01T00:00:00Z' },
  { id: 'u2', email: 'bo@acme.com', name: '', role: 'viewer', disabled: false, sso: false, has_password: false, invite_pending: true, created_at: '2026-01-01T00:00:00Z' },
];

describe('Users', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('adds a user and shows the password link to pass on', async () => {
    const { calls } = mockApi({
      'GET /me': owner,
      'GET /auth/config': { mode: 'local', sso: false, password: true },
      'GET /users': { items: people, next_cursor: null },
      'POST /users': { user: { ...people[1], id: 'u3', email: 'cy@acme.com', role: 'editor' }, invite: { path: '/invite/tok123', expires_at: '2026-10-09T00:00:00Z' } },
    });
    renderAt('/users', <Route path="/users" element={<Users />} />);
    expect(await screen.findByText('People sign in with email and password.')).toBeInTheDocument();
    expect(screen.getByText('Link sent, not used yet')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Add user' }));
    await userEvent.type(screen.getByLabelText('Email'), 'cy@acme.com');
    await userEvent.click(screen.getByRole('radio', { name: /editor/i }));
    await userEvent.click(screen.getByRole('button', { name: 'Add user' }));
    expect(await screen.findByLabelText('password link')).toHaveValue(`${window.location.origin}/invite/tok123`);
    const post = calls.find((c) => c.method === 'POST' && c.url.includes('/users'))!;
    expect(JSON.parse(String(post.init?.body))).toEqual({ email: 'cy@acme.com', name: '', role: 'editor', invite: true });
  });

  it('removes a user after confirming', async () => {
    vi.stubGlobal('confirm', () => true);
    const { calls } = mockApi({
      'GET /me': owner,
      'GET /auth/config': { mode: 'oidc', sso: true, password: false },
      'GET /users': { items: people, next_cursor: null },
      'DELETE /users/u2': () => new Response(null, { status: 204 }),
    });
    renderAt('/users', <Route path="/users" element={<Users />} />);
    await userEvent.click(await screen.findByRole('button', { name: 'Remove bo@acme.com' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.url.endsWith('/users/u2'))).toBe(true));
    expect(screen.queryByRole('button', { name: 'Remove ann@acme.com' })).not.toBeInTheDocument(); // not yourself
    expect(screen.queryByRole('button', { name: /Password link/ })).not.toBeInTheDocument(); // SSO only: no password links
  });
});

describe('Invite page', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('sets a password and signs in', async () => {
    const { calls } = mockApi({
      'GET /auth/invite/tok': { email: 'bo@acme.com', name: 'Bo', expires_at: '2026-10-09T00:00:00Z', password: true, sso: false },
      'POST /auth/invite/tok': { csrf_token: 'c', user: {} },
    });
    renderAt('/invite/tok', <><Route path="/invite/:token" element={<Invite />} /><Route path="/ask" element={<p>ask page</p>} /></>);
    expect(await screen.findByText('Welcome, Bo')).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText('New password'), 'a long passphrase');
    await userEvent.type(screen.getByLabelText('Repeat it'), 'a long passphrasX');
    expect(screen.getByText('The passwords do not match.')).toBeInTheDocument();
    await userEvent.clear(screen.getByLabelText('Repeat it'));
    await userEvent.type(screen.getByLabelText('Repeat it'), 'a long passphrase');
    await userEvent.click(screen.getByRole('button', { name: 'Set password and sign in' }));
    expect(await screen.findByText('ask page')).toBeInTheDocument();
    expect(JSON.parse(String(calls.find((c) => c.method === 'POST')!.init?.body))).toEqual({ password: 'a long passphrase' });
  });

  it('explains a used or expired link', async () => {
    mockApi({ 'GET /auth/invite/old': () => new Response(JSON.stringify({ error: { code: 'INVITE_INVALID', message: 'x', correlation_id: 'c' } }), { status: 410 }) });
    renderAt('/invite/old', <Route path="/invite/:token" element={<Invite />} />);
    expect(await screen.findByText('This link does not work')).toBeInTheDocument();
  });
});
