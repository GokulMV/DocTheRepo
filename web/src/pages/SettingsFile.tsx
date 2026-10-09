import { useMutation, useQuery } from '@tanstack/react-query';
import { Download, Eye, FileUp, Play } from 'lucide-react';
import { useRef, useState } from 'react';
import { api } from '@/api/client';
import { Badge, Button, Card, ErrorNote, PageHeader, Table, Td, Textarea, type Tone } from '@/components/ui';

interface Change {
  kind: string;
  name: string;
  action: 'create' | 'update' | 'unchanged' | 'failed';
  fields?: string[];
  detail?: string;
}

interface ApplyResult {
  dry_run: boolean;
  changes: Change[];
  sources?: string[];
  error?: string;
}

const EXAMPLE = `version: 1
providers:
  - name: anthropic
    kind: anthropic
    api_key: \${vault:secret/data/dth#anthropic}
routes:
  docgen: { provider: anthropic, model: claude-opus-5-5, effort: high }
  qa: { provider: anthropic, model: claude-sonnet-5-5 }
connectors:
  - name: github
    type: github
    mode: webhook
    credentials: \${awssm:prod/dth#github_token}
repos:
  - { full_name: acme/payments, connector: github, docs_path: docs/, push_mode: pr_auto_merge }
spend:
  limits:
    - { scope: global, window: day, max_tokens: 2000000 }
`;

const REFS: [string, string][] = [
  ['${env:NAME}', 'Environment variable (GitHub Actions secrets via env:)'],
  ['${file:keys.json#github.token}', 'A key in a JSON/YAML file, or the whole file without #key'],
  ['${file:application.properties#db.password}', 'A key in a .properties or .env file'],
  ['${vault:secret/data/dth#key}', 'HashiCorp Vault KV v1/v2'],
  ['${gopass:infra/dth#key}', 'Gopass (password store)'],
  ['${awssm:prod/dth#key}', 'AWS Secrets Manager'],
  ['${gcpsm:project/secret}', 'Google Secret Manager'],
];

const actionTone: Record<Change['action'], Tone> = { create: 'green', update: 'blue', unchanged: 'gray', failed: 'red' };

export default function SettingsFile() {
  const [text, setText] = useState('');
  const [result, setResult] = useState<ApplyResult>();
  const fileInput = useRef<HTMLInputElement>(null);
  const sources = useQuery({ queryKey: ['settings-sources'], queryFn: () => api.get<{ enabled: string[]; env_prefix: string }>('/settings/sources') });
  const run = useMutation({
    mutationFn: (dryRun: boolean) => api.post<ApplyResult>('/settings/apply', { document: text, dry_run: dryRun }),
    onSuccess: setResult,
  });
  const exp = useMutation({
    mutationFn: () => api.get<{ yaml: string }>('/settings/export'),
    onSuccess: (r) => {
      setText(r.yaml);
      setResult(undefined);
    },
  });
  const load = async (f: File | undefined) => {
    if (!f) return;
    setText(await f.text());
    setResult(undefined);
  };
  const counts = (result?.changes ?? []).reduce<Record<string, number>>((m, c) => ({ ...m, [c.action]: (m[c.action] ?? 0) + 1 }), {});
  const enabled = sources.data?.enabled ?? [];

  return (
    <>
      <PageHeader
        title="Settings file"
        description="Configure providers, routing, connectors, repositories and spend limits from one YAML or JSON file. Secrets stay in your secret store: the file holds references."
      />
      <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_22rem]">
        <Card
          title="Settings"
          actions={
            <>
              <input ref={fileInput} type="file" accept=".yaml,.yml,.json" className="hidden" aria-label="Settings file" onChange={(e) => load(e.target.files?.[0])} />
              <Button variant="secondary" size="sm" onClick={() => fileInput.current?.click()}><FileUp className="h-3.5 w-3.5" aria-hidden />Load file</Button>
              <Button variant="secondary" size="sm" disabled={exp.isPending} onClick={() => exp.mutate()}><Download className="h-3.5 w-3.5" aria-hidden />Current settings</Button>
            </>
          }
        >
          <Textarea
            aria-label="Settings YAML or JSON"
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              setResult(undefined);
            }}
            placeholder={EXAMPLE}
            spellCheck={false}
            className="min-h-88 font-mono text-[13px] leading-relaxed"
          />
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <Button variant="secondary" disabled={!text.trim() || run.isPending} onClick={() => run.mutate(true)}><Eye className="h-4 w-4" aria-hidden />Preview changes</Button>
            <Button disabled={!text.trim() || run.isPending || !result?.dry_run || !!result.error} onClick={() => run.mutate(false)} title={result?.dry_run ? undefined : 'Preview first'}>
              <Play className="h-4 w-4" aria-hidden />Apply
            </Button>
            <span className="text-xs text-slate-500">Apply creates and updates; it never deletes. Preview first.</span>
          </div>
          <ErrorNote error={run.error ?? exp.error} />
        </Card>

        <Card title="Secret references">
          <p className="text-sm text-slate-600 dark:text-slate-400">Write a reference wherever a value is secret. Mappings work for JSON credentials.</p>
          <ul className="mt-3 space-y-2 text-xs">
            {REFS.map(([ref, what]) => (
              <li key={ref}>
                <code className="rounded-sm bg-slate-100 px-1 py-0.5 font-mono text-[12px] text-slate-800 dark:bg-white/6 dark:text-slate-200">{ref}</code>
                <span className="mt-0.5 block text-slate-500">{what}</span>
              </li>
            ))}
          </ul>
          <div className="mt-4 rounded-lg border border-slate-200 p-3 text-xs text-slate-600 dark:border-white/8 dark:text-slate-400">
            {enabled.length > 0 ? (
              <>Here, the Hub resolves: <b>{enabled.join(', ')}</b>{enabled.includes('env') && <> (variables starting with <code>{sources.data?.env_prefix}</code>)</>}.</>
            ) : (
              <>This Hub resolves no references from pasted settings (set <code>DTH_SETTINGS_SECRET_SOURCES</code> to enable some).</>
            )}{' '}
            To resolve everything on your own machine or CI runner, run <code className="font-mono">dth apply -f hub.yaml</code>.
          </div>
        </Card>
      </div>

      {result && (
        <Card
          className="mt-6"
          title={result.dry_run ? 'Preview' : 'Applied'}
          actions={
            <div className="flex gap-1.5">
              {(['create', 'update', 'unchanged', 'failed'] as const).filter((a) => counts[a]).map((a) => (
                <Badge key={a} tone={actionTone[a]}>{counts[a]} {a}</Badge>
              ))}
            </div>
          }
        >
          {result.error && <p className="mb-3 text-sm text-red-700 dark:text-red-400">Stopped: {result.error}. Everything above it was {result.dry_run ? 'checked' : 'applied'}; fix the file and run again.</p>}
          <Table head={['Action', 'Kind', 'Name', 'Detail']}>
            {result.changes.map((c, i) => (
              <tr key={i}>
                <Td><Badge tone={actionTone[c.action]}>{c.action}</Badge></Td>
                <Td className="text-slate-500">{c.kind}</Td>
                <Td className="font-medium">{c.name}</Td>
                <Td className="text-xs text-slate-500">{[c.fields?.join(', '), c.detail].filter(Boolean).join(' · ')}</Td>
              </tr>
            ))}
          </Table>
          {(result.sources?.length ?? 0) > 0 && <p className="mt-3 text-xs text-slate-500">Secrets read from: {result.sources!.join(', ')} (values are never shown).</p>}
        </Card>
      )}
    </>
  );
}
