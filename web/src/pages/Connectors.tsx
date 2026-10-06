import { ChevronDown } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { api, ApiError } from '@/api/client';
import { keys, useConnectors, useInvalidating } from '@/api/hooks';
import type { Check, Connector, RemoteResult } from '@/api/types';
import { Badge, Button, Card, Dialog, DialogFooter, ErrorNote, Field, Input, PageHeader, Select, Spinner, Table, Td, Textarea, statusTone } from '@/components/ui';
import { relTime } from '@/lib/format';
import { seal } from '@/lib/seal';
import { SealedBadge, SealedHint } from '@/components/Sealed';
import { KNOWLEDGE, knowledgeSpec, SOURCES, sourceSpec } from './signalSources';
import { ConnectAlerts, ConnectDocs, randomSecret } from './ConnectTools';
import { McpSection } from './McpConnections';
import { Link } from 'react-router-dom';
import { nameOf, sentence } from '@/lib/labels';

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
      <ExistingGitHubApp base={base} />
    </div>
  );
}

type ExistingResult = { connector_id: string; app_slug: string; installed: boolean; install_url?: string; account?: string };

/**
 * ExistingGitHubApp reuses a GitHub App you already have (for example from an earlier "Connect with GitHub"):
 * its App ID and a new private key are enough; the Hub finds where it is installed.
 */
function ExistingGitHubApp({ base }: { base: string }) {
  const [open, setOpen] = useState(false);
  const [appId, setAppId] = useState('');
  const [key, setKey] = useState('');
  const [keyFile, setKeyFile] = useState('');
  const [choices, setChoices] = useState<string[]>();
  const [account, setAccount] = useState('');
  const [result, setResult] = useState<ExistingResult>();
  const [err, setErr] = useState<unknown>();
  const [busy, setBusy] = useState(false);
  const [, setParams] = useSearchParams();
  const qc = useQueryClient();
  const web = (base.trim().replace(/\/+$/, '') || 'https://github.com');
  const finish = (id: string) => {
    void qc.invalidateQueries({ queryKey: keys.connectors });
    setParams({ github: 'connected', connector: id }, { replace: true });
  };
  const submit = async () => {
    setBusy(true);
    setErr(undefined);
    try {
      const r = await api.post<ExistingResult>('/github/connect/existing', {
        app_id: appId.trim(), private_key: await seal(key, 'connector.credentials'), base_url: base.trim() || undefined, account: account || undefined,
      });
      if (r.installed) finish(r.connector_id);
      else setResult(r);
    } catch (e) {
      if (e instanceof ApiError && e.code === 'CHOOSE_INSTALLATION') setChoices((e.details?.choices as string[]) ?? []);
      else setErr(e);
    } finally {
      setBusy(false);
    }
  };
  const recheck = async () => {
    if (!result) return;
    setBusy(true);
    setErr(undefined);
    try {
      const r = await api.post<{ installed: boolean; install_url?: string }>(`/github/connect/existing/${result.connector_id}/refresh`, {});
      if (r.installed) finish(result.connector_id);
      else setErr(new Error('GitHub does not show the App as installed yet. Finish the install on GitHub, then check again.'));
    } catch (e) {
      setErr(e);
    } finally {
      setBusy(false);
    }
  };
  if (!open) {
    return (
      <button type="button" onClick={() => setOpen(true)} className="mt-1 block text-xs text-slate-500 hover:text-slate-800 dark:hover:text-slate-200">
        Already created a DocTheRepo app on GitHub? <span className="text-brand-600 underline dark:text-brand-300">Use it instead</span>
      </button>
    );
  }
  if (result) {
    return (
      <div className="mt-3 space-y-2 rounded-lg border border-slate-200 bg-white p-3 text-sm dark:border-white/10 dark:bg-slate-900/60">
        <p className="font-medium">Saved. Now install “{result.app_slug}” on your account</p>
        <p className="text-xs text-slate-600 dark:text-slate-400">The App exists but isn't installed anywhere, so it can't read any repositories yet. Install it, pick the repositories, then come back here.</p>
        <div className="flex flex-wrap gap-2">
          <a href={result.install_url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-2 rounded-lg bg-slate-900 px-3 py-2 text-xs font-medium text-white hover:bg-slate-800 dark:bg-white dark:text-slate-900">
            <GitHubMark className="h-3.5 w-3.5" /> Install on GitHub
          </a>
          <Button size="sm" variant="secondary" disabled={busy} onClick={recheck}>{busy ? 'Checking…' : "I've installed it"}</Button>
        </div>
        <ErrorNote error={err} />
      </div>
    );
  }
  return (
    <div className="mt-3 space-y-3 rounded-lg border border-slate-200 bg-white p-3 text-sm dark:border-white/10 dark:bg-slate-900/60">
      <p className="font-medium">Use an existing GitHub App</p>
      <ol className="list-decimal space-y-1 pl-5 text-xs text-slate-600 dark:text-slate-400">
        <li>Open <a className="text-brand-600 underline dark:text-brand-300" href={`${web}/settings/apps`} target="_blank" rel="noreferrer">GitHub → Settings → Developer settings → GitHub Apps</a> (for an organization: the org's Settings → GitHub Apps) and click <b>Edit</b> next to your app.</li>
        <li>Copy the <b>App ID</b> number near the top of the page.</li>
        <li>Scroll to <b>Private keys</b> and click <b>Generate a private key</b>. GitHub downloads a <code>.pem</code> file: choose it below. (GitHub never shows an old key again, so a new one is needed. You can delete the old keys there.)</li>
      </ol>
      <div className="grid gap-2 sm:grid-cols-2">
        <Field label="App ID"><Input inputMode="numeric" value={appId} onChange={(e) => setAppId(e.target.value.replace(/\D/g, ''))} placeholder="1234567" /></Field>
        <Field label="Private key (.pem)" hint={keyFile ? `Loaded ${keyFile}` : 'Sealed in your browser before it is sent.'}>
          <input
            type="file"
            accept=".pem,application/x-pem-file,text/plain"
            aria-label="Private key file"
            className="block w-full text-xs file:mr-2 file:rounded-md file:border-0 file:bg-slate-100 file:px-2 file:py-1.5 file:text-xs dark:file:bg-white/10"
            onChange={async (e) => {
              const f = e.target.files?.[0];
              if (!f) return;
              setKey(await f.text());
              setKeyFile(f.name);
            }}
          />
        </Field>
      </div>
      {choices && (
        <Field label="Installed on several accounts: which one?">
          <Select value={account} onChange={(e) => setAccount(e.target.value)}>
            <option value="">Choose…</option>
            {choices.map((c) => <option key={c} value={c}>{sentence(c)}</option>)}
          </Select>
        </Field>
      )}
      <ErrorNote error={err} />
      <div className="flex gap-2">
        <Button size="sm" disabled={busy || !appId || !key || (!!choices && !account)} onClick={submit}>{busy ? 'Checking with GitHub…' : 'Connect this app'}</Button>
        <Button size="sm" variant="ghost" onClick={() => setOpen(false)}>Cancel</Button>
      </div>
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
          <p className="mt-2 text-xs text-slate-500">Docs are written as soon as a repository is tracked (it takes a few minutes for a large one), then kept up to date on every commit. To leave files out, add a .dthignore file (same syntax as .gitignore) to the repository.</p>
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
      <button type="button" onClick={() => setOpen(true)} className="text-sm text-brand-600 hover:underline dark:text-brand-300">GitLab, a token, or GitHub Enterprise with your own app</button>
      <Dialog open={open} onOpenChange={setOpen} title="Connect GitLab or GitHub by hand" description="Read access to code, and write access limited to the generated-docs path (enforced by the Hub).">
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
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => setOpen(false)}>Cancel</Button>
            <Button type="submit" disabled={create.isPending}>Connect</Button>
          </DialogFooter>
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
  const [alertTool, setAlertTool] = useState<string>();
  const [docTool, setDocTool] = useState<'confluence' | 'jira' | 'notion'>();
  const [knowledge, setKnowledge] = useState<string>();
  const [allTools, setAllTools] = useState(false);
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
  const list = conns.data ?? [];
  const codeConns = list.filter((c) => c.type === 'github' || c.type === 'gitlab');
  const docConns = list.filter((c) => knowledgeSpec(c.type));
  const alertConns = list.filter((c) => sourceSpec(c.type));
  const shown = allTools ? SOURCES : SOURCES.filter((x) => POPULAR.includes(x.type));
  return (
    <>
      <PageHeader title="Connections" description="Where the Hub reads from: your code, your team's documents, and (optional) the tools that report errors and alerts." />
      {ghError && (
        <Card title="GitHub was not connected" className="mb-4">
          <p className="text-sm text-red-700 dark:text-red-400">{ghError}</p>
          <Button size="sm" variant="secondary" className="mt-2" onClick={clearGitHub}>Dismiss</Button>
        </Card>
      )}
      {ghConnected && <PickRepos connectorId={ghConnected} onDone={clearGitHub} />}
      {knowledge && (
        <Card title="Syncing" className="mb-4">
          <p className="text-sm">Connected. The first {knowledgeSpec(knowledge)?.label} sync starts within a minute; its pages then appear in Ask and Team docs.</p>
          <Button size="sm" variant="secondary" className="mt-2" onClick={() => setKnowledge(undefined)}>Done</Button>
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

      <Group title="Code" text="The repositories the Hub documents and answers questions about.">
        {codeConns.length === 0 ? (
          <div className="max-w-xl"><ConnectGitHub /></div>
        ) : <ConnectorTable items={codeConns} actions={rowActions} />}
        <div className="mt-3"><AddGit onCreated={(id, type, secret) => setCreated({ id, type, secret })} /></div>
      </Group>

      <Group title="Team documents" text="Pages Ask can answer from, next to your code.">
        <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
          {KNOWLEDGE.map((k) => <Tile key={k.type} label={k.label} sub="Site, what to sync, a token" onClick={() => setDocTool(k.type)} />)}
          <Link to="/library" className={tileClass}><span className="font-medium">Upload files</span><span className="text-xs text-slate-500">Markdown, text or HTML, under Team docs</span></Link>
        </div>
        {docConns.length > 0 && <div className="mt-4"><ConnectorTable items={docConns} actions={rowActions} /></div>}
      </Group>

      <Group title="Errors and alerts" badge="Optional" text="Connect the tools that report problems and the Hub groups them into Issues and explains them with your code. Most need no form: you get one link to paste into the tool.">
        <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
          {shown.map((x) => <Tile key={x.type} label={x.label} sub={x.modes.includes('poll') && !x.modes.includes('webhook') && !x.modes.includes('both') ? 'Read on a schedule' : 'Paste one link'} onClick={() => setAlertTool(x.type)} />)}
        </div>
        <button type="button" className="mt-2 text-sm text-brand-600 hover:underline dark:text-brand-300" onClick={() => setAllTools((v) => !v)}>
          {allTools ? 'Show fewer' : `Show all ${SOURCES.length} tools (cloud logs, queues, Wiz, Splunk…)`}
        </button>
        {alertConns.length > 0 && <div className="mt-4"><ConnectorTable items={alertConns} actions={rowActions} /></div>}
      </Group>

      <Group title="Look things up live (MCP)" badge="Optional" text="Let Ask check other products while it answers: current errors in Sentry, logs in Datadog, tickets in Jira, resources in AWS or Google Cloud. Most connect by signing in. Only read-only tools are used unless you turn others on.">
        <McpSection tileClass={tileClass} />
      </Group>

      {sync.data && <p className="mt-2 text-sm text-slate-600">Queued {sync.data.job_ids.length} sync job(s).</p>}
      {testing && <TestResult id={testing} onClose={() => setTesting(undefined)} />}
      {alertTool && <ConnectAlerts type={alertTool} onClose={() => setAlertTool(undefined)} />}
      {docTool && <ConnectDocs type={docTool} onClose={() => setDocTool(undefined)} onDone={(t) => { setDocTool(undefined); setKnowledge(t); }} />}
    </>
  );

  function rowActions(c: Connector) {
    return (
      <div className="flex flex-wrap gap-1">
        {(knowledgeSpec(c.type) || c.type === 'github' || c.type === 'gitlab') && <Button size="sm" variant="secondary" onClick={() => sync.mutate(c.id)}>Sync now</Button>}
        {(c.type === 'github' || c.type === 'gitlab') && <Button size="sm" variant="secondary" onClick={() => setTesting(c.id)}>Test</Button>}
        <Button size="sm" variant="ghost" onClick={() => confirmDisable(c) && toggle.mutate(c)}>{c.enabled ? 'Disable' : 'Enable'}</Button>
        <Button size="sm" variant="ghost" onClick={() => confirmRemove(c) && del.mutate(c)}>Remove</Button>
      </div>
    );
  }
}

/** The alert tools shown first; the rest are one click away. */
const POPULAR = ['sentry', 'datadog', 'pagerduty', 'grafana', 'opsgenie', 'alertmanager', 'cloudwatch', 'generic'];

const tileClass = 'flex flex-col items-start gap-0.5 rounded-lg border border-slate-200 bg-white px-3 py-2.5 text-left text-sm transition-colors hover:border-brand-400 dark:border-white/10 dark:bg-slate-900/60 dark:hover:border-brand-400/60';

function Tile({ label, sub, onClick }: { label: string; sub: string; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} className={tileClass} aria-label={`Connect ${label}`}>
      <span className="font-medium">{label}</span>
      <span className="text-xs text-slate-500">{sub}</span>
    </button>
  );
}

function Group({ title, text, badge, children }: { title: string; text: string; badge?: string; children: React.ReactNode }) {
  return (
    <section className="mb-10">
      <div className="mb-3 flex items-center gap-2">
        <h2 className="text-base font-semibold">{title}</h2>
        {badge && <Badge>{badge}</Badge>}
      </div>
      <p className="mb-4 max-w-3xl text-sm text-slate-500 dark:text-slate-400">{text}</p>
      {children}
    </section>
  );
}

/** ConnectorTable lists connected tools with their health. */
function ConnectorTable({ items, actions }: { items: Connector[]; actions: (c: Connector) => React.ReactNode }) {
  return (
    <Card>
      <Table head={['Name', 'Health', 'Last sync', '']}>
        {items.map((c) => (
          <tr key={c.id}>
            <Td>
              <span className="font-medium">{c.name}</span>{!c.enabled && <Badge tone="amber">Disabled</Badge>}
              <p className="text-xs text-slate-500">{nameOf(c.type)}{c.mode !== 'poll' && sourceSpec(c.type) ? ' · link' : ''}</p>
              {c.has_credentials && <div className="mt-1"><SealedBadge meta={c.credentials_meta} label="Credentials sealed" /></div>}
            </Td>
            <Td><Badge tone={statusTone(c.health)}>{c.health}</Badge>{c.last_error && <p className="max-w-xs truncate text-xs text-red-600" title={c.last_error}>{c.last_error}</p>}</Td>
            <Td>{relTime(c.last_sync_at)}</Td>
            <Td>{actions(c)}</Td>
          </tr>
        ))}
      </Table>
    </Card>
  );
}
