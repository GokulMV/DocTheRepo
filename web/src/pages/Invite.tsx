import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { api, setCSRF } from '@/api/client';
import { Logo } from '@/components/Logo';
import { Button, ErrorNote, Field, Input, Spinner } from '@/components/ui';

interface InviteInfo {
  email: string;
  name: string;
  expires_at: string;
  password: boolean;
  sso: boolean;
}

/** Invite is the public page behind an invite or password-reset link: choose a password, get signed in. */
export default function Invite() {
  const { token = '' } = useParams();
  const nav = useNavigate();
  const qc = useQueryClient();
  const info = useQuery({ queryKey: ['invite', token], queryFn: () => api.get<InviteInfo>(`/auth/invite/${encodeURIComponent(token)}`), retry: false });
  const [pw, setPw] = useState('');
  const [again, setAgain] = useState('');
  const [err, setErr] = useState<unknown>();
  const [busy, setBusy] = useState(false);
  const mismatch = again !== '' && pw !== again;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (pw !== again) return;
    setBusy(true);
    setErr(undefined);
    try {
      // Someone already signed in here (an admin trying the link) needs their CSRF token for the POST.
      await api.get<{ csrf_token?: string }>('/me').then((m) => setCSRF(m.csrf_token), () => undefined);
      const r = await api.post<{ csrf_token: string }>(`/auth/invite/${encodeURIComponent(token)}`, { password: pw });
      setCSRF(r.csrf_token);
      await qc.invalidateQueries({ queryKey: ['me'] });
      nav('/ask', { replace: true });
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
        {info.isLoading && <Spinner />}
        {info.error ? (
          <>
            <h1 className="text-lg font-semibold">This link does not work</h1>
            <p className="mt-1 text-sm text-slate-500">It may have been used already or expired (links last 7 days). Ask an admin for a new one.</p>
            <Link to="/login" className="mt-4 inline-block text-sm font-medium text-brand-600 hover:underline">Go to sign-in</Link>
          </>
        ) : info.data && (
          <>
            <h1 className="text-lg font-semibold">Welcome{info.data.name ? `, ${info.data.name}` : ''}</h1>
            <p className="mt-1 text-sm text-slate-500">Choose a password for <b className="font-medium text-slate-700 dark:text-slate-300">{info.data.email}</b>. You will be signed in right away.</p>
            {info.data.password ? (
              <form onSubmit={submit} className="mt-5 space-y-3">
                <input type="email" autoComplete="username" value={info.data.email} readOnly hidden />
                <Field label="New password" htmlFor="pw" hint="At least 12 characters. A few random words work well.">
                  <Input id="pw" type="password" autoComplete="new-password" required minLength={12} value={pw} onChange={(e) => setPw(e.target.value)} autoFocus />
                </Field>
                <Field label="Repeat it" htmlFor="pw2">
                  <Input id="pw2" type="password" autoComplete="new-password" required value={again} onChange={(e) => setAgain(e.target.value)} />
                </Field>
                {mismatch && <p className="text-xs text-red-600">The passwords do not match.</p>}
                <Button type="submit" className="w-full" disabled={busy || mismatch || pw.length < 12}>{busy ? 'Saving…' : 'Set password and sign in'}</Button>
              </form>
            ) : (
              <p className="mt-4 text-sm text-amber-700 dark:text-amber-300">Password sign-in is turned off. {info.data.sso ? 'Use single sign-on on the sign-in page.' : 'Ask an admin how to sign in.'}</p>
            )}
            <ErrorNote error={err} />
          </>
        )}
      </div>
    </div>
  );
}
