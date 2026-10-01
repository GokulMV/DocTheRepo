import { useEffect, useState } from 'react';
import { api } from '@/api/client';
import { keys, useConnectors, useInvalidating } from '@/api/hooks';
import type { Check, Connector } from '@/api/types';
import { Badge, Button, Card, Dialog, Empty, ErrorNote, Field, Input, PageHeader, Select, Spinner, Table, Td, Textarea, statusTone } from '@/components/ui';
import { relTime } from '@/lib/format';
import { SOURCES, sourceSpec } from './signalSources';

function randomSecret() {
  const b = new Uint8Array(24);
  crypto.getRandomValues(b);
  return Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
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
    const r = await create.mutateAsync({ type, name: name || (type === 'github' ? 'GitHub' : 'GitLab'), mode, config, credentials: token, webhook_secret: secret });
    setOpen(false);
    onCreated(r.id, type, secret);
  };
  return (
    <>
      <Button onClick={() => setOpen(true)}>Connect git host</Button>
      <Dialog open={open} onOpenChange={setOpen} title="Connect GitHub or GitLab" description="Read access to code, and write access limited to the generated-docs path (enforced by the Hub).">
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
          <Field label={type === 'github' && auth === 'app' ? 'App private key (PEM)' : 'Access token'} hint="Stored encrypted; never shown again.">
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
  const create = useInvalidating((b: object) => api.post<{ id: string; webhook_path: string }>('/connectors', b), keys.connectors);
  const pick = (t: string) => { setType(t); setConfig({}); setMode(''); };
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
            const cfg = Object.fromEntries(Object.entries(config).filter(([, v]) => v.trim() !== ''));
            const r = await create.mutateAsync({ type, name: name || spec.label, mode: m, config: cfg, credentials: creds || undefined, webhook_secret: secret || undefined });
            onCreated({ id: r.id, type, secret, path: r.webhook_path });
            setOpen(false);
          }}
        >
          <Field label="Source" hint={spec.help}>
            <Select value={type} onChange={(e) => pick(e.target.value)}>
              {(['Errors & alerts', 'Cloud logs & alarms', 'Event platforms'] as const).map((g) => (
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
          <ErrorNote error={create.error} />
          <Button type="submit" disabled={create.isPending}>Add</Button>
        </form>
      </Dialog>
    </>
  );
}

export default function Connectors() {
  const conns = useConnectors();
  const [testing, setTesting] = useState<string>();
  const [created, setCreated] = useState<{ id: string; type: string; secret: string }>();
  const [signal, setSignal] = useState<{ id: string; type: string; secret: string; path: string }>();
  const sync = useInvalidating((id: string) => api.post<{ job_ids: string[] }>(`/connectors/${id}/sync`));
  const del = useInvalidating((id: string) => api.del(`/connectors/${id}`), keys.connectors, keys.repos);
  const toggle = useInvalidating((c: Connector) => api.patch(`/connectors/${c.id}`, { enabled: !c.enabled }), keys.connectors);
  return (
    <>
      <PageHeader title="Connectors" description="Git hosts, plus the error, alert, log, and event-platform sources the Inbox reads. Confluence, Jira, Wiz and Splunk arrive next."
        actions={<div className="flex gap-2"><AddSignal onCreated={setSignal} /><AddGit onCreated={(id, type, secret) => setCreated({ id, type, secret })} /></div>} />
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
      <ErrorNote error={conns.error ?? sync.error ?? del.error} />
      {conns.data?.length === 0 && <Empty title="No connectors yet">Connect GitHub or GitLab to start, then add the tools that report your errors and alerts.</Empty>}
      {!!conns.data?.length && (
        <Card>
          <Table head={['Name', 'Type', 'Mode', 'Health', 'Last sync', '']}>
            {conns.data.map((c) => (
              <tr key={c.id}>
                <Td><span className="font-medium">{c.name}</span>{!c.enabled && <Badge tone="amber">disabled</Badge>}</Td>
                <Td>{c.type}</Td>
                <Td>{c.mode}</Td>
                <Td><Badge tone={statusTone(c.health)}>{c.health}</Badge>{c.last_error && <p className="max-w-xs truncate text-xs text-red-600" title={c.last_error}>{c.last_error}</p>}</Td>
                <Td>{relTime(c.last_sync_at)}{c.mode !== 'webhook' && sourceSpec(c.type) && <p className="text-xs text-slate-500">polled every {c.poll_seconds}s</p>}</Td>
                <Td>
                  <div className="flex flex-wrap gap-1">
                    {(c.type === 'github' || c.type === 'gitlab') && (
                      <>
                        <Button size="sm" variant="secondary" onClick={() => setTesting(c.id)}>Test</Button>
                        <Button size="sm" variant="secondary" onClick={() => sync.mutate(c.id)}>Sync now</Button>
                      </>
                    )}
                    <Button size="sm" variant="ghost" onClick={() => toggle.mutate(c)}>{c.enabled ? 'Disable' : 'Enable'}</Button>
                    <Button size="sm" variant="ghost" onClick={() => confirm(`Remove ${c.name} and its repositories?`) && del.mutate(c.id)}>Remove</Button>
                  </div>
                </Td>
              </tr>
            ))}
          </Table>
        </Card>
      )}
      {sync.data && <p className="mt-2 text-sm text-slate-600">Queued {sync.data.job_ids.length} push job(s).</p>}
      {testing && <TestResult id={testing} onClose={() => setTesting(undefined)} />}
    </>
  );
}
