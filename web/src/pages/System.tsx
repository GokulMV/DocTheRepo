import { useQuery } from '@tanstack/react-query';
import { Network, RefreshCw } from 'lucide-react';
import { api } from '@/api/client';
import { useInvalidating, useMe, useRepos } from '@/api/hooks';
import { atLeast, type Repo, type SystemView } from '@/api/types';
import { Markdown } from '@/components/Markdown';
import { Badge, Button, ErrorNote, PageHeader, Spinner, Table, Td } from '@/components/ui';
import { relTime } from '@/lib/format';
import { ConfidenceBadge, withCitations } from './RepoDocs';

const KIND: Record<string, string> = { event: 'Event', api: 'API call', call: 'Code call', library: 'Package', image: 'Container image', pipeline: 'CI pipeline', other: 'Link' };

function codeURL(repos: Repo[], repoName: string, path: string, line: string) {
  const r = repos.find((x) => x.full_name === repoName);
  if (!r) return undefined;
  const ref = r.last_processed_sha || r.default_branch;
  if (r.connector_type === 'github') return `https://github.com/${r.full_name}/blob/${ref}/${path}#L${line}`;
  if (r.connector_type === 'gitlab') return `https://gitlab.com/${r.full_name}/-/blob/${ref}/${path}#L${line}`;
  return undefined;
}

/** System is the architecture across repositories: how they talk to each other, and the write-up. */
export default function System() {
  const me = useMe();
  const repos = useRepos();
  const sys = useQuery({ queryKey: ['system'], queryFn: () => api.get<SystemView>('/system'),
    refetchInterval: (q) => (q.state.data?.job && ['queued', 'processing'].includes(q.state.data.job.status) ? 3000 : false) });
  const write = useInvalidating(() => api.post('/system/write'), ['system']);
  const admin = atLeast(me.data?.role, 'admin');
  const d = sys.data;
  // Citations here are repository-qualified: owner/repo/path:line.
  const link = (p: string, l: string) => {
    const parts = p.split('/');
    return parts.length > 2 ? codeURL(repos.data ?? [], parts.slice(0, 2).join('/'), parts.slice(2).join('/'), l) : undefined;
  };
  const writing = d?.job && ['queued', 'processing'].includes(d.job.status);
  return (
    <>
      <PageHeader icon={Network} title="System architecture" description="How your repositories work together: the APIs, events, packages, images and CI pipelines that connect them, found in the code and its configuration."
        actions={admin && d?.links.length ? <Button size="sm" variant="secondary" disabled={write.isPending || !!writing} onClick={() => write.mutate(undefined)}>Write again</Button> : undefined} />
      {sys.isLoading && <Spinner />}
      <ErrorNote error={sys.error ?? write.error} />
      {d && d.links.length === 0 && (
        <p className="text-sm text-slate-500">The repositories you can read do not call each other, share events, build on each other’s packages or images, or reference each other in CI, so there is no system-wide view.</p>
      )}
      {d && d.links.length > 0 && (
        <div className="space-y-8">
          {writing && <p role="status" className="inline-flex items-center gap-2 text-sm text-brand-700 dark:text-brand-300"><RefreshCw className="h-4 w-4 animate-spin" aria-hidden />Writing the System architecture…</p>}
          <section aria-label="Map">
            <h2 className="mb-2 text-lg font-semibold">Map</h2>
            <div className="rounded-xl border border-slate-200 bg-white p-4 dark:border-white/10 dark:bg-slate-900/40"><Markdown>{'```mermaid\n' + d.diagram + '```'}</Markdown></div>
          </section>
          {d.doc && d.doc.status === 'ok' ? (
            <article>
              <div className="flex flex-wrap items-center gap-2">
                <ConfidenceBadge label={d.doc.label} value={d.doc.confidence} why={d.doc.why} />
                <span className="text-xs text-slate-500">Written {relTime(d.doc.updated_at)}</span>
              </div>
              <section aria-label="At a glance" className="mt-4 rounded-xl border border-brand-100 bg-brand-50/60 p-4 text-[15px] leading-relaxed dark:border-brand-400/20 dark:bg-brand-500/10">
                <p className="mb-1 text-xs font-semibold uppercase tracking-wide text-brand-700 dark:text-brand-300">At a glance</p>
                {d.doc.at_a_glance}
              </section>
              <div className="mt-6 space-y-8">
                {d.doc.sections.map((s) => (
                  <section key={s.key} id={s.key} aria-labelledby={`h-${s.key}`}>
                    <h2 id={`h-${s.key}`} className="mb-2 text-lg font-semibold">{s.title}</h2>
                    <Markdown>{withCitations(s.markdown, link)}</Markdown>
                  </section>
                ))}
              </div>
            </article>
          ) : !d.complete ? (
            <p className="text-sm text-slate-500">The write-up covers repositories you cannot read, so only the links between the ones you can are shown. Ask an admin for access to see it.</p>
          ) : !writing && <p className="text-sm text-slate-500">The write-up has not been written yet{admin ? ': use “Write again”.' : '.'}</p>}
          <section aria-label="Links found in the code">
            <h2 className="mb-2 text-lg font-semibold">Links found in the code</h2>
            <Table head={['From', 'To', 'How', 'Through', 'Where']}>
              {d.links.map((l) => {
                const url = l.path ? codeURL(repos.data ?? [], l.from_name, l.path, String(l.line || 1)) : undefined;
                return (
                  <tr key={`${l.from_repo}-${l.to_repo}-${l.kind}-${l.via}`}>
                    <Td>{l.from_name}</Td>
                    <Td>{l.to_name}</Td>
                    <Td><Badge tone={l.kind === 'event' ? 'amber' : l.kind === 'api' ? 'blue' : l.kind === 'pipeline' || l.kind === 'image' ? 'green' : 'gray'}>{KIND[l.kind] ?? l.kind}</Badge></Td>
                    <Td><code className="text-xs">{l.via}</code>{l.n > 1 && <span className="ml-1 text-xs text-slate-500">· {l.n} places</span>}</Td>
                    <Td className="text-xs">{l.path ? (url ? <a className="text-brand-600 underline" href={url} target="_blank" rel="noreferrer">{l.path}:{l.line || 1}</a> : <code>{l.path}:{l.line || 1}</code>) : '—'}</Td>
                  </tr>
                );
              })}
            </Table>
          </section>
        </div>
      )}
    </>
  );
}
