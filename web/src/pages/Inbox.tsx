import { Inbox as InboxIcon } from 'lucide-react';
import { useState } from 'react';
import { Link } from 'react-router-dom';
import { api } from '@/api/client';
import { useInvalidating, useIssues, useMe, type IssueFilters } from '@/api/hooks';
import { atLeast, KNOWN_REASONS, type Issue, type KnownIssue } from '@/api/types';
import { IssueStatusBadge, RuleTester, SeverityBadge, Sparkline, reasonLabel } from '@/components/signals';
import { Button, Card, Dialog, DialogFooter, Empty, ErrorNote, Field, Input, PageHeader, Select, Spinner, Table, Td } from '@/components/ui';
import { num, relTime } from '@/lib/format';
import { SOURCES } from './signalSources';
import { sentence } from '@/lib/labels';

const STATUSES = ['', 'new', 'decoded', 'regressed', 'acknowledged', 'resolved', 'suppressed'];
const KINDS: [string, string][] = [['', 'Any kind'], ['error', 'Errors'], ['alert', 'Alerts'], ['security_finding', 'Security findings'], ['log_match', 'Log matches'], ['event_bus', 'Event bus']];

/** MarkKnown creates one rule covering the selected issues' fingerprints (applies to new events). */
function MarkKnown({ issues, onDone }: { issues: Issue[]; onDone: () => void }) {
  const fps = issues.map((i) => i.fingerprint);
  const services = [...new Set(issues.map((i) => i.service).filter((s) => s && s !== 'unknown'))];
  const [title, setTitle] = useState(issues.length === 1 ? issues[0].title : `${issues.length} known issues`);
  const [reason, setReason] = useState('known_bug');
  const [days, setDays] = useState('');
  const match = { fingerprints: fps, services: services.length === 1 ? services : undefined };
  const create = useInvalidating(
    () =>
      issues.length === 1
        ? api.post<KnownIssue>(`/issues/${issues[0].id}/mark-known`, { title, reason, expires_at: days ? new Date(Date.now() + Number(days) * 86400_000).toISOString() : undefined })
        : api.post<KnownIssue>('/known-issues', { title, reason, match, expires_at: days ? new Date(Date.now() + Number(days) * 86400_000).toISOString() : undefined }),
    ['issues'],
    ['known-issues'],
  );
  return (
    <Dialog open onOpenChange={(o) => !o && onDone()} title="Mark as known" description="Matching events are counted but not decoded or shown in the Inbox. Nothing else is hidden.">
      <form className="space-y-3" onSubmit={async (e) => { e.preventDefault(); await create.mutateAsync(undefined); onDone(); }}>
        <Field label="Title"><Input value={title} onChange={(e) => setTitle(e.target.value)} required /></Field>
        <Field label="Reason">
          <Select value={reason} onChange={(e) => setReason(e.target.value)}>
            {KNOWN_REASONS.map((r) => <option key={r} value={r}>{reasonLabel(r)}</option>)}
          </Select>
        </Field>
        <Field label="Mute for (days)" hint="Empty: until someone disables the rule."><Input type="number" min={1} value={days} onChange={(e) => setDays(e.target.value)} /></Field>
        <p className="text-xs text-slate-500">Covers {fps.length} fingerprint{fps.length === 1 ? '' : 's'}{match.services ? ` in ${match.services[0]}` : ''}.</p>
        <RuleTester match={match} />
        <ErrorNote error={create.error} />
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={() => onDone()}>Cancel</Button>
          <Button type="submit" disabled={create.isPending}>Save rule</Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
}

export default function Inbox() {
  const me = useMe();
  const [f, setF] = useState<IssueFilters>({ since: '7d' });
  const [selected, setSelected] = useState<Record<string, Issue>>({});
  const [marking, setMarking] = useState<Issue[]>();
  const issues = useIssues(f);
  const set = (patch: IssueFilters) => setF((cur) => ({ ...cur, ...patch }));
  const canEdit = atLeast(me.data?.role, 'editor');
  const chosen = Object.values(selected);
  return (
    <>
      <PageHeader
        title="Inbox"
        description="Errors, alerts, findings, and event-bus problems, grouped and explained. Known issues stay out of the way."
        actions={canEdit && chosen.length > 0 && <Button onClick={() => setMarking(chosen)}>Mark {chosen.length} as known</Button>}
      />
      <Card>
        <div className="mb-3 flex flex-wrap gap-2">
          <Input aria-label="Search" placeholder="Search titles and explanations" className="w-64" value={f.q ?? ''} onChange={(e) => set({ q: e.target.value })} />
          <Select aria-label="Status" className="w-44" value={f.status ?? ''} onChange={(e) => set({ status: e.target.value })}>
            {STATUSES.map((s) => <option key={s} value={s}>{s ? sentence(s) : 'Open (not known)'}</option>)}
          </Select>
          <Select aria-label="Severity" className="w-36" value={f.severity ?? ''} onChange={(e) => set({ severity: e.target.value })}>
            <option value="">Any severity</option>
            <option value="warning">Warning and up</option>
            <option value="error">Error and up</option>
            <option value="critical">Critical</option>
          </Select>
          <Select aria-label="Source" className="w-44" value={f.source ?? ''} onChange={(e) => set({ source: e.target.value })}>
            <option value="">Any source</option>
            {SOURCES.map((s) => <option key={s.type} value={s.type}>{s.label}</option>)}
          </Select>
          <Select aria-label="Kind" className="w-40" value={f.kind ?? ''} onChange={(e) => set({ kind: e.target.value })}>
            {KINDS.map(([k, label]) => <option key={k} value={k}>{label}</option>)}
          </Select>
          <Input aria-label="Service" placeholder="Service" className="w-36" value={f.service ?? ''} onChange={(e) => set({ service: e.target.value })} />
          <Input aria-label="Environment" placeholder="Environment" className="w-32" value={f.env ?? ''} onChange={(e) => set({ env: e.target.value })} />
          <Select aria-label="Seen within" className="w-32" value={f.since ?? ''} onChange={(e) => set({ since: e.target.value })}>
            <option value="24h">24 hours</option>
            <option value="7d">7 days</option>
            <option value="30d">30 days</option>
            <option value="">Any time</option>
          </Select>
        </div>
        {issues.isLoading && <Spinner />}
        <ErrorNote error={issues.error} />
        {issues.data?.items.length === 0 && (
          <Empty icon={InboxIcon} title="Nothing here" action={atLeast(me.data?.role, 'admin') && <Link to="/connectors" className="text-sm font-medium text-brand-600 dark:text-brand-300">Add a signal source (Sentry, Datadog, PagerDuty…) →</Link>}>
            No issues match. Errors and alerts from your connected tools appear here within seconds, grouped and explained with the code behind them.
          </Empty>
        )}
        {!!issues.data?.items.length && (
          <Table head={[canEdit ? '' : null, 'Issue', 'Service', 'Status', 'Events', 'Last 24h', 'Last seen'].filter((h) => h !== null)}>
            {issues.data.items.map((i) => (
              <tr key={i.id}>
                {canEdit && (
                  <Td>
                    <input type="checkbox" aria-label={`Select ${i.title}`} checked={!!selected[i.id]}
                      onChange={(e) => setSelected((s) => { const n = { ...s }; if (e.target.checked) n[i.id] = i; else delete n[i.id]; return n; })} />
                  </Td>
                )}
                <Td className="max-w-xl">
                  <div className="flex items-center gap-2">
                    <SeverityBadge severity={i.severity} />
                    <Link to={`/inbox/${i.id}`} className="font-medium hover:underline">{i.title}</Link>
                  </div>
                  {i.decode_summary && <p className="mt-0.5 truncate text-xs text-slate-500" title={i.decode_summary}>{i.decode_summary}</p>}
                </Td>
                <Td>{i.service}{i.environment && <span className="text-xs text-slate-500"> · {i.environment}</span>}</Td>
                <Td><IssueStatusBadge status={i.status} /></Td>
                <Td>{num(i.occurrences)}{i.suppressed_count > 0 && <span className="text-xs text-slate-500"> (+{num(i.suppressed_count)} known)</span>}</Td>
                <Td><Sparkline points={i.sparkline} /></Td>
                <Td>{relTime(i.last_seen)}</Td>
              </tr>
            ))}
          </Table>
        )}
      </Card>
      {marking && <MarkKnown issues={marking} onDone={() => { setMarking(undefined); setSelected({}); }} />}
    </>
  );
}
