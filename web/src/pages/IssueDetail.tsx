import { Fragment, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { Bar, BarChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { api } from '@/api/client';
import { useInvalidating, useIssue, useMe } from '@/api/hooks';
import { atLeast, KNOWN_REASONS, type Decode, type IssueDetail as Detail, type KnownIssue, type SignalEvent } from '@/api/types';
import { IssueStatusBadge, SeverityBadge, reasonLabel } from '@/components/signals';
import { Badge, Button, Card, Dialog, ErrorNote, Field, Input, PageHeader, Select, Spinner } from '@/components/ui';
import { num, relTime, shortSha, usd } from '@/lib/format';

function DecodePanel({ d }: { d: Decode }) {
  const gated = d.provider.startsWith('decide:');
  return (
    <div className="space-y-3 text-sm">
      <div className="flex flex-wrap items-center gap-2">
        <Badge tone={d.confidence === 'high' ? 'green' : d.confidence === 'medium' ? 'amber' : 'gray'}>{d.confidence} confidence</Badge>
        {!d.is_actionable && <Badge>not actionable</Badge>}
        {d.suggest_known_issue && <Badge tone="amber">looks like known noise</Badge>}
        {gated && <Badge tone="blue">decided by the decision model</Badge>}
        <span className="text-xs text-slate-500">{d.model || d.provider} · {num(d.tokens)} tokens · {usd(d.cost_usd)} · {relTime(d.created_at)}</span>
      </div>
      <p className="text-base">{d.summary}</p>
      {d.probable_cause && <div><h3 className="font-medium">Probable cause</h3><p>{d.probable_cause}</p></div>}
      {d.impact && <div><h3 className="font-medium">Impact</h3><p>{d.impact}</p></div>}
      {d.affected_code.length > 0 && (
        <div>
          <h3 className="font-medium">Affected code</h3>
          <ul className="mt-1 space-y-1">
            {d.affected_code.map((a) => (
              <li key={a.chunk_id}>
                <code className="text-xs">{a.path}{a.symbol ? ` · ${a.symbol}` : ''}</code>
                <span className="text-slate-600 dark:text-slate-400"> — {a.reason}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      {d.related_commits.length > 0 && (
        <div>
          <h3 className="font-medium">Recent commits to that code</h3>
          <ul className="mt-1 space-y-1">
            {d.related_commits.slice(0, 8).map((c) => (
              <li key={c.sha} className="text-xs">
                {c.url ? <a className="font-mono text-brand-700 hover:underline dark:text-brand-100" href={c.url} target="_blank" rel="noreferrer">{shortSha(c.sha)}</a> : <span className="font-mono">{shortSha(c.sha)}</span>}
                {' '}{c.message.split('\n')[0]} <span className="text-slate-500">· {c.author} · {relTime(c.at)}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      {d.next_steps.length > 0 && (
        <div>
          <h3 className="font-medium">Next steps</h3>
          <ol className="ml-5 list-decimal">{d.next_steps.map((s, i) => <li key={i}>{s}</li>)}</ol>
        </div>
      )}
    </div>
  );
}

function EventRow({ e }: { e: SignalEvent }) {
  const [open, setOpen] = useState(false);
  return (
    <li className="py-2">
      <button type="button" className="flex w-full items-baseline justify-between gap-3 text-left" onClick={() => setOpen(!open)} aria-expanded={open}>
        <span className="truncate"><SeverityBadge severity={e.severity} /> <span className="ml-1">{e.title}</span></span>
        <span className="shrink-0 text-xs text-slate-500">{e.source} · {relTime(e.occurred_at)}</span>
      </button>
      {open && (
        <div className="mt-2 space-y-2 text-xs">
          {e.message && e.message !== e.title && <pre className="max-h-48 overflow-auto whitespace-pre-wrap rounded bg-slate-100 p-2 dark:bg-slate-800">{e.message}</pre>}
          {!!e.stack?.length && (
            <pre className="max-h-48 overflow-auto rounded bg-slate-100 p-2 dark:bg-slate-800">
              {e.stack.map((f) => `${f.in_app ? '→ ' : '  '}${f.module ?? ''} ${f.function ?? ''} (${f.file ?? '?'}:${f.line ?? 0})`).join('\n')}
            </pre>
          )}
          {e.attrs && Object.keys(e.attrs).length > 0 && (
            <dl className="grid grid-cols-[12rem_1fr] gap-x-2">
              {Object.entries(e.attrs).sort().map(([k, v]) => <Fragment key={k}><dt className="truncate text-slate-500">{k}</dt><dd className="break-all">{v}</dd></Fragment>)}
            </dl>
          )}
        </div>
      )}
    </li>
  );
}

function MarkKnownDialog({ issue, onDone }: { issue: Detail; onDone: () => void }) {
  const [title, setTitle] = useState(issue.title);
  const [reason, setReason] = useState(issue.decode?.suggest_known_issue ? 'expected_noise' : 'known_bug');
  const [days, setDays] = useState('');
  const mark = useInvalidating(
    () => api.post<KnownIssue>(`/issues/${issue.id}/mark-known`, { title, reason, expires_at: days ? new Date(Date.now() + Number(days) * 86400_000).toISOString() : undefined }),
    ['issue', issue.id], ['issues'], ['known-issues'],
  );
  return (
    <Dialog open onOpenChange={(o) => !o && onDone()} title="Mark as known" description={`Creates a rule for this fingerprint${issue.service !== 'unknown' ? ` in ${issue.service}` : ''}${issue.environment ? ` (${issue.environment})` : ''}.`}>
      <form className="space-y-3" onSubmit={async (e) => { e.preventDefault(); await mark.mutateAsync(undefined); onDone(); }}>
        <Field label="Title"><Input value={title} onChange={(e) => setTitle(e.target.value)} required /></Field>
        <Field label="Reason">
          <Select value={reason} onChange={(e) => setReason(e.target.value)}>
            {KNOWN_REASONS.map((r) => <option key={r} value={r}>{reasonLabel(r)}</option>)}
          </Select>
        </Field>
        <Field label="Mute for (days)" hint="Empty: until someone disables the rule."><Input type="number" min={1} value={days} onChange={(e) => setDays(e.target.value)} /></Field>
        <ErrorNote error={mark.error} />
        <Button type="submit" disabled={mark.isPending}>Save rule</Button>
      </form>
    </Dialog>
  );
}

export default function IssueDetail() {
  const { id } = useParams();
  const me = useMe();
  const issue = useIssue(id);
  const [marking, setMarking] = useState(false);
  const setStatus = useInvalidating((status: string) => api.patch(`/issues/${id}`, { status }), ['issue', id], ['issues']);
  const decode = useInvalidating(() => api.post<{ job_id: string }>(`/issues/${id}/decode`, { force: true }), ['issue', id]);
  if (issue.isLoading) return <Spinner />;
  if (issue.error || !issue.data) return <ErrorNote error={issue.error ?? new Error('not found')} />;
  const i = issue.data;
  const canEdit = atLeast(me.data?.role, 'editor');
  return (
    <>
      <PageHeader
        title={i.title}
        description={<span className="flex flex-wrap items-center gap-2"><SeverityBadge severity={i.severity} /><IssueStatusBadge status={i.status} /> {i.kind.replace('_', ' ')} · {i.service}{i.environment && ` · ${i.environment}`} · {i.sources.join(', ')} · first seen {relTime(i.first_seen)}, last {relTime(i.last_seen)}</span>}
        actions={canEdit && (
          <div className="flex flex-wrap gap-2">
            {i.status !== 'acknowledged' && i.status !== 'resolved' && <Button variant="secondary" onClick={() => setStatus.mutate('acknowledged')}>Acknowledge</Button>}
            {i.status !== 'resolved' && <Button variant="secondary" onClick={() => setStatus.mutate('resolved')}>Resolve</Button>}
            {(i.status === 'resolved' || i.status === 'acknowledged') && <Button variant="secondary" onClick={() => setStatus.mutate('new')}>Reopen</Button>}
            <Button variant="secondary" onClick={() => decode.mutate(undefined)} disabled={decode.isPending}>Explain again</Button>
            {!i.known_issue_id && <Button onClick={() => setMarking(true)}>Mark as known</Button>}
          </div>
        )}
      />
      <ErrorNote error={setStatus.error ?? decode.error} />
      {decode.data && <p className="mb-3 text-sm text-slate-600" role="status">Queued a fresh explanation (job {decode.data.job_id.slice(0, 8)}). It appears here when done.</p>}
      <div className="grid gap-6 xl:grid-cols-3">
        <div className="space-y-6 xl:col-span-2">
          <Card title="Explanation">
            {i.decode ? <DecodePanel d={i.decode} /> : <p className="text-sm text-slate-500">Not explained yet. New issues are explained automatically when a decode model is configured.</p>}
          </Card>
          <Card title={`Recent events (${i.events.length})`}>
            <ul className="divide-y divide-slate-100 text-sm dark:divide-slate-800">{i.events.map((e) => <EventRow key={e.external_id + e.occurred_at} e={e} />)}</ul>
            {i.events.length === 0 && <p className="text-sm text-slate-500">No samples kept yet.</p>}
          </Card>
        </div>
        <div className="space-y-6">
          <Card title="Occurrences">
            <p className="text-sm">{num(i.occurrences)} events{i.suppressed_count > 0 && `, ${num(i.suppressed_count)} more matched a known-issue rule`}.</p>
            {i.hourly.length > 0 && (
              <div className="mt-2 h-40">
                <ResponsiveContainer width="100%" height="100%">
                  <BarChart data={i.hourly.map((p) => ({ ...p, hour: new Date(p.t).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit' }) }))}>
                    <XAxis dataKey="hour" hide />
                    <YAxis width={32} allowDecimals={false} />
                    <Tooltip />
                    <Bar dataKey="count" name="events" fill="#2563eb" />
                    <Bar dataKey="suppressed" name="known" fill="#94a3b8" />
                  </BarChart>
                </ResponsiveContainer>
              </div>
            )}
          </Card>
          {i.known_issue && (
            <Card title="Known issue">
              <p className="text-sm font-medium">{i.known_issue.title}</p>
              <p className="text-xs text-slate-500">{reasonLabel(i.known_issue.reason)} · {i.known_issue.enabled ? 'active' : 'disabled'} · {num(i.known_issue.hits)} hits</p>
              <Link className="mt-1 inline-block text-sm text-brand-700 hover:underline dark:text-brand-100" to="/known-issues">Manage rules</Link>
            </Card>
          )}
          {i.similar_issues.length > 0 && (
            <Card title="Similar issues">
              <ul className="space-y-1 text-sm">
                {i.similar_issues.map((s) => <li key={s.id}><Link className="hover:underline" to={`/inbox/${s.id}`}>{s.title}</Link> <IssueStatusBadge status={s.status} /></li>)}
              </ul>
            </Card>
          )}
          <Card title="Fingerprint"><code className="break-all text-xs">{i.fingerprint}</code></Card>
        </div>
      </div>
      {marking && <MarkKnownDialog issue={i} onDone={() => setMarking(false)} />}
    </>
  );
}
