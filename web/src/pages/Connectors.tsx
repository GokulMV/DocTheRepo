import { ChevronDown, Plug } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { api } from '@/api/client';
import { keys, useConnectors, useInvalidating } from '@/api/hooks';
import type { Check, Connector, RemoteResult } from '@/api/types';
import { Badge, Button, Card, Dialog, Empty, ErrorNote, Field, Input, PageHeader, Select, Spinner, Table, Td, Textarea, statusTone } from '@/components/ui';
import { relTime } from '@/lib/format';
import { seal } from '@/lib/seal';
import { SealedBadge, SealedHint } from '@/components/Sealed';
import { KNOWLEDGE, knowledgeSpec, SOURCES, sourceSpec } from './signalSources';

function randomSecret() {
  const b = new Uint8Array(24);
  crypto.getRandomValues(b);
  return Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
}

/** GitHubMark is GitHub's logo, for the "Connect with GitHub" button. */
function GitHubMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 16 16" className={className} fill="currentColor" aria-hidden>
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z" />
    </svg>
  );
}

/** ConnectGitHub starts the one-click flow: GitHub creates a private app for this Hub, then you pick repositories. */
function ConnectGitHub() {
  const [more, setMore] = useState(false);
  const [org, setOrg] = useState('');
  const [base, setBase] = useState('');
  const go = () => {
    const q = new URLSearchParams();
    if (org.trim()) q.set('org', org.trim());
    if (base.trim()) q.set('base_url', base.trim());
    window.location.assign(`/api/v1/github/connect/start?${q.toString()}`);
  };
  return (
    <div className="rounded-xl border border-brand-200 bg-brand-50/60 p-4 dark:border-brand-500/30 dark:bg-brand-500/10">
      <p className="text-sm font-medium">Recommended: one click</p>
      <p className="mt-1 text-xs text-slate-600 dark:text-slate-400">
        GitHub creates a private app for this Hub and asks which repositories it may use. No tokens or keys to copy: the Hub receives them from GitHub and keeps them encrypted.
      </p>
      <button type="button" onClick={go} className="mt-3 inline-flex w-full items-center justify-center gap-2 rounded-lg bg-slate-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-slate-800 dark:bg-white dark:text-slate-900 dark:hover:bg-slate-200">
        <GitHubMark className="h-4 w-4" /> Connect with GitHub
      </button>
      <button type="button" onClick={() => setMore((m) => !m)} className="mt-2 inline-flex items-center gap-1 text-xs text-slate-500 hover:text-slate-800 dark:hover:text-slate-200" aria-expanded={more}>
        <ChevronDown className={more ? 'h-3.5 w-3.5 rotate-180' : 'h-3.5 w-3.5'} aria-hidden /> Organization or GitHub Enterprise
      </button>
      {more && (
        <div className="mt-2 grid gap-2 sm:grid-cols-2">
          <Field label="Organization (optional)" hint="Create the app in an org you administer."><Input value={org} onChange={(e) => setOrg(e.target.value)} placeholder="acme" /></Field>
          <Field label="Enterprise Server URL (optional)" hint="Empty for github.com."><Input value={base} onChange={(e) => setBase(e.target.value)} placeholder="https://ghe.example.com" /></Field>
        </div>
      )}
    </div>
  );
}

/** PickRepos lists what a just-connected git host can read and tracks the selected repositories. */
function PickRepos({ connectorId, onDone }: { connectorId: string; onDone: () => void }) {
  const [items, setItems] = useState<{ full_name: string; tracked: boolean }[]>();
  const [err, setErr] = useState<unknown>();
  const [sel, setSel] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(0);
  useEffect(() => {
    api.get<{ items: { full_name: string; tracked: boolean }[] }>(`/connectors/${connectorId}/available-repos`).then((r) => setItems(r.items), setErr);
  }, [connectorId]);
  const track = async () => {
    setBusy(true);
    setErr(undefined);
    try {
      for (const full_name of sel) {
        await api.post('/repos', { connector_id: connectorId, full_name });
        setDone((n) => n + 1);
      }
      onDone();
    } catch (e) {
      setErr(e);
    } finally {
      setBusy(false);
    }
  };
  const open = (items ?? []).filter((i) => !i.tracked);
  return (
    <Card title="GitHub connected — choose repositories to document" className="mb-4">
      {!items && !err && <Spinner />}
      <ErrorNote error={err} />
      {items && open.length === 0 && <p className="text-sm text-slate-600 dark:text-slate-400">Every repository the app can read is already tracked. Change which repositories it can read under the app's settings on GitHub.</p>}
      {open.length > 0 && (
        <>
          <div className="mb-2 flex items-center gap-3 text-xs">
            <button type="button" className="text-brand-600 hover:underline dark:text-brand-300" onClick={() => setSel(open.map((i) => i.full_name))}>Select all ({open.length})</button>
            <button type="button" className="text-slate-500 hover:underline" onClick={() => setSel([])}>None</button>
          </div>
          <div className="grid max-h-72 gap-1 overflow-y-auto sm:grid-cols-2">
            {open.map((i) => (
              <label key={i.full_name} className="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-slate-50 dark:hover:bg-white/[0.04]">
                <input type="checkbox" checked={sel.includes(i.full_name)} onChange={(e) => setSel((s) => (e.target.checked ? [...s, i.full_name] : s.filter((x) => x !== i.full_name)))} />
                <span className="truncate">{i.full_name}</span>
              </label>
            ))}
          </div>
          <div className="mt-3 flex items-center gap-2">
            <Button disabled={!sel.length || busy} onClick={track}>{busy ? `Tracking ${done}/${sel.length}…` : `Track ${sel.length || ''} repositor${sel.length === 1 ? 'y' : 'ies'}`}</Button>
            <Button variant="ghost" onClick={onDone}>Later</Button>
          </div>
          <p className="mt-2 text-xs text-slate-500">Each tracked repository is documented on its next push; a dry run and doc import are on the Repositories page.</p>
        </>
      )}
      {items && open.length === 0 && <Button size="sm" variant="secondary" className="mt-2" onClick={onDone}>Done</Button>}
    </Card>
  );
}

function AddGit({ onCreated }: { onCreated: (id: string, type: string, secret: string) => void }) {
  const [open, setOpen] = useState(false);
  const [type, setType] = useState<'github' | 'gitlab'>('github');
  const [auth, setAuth] = useState<'token' | 'app'>('token');
  const [name, setName] = useState('');
  const [baseURL, setBaseURL] = useState('');
  const [token, setToken] = useState('');
  const [appId, setAppId] = useState('');
  const [installationId, setInstallationId] = useState('');
  const [group, setGroup] = useState('');
  const [mode, setMode] = useState('webhook');
  const create = useInvalidating((b: object) => api.post<{ id: string }>('/connectors', b), keys.connectors);
  const submit = async () => {
    const secret = randomSecret();
    const config: Record<string, string> = {};
    if (baseURL) config.base_url = baseURL;
    if (type === 'github' && auth === 'app') Object.assign(config, { app_id: appId, installation_id: installationId });
    if (type === 'github' && auth === 'token') config.auth = 'token';
    if (type === 'gitlab' && group) config.group = group;
    const r = await create.mutateAsync({ type, name: name || (type === 'github' ? 'GitHub' : 'GitLab'), mode, config, credentials: await seal(token, 'connector.credentials'), webhook_secret: await seal(secret, 'connector.webhook_secret') });
    setOpen(false);
    onCreated(r.id, type, secret);
  };
  return (
    <>
      <Button onClick={() => setOpen(true)}>Connect git host</Button>
      <Dialog open={open} onOpenChange={setOpen} title="Connect GitHub or GitLab" description="Read access to code, and write access limited to the generated-docs path (enforced by the Hub).">
        <ConnectGitHub />
        <div className="my-4 flex items-center gap-3 text-xs text-slate-400"><span className="h-px flex-1 bg-slate-200 dark:bg-white/10" />or enter the details yourself (GitLab, a token, or an existing app)<span className="h-px flex-1 bg-slate-200 dark:bg-white/10" /></div>
        <form className="space-y-3" onSubmit={(e) => { e.preventDefault(); submit(); }}>
          <Field label="Host">
            <Select value={type} onChange={(e) => setType(e.target.value as 'github' | 'gitlab')}>
              <option value="github">GitHub (cloud or Enterprise Server)</option>
              <option value="gitlab">GitLab (SaaS or self-managed)</option>
            </Select>
          </Field>
          <Field label="Name"><Input value={name} onChange={(e) => setName(e.target.value)} placeholder={type === 'github' ? 'GitHub' : 'GitLab'} /></Field>
          <Field label="API base URL" hint={type === 'github' ? 'Empty for github.com; https://ghe.example.com/api/v3/ for Enterprise' : 'Empty for gitlab.com; https://gitlab.example.com for self-managed'}>
            <Input value={baseURL} onChange={(e) => setBaseURL(e.target.value)} />
          </Field>
          {type === 'github' && (
            <Field label="Authentication">
              <Select value={auth} onChange={(e) => setAuth(e.target.value as 'token' | 'app')}>
                <option value="token">Fine-grained token</option>
                <option value="app">GitHub App (recommended for orgs)</option>
              </Select>
            </Field>
          )}
          {type === 'github' && auth === 'app' && (
            <div className="grid grid-cols-2 gap-3">
              <Field label="App ID"><Input required value={appId} onChange={(e) => setAppId(e.target.value)} /></Field>
              <Field label="Installation ID"><Input required value={installationId} onChange={(e) => setInstallationId(e.target.value)} /></Field>
            </div>
          )}
          <Field label={type === 'github' && auth === 'app' ? 'App private key (PEM)' : 'Access token'} hint={<SealedHint />}>
            {type === 'github' && auth === 'app' ? (
              <Textarea required rows={4} className="font-mono text-xs" value={token} onChange={(e) => setToken(e.target.value)} />
            ) : (
              <Input required type="password" value={token} onChange={(e) => setToken(e.target.value)} />
            )}
          </Field>
          {type === 'gitlab' && <Field label="Group (optional)" hint="Limits repository discovery to this group and its subgroups."><Input value={group} onChange={(e) => setGroup(e.target.value)} /></Field>}
          <Field label="How pushes reach the Hub" hint="Use polling when the Hub is not reachable from the git host (laptop, private network).">
            <Select value={mode} onChange={(e) => setMode(e.target.value)}>
              <option value="webhook">Webhooks</option>
              <option value="poll">Polling</option>
              <option value="both">Both</option>
            </Select>
          </Field>
          <ErrorNote error={create.error} />
          <Button type="submit" disabled={create.isPending}>Connect</Button>
        </form>
      </Dialog>
    </>
  );
}

function TestResult({ id, onClose }: { id: string; onClose: () => void }) {
  const [res, setRes] = useState<{ ok: boolean; checks: Check[] }>();
  const [err, setErr] = useState<unknown>();
  useEffect(() => {
    api.post<{ ok: boolean; checks: Check[] }>(`/connectors/${id}/test`).then(setRes, setErr);
  }, [id]);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title="Connector test" description="Read-only checks against the host.">
      {!res && !err && <Spinner label="Testing" />}
      <ErrorNote error={err} />
      <ul className="space-y-1 text-sm">
        {res?.checks.map((c) => (
          <li key={c.name}><Badge tone={c.ok ? 'green' : 'red'}>{c.ok ? 'ok' : 'failed'}</Badge> {c.name} <span className="text-slate-500">{c.detail}</span></li>
        ))}
      </ul>
    </Dialog>
  );
}

function AddSignal({ onCreated }: { onCreated: (c: { id: string; type: string; secret: string; path: string }) => void }) {
  const [open, setOpen] = useState(false);
  const [type, setType] = useState('sentry');
  const spec = sourceSpec(type)!;
  const [name, setName] = useState('');
  const [mode, setMode] = useState<string>('');
  const [config, setConfig] = useState<Record<string, string>>({});
  const [creds, setCreds] = useState('');
  const [noLLM, setNoLLM] = useState(false);
  const create = useInvalidating((b: object) => api.post<{ id: string; webhook_path: string }>('/connectors', b), keys.connectors);
  const pick = (t: string) => { setType(t); setConfig({}); setMode(''); setNoLLM(t === 'wiz'); };
  return (
    <>
      <Button variant="secondary" onClick={() => setOpen(true)}>Add signal source</Button>
      <Dialog open={open} onOpenChange={setOpen} title="Add a signal source" description="Errors, alerts, logs, and event platforms. Read-only: the Hub never changes anything in these tools.">
        <form
          className="space-y-3"
          onSubmit={async (e) => {
            e.preventDefault();
            const secret = spec.secret ? randomSecret() : '';
            const m = mode || spec.modes[0];
            const cfg: Record<string, string> = Object.fromEntries(Object.entries(config).filter(([, v]) => v.trim() !== ''));
            if (noLLM) cfg.never_send_to_llm = 'true';
            const r = await create.mutateAsync({ type, name: name || spec.label, mode: m, config: cfg, credentials: creds ? await seal(creds, 'connector.credentials') : undefined, webhook_secret: secret ? await seal(secret, 'connector.webhook_secret') : undefined });
            onCreated({ id: r.id, type, secret, path: r.webhook_path });
            setOpen(false);
          }}
        >
          <Field label="Source" hint={spec.help}>
            <Select value={type} onChange={(e) => pick(e.target.value)}>
              {(['Errors & alerts', 'Security & log platforms', 'Cloud logs & alarms', 'Event platforms'] as const).map((g) => (
                <optgroup key={g} label={g}>{SOURCES.filter((s) => s.group === g).map((s) => <option key={s.type} value={s.type}>{s.label}</option>)}</optgroup>
              ))}
            </Select>
          </Field>
          <Field label="Name"><Input value={name} onChange={(e) => setName(e.target.value)} placeholder={spec.label} /></Field>
          {spec.modes.length > 1 && (
            <Field label="How events arrive">
              <Select value={mode || spec.modes[0]} onChange={(e) => setMode(e.target.value)}>{spec.modes.map((m) => <option key={m} value={m}>{m}</option>)}</Select>
            </Field>
          )}
          {spec.config?.map((c) => (
            <Field key={c.key} label={c.key + (c.required ? '' : ' (optional)')} hint={c.hint || undefined}>
              <Input value={config[c.key] ?? ''} required={c.required} onChange={(e) => setConfig({ ...config, [c.key]: e.target.value })} />
            </Field>
          ))}
          {spec.credentials && (
            <Field label="Credentials (optional)" hint={spec.credentials + '. Stored encrypted; never shown again.'}>
              <Textarea rows={3} className="font-mono text-xs" value={creds} onChange={(e) => setCreds(e.target.value)} />
            </Field>
          )}
          <label className="flex items-start gap-2 text-sm">
            <input type="checkbox" className="mt-1" checked={noLLM} onChange={(e) => setNoLLM(e.target.checked)} />
            <span>Never send this source’s data to a model<span className="block text-xs text-slate-500">Issues are grouped and shown but not explained, and are left out of rule proposals.</span></span>
          </label>
          <ErrorNote error={create.error} />
          <Button type="submit" disabled={create.isPending}>Add</Button>
        </form>
      </Dialog>
    </>
  );
}

function AddKnowledge({ onCreated }: { onCreated: (type: string) => void }) {
  const [open, setOpen] = useState(false);
  const [type, setType] = useState<'confluence' | 'jira'>('confluence');
  const spec = knowledgeSpec(type)!;
  const [name, setName] = useState('');
  const [config, setConfig] = useState<Record<string, string>>({});
  const [token, setToken] = useState('');
  const create = useInvalidating((b: object) => api.post<{ id: string }>('/connectors', b), keys.connectors);
  return (
    <>
      <Button variant="secondary" onClick={() => setOpen(true)}>Add knowledge source</Button>
      <Dialog open={open} onOpenChange={setOpen} title="Add a knowledge source" description="Confluence spaces and Jira projects, synced read-only for answers, decodes, the Library, and known issues.">
        <form
          className="space-y-3"
          onSubmit={async (e) => {
            e.preventDefault();
            const cfg = Object.fromEntries(Object.entries(config).filter(([, v]) => v.trim() !== ''));
            await create.mutateAsync({ type, name: name || spec.label, mode: 'poll', config: cfg, credentials: await seal(token, 'connector.credentials') });
            onCreated(type);
            setOpen(false);
            setConfig({});
            setToken('');
          }}
        >
          <Field label="Source" hint={spec.help}>
            <Select value={type} onChange={(e) => { setType(e.target.value as 'confluence' | 'jira'); setConfig({}); }}>
              {KNOWLEDGE.map((k) => <option key={k.type} value={k.type}>{k.label}</option>)}
            </Select>
          </Field>
          <Field label="Name"><Input value={name} onChange={(e) => setName(e.target.value)} placeholder={spec.label} /></Field>
          {spec.config.map((c) => (
            <Field key={c.key} label={c.label + (c.required ? '' : ' (optional)')} hint={c.hint}>
              <Input value={config[c.key] ?? ''} required={c.required} placeholder={c.placeholder} onChange={(e) => setConfig({ ...config, [c.key]: e.target.value })} />
            </Field>
          ))}
          <Field label="API token" hint="Cloud: an API token for the e-mail above · Data Center: a personal access token. Stored encrypted; never shown again.">
            <Input type="password" required value={token} onChange={(e) => setToken(e.target.value)} autoComplete="off" />
          </Field>
          <ErrorNote error={create.error} />
          <Button type="submit" disabled={create.isPending}>Add</Button>
        </form>
      </Dialog>
    </>
  );
}

const remoteDone: Record<RemoteResult['action'], string> = {
  suspended: 'The GitHub App installation is suspended: GitHub sends no events and the App cannot read your repositories until you enable it again.',
  resumed: 'The GitHub App installation is active again.',
  uninstalled: 'The GitHub App was uninstalled from GitHub and no longer has access to your repositories.',
};

function RemoteNotice({ name, result, onClose }: { name: string; result: RemoteResult; onClose: () => void }) {
  return (
    <Card title={result.ok ? `${name}: done on GitHub` : `${name}: GitHub did not confirm`} className="mb-4">
      {result.ok ? (
        <p className="text-sm">{remoteDone[result.action]}</p>
      ) : (
        <p className="text-sm text-red-700 dark:text-red-400">
          The change was saved in the Hub, but GitHub refused to {result.action === 'uninstalled' ? 'uninstall' : result.action === 'suspended' ? 'suspend' : 'resume'} the App: {result.error}.
          You can do it on GitHub under Settings → Applications.
        </p>
      )}
      {result.action === 'uninstalled' && result.settings_url && (
        <p className="mt-2 text-sm">
          GitHub does not let other apps delete a GitHub App, so the App itself still exists. To delete it, open{' '}
          <a className="text-indigo-600 underline dark:text-indigo-400" href={`${result.settings_url}/advanced`} target="_blank" rel="noreferrer">its settings → Advanced → Delete GitHub App</a>.
        </p>
      )}
      <Button size="sm" variant="secondary" className="mt-2" onClick={onClose}>Dismiss</Button>
    </Card>
  );
}

export default function Connectors() {
  const conns = useConnectors();
  const [params, setParams] = useSearchParams();
  const ghConnected = params.get('github') === 'connected' ? params.get('connector') : null;
  const ghError = params.get('github_error');
  const clearGitHub = () => setParams({}, { replace: true });
  const [testing, setTesting] = useState<string>();
  const [created, setCreated] = useState<{ id: string; type: string; secret: string }>();
  const [signal, setSignal] = useState<{ id: string; type: string; secret: string; path: string }>();
  const [knowledge, setKnowledge] = useState<string>();
  const sync = useInvalidating((id: string) => api.post<{ job_ids: string[] }>(`/connectors/${id}/sync`));
  const [remote, setRemote] = useState<{ name: string; result: RemoteResult }>();
  const del = useInvalidating(async (c: Connector) => {
    const out = await api.del<{ github?: RemoteResult | null }>(`/connectors/${c.id}`);
    setRemote(out?.github ? { name: c.name, result: out.github } : undefined);
  }, keys.connectors, keys.repos);
  const toggle = useInvalidating(async (c: Connector) => {
    const out = await api.patch<{ github?: RemoteResult | null }>(`/connectors/${c.id}`, { enabled: !c.enabled });
    setRemote(out?.github ? { name: c.name, result: out.github } : undefined);
  }, keys.connectors);
  const isApp = (c: Connector) => c.type === 'github' && !!c.config?.app_id;
  const confirmRemove = (c: Connector) => confirm(isApp(c)
    ? `Remove ${c.name} and its repositories?\n\nThe GitHub App is also uninstalled from GitHub, so it loses access to your repositories at once.`
    : `Remove ${c.name} and its repositories?`);
  const confirmDisable = (c: Connector) => !c.enabled || !isApp(c) || confirm(
    `Disable ${c.name}?\n\nThe GitHub App installation is suspended on GitHub until you enable it again.`);
  return (
    <>
      <PageHeader title="Connectors" description="Git hosts, the error, alert, log, and event-platform sources the Inbox reads, and Confluence and Jira for knowledge. Wiz and Splunk arrive next."
        actions={<div className="flex flex-wrap gap-2"><AddKnowledge onCreated={setKnowledge} /><AddSignal onCreated={setSignal} /><AddGit onCreated={(id, type, secret) => setCreated({ id, type, secret })} /></div>} />
      {ghError && (
        <Card title="GitHub was not connected" className="mb-4">
          <p className="text-sm text-red-700 dark:text-red-400">{ghError}</p>
          <Button size="sm" variant="secondary" className="mt-2" onClick={clearGitHub}>Dismiss</Button>
        </Card>
      )}
      {ghConnected && <PickRepos connectorId={ghConnected} onDone={clearGitHub} />}
      {knowledge && (
        <Card title="Syncing" className="mb-4">
          <p className="text-sm">Saved. The first {knowledgeSpec(knowledge)?.label} sync starts within a minute; pages and issues then appear in Ask, the Library, and (when labelled) Known Issues. Health shows here after each sync.</p>
          <Button size="sm" variant="secondary" className="mt-2" onClick={() => setKnowledge(undefined)}>Done</Button>
        </Card>
      )}
      {signal && (
        <Card title="Finish setup" className="mb-4">
          {signal.path ? (
            <>
              <p className="text-sm">Point {sourceSpec(signal.type)?.label ?? signal.type} at this URL{signal.secret && ' and give it the secret'} ({sourceSpec(signal.type)?.help})</p>
              <dl aria-label="Webhook details" className="mt-2 grid grid-cols-[8rem_1fr] gap-y-1 text-sm">
                <dt className="text-slate-500">URL</dt>
                <dd className="font-mono text-xs">{window.location.origin}{signal.path}</dd>
                {signal.secret && <><dt className="text-slate-500">Secret</dt><dd className="font-mono text-xs">{signal.secret}</dd></>}
              </dl>
              {signal.secret && <p className="mt-2 text-xs text-slate-500">The secret is shown once.</p>}
            </>
          ) : (
            <p className="text-sm">Saved. The Hub starts reading {sourceSpec(signal.type)?.label ?? signal.type} within a minute; its health shows here after the first poll.</p>
          )}
          <Button size="sm" variant="secondary" className="mt-2" onClick={() => setSignal(undefined)}>Done</Button>
        </Card>
      )}
      {created && (
        <Card title="Finish webhook setup" className="mb-4">
          <p className="text-sm">The Hub registers this webhook on each repository you track. To add it by hand instead (for example on the whole organisation), use push{created.type === 'github' ? ' and pull request review' : ' and merge request'} events:</p>
          <dl className="mt-2 grid grid-cols-[8rem_1fr] gap-y-1 text-sm">
            <dt className="text-slate-500">URL</dt>
            <dd className="font-mono text-xs">{window.location.origin}/hooks/{created.type}/{created.id}</dd>
            <dt className="text-slate-500">{created.type === 'github' ? 'Secret' : 'Secret token'}</dt>
            <dd className="font-mono text-xs">{created.secret}</dd>
          </dl>
          <p className="mt-2 text-xs text-slate-500">The secret is shown once. Polling works without webhooks.</p>
          <Button size="sm" variant="secondary" className="mt-2" onClick={() => setCreated(undefined)}>Done</Button>
        </Card>
      )}
      {conns.isLoading && <Spinner />}
      <ErrorNote error={conns.error ?? sync.error ?? del.error ?? toggle.error} />
      {remote && <RemoteNotice name={remote.name} result={remote.result} onClose={() => setRemote(undefined)} />}
      {conns.data?.length === 0 && <Empty icon={Plug} title="No connectors yet">Connect GitHub or GitLab to start, then add the tools that report your errors and alerts.</Empty>}
      {!!conns.data?.length && (
        <Card>
          <Table head={['Name', 'Type', 'Mode', 'Health', 'Last sync', '']}>
            {conns.data.map((c) => (
              <tr key={c.id}>
                <Td>
                  <span className="font-medium">{c.name}</span>{!c.enabled && <Badge tone="amber">disabled</Badge>}
                  {c.has_credentials && <div className="mt-1"><SealedBadge meta={c.credentials_meta} label="Credentials sealed" /></div>}
                </Td>
                <Td>{c.type}</Td>
                <Td>{c.mode}</Td>
                <Td><Badge tone={statusTone(c.health)}>{c.health}</Badge>{c.last_error && <p className="max-w-xs truncate text-xs text-red-600" title={c.last_error}>{c.last_error}</p>}</Td>
                <Td>{relTime(c.last_sync_at)}{c.mode !== 'webhook' && sourceSpec(c.type) && <p className="text-xs text-slate-500">polled every {c.poll_seconds}s</p>}{knowledgeSpec(c.type) && <p className="text-xs text-slate-500">synced every {Math.round(c.poll_seconds / 60)} min</p>}</Td>
                <Td>
                  <div className="flex flex-wrap gap-1">
                    {knowledgeSpec(c.type) && <Button size="sm" variant="secondary" onClick={() => sync.mutate(c.id)}>Sync now</Button>}
                    {(c.type === 'github' || c.type === 'gitlab') && (
                      <>
                        <Button size="sm" variant="secondary" onClick={() => setTesting(c.id)}>Test</Button>
                        <Button size="sm" variant="secondary" onClick={() => sync.mutate(c.id)}>Sync now</Button>
                      </>
                    )}
                    <Button size="sm" variant="ghost" onClick={() => confirmDisable(c) && toggle.mutate(c)}>{c.enabled ? 'Disable' : 'Enable'}</Button>
                    <Button size="sm" variant="ghost" onClick={() => confirmRemove(c) && del.mutate(c)}>Remove</Button>
                  </div>
                </Td>
              </tr>
            ))}
          </Table>
        </Card>
      )}
      {sync.data && <p className="mt-2 text-sm text-slate-600">Queued {sync.data.job_ids.length} sync job(s).</p>}
      {testing && <TestResult id={testing} onClose={() => setTesting(undefined)} />}
    </>
  );
}
