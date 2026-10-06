import { CheckCircle2 } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { api } from '@/api/client';
import { keys, useInvalidating, useLimits, useUsage } from '@/api/hooks';
import type { Provider, Route, SpendLimit } from '@/api/types';
import { Button, Card, ErrorNote, Field, Input, Select } from '@/components/ui';
import { capFirst } from '@/lib/labels';
import { usd } from '@/lib/format';
import { providerKind } from './providerKinds';

/** Which features each simple choice drives. The rest (decide) keep whatever they had. */
export const MAIN_FEATURES = ['docgen', 'qa', 'decode', 'suggest', 'security'];
export const FAST_FEATURES = ['docgen_fast', 'triage', 'sift'];

interface Choice { providerId: string; model: string }

const pick = (routes: Route[], features: string[]): Choice => {
  const r = features.map((f) => routes.find((x) => x.feature === f)).find(Boolean);
  return { providerId: r?.provider_id ?? '', model: r?.model ?? '' };
};

/** ModelPicker is a provider + model pair. */
function ModelPicker({ label, hint, value, onChange, providers, kinds }: {
  label: string; hint: string; value: Choice; onChange: (c: Choice) => void; providers: Provider[]; kinds?: (p: Provider) => boolean;
}) {
  const list = providers.filter((p) => !kinds || kinds(p));
  const placeholder = providerKind(providers.find((p) => p.id === value.providerId)?.kind ?? '')?.defaultModel ?? 'Model name';
  return (
    <Field label={label} hint={hint}>
      <div className="flex flex-wrap gap-2">
        <Select className="w-44" aria-label={`${label}: provider`} value={value.providerId} onChange={(e) => onChange({ ...value, providerId: e.target.value })}>
          <option value="">Not set</option>
          {list.map((p) => <option key={p.id} value={p.id}>{capFirst(p.name)}</option>)}
        </Select>
        <Input className="min-w-0 flex-1" aria-label={`${label}: model`} value={value.model} placeholder={placeholder} onChange={(e) => onChange({ ...value, model: e.target.value })} />
      </div>
    </Field>
  );
}

/** SimpleModels sets every feature from three choices: a main model, a fast model and the search model. */
export function SimpleModels({ providers, routes }: { providers: Provider[]; routes: Route[] }) {
  const initial = useMemo(() => ({ main: pick(routes, MAIN_FEATURES), fast: pick(routes, FAST_FEATURES), embed: pick(routes, ['embedding']) }), [routes]);
  const [v, setV] = useState(initial);
  const [done, setDone] = useState<string>();
  useEffect(() => {
    setV(initial);
  }, [initial]);
  const dirty = JSON.stringify(v) !== JSON.stringify(initial);
  const save = useInvalidating(async () => {
    let docs = 0;
    const put = async (feature: string, c: Choice) => {
      if (!c.providerId || !c.model.trim()) return;
      const cur = routes.find((r) => r.feature === feature);
      if (cur && cur.provider_id === c.providerId && cur.model === c.model.trim()) return;
      const r = await api.put<{ docs_queued?: number }>(`/routes/${feature}`, {
        provider_id: c.providerId, model: c.model.trim(), effort: cur?.effort ?? '', fallback_provider_id: cur?.fallback_provider_id ?? '', fallback_model: cur?.fallback_model ?? '',
      });
      docs += r?.docs_queued ?? 0;
    };
    for (const f of MAIN_FEATURES) await put(f, v.main);
    // Without a separate fast model, the main one does the quick jobs too (the source picker then stays off).
    const fast = v.fast.providerId && v.fast.model.trim() ? v.fast : v.main;
    for (const f of FAST_FEATURES) if (f !== 'sift' || fast !== v.main) await put(f, fast);
    await put('embedding', v.embed);
    return docs;
  }, keys.routes);
  const onSave = async () => {
    const docs = await save.mutateAsync();
    setDone(docs ? `Saved · writing docs for ${docs} repositor${docs === 1 ? 'y' : 'ies'}` : 'Saved');
  };
  const chat = (p: Provider) => p.kind !== 'jev';
  return (
    <Card title="Models" className="mb-6">
      <p className="mb-4 text-sm text-slate-600 dark:text-slate-400">Choose three models and the Hub uses each where it fits. To choose a model per feature, open Advanced below.</p>
      <div className="grid gap-4 lg:grid-cols-3">
        <ModelPicker label="Main model" hint="Writes docs, answers questions, explains errors, runs security scans." value={v.main} onChange={(main) => { setDone(undefined); setV({ ...v, main }); }} providers={providers} kinds={chat} />
        <ModelPicker label="Fast model (optional)" hint="Short code, change triage, and picking the sources Ask reads. A cheaper model saves money." value={v.fast} onChange={(fast) => { setDone(undefined); setV({ ...v, fast }); }} providers={providers} kinds={chat} />
        <ModelPicker label="Search model (optional)" hint="Embeddings for search by meaning. Changing it re-indexes everything." value={v.embed} onChange={(embed) => { setDone(undefined); setV({ ...v, embed }); }} providers={providers} />
      </div>
      <div className="mt-4 flex items-center gap-3">
        <Button disabled={!dirty || !v.main.providerId || !v.main.model.trim() || save.isPending} onClick={() => void onSave()}>{save.isPending ? 'Saving…' : 'Save models'}</Button>
        {done && !dirty && <span role="status" className="inline-flex items-center gap-1 text-sm text-emerald-700 dark:text-emerald-400"><CheckCircle2 className="h-4 w-4" aria-hidden />{done}</span>}
        {providers.length === 0 && <span className="text-sm text-slate-500">Add a provider first.</span>}
      </div>
      <ErrorNote error={save.error} />
    </Card>
  );
}

const isBudget = (l: SpendLimit) => l.scope === 'global' && l.window === 'month' && l.max_cost_usd != null;

function monthStart(now = new Date()): string {
  return new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), 1)).toISOString().replace(/\.\d{3}Z$/, 'Z');
}

/** MonthlyBudget is one number: the most the Hub may spend on models this calendar month. */
export function MonthlyBudget() {
  const limits = useLimits();
  const from = useMemo(() => monthStart(), []);
  const usage = useUsage('feature', 'day', from);
  const current = limits.data?.find(isBudget);
  const [value, setValue] = useState('');
  const [saved, setSaved] = useState(false);
  useEffect(() => {
    setValue(current?.max_cost_usd != null ? String(current.max_cost_usd) : '');
  }, [current?.max_cost_usd]);
  const save = useInvalidating(async (amount: number | null) => {
    const others = (limits.data ?? []).filter((l) => !isBudget(l));
    const items = amount == null ? others : [...others, { scope: 'global', scope_key: '', window: 'month', max_tokens: null, max_cost_usd: amount, on_breach: 'block' } as SpendLimit];
    await api.put('/spend/limits', { items });
  }, keys.limits);
  const spent = usage.data?.totals.cost_usd ?? 0;
  const budget = current?.max_cost_usd ?? null;
  const pct = budget ? Math.min(100, Math.round((spent / budget) * 100)) : 0;
  const amount = value.trim() === '' ? null : Number(value);
  const valid = amount == null || (Number.isFinite(amount) && amount > 0);
  return (
    <Card title="Monthly budget" className="mb-6">
      <div className="flex flex-wrap items-end gap-6">
        <Field label="Budget (USD per month)" hint="When it is reached, the Hub stops calling models until next month. Empty means no monthly cap.">
          <div className="flex gap-2">
            <Input className="w-36" aria-label="Monthly budget in US dollars" type="number" min={1} step="1" value={value} placeholder="No cap" onChange={(e) => { setSaved(false); setValue(e.target.value); }} />
            <Button disabled={!valid || save.isPending || amount === budget} onClick={async () => { await save.mutateAsync(amount); setSaved(true); }}>{save.isPending ? 'Saving…' : 'Save'}</Button>
          </div>
        </Field>
        <div className="min-w-[14rem] flex-1">
          <p className="text-sm text-slate-600 dark:text-slate-400">
            Spent this month: <b className="text-slate-900 dark:text-slate-100">{usd(spent)}</b>{budget ? <> of {usd(budget)}</> : null}
          </p>
          {budget ? (
            <div className="mt-2 h-2 overflow-hidden rounded-full bg-slate-100 dark:bg-white/10" role="meter" aria-label="Budget used" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
              <div className={pct >= 90 ? 'h-full bg-red-500' : pct >= 70 ? 'h-full bg-amber-500' : 'h-full bg-emerald-500'} style={{ width: `${pct}%` }} />
            </div>
          ) : null}
          {saved && <p role="status" className="mt-1 text-xs text-emerald-700 dark:text-emerald-400">Saved</p>}
        </div>
      </div>
      <ErrorNote error={limits.error ?? save.error} />
    </Card>
  );
}
