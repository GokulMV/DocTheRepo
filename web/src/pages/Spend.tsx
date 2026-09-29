import { useEffect, useState } from 'react';
import { api } from '@/api/client';
import { keys, useInvalidating, useLimits } from '@/api/hooks';
import type { SpendLimit } from '@/api/types';
import { Button, Card, ErrorNote, Input, PageHeader, Select, Spinner, Table, Td } from '@/components/ui';

const blank: SpendLimit = { scope: 'feature', scope_key: 'qa', window: 'day', max_tokens: 500000, max_cost_usd: null, on_breach: 'block' };

export default function Spend() {
  const limits = useLimits();
  const [rows, setRows] = useState<SpendLimit[]>([]);
  useEffect(() => setRows(limits.data ?? []), [limits.data]);
  const save = useInvalidating((items: SpendLimit[]) => api.put('/spend/limits', { items }), keys.limits);
  const set = (i: number, patch: Partial<SpendLimit>) => setRows((rs) => rs.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  const numOrNull = (v: string) => (v.trim() === '' ? null : Number(v));
  return (
    <>
      <PageHeader
        title="Spend limits"
        description="Hard ceilings checked before every paid call, on your own provider accounts. A breach blocks the call (and can alert); nothing is charged beyond a ceiling unless an admin retries with an audited override."
      />
      {limits.isLoading && <Spinner />}
      <ErrorNote error={limits.error ?? save.error} />
      <Card
        actions={
          <>
            <Button variant="secondary" size="sm" onClick={() => setRows((r) => [...r, { ...blank }])}>Add limit</Button>
            <Button size="sm" disabled={save.isPending} onClick={() => save.mutate(rows)}>Save all</Button>
          </>
        }
      >
        <Table head={['Scope', 'Key', 'Window', 'Max tokens', 'Max cost (USD)', 'On breach', '']}>
          {rows.map((r, i) => (
            <tr key={i}>
              <Td>
                <Select aria-label="Scope" value={r.scope} onChange={(e) => set(i, { scope: e.target.value as SpendLimit['scope'] })}>
                  {['global', 'feature', 'provider', 'repo'].map((s) => <option key={s}>{s}</option>)}
                </Select>
              </Td>
              <Td><Input aria-label="Scope key" disabled={r.scope === 'global'} value={r.scope === 'global' ? '' : r.scope_key} placeholder={r.scope === 'feature' ? 'qa, docgen…' : 'id'} onChange={(e) => set(i, { scope_key: e.target.value })} /></Td>
              <Td>
                <Select aria-label="Window" value={r.window} onChange={(e) => set(i, { window: e.target.value as SpendLimit['window'] })}>
                  <option value="day">per day</option>
                  <option value="month">per month</option>
                </Select>
              </Td>
              <Td><Input aria-label="Max tokens" type="number" min={0} value={r.max_tokens ?? ''} onChange={(e) => set(i, { max_tokens: numOrNull(e.target.value) })} /></Td>
              <Td><Input aria-label="Max cost" type="number" min={0} step="0.01" value={r.max_cost_usd ?? ''} onChange={(e) => set(i, { max_cost_usd: numOrNull(e.target.value) })} /></Td>
              <Td>
                <Select aria-label="On breach" value={r.on_breach} onChange={(e) => set(i, { on_breach: e.target.value as SpendLimit['on_breach'] })}>
                  <option value="block">block</option>
                  <option value="block_and_alert">block and alert</option>
                </Select>
                {r.on_breach === 'block_and_alert' && <Input aria-label="Alert URL" className="mt-1" placeholder="https://hooks.slack.com/…" value={r.alert_url ?? ''} onChange={(e) => set(i, { alert_url: e.target.value })} />}
              </Td>
              <Td><Button size="sm" variant="ghost" onClick={() => setRows((rs) => rs.filter((_, j) => j !== i))}>Remove</Button></Td>
            </tr>
          ))}
        </Table>
        {rows.every((r) => r.scope !== 'global') && <p className="mt-3 text-sm text-amber-700">Tip: keep a global daily limit as a backstop.</p>}
      </Card>
    </>
  );
}
