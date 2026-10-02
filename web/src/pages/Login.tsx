import { Logo } from '@/components/Logo';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { api, setCSRF } from '@/api/client';
import { Button, ErrorNote, Field, Input } from '@/components/ui';

interface AuthConfig {
  mode: 'local' | 'oidc';
  sso: boolean;
  password: boolean;
}

export default function Login() {
  const [params] = useSearchParams();
  const ret = params.get('return') ?? '/ask';
  const nav = useNavigate();
  const qc = useQueryClient();
  const cfg = useQuery({ queryKey: ['auth-config'], queryFn: () => api.get<AuthConfig>('/auth/config') });
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [err, setErr] = useState<unknown>();
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(undefined);
    try {
      const r = await api.post<{ csrf_token: string }>('/auth/local/login', { email, password });
      setCSRF(r.csrf_token);
      await qc.invalidateQueries({ queryKey: ['me'] });
      nav(ret.startsWith('/') && !ret.startsWith('//') ? ret : '/ask', { replace: true });
    } catch (e) {
      setErr(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <div className="w-full max-w-sm rounded-lg border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900">
        <Logo className="mb-4 h-10 w-10" />
        <h1 className="text-lg font-semibold">Sign in to DocTheRepo Hub</h1>
        <p className="mt-1 text-sm text-slate-500">Your repositories, docs, and answers in one place.</p>
        <div className="mt-5 space-y-4">
          {cfg.data?.sso && (
            <Button className="w-full" onClick={() => window.location.assign(`/api/v1/auth/login?return=${encodeURIComponent(ret)}`)}>
              Sign in with single sign-on
            </Button>
          )}
          {cfg.data?.password && (
            <form onSubmit={submit} className="space-y-3">
              <Field label="Email" htmlFor="email">
                <Input id="email" type="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} />
              </Field>
              <Field label="Password" htmlFor="password">
                <Input id="password" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
              </Field>
              <Button type="submit" className="w-full" disabled={busy}>
                {busy ? 'Signing in…' : 'Sign in'}
              </Button>
            </form>
          )}
          <ErrorNote error={err ?? cfg.error} />
        </div>
      </div>
    </div>
  );
}
