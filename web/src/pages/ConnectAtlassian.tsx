import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { api } from '@/api/client';
import { keys, useConnectors, useInvalidating } from '@/api/hooks';
import type { AtlassianApp, AtlassianSite, Connector } from '@/api/types';
import { CopyField } from '@/components/CopyField';
import { SealedHint } from '@/components/Sealed';
import { Button, Card, ErrorNote, Field, Input, Select } from '@/components/ui';
import { seal } from '@/lib/seal';

// "Connect with Atlassian": Confluence and Jira Cloud connect by approving on Atlassian's own page (OAuth
// 2.0, 3LO) instead of pasting an API token. The Hub's OAuth app is registered once by an admin.

export const atlassianAppKey = ['atlassian-oauth-app'] as const;

export function useAtlassianApp() {
  return useQuery({ queryKey: atlassianAppKey, queryFn: () => api.get<AtlassianApp>('/atlassian/oauth-app'), retry: false });
}

type Kind = 'confluence' | 'jira';
const LABEL: Record<Kind, string> = { confluence: 'Confluence', jira: 'Jira' };
const KEYS: Record<Kind, 'spaces' | 'projects'> = { confluence: 'spaces', jira: 'projects' };

/** startAtlassian asks the Hub for Atlassian's consent page and goes there; Atlassian sends the browser back. */
export async function startAtlassian(body: { type: Kind; connector_id?: string; keys?: string; site?: string }) {
  const { url } = await api.post<{ url: string }>('/atlassian/connect', body);
  window.location.assign(url);
}

/** AtlassianMark is a neutral mark for the button (two strokes, not Atlassian's logo). */
function AtlassianMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 16 16" className={className} fill="currentColor" aria-hidden>
      <path d="M4.6 7.5c-.2-.3-.6-.2-.7.1L1 13.4c-.1.3.1.6.4.6h4.1c.2 0 .3-.1.4-.2.9-1.9.4-4.7-1.3-6.3zM7.8 2.2c-1.6 2.6-1.5 5.4-.4 7.6l2 4c.1.1.2.2.4.2h4.1c.3 0 .5-.3.4-.6L8.5 2.2c-.2-.3-.6-.3-.7 0z" />
    </svg>
  );
}

/**
 * AtlassianSignIn is the recommended way to connect Confluence or Jira Cloud: one button, then Atlassian's
 * page, then back here, connected. Without the one-time app registration it shows how an admin sets it up.
 */
export function AtlassianSignIn({ type, keys: keyList, site }: { type: Kind; keys?: string; site?: string }) {
  const app = useAtlassianApp();
  const [setup, setSetup] = useState(false);
  const go = useInvalidating(() => startAtlassian({ type, keys: keyList?.trim() || undefined, site: site?.trim() || undefined }));
  const configured = !!app.data?.configured;
  return (
    <div className="rounded-xl border border-brand-200 bg-brand-50/60 p-4 dark:border-brand-500/30 dark:bg-brand-500/10">
      <p className="text-sm font-medium">Recommended: Atlassian Cloud sign-in</p>
      <p className="mt-1 text-xs text-slate-600 dark:text-slate-400">
        Approve read-only access on Atlassian’s page and pick the site. No token to create or paste; access renews on its own.
      </p>
      <button type="button" disabled={!configured || go.isPending} onClick={() => go.mutate(undefined)}
        className="mt-3 inline-flex w-full items-center justify-center gap-2 rounded-lg bg-[#0052cc] px-4 py-2.5 text-sm font-medium text-white hover:bg-[#0747a6] disabled:cursor-not-allowed disabled:opacity-50">
        <AtlassianMark className="h-4 w-4" /> {go.isPending ? 'Opening Atlassian…' : 'Connect with Atlassian'}
      </button>
      <ErrorNote error={go.error} />
      {app.isSuccess && !configured && (
        <p className="mt-2 text-xs text-slate-600 dark:text-slate-400">
          An admin registers this Hub with Atlassian once (about two minutes).{' '}
          <button type="button" className="text-brand-600 hover:underline dark:text-brand-300" onClick={() => setSetup((s) => !s)} aria-expanded={setup}>
            One-time setup
          </button>
        </p>
      )}
      {configured && (
        <button type="button" className="mt-2 text-xs text-slate-500 hover:text-slate-800 dark:hover:text-slate-200" onClick={() => setSetup((s) => !s)} aria-expanded={setup}>
          Atlassian app settings
        </button>
      )}
      {setup && app.data && <AtlassianSetup app={app.data} onSaved={() => setSetup(false)} />}
    </div>
  );
}

/** AtlassianSetup registers the Hub's OAuth app: exact console steps, the callback URL, the scopes, then the app's ID and secret. */
export function AtlassianSetup({ app, onSaved }: { app: AtlassianApp; onSaved?: () => void }) {
  const [clientID, setClientID] = useState(app.client_id ?? '');
  const [secret, setSecret] = useState('');
  const save = useInvalidating(async () => {
    await api.put('/atlassian/oauth-app', { client_id: clientID.trim(), client_secret: secret ? await seal(secret.trim(), 'atlassian.client_secret') : '' });
    onSaved?.();
  }, atlassianAppKey);
  const scopes = (k: Kind) => app.scopes[k].filter((s) => s !== 'offline_access').join(', ');
  return (
    <div className="mt-3 space-y-3 rounded-lg border border-slate-200 bg-white p-3 text-sm dark:border-white/10 dark:bg-slate-900/60">
      <ol className="list-decimal space-y-2 pl-5 text-slate-700 dark:text-slate-300">
        <li>Open the <a className="text-brand-600 hover:underline dark:text-brand-300" href={app.console_url} target="_blank" rel="noreferrer">Atlassian developer console</a> and choose <b>Create → OAuth 2.0 integration</b>.</li>
        <li>Under <b>Authorization</b>, add OAuth 2.0 (3LO) with this callback URL:<div className="mt-1"><CopyField label="Callback URL" value={app.callback_url} /></div></li>
        <li>Under <b>Permissions</b>, add <b>Confluence API</b> with the classic scopes <code className="text-xs">{scopes('confluence')}</code> and <b>Jira API</b> with <code className="text-xs">{scopes('jira')}</code>.</li>
        <li>Under <b>Distribution</b>, make it available to your organization's users (sharing), so other admins can sign in too.</li>
        <li>Under <b>Settings</b>, copy the <b>Client ID</b> and <b>Secret</b> here.</li>
      </ol>
      <Field label="Client ID"><Input value={clientID} onChange={(e) => setClientID(e.target.value)} autoComplete="off" /></Field>
      <Field label="Secret" hint={app.configured ? <>Leave empty to keep the stored secret. <SealedHint /></> : <SealedHint />}>
        <Input type="password" value={secret} onChange={(e) => setSecret(e.target.value)} autoComplete="off" />
      </Field>
      <ErrorNote error={save.error} />
      <Button disabled={!clientID.trim() || (!app.configured && !secret) || save.isPending} onClick={() => save.mutate(undefined)}>
        {save.isPending ? 'Saving…' : 'Save'}
      </Button>
    </div>
  );
}

/** isAtlassianOAuth reports a connector made with "Connect with Atlassian". */
export const isAtlassianOAuth = (c: Connector) => (c.type === 'confluence' || c.type === 'jira') && c.config?.auth === 'oauth';

/** needsAtlassianSignIn reports an OAuth connector whose sign-in expired or was revoked. */
export const needsAtlassianSignIn = (c: Connector) => isAtlassianOAuth(c) && c.config?.oauth_status === 'needs_sign_in';

/**
 * AtlassianReturn finishes on the Connections page after Atlassian sent the browser back: choose the site when
 * the sign-in covers several, then which spaces or projects to sync.
 */
export function AtlassianReturn({ connectorId, onDone }: { connectorId: string; onDone: () => void }) {
  const conns = useConnectors();
  const c = conns.data?.find((x) => x.id === connectorId);
  const [cloud, setCloud] = useState('');
  const [list, setList] = useState<string>();
  const choose = useInvalidating((id: string) => api.post(`/atlassian/connectors/${connectorId}/site`, { cloud_id: id }), keys.connectors);
  const saveKeys = useInvalidating(async (v: string) => {
    await api.patch(`/connectors/${connectorId}`, { config: { ...c!.config, [KEYS[c!.type as Kind]]: v.trim() } });
    onDone();
  }, keys.connectors);
  if (!c) return conns.isLoading ? null : <Card title="Atlassian connected" className="mb-4"><Button size="sm" variant="secondary" onClick={onDone}>Done</Button></Card>;
  const kind = c.type as Kind;
  if (c.config?.oauth_status === 'choose_site') {
    let sites: AtlassianSite[] = [];
    try { sites = JSON.parse(c.config.oauth_sites ?? '[]'); } catch { /* shown as empty */ }
    return (
      <Card title={`Which ${LABEL[kind]} site?`} className="mb-4">
        <p className="mb-2 text-sm">Your Atlassian sign-in can read several sites. Choose the one to sync.</p>
        <div className="flex max-w-lg gap-2">
          <Select aria-label="Atlassian site" value={cloud} onChange={(e) => setCloud(e.target.value)}>
            <option value="">Choose a site…</option>
            {sites.map((s) => <option key={s.id} value={s.id}>{s.name ? `${s.name} (${s.url})` : s.url}</option>)}
          </Select>
          <Button disabled={!cloud || choose.isPending} onClick={() => choose.mutate(cloud)}>Use this site</Button>
        </div>
        <ErrorNote error={choose.error} />
      </Card>
    );
  }
  const field = KEYS[kind];
  const value = list ?? c.config?.[field] ?? '';
  return (
    <Card title={`${LABEL[kind]} connected${c.config?.site_name ? ` to ${c.config.site_name}` : ''}`} className="mb-4">
      <p className="mb-2 text-sm">Signed in with Atlassian. {kind === 'confluence' ? 'Which spaces should the Hub read?' : 'Which projects should the Hub read?'} The first sync starts within a minute of saving.</p>
      <form className="flex max-w-lg items-end gap-2" onSubmit={(e) => { e.preventDefault(); saveKeys.mutate(value); }}>
        <div className="flex-1">
          <Field label={kind === 'confluence' ? 'Spaces' : 'Projects'} hint={kind === 'confluence' ? 'Space keys, comma-separated' : 'Project keys, comma-separated'}>
            <Input value={value} required placeholder="ENG, OPS" onChange={(e) => setList(e.target.value)} />
          </Field>
        </div>
        <Button type="submit" disabled={!value.trim() || saveKeys.isPending}>{saveKeys.isPending ? 'Saving…' : 'Save'}</Button>
      </form>
      <ErrorNote error={saveKeys.error} />
    </Card>
  );
}
