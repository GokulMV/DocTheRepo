import { useQuery, useQueryClient } from '@tanstack/react-query';
import { CheckCircle2, ChevronDown, Cpu, ExternalLink, Plus } from 'lucide-react';
import { useEffect, useState } from 'react';
import { api } from '@/api/client';
import { keys, useInvalidating, useProviders, useRoutes } from '@/api/hooks';
import type { Provider, Route } from '@/api/types';
import { Badge, Button, Card, Dialog, DialogFooter, Empty, ErrorNote, Field, Input, PageHeader, Select, Spinner, Table, Td, Textarea, Toggle, cx } from '@/components/ui';
import { CHAT_FEATURES, PROVIDER_KINDS, providerKind } from './providerKinds';
import { SealedBadge, SealedHint } from '@/components/Sealed';
import { seal } from '@/lib/seal';
import { capFirst, featureLabel, nameOf, sentence } from '@/lib/labels';

const FEATURE_HELP: Record<string, string> = {
  docgen: 'Writes documentation for changed code',
  docgen_fast: 'Optional cheaper model for short code (docs generation uses the main model when unset)',
  qa: 'Answers questions in Ask',
  embedding: 'Vectors for search (changing it requires a reindex)',
  triage: 'Classifies changes in files without a parser',
  decode: 'Explains new errors and alerts',
  suggest: 'Proposes known-issue rules from pasted text',
  decide: 'Typed yes/no decisions behind confidence gates (e.g. skip decoding obvious noise); TypeSafe Jev or any chat model',
  security: 'Attacks, verifies and fixes code on the Security page (uses the Ask model when unset); a strong model finds more',
  sift: 'Cheap yes/no judging that picks which sources an Ask answer reads, so the Ask model reads less; TypeSafe Jev or a small fast model. Uses decide, then docs (short code), when unset; skipped when it would not save',
};

type RouteOut = { docs_queued?: number };

/**
 * AddProvider asks only for what the chosen kind needs, says where to get each value, and can route every
 * feature to the new provider in the same step.
 */
function AddProvider({ kinds, routed }: { kinds: string[]; routed: string[] }) {
  const [open, setOpen] = useState(false);
  const [kind, setKind] = useState('anthropic');
  const [name, setName] = useState('');
  const [baseURL, setBaseURL] = useState('');
  const [key, setKey] = useState('');
  const [extras, setExtras] = useState<Record<string, string>>({});
  const [advanced, setAdvanced] = useState('');
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [redact, setRedact] = useState(false);
  const [useForAll, setUseForAll] = useState(true);
  const [model, setModel] = useState('');
  const [embedModel, setEmbedModel] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  const [done, setDone] = useState<{ name: string; features: string[]; docs: number }>();
  const qc = useQueryClient();
  const spec = providerKind(kind);
  const available = PROVIDER_KINDS.filter((k) => kinds.length === 0 || kinds.includes(k.kind));
  const chat = spec?.features ?? CHAT_FEATURES;
  const reset = (k: string) => {
    const sp = providerKind(k);
    setKind(k);
    setExtras({});
    setBaseURL('');
    setKey('');
    setModel(sp?.defaultModel ?? '');
    setEmbedModel(sp?.embeddings?.default ?? '');
    setErr(undefined);
  };
  const openDialog = () => {
    reset(kind);
    setUseForAll(routed.length === 0);
    setDone(undefined);
    setOpen(true);
  };
  const missing =
    (spec?.key === 'required' && !key.trim()) ||
    (spec?.baseURL === 'required' && !baseURL.trim()) ||
    (spec?.extras ?? []).some((f) => f.required && !extras[f.key]?.trim()) ||
    (useForAll && !model.trim());
  const submit = async () => {
    setBusy(true);
    setErr(undefined);
    try {
      const ex: Record<string, string> = {};
      advanced.split('\n').map((l) => l.split('=')).forEach(([k, ...v]) => k.trim() && (ex[k.trim()] = v.join('=').trim()));
      for (const [k, v] of Object.entries(extras)) if (v.trim()) ex[k] = v.trim();
      const label = name.trim() || spec?.label || kind;
      const created = await api.post<{ id: string }>('/providers', {
        kind, name: label, base_url: baseURL.trim() || undefined, api_key: key ? await seal(key, 'provider.api_key') : undefined, extra: ex, redact_pii: redact,
      });
      const features: string[] = [];
      let docs = 0;
      if (useForAll) {
        for (const f of chat) {
          const r = await api.put<RouteOut>(`/routes/${f}`, { provider_id: created.id, model: model.trim() });
          features.push(f);
          docs += r?.docs_queued ?? 0;
        }
        if (spec?.embeddings && embedModel.trim() && !spec.features) {
          await api.put(`/routes/embedding`, { provider_id: created.id, model: embedModel.trim() });
          features.push('embedding');
        }
      }
      await qc.invalidateQueries({ queryKey: keys.providers });
      await qc.invalidateQueries({ queryKey: keys.routes });
      setDone({ name: label, features, docs });
    } catch (e) {
      setErr(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <Button onClick={openDialog}><Plus className="h-4 w-4" aria-hidden />Add provider</Button>
      <Dialog open={open} onOpenChange={setOpen} title="Add an LLM provider" description="Use your own account. The Hub never supplies models or keys.">
        {done ? (
          <div className="space-y-3 text-sm">
            <p className="flex items-center gap-2 font-medium text-emerald-700 dark:text-emerald-400"><CheckCircle2 className="h-5 w-5" aria-hidden />{done.name} is ready</p>
            {done.features.length > 0 ? (
              <p className="text-slate-600 dark:text-slate-400">It now handles: {done.features.map(featureLabel).join(', ')}. You can change any of them under Feature routing.</p>
            ) : (
              <p className="text-slate-600 dark:text-slate-400">Choose which features use it under Feature routing below.</p>
            )}
            {done.docs > 0 && <p className="text-slate-600 dark:text-slate-400">Docs are being written for {done.docs} repositor{done.docs === 1 ? 'y' : 'ies'} that were synced earlier; they appear under Docs as each finishes.</p>}
            <DialogFooter><Button onClick={() => setOpen(false)}>Done</Button></DialogFooter>
          </div>
        ) : (
          <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
            <Field label="Provider" hint={spec?.blurb}>
              <Select value={kind} onChange={(e) => reset(e.target.value)}>
                {available.map((k) => <option key={k.kind} value={k.kind}>{k.label}</option>)}
              </Select>
            </Field>
            {spec?.keySteps && (
              <div className="rounded-lg border border-brand-200 bg-brand-50/60 p-3 text-xs dark:border-brand-500/30 dark:bg-brand-500/10">
                <p className="font-medium text-slate-700 dark:text-slate-200">Where to get the key</p>
                <ol className="mt-1 list-decimal space-y-0.5 pl-4 text-slate-600 dark:text-slate-400">
                  {spec.keySteps.map((st) => (
                    <li key={st.text}>{st.href ? <a className="text-brand-700 underline dark:text-brand-300" href={st.href} target="_blank" rel="noreferrer">{st.text}<ExternalLink className="ml-0.5 inline h-3 w-3" aria-hidden /></a> : st.text}</li>
                  ))}
                </ol>
              </div>
            )}
            {spec?.authNote && <p className="rounded-lg bg-slate-50 p-3 text-xs text-slate-600 dark:bg-white/[0.04] dark:text-slate-400">{spec.authNote}</p>}
            {spec?.key !== 'none' && (
              <Field label={spec?.keyLabel ?? 'API key'} hint={<SealedHint />}>
                <Input type="password" autoComplete="off" value={key} onChange={(e) => setKey(e.target.value)} />
              </Field>
            )}
            {spec?.baseURL !== 'hidden' && (
              <Field label={spec?.baseURLLabel ?? (spec?.baseURL === 'required' ? 'Base URL' : 'Base URL (optional)')}>
                <Input value={baseURL} onChange={(e) => setBaseURL(e.target.value)} placeholder={spec?.baseURLPlaceholder} />
              </Field>
            )}
            {(spec?.extras ?? []).map((f) => (
              <Field key={f.key} label={f.label} hint={f.hint}>
                <Input value={extras[f.key] ?? ''} onChange={(e) => setExtras((x) => ({ ...x, [f.key]: e.target.value }))} placeholder={f.placeholder} />
              </Field>
            ))}
            <div className="rounded-lg border border-slate-200 p-3 dark:border-white/10">
              <Toggle label={spec?.features ? `Use it for ${spec.features.map(featureLabel).join(', ')}` : 'Use it for everything (writing docs, answering, explaining errors)'} checked={useForAll} onChange={setUseForAll} />
              {useForAll && (
                <div className="mt-3 grid gap-3 sm:grid-cols-2">
                  <Field label="Model" hint={spec?.defaultModel ? 'Prefilled with a good default; change it if you like.' : 'As named by the provider.'}>
                    <Input value={model} onChange={(e) => setModel(e.target.value)} placeholder={spec?.modelPlaceholder} />
                  </Field>
                  {spec?.embeddings && !spec.features ? (
                    <Field label="Embedding model (optional)" hint="Enables vector search; keyword and graph search work without it.">
                      <Input value={embedModel} onChange={(e) => setEmbedModel(e.target.value)} placeholder={spec.embeddings.placeholder} />
                    </Field>
                  ) : !spec?.features ? (
                    <p className="self-end pb-1 text-xs text-slate-500">This provider has no embeddings: search uses keywords and the knowledge graph, which works well. Add an embedding provider later for vector search.</p>
                  ) : null}
                </div>
              )}
            </div>
            <button type="button" onClick={() => setShowAdvanced((v) => !v)} aria-expanded={showAdvanced} className="inline-flex items-center gap-1 text-xs text-slate-500 hover:text-slate-800 dark:hover:text-slate-200">
              <ChevronDown className={cx('h-3.5 w-3.5 transition-transform', showAdvanced && 'rotate-180')} aria-hidden />Advanced
            </button>
            {showAdvanced && (
              <div className="space-y-3">
                <Field label="Display name"><Input value={name} onChange={(e) => setName(e.target.value)} placeholder={spec?.label} /></Field>
                {spec?.baseURL === 'hidden' && <Field label="Base URL (proxy or gateway)"><Input value={baseURL} onChange={(e) => setBaseURL(e.target.value)} /></Field>}
                <Field label="Extra settings" hint="key=value per line">
                  <Textarea rows={2} className="font-mono text-xs" value={advanced} onChange={(e) => setAdvanced(e.target.value)} />
                </Field>
                <Toggle label="Redact personal data (emails, phone numbers…) before sending" checked={redact} onChange={setRedact} />
              </div>
            )}
            <ErrorNote error={err} />
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => setOpen(false)}>Cancel</Button>
              <Button type="submit" disabled={busy || missing}>{busy ? 'Adding…' : 'Add provider'}</Button>
            </DialogFooter>
          </form>
        )}
      </Dialog>
    </>
  );
}

/** ReplaceKey sets a new key; the old one cannot be seen, only replaced. */
function ReplaceKey({ p }: { p: Provider }) {
  const [open, setOpen] = useState(false);
  const [key, setKey] = useState('');
  const save = useInvalidating(async () => api.patch(`/providers/${p.id}`, { api_key: await seal(key, 'provider.api_key') }), keys.providers);
  return (
    <>
      <Button size="sm" variant="ghost" onClick={() => setOpen(true)}>{p.has_key ? 'Replace key' : 'Set key'}</Button>
      <Dialog open={open} onOpenChange={setOpen} title={`${p.has_key ? 'Replace' : 'Set'} the ${p.name} key`} description="The current key is write-only: it cannot be shown, only replaced.">
        <form className="space-y-3" onSubmit={async (e) => { e.preventDefault(); await save.mutateAsync(); setKey(''); setOpen(false); }}>
          <Field label="New API key" hint={<SealedHint />}><Input type="password" autoComplete="off" required value={key} onChange={(e) => setKey(e.target.value)} /></Field>
          <ErrorNote error={save.error} />
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => setOpen(false)}>Cancel</Button>
            <Button type="submit" disabled={save.isPending || !key}>{save.isPending ? 'Saving…' : 'Save key'}</Button>
          </DialogFooter>
        </form>
      </Dialog>
    </>
  );
}

function ProviderRow({ p }: { p: Provider }) {
  const [model, setModel] = useState('');
  const [result, setResult] = useState<{ ok: boolean; error?: string; latency_ms: number }>();
  const test = useInvalidating((m: string) => api.post<{ ok: boolean; error?: string; latency_ms: number }>(`/providers/${p.id}/test`, { model: m }));
  const del = useInvalidating(() => api.del(`/providers/${p.id}`), keys.providers);
  const toggle = useInvalidating(() => api.patch(`/providers/${p.id}`, { enabled: !p.enabled }), keys.providers);
  return (
    <tr>
      <Td><span className="font-medium">{capFirst(p.name)}</span> {!p.enabled && <Badge tone="amber">disabled</Badge>}</Td>
      <Td>{nameOf(p.kind)}</Td>
      <Td>
        <div className="flex flex-wrap items-center gap-1.5">
          {p.has_key ? <SealedBadge meta={p.key_meta} /> : <Badge>no key</Badge>}
          <ReplaceKey p={p} />
        </div>
      </Td>
      <Td>
        <div className="flex items-center gap-1">
          <Input aria-label="Model to test" className="h-8 w-40 text-xs" placeholder="Model" value={model} onChange={(e) => setModel(e.target.value)} />
          <Button size="sm" variant="secondary" disabled={test.isPending} onClick={async () => setResult(await test.mutateAsync(model))}>Test</Button>
        </div>
        {result && <p className={result.ok ? 'text-xs text-emerald-700' : 'text-xs text-red-700'}>{result.ok ? `ok (${result.latency_ms} ms)` : result.error}</p>}
      </Td>
      <Td>
        <div className="flex gap-1">
          <Button size="sm" variant="ghost" onClick={() => toggle.mutate()}>{p.enabled ? 'Disable' : 'Enable'}</Button>
          <Button size="sm" variant="ghost" onClick={() => confirm(`Remove ${p.name}?`) && del.mutate()}>Remove</Button>
        </div>
        <ErrorNote error={del.error} />
      </Td>
    </tr>
  );
}

function RouteRow({ feature, route, providers }: { feature: string; route?: Route; providers: Provider[] }) {
  const initial = {
    providerId: route?.provider_id ?? '', model: route?.model ?? '', effort: route?.effort ?? '',
    fallbackId: route?.fallback_provider_id ?? '', fallbackModel: route?.fallback_model ?? '',
  };
  const [v, setV] = useState(initial);
  const [saved, setSaved] = useState<string>();
  const routeKey = JSON.stringify(initial);
  useEffect(() => {
    setV(JSON.parse(routeKey));
  }, [routeKey]);
  const dirty = JSON.stringify(v) !== routeKey;
  const set = (patch: Partial<typeof v>) => {
    setSaved(undefined);
    setV((x) => ({ ...x, ...patch }));
  };
  const save = useInvalidating(
    () => api.put<RouteOut>(`/routes/${feature}`, { provider_id: v.providerId, model: v.model.trim(), effort: v.effort, fallback_provider_id: v.fallbackId, fallback_model: v.fallbackModel }),
    keys.routes,
  );
  const onSave = async () => {
    const r = await save.mutateAsync();
    setSaved(r?.docs_queued ? `Saved · writing docs for ${r.docs_queued} repositor${r.docs_queued === 1 ? 'y' : 'ies'}` : 'Saved');
  };
  return (
    <tr>
      <Td><span className="font-medium">{featureLabel(feature)}</span> <code className="text-[11px] text-slate-400">{feature}</code><p className="text-xs text-slate-500">{FEATURE_HELP[feature]}</p></Td>
      <Td>
        <Select className="w-44" aria-label={`${featureLabel(feature)}: provider`} value={v.providerId} onChange={(e) => set({ providerId: e.target.value })}>
          <option value="">— Not routed —</option>
          {providers.map((p) => <option key={p.id} value={p.id}>{capFirst(p.name)}</option>)}
        </Select>
      </Td>
      <Td><Input className="w-56" aria-label={`${featureLabel(feature)}: model`} value={v.model} onChange={(e) => set({ model: e.target.value })} placeholder={providerKind(providers.find((p) => p.id === v.providerId)?.kind ?? '')?.defaultModel ?? 'Model name'} /></Td>
      <Td>
        {feature !== 'embedding' && (
          <Select className="w-28" aria-label={`${featureLabel(feature)}: effort`} value={v.effort} onChange={(e) => set({ effort: e.target.value })}>
            <option value="">Default</option>
            {['low', 'medium', 'high', 'xhigh', 'max'].map((x) => <option key={x} value={x}>{x === 'xhigh' ? 'Extra high' : sentence(x)}</option>)}
          </Select>
        )}
      </Td>
      <Td>
        <div className="flex gap-1">
          <Select className="w-32" aria-label={`${featureLabel(feature)}: fallback provider`} value={v.fallbackId} onChange={(e) => set({ fallbackId: e.target.value })}>
            <option value="">None</option>
            {providers.map((p) => <option key={p.id} value={p.id}>{capFirst(p.name)}</option>)}
          </Select>
          {v.fallbackId && <Input className="w-40" aria-label={`${featureLabel(feature)}: fallback model`} value={v.fallbackModel} onChange={(e) => set({ fallbackModel: e.target.value })} placeholder="Model" />}
        </div>
      </Td>
      <Td>
        <div className="flex min-w-[7rem] flex-col items-start gap-1">
          {dirty || !route ? (
            <Button size="sm" disabled={!v.providerId || !v.model.trim() || save.isPending} onClick={() => void onSave()}>{save.isPending ? 'Saving…' : 'Save'}</Button>
          ) : (
            <span className="inline-flex items-center gap-1 text-xs font-medium text-emerald-700 dark:text-emerald-400" role="status">
              <CheckCircle2 className="h-4 w-4" aria-hidden />{saved ?? 'Active'}
            </span>
          )}
          {dirty && route && <span className="text-[11px] text-amber-600">Unsaved changes</span>}
          <ErrorNote error={save.error} />
        </div>
      </Td>
    </tr>
  );
}

const DOC_MODES: { mode: string; title: string; text: string }[] = [
  { mode: 'thorough', title: 'Thorough', text: 'Every piece of code goes to the main docs model. Highest cost.' },
  { mode: 'balanced', title: 'Balanced', text: 'Tiny code that already has a doc comment uses it; short code goes to the cheaper model when “Docs (short code)” is routed.' },
  { mode: 'economy', title: 'Economy', text: 'Most commented code uses its comment, and the cheaper model writes almost everything else. Lowest cost.' },
];

/** DocsCost chooses how much docs generation may spend; the same code is never documented twice in any mode. */
function DocsCost() {
  const q = useQuery({ queryKey: ['docs-mode'], queryFn: () => api.get<{ mode: string; source: string }>('/docs/mode') });
  const save = useInvalidating((mode: string) => api.put('/docs/mode', { mode }), ['docs-mode']);
  return (
    <Card title="Docs generation cost" className="mt-6">
      <p className="text-sm text-slate-600 dark:text-slate-400">
        Before any model call, the Hub decides how each piece of code gets its doc. In every mode, code that has not changed keeps its doc,
        and code documented before (a retry, a revert, moved code) reuses it, so neither costs anything.
      </p>
      <div role="radiogroup" aria-label="Docs generation mode" className="mt-3 grid gap-2 sm:grid-cols-3">
        {DOC_MODES.map((m) => (
          <label key={m.mode} className={cx('flex cursor-pointer gap-2.5 rounded-lg border p-3 text-sm', q.data?.mode === m.mode ? 'border-brand-400 bg-brand-50/50 dark:border-brand-400/50 dark:bg-brand-500/10' : 'border-slate-200 dark:border-white/10')}>
            <input type="radio" name="docs-mode" className="mt-0.5 accent-brand-600" checked={q.data?.mode === m.mode} disabled={save.isPending} onChange={() => save.mutate(m.mode)} />
            <span><span className="font-medium">{m.title}</span><span className="mt-0.5 block text-xs text-slate-500">{m.text}</span></span>
          </label>
        ))}
      </div>
      <p className="mt-2 text-xs text-slate-500">Tip: route “Docs generation” to a strong model and “Docs (short code)” to a fast one (for example Claude Haiku). Each job’s result shows how its code was routed.</p>
      <ErrorNote error={q.error ?? save.error} />
    </Card>
  );
}

export default function Providers() {
  const providers = useProviders();
  const routes = useRoutes();
  const list = providers.data?.items ?? [];
  return (
    <>
      <PageHeader title="Providers & routing" description="Bring your own LLM accounts, then choose which provider and model each feature uses. Every call is checked against your spend limits first." actions={<AddProvider kinds={providers.data?.kinds ?? []} routed={(routes.data?.items ?? []).map((r) => r.feature)} />} />
      {(providers.isLoading || routes.isLoading) && <Spinner />}
      <ErrorNote error={providers.error ?? routes.error} />
      <Card title="Providers" className="mb-6">
        {list.length === 0 ? (
          <Empty icon={Cpu} title="No providers">Add Anthropic, OpenAI, Azure OpenAI, Bedrock, Vertex, Ollama, or any OpenAI-compatible endpoint.</Empty>
        ) : (
          <Table head={['Name', 'Kind', 'Key', 'Test', '']}>{list.map((p) => <ProviderRow key={p.id} p={p} />)}</Table>
        )}
      </Card>
      <Card title="Feature routing">
        <Table head={['Feature', 'Provider', 'Model', 'Effort', 'Fallback', '']}>
          {(routes.data?.features ?? []).map((f) => (
            <RouteRow key={f} feature={f} route={routes.data?.items.find((r) => r.feature === f)} providers={list} />
          ))}
        </Table>
      </Card>
      <DocsCost />
    </>
  );
}
