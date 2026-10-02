import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import SignIn from '@/pages/SignIn';
import { me, mockApi, openSealed, renderAt } from './helpers';

const passwordsOnly = { state: { password: true, password_default: true, password_set: false, sso: false }, callback_url: 'https://hub.acme.com/api/v1/auth/callback' };

describe('Sign-in & SSO', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('guides SSO setup and seals the client secret', async () => {
    const puts: unknown[] = [];
    mockApi({
      'GET /me': me('owner'),
      'GET /auth/settings': passwordsOnly,
      'PUT /auth/settings': (_u: string, init?: RequestInit) => { puts.push(JSON.parse(String(init?.body))); return passwordsOnly; },
    });
    renderAt('/sign-in', <Route path="/sign-in" element={<SignIn />} />);
    expect(await screen.findByLabelText('callback URL')).toHaveValue('https://hub.acme.com/api/v1/auth/callback');
    // Passwords cannot be turned off before SSO works.
    expect(screen.getByRole('button', { name: 'Turn off passwords' })).toBeDisabled();

    await userEvent.click(screen.getByRole('radio', { name: 'Microsoft Entra ID' }));
    expect(screen.getByText(/Add optional claim → ID → email/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Check and save' })).toBeDisabled(); // the issuer still has <tenant-id>
    const issuer = screen.getByLabelText('Issuer URL');
    await userEvent.clear(issuer);
    await userEvent.type(issuer, 'https://login.microsoftonline.com/1234/v2.0');
    await userEvent.type(screen.getByLabelText('Client ID'), 'app-1');
    await userEvent.type(screen.getByLabelText('Client secret'), 'shh');
    await userEvent.type(screen.getByLabelText('Allowed email domains'), 'acme.com, acme.co.uk');
    await userEvent.click(screen.getByRole('button', { name: 'Check and save' }));
    await waitFor(() => expect(puts).toHaveLength(1));
    const sso = (puts[0] as { sso: Record<string, unknown> }).sso;
    expect(sso).toMatchObject({ provider: 'microsoft', issuer: 'https://login.microsoftonline.com/1234/v2.0', client_id: 'app-1', allowed_domains: ['acme.com', 'acme.co.uk'] });
    expect(openSealed(sso.client_secret as string, 'auth.oidc_client_secret')).toBe('shh');
    expect(await screen.findByText(/Saved and reachable/)).toBeInTheDocument();
  });
});
