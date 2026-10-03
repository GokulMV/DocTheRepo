import { useState } from 'react';
import { Link } from 'react-router-dom';
import { api } from '@/api/client';
import type { IssueStatus, Match, RuleTest, Severity } from '@/api/types';
import { Badge, Button, ErrorNote, Field, Input, Select, type Tone } from '@/components/ui';
import { sentence } from '@/lib/labels';

const severityTone: Record<Severity, Tone> = { critical: 'red', error: 'red', warning: 'amber', info: 'gray' };

export function SeverityBadge({ severity }: { severity: Severity }) {
  return <Badge tone={severityTone[severity] ?? 'gray'}>{severity}</Badge>;
}

const statusToneMap: Record<IssueStatus, Tone> = {
  new: 'blue',
  regressed: 'red',
  decoded: 'amber',
  acknowledged: 'gray',
  resolved: 'green',
  suppressed: 'gray',
};

export function IssueStatusBadge({ status }: { status: IssueStatus }) {
  return <Badge tone={statusToneMap[status] ?? 'gray'}>{status}</Badge>;
}

/** Sparkline draws hourly counts (oldest first) as bars; the label carries the numbers for screen readers. */
export function Sparkline({ points, width = 96, height = 24 }: { points: number[]; width?: number; height?: number }) {
  const max = Math.max(1, ...points);
  const bw = width / Math.max(points.length, 1);
  const total = points.reduce((a, b) => a + b, 0);
  return (
    <svg width={width} height={height} role="img" aria-label={`${total} occurrences in the last ${points.length} hours`} className="text-brand-600">
      {points.map((v, i) => {
        const h = v === 0 ? 0 : Math.max(1, (v / max) * height);
        return <rect key={i} x={i * bw} y={height - h} width={Math.max(bw - 1, 1)} height={h} fill="currentColor" opacity={0.8} />;
      })}
    </svg>
  );
}

const list = (s: string) => s.split(',').map((x) => x.trim()).filter(Boolean);

/** MatchEditor edits a known-issue rule's match. All present fields must match (AND). */
export function MatchEditor({ value, onChange }: { value: Match; onChange: (m: Match) => void }) {
  const set = (patch: Partial<Match>) => onChange({ ...value, ...patch });
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <Field label="Fingerprints" hint="Comma-separated; the exact issues this rule covers.">
        <Input value={(value.fingerprints ?? []).join(', ')} onChange={(e) => set({ fingerprints: list(e.target.value) })} />
      </Field>
      <Field label="Message pattern" hint="RE2 regular expression, e.g. timeout .* payments-gw">
        <Input value={value.message_regex ?? ''} onChange={(e) => set({ message_regex: e.target.value })} />
      </Field>
      <Field label="Services"><Input value={(value.services ?? []).join(', ')} onChange={(e) => set({ services: list(e.target.value) })} /></Field>
      <Field label="Environments"><Input value={(value.environments ?? []).join(', ')} onChange={(e) => set({ environments: list(e.target.value) })} /></Field>
      <Field label="Sources" hint="sentry, cloudwatch, alertmanager, …"><Input value={(value.sources ?? []).join(', ')} onChange={(e) => set({ sources: list(e.target.value) })} /></Field>
      <Field label="Highest severity covered">
        <Select value={value.max_severity ?? ''} onChange={(e) => set({ max_severity: e.target.value as Severity | '' })}>
          <option value="">Any</option>
          {(['info', 'warning', 'error', 'critical'] as const).map((s) => <option key={s} value={s}>{sentence(s)}</option>)}
        </Select>
      </Field>
    </div>
  );
}

/** RuleTester dry-runs a match against the last 7 days before anything is saved. */
export function RuleTester({ match }: { match: Match }) {
  const [res, setRes] = useState<RuleTest>();
  const [err, setErr] = useState<unknown>();
  const [busy, setBusy] = useState(false);
  const run = async () => {
    setBusy(true);
    setErr(undefined);
    try {
      setRes(await api.post<RuleTest>('/known-issues/test', { match }));
    } catch (e) {
      setRes(undefined);
      setErr(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="space-y-2 rounded border border-slate-200 p-3 dark:border-slate-800">
      <div className="flex items-center gap-3">
        <Button type="button" size="sm" variant="secondary" onClick={run} disabled={busy}>Test against the last 7 days</Button>
        {res && <span className="text-sm" role="status">Would match <strong>{res.would_match_last_7d}</strong> issue{res.would_match_last_7d === 1 ? '' : 's'}.</span>}
      </div>
      {res && res.sample_issue_ids.length > 0 && (
        <ul className="flex flex-wrap gap-2 text-xs">
          {res.sample_issue_ids.map((id) => <li key={id}><Link className="text-brand-700 hover:underline dark:text-brand-100" to={`/inbox/${id}`}>{id.slice(0, 8)}</Link></li>)}
        </ul>
      )}
      <ErrorNote error={err} />
    </div>
  );
}

export const reasonLabel = (r: string) => sentence(r);
