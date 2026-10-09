import { useQuery, useQueryClient } from '@tanstack/react-query';
import { CircleCheck, ExternalLink, KeyRound, ShieldCheck } from 'lucide-react';
import { useState, type FormEvent } from 'react';
import { api } from '@/api/client';
import { CopyField } from '@/components/CopyField';
import { Badge, Button, Card, ErrorNote, Field, Input, PageHeader, Spinner, cx } from '@/components/ui';
import { seal } from '@/lib/seal';

interface SSOSettings {
  provider: string;
  issuer: string;
  client_id: string;
  has_secret: boolean;
  allowed_domains: string[];
  groups_claim: string;
}

interface SignInState {
  password: boolean;
  password_default: boolean;
  password_set: boolean;
  sso: boolean;
  sso_source?: 'settings' | 'config';
  settings?: SSOSettings;
  config_issuer?: string;
}

interface Preset {
  id: string;
  label: string;
  issuer: string;
  issuerHint: string;
  /** Where to create the app, as steps; {callback} is replaced with the callback URL. */
  steps: { text: string; href?: string }[];
  domainsHint: string;
  groupsHint?: string;
}

/** Where each identity provider's settings live. The callback URL is shown next to the steps. */
const PRESETS: Preset[] = [
  {
    id: 'google',
    label: 'Google Workspace',
    issuer: 'https://accounts.google.com',
    issuerHint: 'Always https://accounts.google.com.',
    steps: [
      { text: 'Open Google Cloud Console → APIs & Services → Credentials (any project of your organization).', href: 'https://console.cloud.google.com/apis/credentials' },
      { text: 'If asked, configure the OAuth consent screen first: choose “Internal” so only your organization can sign in.' },
      { text: 'Create credentials → OAuth client ID → Application type “Web application”.' },
      { text: 'Under “Authorized redirect URIs”, add the callback URL below. Create, then copy the Client ID and Client secret here.' },
    ],
    domainsHint: 'Your Workspace domain, e.g. acme.com. Without it, any Google account could sign in.',
    groupsHint: 'Google does not put groups in its sign-in token; grant repositories per user instead.',
  },
  {
    id: 'microsoft',
    label: 'Microsoft Entra ID',
    issuer: 'https://login.microsoftonline.com/<tenant-id>/v2.0',
    issuerHint: 'Replace <tenant-id> with the Directory (tenant) ID from the app’s Overview page.',
    steps: [
      { text: 'Open the Microsoft Entra admin center → App registrations → New registration.', href: 'https://entra.microsoft.com/#view/Microsoft_AAD_RegisteredApps/ApplicationsListBlade' },
      { text: 'Supported account types: “this organizational directory only”. Redirect URI: platform “Web”, the callback URL below.' },
      { text: 'Certificates & secrets → New client secret; copy its Value (not the Secret ID).' },
      { text: 'Token configuration → Add optional claim → ID → email. The Hub needs the email to identify people.' },
      { text: 'Copy the Application (client) ID and the Directory (tenant) ID from Overview.' },
    ],
    domainsHint: 'Optional with a single-tenant app (only your directory can sign in anyway).',
    groupsHint: 'To use groups: Token configuration → Add groups claim, then enter “groups”. Entra sends group IDs.',
  },
  {
    id: 'okta',
    label: 'Okta',
    issuer: 'https://<your-org>.okta.com',
    issuerHint: 'Your Okta domain, e.g. https://acme.okta.com.',
    steps: [
      { text: 'In the Okta Admin Console: Applications → Applications → Create App Integration.' },
      { text: 'Sign-in method “OIDC - OpenID Connect”, application type “Web Application”.' },
      { text: 'Sign-in redirect URIs: the callback URL below. Under Assignments, choose who may use it.' },
      { text: 'Save, then copy the Client ID and Client secret from the General tab.' },
    ],
    domainsHint: 'Optional: Okta already limits sign-in to the people assigned to the app.',
    groupsHint: 'To use groups: add a “groups” claim to the app’s ID token (Sign On tab), then enter “groups”.',
  },
  {
    id: 'keycloak',
    label: 'Keycloak',
    issuer: 'https://<keycloak-host>/realms/<realm>',
    issuerHint: 'Your Keycloak address and realm, e.g. https://sso.acme.com/realms/acme.',
    steps: [
      { text: 'In the Keycloak admin console, choose the realm → Clients → Create client (OpenID Connect).' },
      { text: 'Turn on “Client authentication”; keep “Standard flow”.' },
      { text: 'Valid redirect URIs: the callback URL below.' },
      { text: 'Credentials tab: copy the Client secret. The Client ID is the one you chose.' },
    ],
    domainsHint: 'Optional.',
    groupsHint: 'To use groups: add a “Group Membership” mapper named “groups” to the client, then enter “groups”.',
  },
  {
    id: 'other',
    label: 'Other (OpenID Connect)',
    issuer: '',
    issuerHint: 'Any OpenID Connect provider: Auth0, Authentik, JumpCloud, OneLogin, Ping… The issuer is the URL before /.well-known/openid-configuration.',
    steps: [
      { text: 'Create a “web” (confidential) OpenID Connect application in your provider.' },
      { text: 'Set its redirect / callback URL to the URL below, and allow the openid, email and profile scopes.' },
      { text: 'Copy the issuer URL, client ID and client secret here.' },
    ],
    domainsHint: 'Optional: limit sign-in to these email domains.',
  },
];

function PasswordCard({ state, onSaved }: { state: SignInState; onSaved: () => void }) {
  const [err, setErr] = useState<unknown>();
  const [busy, setBusy] = useState(false);
  const toggle = async () => {
    setBusy(true);
    setErr(undefined);
    try {
      await api.put('/auth/settings', { password: !state.password });
      onSaved();
    } catch (e) {
      setErr(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card title={<span className="flex items-center gap-2"><KeyRound className="h-4 w-4 text-slate-400" aria-hidden />Email and password</span>}
      actions={<Badge tone={state.password ? 'green' : 'gray'}>{state.password ? 'on' : 'off'}</Badge>}>
      <p className="text-sm text-slate-600 dark:text-slate-400">
        People you add under Users get a link to choose a password. Good for small teams, or as a way in besides single sign-on.
      </p>
      {!state.password_set && <p className="mt-1 text-xs text-slate-500">Not set here: {state.password_default ? 'on' : 'off'} by the deployment’s default.</p>}
      <div className="mt-3 flex flex-wrap items-center gap-3">
        <Button variant={state.password ? 'secondary' : 'primary'} disabled={busy || (state.password && !state.sso)} onClick={() => void toggle()}>
          {state.password ? 'Turn off passwords' : 'Turn on passwords'}
        </Button>
        {state.password && !state.sso && <span className="text-xs text-slate-500">Set up single sign-on first, so there is still a way in.</span>}
        {state.password && state.sso && <span className="text-xs text-slate-500">Everyone then signs in with single sign-on. Check it works for you first.</span>}
      </div>
      <ErrorNote error={err} />
    </Card>
  );
}

function SSOCard({ state, callback, onSaved }: { state: SignInState; callback: string; onSaved: () => void }) {
  const saved = state.settings;
  const [provider, setProvider] = useState(saved?.provider || 'google');
  const preset = PRESETS.find((p) => p.id === provider) ?? PRESETS[PRESETS.length - 1];
  const [issuer, setIssuer] = useState(saved?.issuer ?? PRESETS[0].issuer);
  const [clientId, setClientId] = useState(saved?.client_id ?? '');
  const [secret, setSecret] = useState('');
  const [domains, setDomains] = useState((saved?.allowed_domains ?? []).join(', '));
  const [groups, setGroups] = useState(saved?.groups_claim ?? '');
  const [err, setErr] = useState<unknown>();
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);

  const pick = (p: Preset) => {
    setDone(false);
    setProvider(p.id);
    if (!saved || saved.provider !== p.id) setIssuer(p.issuer);
  };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(undefined);
    try {
      await api.put('/auth/settings', {
        sso: {
          provider, issuer: issuer.trim(), client_id: clientId.trim(),
          client_secret: secret ? await seal(secret, 'auth.oidc_client_secret') : '',
          allowed_domains: domains.split(/[\s,]+/).filter(Boolean), groups_claim: groups.trim(),
        },
      });
      setSecret('');
      setDone(true);
      onSaved();
    } catch (e) {
      setErr(e);
    } finally {
      setBusy(false);
    }
  };
  const remove = async () => {
    if (!confirm('Remove single sign-on? People who only use it will not be able to sign in.')) return;
    setErr(undefined);
    try {
      await api.put('/auth/settings', { clear_sso: true });
      onSaved();
    } catch (e) {
      setErr(e);
    }
  };
  const placeholderIssuer = issuer.includes('<');
  return (
    <Card title={<span className="flex items-center gap-2"><ShieldCheck className="h-4 w-4 text-slate-400" aria-hidden />Single sign-on</span>}
      actions={<Badge tone={state.sso ? 'green' : 'gray'}>{state.sso ? (state.sso_source === 'config' ? 'on (config file)' : 'on') : 'off'}</Badge>}>
      <p className="text-sm text-slate-600 dark:text-slate-400">
        People sign in with your company account. Anyone your identity provider lets in (and from an allowed domain) can sign in; new people start as viewers.
        Add them under Users first to give them another role.
      </p>
      {state.sso_source === 'config' && (
        <p className="mt-2 rounded-lg bg-slate-100 p-2 text-xs text-slate-600 dark:bg-white/5 dark:text-slate-400">
          Single sign-on is configured in the deployment’s config file ({state.config_issuer}). Saving here replaces it until you remove it again.
        </p>
      )}
      <form onSubmit={submit} className="mt-4 space-y-4">
        <div>
          <p className="mb-1.5 text-sm font-medium">1. Your identity provider</p>
          <div role="radiogroup" aria-label="Identity provider" className="flex flex-wrap gap-1.5">
            {PRESETS.map((p) => (
              <button key={p.id} type="button" role="radio" aria-checked={provider === p.id} onClick={() => pick(p)}
                className={cx('rounded-lg border px-3 py-1.5 text-sm', provider === p.id ? 'border-brand-400 bg-brand-50 font-medium text-brand-800 dark:border-brand-400/50 dark:bg-brand-500/15 dark:text-white' : 'border-slate-200 text-slate-600 hover:border-slate-300 dark:border-white/10 dark:text-slate-300')}>
                {p.label}
              </button>
            ))}
          </div>
        </div>
        <div>
          <p className="mb-1.5 text-sm font-medium">2. Create an app for the Hub in {preset.label}</p>
          <ol className="list-decimal space-y-1 pl-5 text-sm text-slate-600 dark:text-slate-400">
            {preset.steps.map((s) => (
              <li key={s.text}>
                {s.text}
                {s.href && <a href={s.href} target="_blank" rel="noreferrer" className="ml-1 inline-flex items-center gap-0.5 text-brand-600 hover:underline dark:text-brand-300">Open<ExternalLink className="h-3 w-3" aria-hidden /></a>}
              </li>
            ))}
          </ol>
          <div className="mt-2">
            <p className="mb-1 text-xs font-medium text-slate-500">Callback (redirect) URL</p>
            <CopyField value={callback} label="callback URL" />
            {callback.startsWith('http://') && !/^http:\/\/(localhost|127\.0\.0\.1|\[::1\])[:/]/.test(callback) && (
              <p className="mt-1 text-xs text-amber-700 dark:text-amber-300">Most providers require https. Serve the Hub over https and set DTH_PUBLIC_URL.</p>
            )}
          </div>
        </div>
        <div className="space-y-3">
          <p className="text-sm font-medium">3. Copy its details here</p>
          <Field label="Issuer URL" htmlFor="sso-issuer" hint={preset.issuerHint}>
            <Input id="sso-issuer" required value={issuer} onChange={(e) => { setDone(false); setIssuer(e.target.value); }} placeholder="https://…" />
          </Field>
          <Field label="Client ID" htmlFor="sso-client"><Input id="sso-client" required value={clientId} onChange={(e) => { setDone(false); setClientId(e.target.value); }} /></Field>
          <Field label="Client secret" htmlFor="sso-secret" hint={saved?.has_secret ? 'Saved. Leave empty to keep it.' : 'Encrypted in your browser before it is sent; never shown again.'}>
            <Input id="sso-secret" type="password" autoComplete="off" required={!saved?.has_secret} value={secret} onChange={(e) => { setDone(false); setSecret(e.target.value); }} placeholder={saved?.has_secret ? '•••••••• (saved)' : ''} />
          </Field>
          <Field label="Allowed email domains" htmlFor="sso-domains" hint={preset.domainsHint}>
            <Input id="sso-domains" value={domains} onChange={(e) => { setDone(false); setDomains(e.target.value); }} placeholder="acme.com, acme.co.uk" />
          </Field>
          <details className="text-sm">
            <summary className="cursor-pointer text-slate-500">Groups (optional)</summary>
            <div className="mt-2">
              <Field label="Groups claim" htmlFor="sso-groups" hint={preset.groupsHint ?? 'The ID-token claim that lists the person’s groups. Groups can then be given repositories.'}>
                <Input id="sso-groups" value={groups} onChange={(e) => { setDone(false); setGroups(e.target.value); }} placeholder="For example: groups" />
              </Field>
            </div>
          </details>
        </div>
        <ErrorNote error={err} />
        {done && (
          <p className="flex items-start gap-2 rounded-lg bg-emerald-50 p-3 text-sm text-emerald-800 dark:bg-emerald-500/10 dark:text-emerald-300">
            <CircleCheck className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
            Saved and reachable. Try it in a private window: “Sign in with single sign-on” is now on the sign-in page.
          </p>
        )}
        <div className="flex flex-wrap items-center gap-2">
          <Button type="submit" disabled={busy || placeholderIssuer}>{busy ? 'Checking…' : 'Check and save'}</Button>
          {placeholderIssuer && <span className="text-xs text-slate-500">Replace the &lt;…&gt; part of the issuer URL.</span>}
          {state.sso_source === 'settings' && <Button type="button" variant="ghost" onClick={() => void remove()}>Remove single sign-on</Button>}
        </div>
        <p className="text-xs text-slate-500">“Check and save” loads the provider’s OpenID configuration first, so a wrong issuer is caught before anything changes.</p>
      </form>
    </Card>
  );
}

/** SignIn (owners) chooses how people sign in: passwords, single sign-on, or both. */
export default function SignIn() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ['auth-settings'], queryFn: () => api.get<{ state: SignInState; callback_url: string }>('/auth/settings') });
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['auth-settings'] });
    void qc.invalidateQueries({ queryKey: ['auth-config'] });
  };
  return (
    <>
      <PageHeader title="Sign-in & SSO" description="How people sign in to the Hub. Who may sign in, and with which role, is under People." />
      {q.isLoading && <Spinner />}
      <ErrorNote error={q.error} />
      {q.data && (
        <div className="max-w-3xl space-y-4">
          <SSOCard key={q.data.state.settings?.issuer ?? 'new'} state={q.data.state} callback={q.data.callback_url} onSaved={refresh} />
          <PasswordCard state={q.data.state} onSaved={refresh} />
        </div>
      )}
    </>
  );
}
