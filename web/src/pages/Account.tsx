import { Bot, Check, Copy, KeyRound, SquareTerminal, Workflow } from 'lucide-react';
import { useState } from 'react';
import { api } from '@/api/client';
import { keys, useInvalidating, useMe, useTokens } from '@/api/hooks';
import { Button, Card, ErrorNote, Field, Input, PageHeader, Select, Table, Td, cx } from '@/components/ui';
import { relTime } from '@/lib/format';

/** CopyBlock is a code snippet with a copy button. */
function CopyBlock({ text, label }: { text: string; label: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="relative">
      <pre aria-label={label} className="overflow-x-auto whitespace-pre-wrap break-all rounded-lg bg-slate-900 p-3 pr-12 font-mono text-xs text-slate-100 dark:bg-black/40">{text}</pre>
      <button
        type="button"
        onClick={async () => {
          await navigator.clipboard?.writeText(text).catch(() => undefined);
          setCopied(true);
          setTimeout(() => setCopied(false), 2000);
        }}
        aria-label={`Copy ${label}`}
        className="absolute right-2 top-2 rounded-md bg-white/10 p-1.5 text-slate-200 hover:bg-white/20"
      >
        {copied ? <Check className="h-3.5 w-3.5" aria-hidden /> : <Copy className="h-3.5 w-3.5" aria-hidden />}
      </button>
    </div>
  );
}

const USES = [
  { icon: SquareTerminal, title: 'The dth command line', text: 'Ask questions, run dry runs and apply a settings file from your terminal.' },
  { icon: Bot, title: 'AI coding tools', text: 'Let opencode, Claude Code or Cursor look things up in the Hub (MCP).' },
  { icon: Workflow, title: 'Scripts and CI', text: 'Call the Hub’s API from a script or a pipeline.' },
];

/** Snippets shows how to use a token, with the token filled in. */
function Snippets({ token }: { token: string }) {
  const [tab, setTab] = useState<'cli' | 'claude' | 'curl'>('cli');
  const server = window.location.origin;
  const text = {
    cli: `dth login --server ${server} --token ${token}\ndth ask "How does checkout handle a failed payment?"`,
    claude: `claude mcp add doctherepo -e DTH_SERVER=${server} -e DTH_TOKEN=${token} -- dth mcp`,
    curl: `curl -H "Authorization: Bearer ${token}" ${server}/api/v1/repos`,
  }[tab];
  return (
    <div className="space-y-2">
      <div role="tablist" aria-label="How to use the token" className="flex gap-1 text-xs">
        {([['cli', 'dth CLI'], ['claude', 'Claude Code (MCP)'], ['curl', 'curl']] as const).map(([k, l]) => (
          <button key={k} role="tab" type="button" aria-selected={tab === k} onClick={() => setTab(k)}
            className={cx('rounded-md px-2.5 py-1 font-medium', tab === k ? 'bg-slate-900 text-white dark:bg-white dark:text-slate-900' : 'text-slate-500 hover:bg-slate-100 dark:hover:bg-white/6')}>
            {l}
          </button>
        ))}
      </div>
      <CopyBlock text={text} label="snippet" />
      {tab === 'claude' && <p className="text-xs text-slate-500">opencode and Cursor: see docs/opencode.md for their config files. Install the CLI from the Hub’s releases, or build it with <code>make build</code>.</p>}
    </div>
  );
}

export default function Account() {
  const me = useMe();
  const tokens = useTokens();
  const [name, setName] = useState('');
  const [days, setDays] = useState(90);
  const [secret, setSecret] = useState<{ name: string; token: string }>();
  const create = useInvalidating((b: object) => api.post<{ token: string }>('/tokens', b), keys.tokens);
  const revoke = useInvalidating((id: string) => api.del(`/tokens/${id}`), keys.tokens);
  return (
    <>
      <PageHeader title="Your account" description={`${me.data?.email} · ${me.data?.role}`} />
      <Card title={<span className="flex items-center gap-2"><KeyRound className="h-4 w-4 text-slate-400" aria-hidden />Access tokens</span>} className="max-w-3xl">
        <p className="text-sm text-slate-600 dark:text-slate-400">
          <b className="font-medium text-slate-800 dark:text-slate-200">You don’t need one to use the Hub in this browser.</b> A token lets something outside the browser act as you, with your role and the repositories you can see:
        </p>
        <ul className="mt-3 grid gap-2 sm:grid-cols-3">
          {USES.map((u) => (
            <li key={u.title} className="rounded-lg border border-slate-200 p-3 dark:border-white/10">
              <u.icon className="h-4 w-4 text-brand-500" aria-hidden />
              <p className="mt-1.5 text-sm font-medium">{u.title}</p>
              <p className="mt-0.5 text-xs text-slate-500">{u.text}</p>
            </li>
          ))}
        </ul>
        <form
          className="mt-5 flex flex-wrap items-end gap-3"
          onSubmit={async (e) => {
            e.preventDefault();
            const r = await create.mutateAsync({ name, expires_in_days: days });
            setSecret({ name, token: r.token });
            setName('');
          }}
        >
          <Field label="What is it for?"><Input required value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. laptop CLI" className="w-56" /></Field>
          <Field label="Expires">
            <Select value={days} onChange={(e) => setDays(Number(e.target.value))} className="w-40">
              <option value={30}>In 30 days</option>
              <option value={90}>In 90 days</option>
              <option value={365}>In a year</option>
              <option value={0}>Never</option>
            </Select>
          </Field>
          <Button type="submit" disabled={create.isPending || !name.trim()}>{create.isPending ? 'Creating…' : 'Create token'}</Button>
        </form>
        {secret && (
          <div className="mt-4 space-y-3 rounded-xl border border-amber-300 bg-amber-50/70 p-4 text-sm dark:border-amber-500/30 dark:bg-amber-500/10">
            <p className="font-medium">Token “{secret.name}” created. Copy it now: it is shown only once.</p>
            <CopyBlock text={secret.token} label="token" />
            <p className="text-xs text-slate-600 dark:text-slate-400">Use it like this:</p>
            <Snippets token={secret.token} />
            <Button size="sm" variant="secondary" onClick={() => setSecret(undefined)}>I’ve saved it</Button>
          </div>
        )}
        <ErrorNote error={create.error ?? revoke.error ?? tokens.error} />
        {(tokens.data?.length ?? 0) > 0 && (
          <div className="mt-5">
            <Table head={['Name', 'Created', 'Last used', 'Expires', '']}>
              {tokens.data?.map((t) => (
                <tr key={t.id}>
                  <Td className="font-medium">{t.name}</Td>
                  <Td>{relTime(t.created_at)}</Td>
                  <Td>{t.last_used_at ? relTime(t.last_used_at) : <span className="text-slate-400">never</span>}</Td>
                  <Td>{t.expires_at ? new Date(t.expires_at).toLocaleDateString() : 'never'}</Td>
                  <Td><Button size="sm" variant="ghost" onClick={() => confirm(`Revoke “${t.name}”? Anything using it stops working.`) && revoke.mutate(t.id)}>Revoke</Button></Td>
                </tr>
              ))}
            </Table>
          </div>
        )}
      </Card>
    </>
  );
}
