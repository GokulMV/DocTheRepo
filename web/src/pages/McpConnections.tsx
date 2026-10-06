import { useQuery } from '@tanstack/react-query';
import { useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { api } from '@/api/client';
import { useInvalidating } from '@/api/hooks';
import type { McpServer } from '@/api/types';
import { Badge, Button, Dialog, DialogFooter, ErrorNote, Field, Input, Select, Table, Td, Textarea, Toggle, type Tone } from '@/components/ui';
import { seal } from '@/lib/seal';
import { AUTH_LABEL, CUSTOM_MCP, MCP_CATALOG, mcpEntry, type McpAuth, type McpEntry } from './mcpCatalog';

const mcpKey = ['mcp-servers'] as const;

export function useMcpServers() {
  return useQuery({ queryKey: mcpKey, queryFn: () => api.get<{ items: McpServer[]; redirect_uri: string }>('/mcp/servers') });
}

const STATUS: Record<McpServer['status'], { label: string; tone: Tone }> = {
  ok: { label: 'Connected', tone: 'green' },
  needs_sign_in: { label: 'Needs sign-in', tone: 'amber' },
  error: { label: 'Not working', tone: 'red' },
  new: { label: 'Not checked', tone: 'gray' },
};

const ROLES = [
  { value: 'viewer', label: 'Everyone who can sign in' },
  { value: 'editor', label: 'Editors and above' },
  { value: 'admin', label: 'Admins only' },
];

const POPULAR = ['aws', 'gcp', 'sentry', 'datadog', 'atlassian', 'github', 'pagerduty', 'grafana'];

/** McpSection is the Connections group for other products' MCP servers. */
export function McpSection({ tileClass }: { tileClass: string }) {
  const servers = useMcpServers();
  const [open, setOpen] = useState<McpEntry>();
  const [tools, setTools] = useState<McpServer>();
  const [all, setAll] = useState(false);
  const [params, setParams] = useSearchParams();
  const signedIn = params.get('mcp');
  const signInError = params.get('mcp_error');
  const items = servers.data?.items ?? [];
  const shown = all ? MCP_CATALOG : MCP_CATALOG.filter((e) => POPULAR.includes(e.key));
  const done = items.find((s) => s.id === signedIn);
  return (
    <>
      {(done || signInError) && (
        <div role="status" className={`mb-4 rounded-lg border px-3 py-2 text-sm ${signInError ? 'border-red-200 bg-red-50 text-red-800 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200' : 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-200'}`}>
          {signInError ? `Sign-in did not finish: ${signInError}` : `Signed in to ${done!.name}. Ask can use ${done!.tools.filter((t) => toolOn(done!, t)).length} of its tools.`}
          <button type="button" className="ml-3 underline" onClick={() => { params.delete('mcp'); params.delete('mcp_error'); setParams(params, { replace: true }); }}>Dismiss</button>
        </div>
      )}
      <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
        {shown.map((e) => (
          <button key={e.key} type="button" className={tileClass} aria-label={`Live lookups in ${e.name}`} onClick={() => setOpen(e)}>
            <span className="font-medium">{e.name}</span>
            <span className="text-xs text-slate-500">{e.what}</span>
          </button>
        ))}
        <button type="button" className={tileClass} aria-label="Live lookups in any MCP server" onClick={() => setOpen(CUSTOM_MCP)}>
          <span className="font-medium">Any MCP server</span>
          <span className="text-xs text-slate-500">Paste its address</span>
        </button>
      </div>
      <button type="button" className="mt-2 text-sm text-brand-600 hover:underline dark:text-brand-300" onClick={() => setAll((v) => !v)}>
        {all ? 'Show fewer' : `Show all ${MCP_CATALOG.length} products`}
      </button>
      {items.length > 0 && <div className="mt-4"><McpTable items={items} onTools={setTools} /></div>}
      <ErrorNote error={servers.error} />
      {open && <ConnectMcp entry={open} taken={items.map((s) => s.name)} onClose={() => setOpen(undefined)} />}
      {tools && <McpTools server={items.find((s) => s.id === tools.id) ?? tools} onClose={() => setTools(undefined)} />}
    </>
  );
}

export function toolOn(s: McpServer, t: McpServer['tools'][number]) {
  return s.tool_choices[t.name] ?? t.read_only;
}

function McpTable({ items, onTools }: { items: McpServer[]; onTools: (s: McpServer) => void }) {
  const act = useInvalidating(async ({ id, op }: { id: string; op: 'check' | 'sign-in' | 'delete' | 'enable' | 'disable' }) => {
    if (op === 'sign-in') {
      const r = await api.post<{ url: string }>(`/mcp/servers/${id}/sign-in`);
      window.location.assign(r.url);
      return;
    }
    if (op === 'delete') return api.del(`/mcp/servers/${id}`);
    if (op === 'check') return api.post(`/mcp/servers/${id}/check`);
    return api.patch(`/mcp/servers/${id}`, { enabled: op === 'enable' });
  }, mcpKey);
  return (
    <>
      <Table head={['Name', 'Status', 'Tools Ask may use', 'Who can use it', '']}>
        {items.map((s) => {
          const st = STATUS[s.status];
          const on = s.tools.filter((t) => toolOn(s, t)).length;
          return (
            <tr key={s.id}>
              <Td><span className="font-medium">{s.name}</span><div className="max-w-xs truncate text-xs text-slate-500">{s.url}</div></Td>
              <Td>
                <Badge tone={s.enabled ? st.tone : 'gray'}>{s.enabled ? st.label : 'Off'}</Badge>
                {s.last_error && <div className="mt-1 max-w-xs text-xs text-red-700 dark:text-red-300">{s.last_error}</div>}
              </Td>
              <Td>{s.status === 'ok' ? `${on} of ${s.tools.length}` : '—'}</Td>
              <Td className="text-xs">{ROLES.find((r) => r.value === s.min_role)?.label ?? s.min_role}</Td>
              <Td className="whitespace-nowrap text-right">
                {s.auth === 'oauth' && (
                  <Button size="sm" variant={s.status === 'needs_sign_in' ? 'primary' : 'secondary'} onClick={() => act.mutate({ id: s.id, op: 'sign-in' })} aria-label={`Sign in to ${s.name}`}>
                    {s.signed_in ? 'Sign in again' : 'Sign in'}
                  </Button>
                )}{' '}
                {s.status === 'ok' && <Button size="sm" variant="secondary" onClick={() => onTools(s)} aria-label={`Choose tools for ${s.name}`}>Tools</Button>}{' '}
                <Button size="sm" variant="ghost" onClick={() => act.mutate({ id: s.id, op: 'check' })} aria-label={`Check ${s.name}`}>Check</Button>
                <Button size="sm" variant="ghost" onClick={() => act.mutate({ id: s.id, op: s.enabled ? 'disable' : 'enable' })}>{s.enabled ? 'Turn off' : 'Turn on'}</Button>
                <Button size="sm" variant="ghost" aria-label={`Remove ${s.name}`} onClick={() => { if (confirm(`Remove ${s.name}? Ask stops using it.`)) act.mutate({ id: s.id, op: 'delete' }); }}>Remove</Button>
              </Td>
            </tr>
          );
        })}
      </Table>
      <ErrorNote error={act.error} />
    </>
  );
}

function fill(url: string, values: Record<string, string>) {
  return url.replace(/\{(\w+)\}/g, (m, k: string) => values[k]?.trim().replace(/\/+$/, '') || m);
}

function uniqueName(base: string, taken: string[]) {
  if (!taken.includes(base)) return base;
  let n = 2;
  while (taken.includes(`${base} ${n}`)) n++;
  return `${base} ${n}`;
}

/** ConnectMcp adds one MCP connection: the address, how it signs in, and who may use it. */
export function ConnectMcp({ entry, taken, onClose }: { entry: McpEntry; taken: string[]; onClose: () => void }) {
  const authChoices: McpAuth[] = [entry.auth, ...(entry.alsoAuth ?? [])];
  const [variant, setVariant] = useState(entry.variants?.[0]?.label ?? '');
  const baseURL = entry.variants?.find((v) => v.label === variant)?.url ?? entry.url;
  const [parts, setParts] = useState<Record<string, string>>({});
  const [url, setUrl] = useState('');
  const [editedURL, setEditedURL] = useState(false);
  const [auth, setAuth] = useState<McpAuth>(entry.auth);
  const [name, setName] = useState(() => uniqueName(entry.key === 'custom' ? '' : entry.name, taken));
  const [secret, setSecret] = useState('');
  const [headerName, setHeaderName] = useState(entry.headerName ?? '');
  const [extra, setExtra] = useState('');
  const [ambient, setAmbient] = useState(true); // use the Hub's own cloud identity
  const [accessKey, setAccessKey] = useState('');
  const [region, setRegion] = useState('');
  const [service, setService] = useState('');
  const [clientId, setClientId] = useState('');
  const [role, setRole] = useState('editor');
  const [more, setMore] = useState(false);
  const shownURL = editedURL ? url : fill(baseURL, parts);
  useEffect(() => {
    if (entry.variants && entry.key === 'gcp' && !editedURL) {
      setName(uniqueName(`Google Cloud ${variant}`, taken));
    }
  }, [variant, entry, editedURL, taken]);
  const unfilled = /\{\w+\}/.test(shownURL) || !shownURL.trim();
  const needsSecret = (auth === 'bearer' || auth === 'header') || ((auth === 'aws' || auth === 'google') && !ambient);
  const create = useInvalidating(async () => {
    const config: Record<string, string> = {};
    if (auth === 'header') config.header_name = headerName.trim();
    if (entry.extraHeader && extra.trim()) config[`header:${entry.extraHeader.name}`] = extra.trim();
    if (auth === 'aws') {
      if (region.trim()) config.region = region.trim();
      if (service.trim()) config.service = service.trim();
    }
    if (auth === 'oauth' && clientId.trim()) config.client_id = clientId.trim();
    let raw = '';
    if (auth === 'bearer' || auth === 'header' || auth === 'oauth') raw = secret.trim();
    if (auth === 'aws' && !ambient) raw = JSON.stringify({ access_key_id: accessKey.trim(), secret_access_key: secret.trim() });
    if (auth === 'google' && !ambient) raw = secret.trim();
    const s = await api.post<McpServer>('/mcp/servers', {
      name: name.trim(), url: shownURL.trim(), catalog_key: entry.key, auth, config, min_role: role,
      secret: raw ? await seal(raw, 'mcp.secret') : '',
    });
    if (auth === 'oauth') {
      const r = await api.post<{ url: string }>(`/mcp/servers/${s.id}/sign-in`);
      window.location.assign(r.url);
    }
    return s;
  }, mcpKey);
  const result = create.data;
  if (result && auth !== 'oauth') {
    const st = STATUS[result.status];
    return (
      <Dialog open onOpenChange={(o) => !o && onClose()} title={`${result.name}: ${st.label}`}>
        {result.status === 'ok' ? (
          <p className="text-sm">Ask can now use {result.tools.filter((t) => toolOn(result, t)).length} of its {result.tools.length} tools: the ones {result.name} marks as read-only. Change this under <b>Tools</b>.</p>
        ) : (
          <p className="text-sm text-red-700 dark:text-red-300">{result.last_error || 'It could not connect.'} Fix it and click <b>Check</b>.</p>
        )}
        <DialogFooter><Button onClick={onClose}>Done</Button></DialogFooter>
      </Dialog>
    );
  }
  const valid = name.trim() && !unfilled && (!needsSecret || secret.trim()) && (auth !== 'header' || headerName.trim()) && (auth !== 'aws' || ambient || accessKey.trim());
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={entry.key === 'custom' ? 'Connect an MCP server' : `Live lookups in ${entry.name}`}
      description={`Ask will be able to look things up in ${entry.key === 'custom' ? 'it' : entry.name} while answering. Only read-only tools are on at first.`}>
      <div className="space-y-4">
        {entry.note && <p className="rounded-lg bg-slate-50 px-3 py-2 text-sm text-slate-600 dark:bg-white/5 dark:text-slate-300">{entry.note}</p>}
        {entry.variants && (
          <Field label={entry.key === 'gcp' ? 'Product' : 'Region or site'}>
            <Select aria-label={entry.key === 'gcp' ? 'Product' : 'Region or site'} value={variant} onChange={(e) => { setEditedURL(false); setVariant(e.target.value); }}>
              {entry.variants.map((v) => <option key={v.label}>{v.label}</option>)}
            </Select>
          </Field>
        )}
        {Object.entries(entry.placeholders ?? {}).map(([k, label]) => (
          <Field key={k} label={label}><Input aria-label={label} value={parts[k] ?? ''} onChange={(e) => setParts({ ...parts, [k]: e.target.value })} /></Field>
        ))}
        {entry.extraHeader && (
          <Field label={entry.extraHeader.label}><Input aria-label={entry.extraHeader.label} placeholder={entry.extraHeader.placeholder} value={extra} onChange={(e) => setExtra(e.target.value)} /></Field>
        )}
        {authChoices.length > 1 && (
          <Field label="How it signs in">
            <Select aria-label="How it signs in" value={auth} onChange={(e) => setAuth(e.target.value as McpAuth)}>
              {authChoices.map((a) => <option key={a} value={a}>{AUTH_LABEL[a]}</option>)}
            </Select>
          </Field>
        )}
        {auth === 'oauth' && <p className="text-sm text-slate-600 dark:text-slate-400">After you click Connect, {entry.key === 'custom' ? 'the product' : entry.name} opens so you can sign in and approve access. You come back here when it is done.</p>}
        {auth === 'bearer' && (
          <Field label={capital(entry.keyName ?? 'token')} hint="Stored sealed. Use one that can only read.">
            <Input aria-label="Token" type="password" autoComplete="off" value={secret} onChange={(e) => setSecret(e.target.value)} />
          </Field>
        )}
        {auth === 'header' && (
          <>
            {!entry.headerName && <Field label="Header name"><Input aria-label="Header name" placeholder="X-API-Key" value={headerName} onChange={(e) => setHeaderName(e.target.value)} /></Field>}
            <Field label={capital(entry.keyName ?? 'key')} hint={`Sent as the ${headerName || 'header'} header. Stored sealed.`}>
              <Input aria-label="Key" type="password" autoComplete="off" value={secret} onChange={(e) => setSecret(e.target.value)} />
            </Field>
          </>
        )}
        {(auth === 'aws' || auth === 'google') && (
          <Toggle checked={ambient} onChange={setAmbient} label={auth === 'aws' ? 'Use the IAM role the Hub runs with (recommended on AWS)' : 'Use the service account the Hub runs with (recommended on Google Cloud)'} />
        )}
        {auth === 'aws' && !ambient && (
          <>
            <Field label="Access key ID"><Input aria-label="Access key ID" autoComplete="off" value={accessKey} onChange={(e) => setAccessKey(e.target.value)} /></Field>
            <Field label="Secret access key" hint="Stored sealed. Give this user read-only policies."><Input aria-label="Secret access key" type="password" autoComplete="off" value={secret} onChange={(e) => setSecret(e.target.value)} /></Field>
          </>
        )}
        {auth === 'google' && !ambient && (
          <Field label="Service account key (JSON)" hint="Stored sealed. Give the account viewer roles only.">
            <Textarea aria-label="Service account key" rows={4} value={secret} onChange={(e) => setSecret(e.target.value)} />
          </Field>
        )}
        <Field label="Who can use it in Ask" hint="Answers can include what this connection returns.">
          <Select aria-label="Who can use it in Ask" value={role} onChange={(e) => setRole(e.target.value)}>
            {ROLES.map((r) => <option key={r.value} value={r.value}>{r.label}</option>)}
          </Select>
        </Field>
        <details open={more || entry.key === 'custom'} onToggle={(e) => setMore((e.target as HTMLDetailsElement).open)} className="rounded-lg border border-slate-200 px-3 py-2 dark:border-white/10">
          <summary className="cursor-pointer text-sm font-medium">More options</summary>
          <div className="mt-3 space-y-4">
            <Field label="Name"><Input aria-label="Name" value={name} onChange={(e) => setName(e.target.value)} /></Field>
            <Field label="Server address" hint={entry.check ? 'Compare this with the vendor’s page; it may have changed.' : undefined}>
              <Input aria-label="Server address" value={shownURL} placeholder="https://example.com/mcp" onChange={(e) => { setEditedURL(true); setUrl(e.target.value); }} />
            </Field>
            {auth === 'oauth' && (
              <>
                <Field label="OAuth client ID (optional)" hint="Only when the product does not let apps register themselves (Google Cloud, Slack). Its redirect address must be the one below.">
                  <Input aria-label="OAuth client ID" value={clientId} onChange={(e) => setClientId(e.target.value)} />
                </Field>
                {clientId && <Field label="OAuth client secret"><Input aria-label="OAuth client secret" type="password" autoComplete="off" value={secret} onChange={(e) => setSecret(e.target.value)} /></Field>}
                <RedirectHint />
              </>
            )}
            {auth === 'aws' && (
              <div className="grid grid-cols-2 gap-3">
                <Field label="Region" hint="Read from the address when empty."><Input aria-label="Region" placeholder="us-east-1" value={region} onChange={(e) => setRegion(e.target.value)} /></Field>
                <Field label="Signing service"><Input aria-label="Signing service" placeholder="aws-mcp" value={service} onChange={(e) => setService(e.target.value)} /></Field>
              </div>
            )}
            {entry.key !== 'custom' && <a className="text-sm text-brand-600 hover:underline dark:text-brand-300" href={entry.docs} target="_blank" rel="noreferrer">{entry.name}’s setup page</a>}
          </div>
        </details>
        <ErrorNote error={create.error} />
      </div>
      <DialogFooter>
        <Button variant="secondary" onClick={onClose}>Cancel</Button>
        <Button disabled={!valid || create.isPending} onClick={() => create.mutate(undefined)}>{create.isPending ? 'Connecting…' : auth === 'oauth' ? 'Connect and sign in' : 'Connect'}</Button>
      </DialogFooter>
    </Dialog>
  );
}

function RedirectHint() {
  const servers = useMcpServers();
  if (!servers.data?.redirect_uri) return null;
  return <p className="text-xs text-slate-500">Redirect address: <code className="break-all">{servers.data.redirect_uri}</code></p>;
}

function capital(s: string) {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

/** McpTools turns a connection's tools on and off for Ask. */
function McpTools({ server, onClose }: { server: McpServer; onClose: () => void }) {
  const initial = useMemo(() => Object.fromEntries(server.tools.map((t) => [t.name, toolOn(server, t)])), [server]);
  const [on, setOn] = useState<Record<string, boolean>>(initial);
  const save = useInvalidating(() => api.patch(`/mcp/servers/${server.id}`, { tool_choices: on }), mcpKey);
  const writes = server.tools.filter((t) => !t.read_only && on[t.name]).length;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={`${server.name}: tools`} description="Ask calls only the tools that are on. Tools the server does not mark read-only start off: they may change things.">
      <ul className="max-h-[50vh] space-y-2 overflow-y-auto">
        {server.tools.map((t) => (
          <li key={t.name} className="flex items-start gap-3 rounded-lg border border-slate-200 px-3 py-2 dark:border-white/10">
            <input type="checkbox" className="mt-1" aria-label={t.name} checked={!!on[t.name]} onChange={(e) => setOn({ ...on, [t.name]: e.target.checked })} />
            <div className="min-w-0">
              <div className="flex items-center gap-2 text-sm font-medium"><code>{t.name}</code>{t.read_only ? <Badge tone="green">Read-only</Badge> : <Badge tone="amber">May change things</Badge>}</div>
              {t.description && <p className="mt-0.5 line-clamp-2 text-xs text-slate-500">{t.description}</p>}
            </div>
          </li>
        ))}
      </ul>
      {writes > 0 && <p className="mt-3 text-sm text-amber-700 dark:text-amber-300">{writes} tool{writes === 1 ? '' : 's'} that may change things {writes === 1 ? 'is' : 'are'} on. Ask could call {writes === 1 ? 'it' : 'them'} while answering.</p>}
      <ErrorNote error={save.error} />
      <DialogFooter>
        <Button variant="secondary" onClick={onClose}>Cancel</Button>
        <Button disabled={save.isPending} onClick={async () => { await save.mutateAsync(undefined); onClose(); }}>Save</Button>
      </DialogFooter>
    </Dialog>
  );
}

export { mcpEntry };
