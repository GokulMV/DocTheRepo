import { useMemo, useState } from 'react';
import { Bar, BarChart, CartesianGrid, Legend, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { usePipelineStats, useSavings, useUsage } from '@/api/hooks';
import { Card, ErrorNote, PageHeader, Select, Spinner, Table, Td } from '@/components/ui';
import { daysAgoISO, num, relTime, shortSha, usd } from '@/lib/format';
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
};

export default function Analytics() {
  const [days, setDays] = useState(7);
  const [groupBy, setGroupBy] = useState('feature');
  const [metric, setMetric] = useState<'tokens' | 'cost_usd' | 'calls'>('tokens');
  const from = useMemo(() => daysAgoISO(days), [days]);
  const gran = days <= 2 ? 'hour' : 'day';
  const usage = useUsage(groupBy, gran, from);
  const savings = useSavings(from);
  const pipe = usePipelineStats();

  // Feature series read as their names ("docgen" → "Docs generation").
  const seriesLabel = (k: string) => (!k ? '—' : groupBy === 'feature' ? featureLabel(k) : capFirst(k));
  const chart = useMemo(() => {
    const rows = new Map<string, Record<string, number | string>>();
    for (const s of usage.data?.series ?? []) {
      for (const p of s.points) {
        const t = gran === 'hour' ? p.t.slice(5, 16).replace('T', ' ') : p.t.slice(0, 10);
        const row = rows.get(t) ?? { t };
        row[seriesLabel(s.key)] = p[metric];
        rows.set(t, row);
      }
    }
    return [...rows.values()];
  }, [usage.data, metric, gran]);
  const seriesKeys = usage.data?.series.map((s) => seriesLabel(s.key)) ?? [];
  const t = usage.data?.totals;

  return (
    <>
      <PageHeader
        title="Analytics"
        description="Where your LLM usage goes, what the Hub avoided spending, and how fresh each repository's docs are."
        actions={
          <Select aria-label="Period" value={days} onChange={(e) => setDays(Number(e.target.value))} className="w-36">
            <option value={1}>Last 24 hours</option>
            <option value={7}>Last 7 days</option>
            <option value={30}>Last 30 days</option>
            <option value={90}>Last 90 days</option>
          </Select>
        }
      />
      <ErrorNote error={usage.error ?? savings.error ?? pipe.error} />
      <div className="mb-6 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Stat label="Calls" value={num(t?.calls)} hint={t ? `${num(t.cached_calls)} cached, ${num(t.blocked)} blocked` : undefined} />
        <Stat label="Tokens" value={num(t?.tokens)} />
        <Stat label="Cost" value={usd(t?.cost_usd)} hint="At your cost table prices" />
        <Stat label="Saved" value={usd(savings.data?.total.cost_avoided_usd)} hint={savings.data ? `${num(savings.data.total.tokens_avoided)} tokens avoided` : undefined} />
      </div>
      <Card
        title="Usage"
        className="mb-6"
        actions={
          <>
            <Select aria-label="Group by" value={groupBy} onChange={(e) => setGroupBy(e.target.value)} className="w-36">
              {['feature', 'provider', 'model', 'repo', 'user'].map((g) => <option key={g} value={g}>By {g === 'repo' ? 'repository' : g}</option>)}
            </Select>
            <Select aria-label="Metric" value={metric} onChange={(e) => setMetric(e.target.value as typeof metric)} className="w-32">
              <option value="tokens">Tokens</option>
              <option value="cost_usd">Cost</option>
              <option value="calls">Calls</option>
            </Select>
          </>
        }
      >
        {usage.isLoading ? (
          <Spinner />
        ) : chart.length === 0 ? (
          <p className="text-sm text-slate-500">No usage in this period.</p>
        ) : (
          <div className="h-72">
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={chart}>
                <CartesianGrid strokeDasharray="3 3" stroke="#e2e8f0" />
                <XAxis dataKey="t" fontSize={11} />
                <YAxis fontSize={11} />
                <Tooltip />
                <Legend />
                {seriesKeys.map((k, i) => <Bar key={k} dataKey={k} stackId="a" fill={COLORS[i % COLORS.length]} />)}
              </BarChart>
            </ResponsiveContainer>
          </div>
        )}
      </Card>
      <div className="grid gap-6 lg:grid-cols-2">
        <Card title="Savings">
          <Table head={['How', 'Events', 'Tokens avoided', 'Cost avoided']}>
            {savings.data?.by_kind.map((k) => (
              <tr key={k.kind}>
                <Td>{SAVINGS_LABEL[k.kind] ?? k.kind.replaceAll('_', ' ')}</Td>
                <Td>{num(k.events)}</Td>
                <Td>{num(k.tokens_avoided)}</Td>
                <Td>{usd(k.cost_avoided_usd)}</Td>
              </tr>
            ))}
          </Table>
          {savings.data?.by_kind.length === 0 && <p className="text-sm text-slate-500">No savings recorded yet.</p>}
        </Card>
        <Card title="Pipeline">
          {pipe.data && (
            <>
              <p className="mb-2 text-sm">Cosmetic pushes skipped by triage: <strong>{Math.round(pipe.data.triage_abort_rate * 100)}%</strong></p>
              <div className="h-40">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={pipe.data.jobs.filter((j) => j.status === 'done')}>
                    <XAxis dataKey="type" fontSize={11} />
                    <YAxis fontSize={11} unit="s" />
                    <Tooltip />
                    <Line dataKey="p50_seconds" stroke="#2f6fed" name="p50" />
                    <Line dataKey="p95_seconds" stroke="#ea580c" name="p95" />
                  </LineChart>
                </ResponsiveContainer>
              </div>
              <Table head={['Repository', 'Processed', 'Last success']}>
                {pipe.data.freshness.map((f) => (
                  <tr key={f.repo_id}>
                    <Td>{f.repo}</Td>
                    <Td className="font-mono text-xs">{shortSha(f.last_processed_sha)}</Td>
                    <Td>{relTime(f.last_success)}</Td>
                  </tr>
                ))}
              </Table>
            </>
          )}
        </Card>
      </div>
    </>
  );
}
