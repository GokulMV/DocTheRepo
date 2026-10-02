import { Cpu } from 'lucide-react';
import { useEffect, useState } from 'react';
import { api } from '@/api/client';
import { keys, useInvalidating, useProviders, useRoutes } from '@/api/hooks';
import type { Provider, Route } from '@/api/types';
import { Badge, Button, Card, Dialog, Empty, ErrorNote, Field, Input, PageHeader, Select, Spinner, Table, Td, Toggle } from '@/components/ui';
import { SealedBadge, SealedHint } from '@/components/Sealed';
import { seal } from '@/lib/seal';

const FEATURE_HELP: Record<string, string> = {
  docgen: 'Writes documentation for changed code',
  qa: 'Answers questions in Ask',
  embedding: 'Vectors for search (changing it requires a reindex)',
  triage: 'Classifies changes in files without a parser',
  decode: 'Explains new errors and alerts',
  suggest: 'Proposes known-issue rules from pasted text',
  decide: 'Typed yes/no decisions behind confidence gates (e.g. skip decoding obvious noise); TypeSafe Jev or any chat model',
};

const KIND_HELP: Record<string, string> = {
  anthropic: 'Claude API (or Bedrock/Vertex via extra settings)',
  openai: 'OpenAI API',
  azure_openai: 'Azure OpenAI deployment',
  bedrock: 'AWS Bedrock (region in extra)',
  vertex: 'Google Vertex AI (project/region in extra)',
  openai_compat: 'Any OpenAI-compatible server (vLLM, LiteLLM…)',
  ollama: 'Local Ollama',
  external_cli: 'Headless agent CLI via the DocGen contract',
  jev: 'TypeSafe Jev: calibrated decisions only — route it to "decide"',
};

function AddProvider({ kinds }: { kinds: string[] }) {
  const [open, setOpen] = useState(false);
  const [kind, setKind] = useState('anthropic');
  const [name, setName] = useState('');
  const [baseURL, setBaseURL] = useState('');
  const [key, setKey] = useState('');
  const [extra, setExtra] = useState('');
  const [redact, setRedact] = useState(false);
  const create = useInvalidating((b: object) => api.post('/providers', b), keys.providers);
  return (
    <>
      <Button onClick={() => setOpen(true)}>Add provider</Button>
      <Dialog open={open} onOpenChange={setOpen} title="Add an LLM provider" description="Your own key and account. The Hub never supplies models or keys.">
        <form
          className="space-y-3"
          onSubmit={async (e) => {
            e.preventDefault();
            const ex: Record<string, string> = {};
            extra.split('\n').map((l) => l.split('=')).forEach(([k, ...v]) => k.trim() && (ex[k.trim()] = v.join('=').trim()));
            await create.mutateAsync({ kind, name: name || kind, base_url: baseURL || undefined, api_key: key ? await seal(key, 'provider.api_key') : undefined, extra: ex, redact_pii: redact });
            setOpen(false);
          }}
        >
          <Field label="Kind" hint={KIND_HELP[kind]}>
            <Select value={kind} onChange={(e) => setKind(e.target.value)}>
              {kinds.sort().map((k) => <option key={k} value={k}>{k}</option>)}
            </Select>
          </Field>
          <Field label="Name"><Input value={name} onChange={(e) => setName(e.target.value)} placeholder={kind} /></Field>
          <Field label="Base URL" hint="Only for self-hosted or proxied endpoints."><Input value={baseURL} onChange={(e) => setBaseURL(e.target.value)} /></Field>
          <Field label="API key" hint={<SealedHint />}><Input type="password" autoComplete="off" value={key} onChange={(e) => setKey(e.target.value)} /></Field>
          <Field label="Extra settings" hint="key=value per line, e.g. region=us-east-1, project=my-proj, deployment=gpt4o">
            <textarea className="w-full rounded-md border border-slate-300 p-2 font-mono text-xs dark:border-slate-700 dark:bg-slate-900" rows={3} value={extra} onChange={(e) => setExtra(e.target.value)} />
          </Field>
          <Toggle label="Redact personal data before sending" checked={redact} onChange={setRedact} />
          <ErrorNote error={create.error} />
          <Button type="submit" disabled={create.isPending}>Add</Button>
        </form>
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
          <Button type="submit" disabled={save.isPending || !key}>Save</Button>
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
      <Td><span className="font-medium">{p.name}</span> {!p.enabled && <Badge tone="amber">disabled</Badge>}</Td>
      <Td>{p.kind}</Td>
      <Td>
        <div className="flex flex-wrap items-center gap-1.5">
          {p.has_key ? <SealedBadge meta={p.key_meta} /> : <Badge>no key</Badge>}
          <ReplaceKey p={p} />
        </div>
      </Td>
      <Td>
        <div className="flex items-center gap-1">
          <Input aria-label="Model to test" className="h-8 w-40 text-xs" placeholder="model" value={model} onChange={(e) => setModel(e.target.value)} />
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
  const [providerId, setProviderId] = useState(route?.provider_id ?? '');
  const [model, setModel] = useState(route?.model ?? '');
  const [effort, setEffort] = useState(route?.effort ?? '');
  const [fallbackId, setFallbackId] = useState(route?.fallback_provider_id ?? '');
  const [fallbackModel, setFallbackModel] = useState(route?.fallback_model ?? '');
  useEffect(() => {
    setProviderId(route?.provider_id ?? '');
    setModel(route?.model ?? '');
  }, [route]);
  const save = useInvalidating(
    () => api.put(`/routes/${feature}`, { provider_id: providerId, model, effort, fallback_provider_id: fallbackId, fallback_model: fallbackModel }),
    keys.routes,
  );
  return (
    <tr>
      <Td><span className="font-medium">{feature}</span><p className="text-xs text-slate-500">{FEATURE_HELP[feature]}</p></Td>
      <Td>
        <Select className="w-44" aria-label={`${feature} provider`} value={providerId} onChange={(e) => setProviderId(e.target.value)}>
          <option value="">— not routed —</option>
          {providers.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </Select>
      </Td>
      <Td><Input className="w-56" aria-label={`${feature} model`} value={model} onChange={(e) => setModel(e.target.value)} placeholder="e.g. claude-opus-5-5" /></Td>
      <Td>
        {feature !== 'embedding' && (
          <Select className="w-28" aria-label={`${feature} effort`} value={effort} onChange={(e) => setEffort(e.target.value)}>
            <option value="">default</option>
            {['low', 'medium', 'high', 'xhigh', 'max'].map((x) => <option key={x}>{x}</option>)}
          </Select>
        )}
      </Td>
      <Td>
        <div className="flex gap-1">
          <Select className="w-32" aria-label={`${feature} fallback`} value={fallbackId} onChange={(e) => setFallbackId(e.target.value)}>
            <option value="">none</option>
            {providers.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
          </Select>
          {fallbackId && <Input className="w-40" aria-label={`${feature} fallback model`} value={fallbackModel} onChange={(e) => setFallbackModel(e.target.value)} placeholder="model" />}
        </div>
      </Td>
      <Td>
        <Button size="sm" disabled={!providerId || !model || save.isPending} onClick={() => save.mutate()}>Save</Button>
        <ErrorNote error={save.error} />
      </Td>
    </tr>
  );
}

export default function Providers() {
  const providers = useProviders();
  const routes = useRoutes();
  const list = providers.data?.items ?? [];
  return (
    <>
      <PageHeader title="Providers & routing" description="Bring your own LLM accounts, then choose which provider and model each feature uses. Every call is checked against your spend limits first." actions={<AddProvider kinds={providers.data?.kinds ?? []} />} />
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
    </>
  );
}
