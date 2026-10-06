import { useMemo, useState } from 'react';
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { useLimits, useSavings, useSift, useUsage, type SiftReport } from '@/api/hooks';
import { Link } from 'react-router-dom';
import { Card, ErrorNote, PageHeader, Select, Spinner, Table, Td } from '@/components/ui';
import { daysAgoISO, num, usd } from '@/lib/format';
import { capFirst, featureLabel } from '@/lib/labels';

const COLORS = ['#2f6fed', '#0f766e', '#ea580c', '#7c3aed', '#db2777', '#ca8a04', '#64748b'];

function Stat({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
      <p className="text-xs uppercase text-slate-500">{label}</p>
      <p className="mt-1 text-2xl font-semibold">{value}</p>
      {hint && <p className="text-xs text-slate-500">{hint}</p>}
    </div>
  );
}

const SAVINGS_LABEL: Record<string, string> = {
  triage_abort: 'Cosmetic pushes skipped',
  answer_cache_hit: 'Answers served from cache',
  rename_rekey: 'Renames re-keyed without re-embedding',
  known_issue_suppressed: 'Known issues not decoded',
  decode_reused: 'Explanations reused (code unchanged)',
  decision_gate: 'Noise skipped by the decision gate',
  evidence_sifted: 'Ask sources trimmed before answering',
};

/** SiftCard shows what Ask's source picker trimmed and saved in the period. */
export function SiftCard({ data, loading }: { data?: SiftReport; loading: boolean }) {
  if (loading) return <Card title="Ask source picking" className="mb-6"><Spinner /></Card>;
  if (!data || data.picked === 0) {
    return (
      <Card title="Ask source picking" className="mb-6">
        <p className="text-sm text-slate-500">
          No answers used the source picker in this period. Route <b>Ask source picking</b> to TypeSafe Jev or a small, cheap model under{' '}
          <Link className="underline" to="/providers">Settings → AI models</Link> and Ask sends its answering model only the sources it needs.
        </p>
      </Card>
    );
  }
  const share = data.candidates > 0 ? Math.round((data.kept / data.candidates) * 100) : 0;
  const days = data.daily.map((d) => ({ t: d.day.slice(0, 10), 'Tokens not sent': d.tokens_saved }));
  return (
    <Card title="Ask source picking" className="mb-6">
      <p className="mb-3 text-sm text-slate-500">
        A cheap judge picks which retrieved sources each answer needs, so the answering model reads less. It ran on {num(data.picked)} of {num(data.answers)} answers and trimmed {num(data.trimmed)}.
      </p>
      <div className="mb-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Stat label="Tokens not sent" value={num(data.tokens_saved)} hint="Answering-model input avoided" />
        <Stat label="Net saved" value={data.saved_usd >= 0 ? usd(data.saved_usd) : `-${usd(-data.saved_usd)}`} hint="After paying the judge" />
        <Stat label="Sources read" value={`${share}%`} hint={`${num(data.kept)} of ${num(data.candidates)} retrieved`} />
        <Stat label="Judge cost" value={usd(data.judge_cost_usd)} hint={`${num(data.judge_tokens)} tokens${data.explored ? `, ${num(data.explored)} files explored` : ''}`} />
      </div>
      {days.length > 1 && (
        <div className="h-48">
          <ResponsiveContainer width="100%" height="100%">
            <BarChart data={days}>
              <CartesianGrid strokeDasharray="3 3" stroke="#e2e8f0" />
              <XAxis dataKey="t" fontSize={11} />
              <YAxis fontSize={11} />
              <Tooltip />
              <Bar dataKey="Tokens not sent" fill={COLORS[1]} />
            </BarChart>
          </ResponsiveContainer>
        </div>
      )}
    </Card>
  );
}

const isMonthBudget = (l: { scope: string; window: string; max_cost_usd?: number | null }) => l.scope === 'global' && l.window === 'month' && l.max_cost_usd != null;

export default function Analytics() {
  const [days, setDays] = useState(30);
  const from = useMemo(() => daysAgoISO(days), [days]);
  const monthFrom = useMemo(() => {
    const n = new Date();
    return new Date(Date.UTC(n.getUTCFullYear(), n.getUTCMonth(), 1)).toISOString().replace(/\.\d{3}Z$/, 'Z');
  }, []);
  const usage = useUsage('feature', 'day', from);
  const month = useUsage('feature', 'day', monthFrom);
  const savings = useSavings(from);
  const sift = useSift(from);
  const limits = useLimits();

  // One bar per day: what all features cost together.
  const perDay = useMemo(() => {
    const byDay = new Map<string, number>();
    for (const s of usage.data?.series ?? []) for (const p of s.points) byDay.set(p.t.slice(0, 10), (byDay.get(p.t.slice(0, 10)) ?? 0) + p.cost_usd);
    return [...byDay.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([t, cost]) => ({ t, Cost: Math.round(cost * 100) / 100 }));
  }, [usage.data]);
  const byFeature = useMemo(() => {
    const total = usage.data?.totals.cost_usd ?? 0;
    return (usage.data?.series ?? [])
      .map((s) => ({ key: s.key, cost: s.points.reduce((n, p) => n + p.cost_usd, 0), tokens: s.points.reduce((n, p) => n + p.tokens, 0) }))
      .filter((r) => r.cost > 0 || r.tokens > 0)
      .sort((a, b) => b.cost - a.cost || b.tokens - a.tokens)
      .map((r) => ({ ...r, share: total > 0 ? Math.round((r.cost / total) * 100) : 0 }));
  }, [usage.data]);

  const t = usage.data?.totals;
  const budget = limits.data?.find(isMonthBudget)?.max_cost_usd ?? null;
  const spentMonth = month.data?.totals.cost_usd ?? 0;
  const period = days === 1 ? 'last 24 hours' : `last ${days} days`;
  return (
    <>
      <PageHeader
        title="Usage"
        description="What the AI models cost, what the Hub avoided spending, and how much of your budget is left."
        actions={
          <Select aria-label="Period" value={days} onChange={(e) => setDays(Number(e.target.value))} className="w-36">
            <option value={1}>Last 24 hours</option>
            <option value={7}>Last 7 days</option>
            <option value={30}>Last 30 days</option>
            <option value={90}>Last 90 days</option>
          </Select>
        }
      />
      <ErrorNote error={usage.error ?? savings.error ?? sift.error} />
      <div className="mb-6 grid gap-4 sm:grid-cols-3">
        <Stat label="Spent" value={usd(t?.cost_usd)} hint={t ? `${num(t.tokens)} tokens, ${period}` : undefined} />
        <Stat label="Saved" value={usd(savings.data?.total.cost_avoided_usd)} hint={savings.data ? `${num(savings.data.total.tokens_avoided)} tokens not spent` : undefined} />
        {budget ? (
          <Stat label="Budget left this month" value={usd(Math.max(0, budget - spentMonth))} hint={`${usd(spentMonth)} of ${usd(budget)} used`} />
        ) : (
          <Stat label="Monthly budget" value="None" hint="Set one under Settings → AI models" />
        )}
      </div>
      <Card title="Cost per day" className="mb-6">
        {usage.isLoading ? (
          <Spinner />
        ) : perDay.length === 0 ? (
          <p className="text-sm text-slate-500">No usage in this period.</p>
        ) : (
          <div className="h-64">
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={perDay}>
                <CartesianGrid strokeDasharray="3 3" stroke="#e2e8f0" vertical={false} />
                <XAxis dataKey="t" fontSize={11} />
                <YAxis fontSize={11} tickFormatter={(v: number) => `$${v}`} />
                <Tooltip formatter={(v: number) => usd(v)} />
                <Bar dataKey="Cost" fill={COLORS[0]} radius={[3, 3, 0, 0]} />
              </BarChart>
            </ResponsiveContainer>
          </div>
        )}
      </Card>
      <div className="mb-6 grid gap-6 lg:grid-cols-2">
        <Card title="Where it went">
          {byFeature.length === 0 ? <p className="text-sm text-slate-500">Nothing yet.</p> : (
            <Table head={['Feature', 'Cost', 'Share']}>
              {byFeature.map((r) => (
                <tr key={r.key}>
                  <Td>{featureLabel(r.key)}</Td>
                  <Td>{usd(r.cost)}</Td>
                  <Td>{r.share}%</Td>
                </tr>
              ))}
            </Table>
          )}
        </Card>
        <Card title="Saved by">
          {savings.data?.by_kind.length ? (
            <Table head={['How', 'Times', 'Saved']}>
              {savings.data.by_kind.map((k) => (
                <tr key={k.kind}>
                  <Td>{SAVINGS_LABEL[k.kind] ?? capFirst(k.kind.replaceAll('_', ' '))}</Td>
                  <Td>{num(k.events)}</Td>
                  <Td>{usd(k.cost_avoided_usd)}</Td>
                </tr>
              ))}
            </Table>
          ) : <p className="text-sm text-slate-500">No savings recorded yet.</p>}
        </Card>
      </div>
      <SiftCard data={sift.data} loading={sift.isLoading} />
    </>
  );
}
