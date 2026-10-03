import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, ExternalLink, FileCode2, FolderGit2, Network, RefreshCw, Search, Workflow } from 'lucide-react';
import { useMemo, useState } from 'react';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { api } from '@/api/client';
import { useMe } from '@/api/hooks';
import { atLeast } from '@/api/types';
import { ArchitectureDiagram, ArchitectureLegend, LAYERS, nodeSubtitle, type ArchLink, type ArchNode } from '@/components/ArchitectureDiagram';
import { kindMeta } from '@/components/palaceKinds';
import { Badge, Button, Card, Empty, ErrorNote, Input, PageHeader, Spinner, cx } from '@/components/ui';

interface Summary {
  repo_id: string;
  full_name: string;
  service_name?: string;
  endpoints: number;
  modules: number;
  topics: number;
  datastores: number;
  diagrams: number;
}

interface Diagram {
  id: string;
  path: string;
  title: string;
  generator?: string;
  commit_sha?: string;
  size_bytes: number;
  updated_at: string;
}

interface Architecture {
  repo: { id: string; full_name: string; service_name?: string };
  nodes: ArchNode[];
  links: ArchLink[];
  hidden: Record<string, number>;
  counts?: Record<string, number>;
  restricted: number;
  env: string[];
  docs: { entity_id: string; kind: string; name: string; key: string; relation: string }[];
  owners: string[];
  diagrams: Diagram[];
  updated_at?: string;
}

/** ArchSummary says in one sentence what the diagram shows. */
function ArchSummary({ a }: { a: Architecture }) {
  const of = (kind: string) => a.nodes.filter((n) => n.kind === kind);
  const groups = of('endpoint_group');
  const endpoints = a.counts?.endpoints ?? groups.reduce((s, n) => s + (n.count ?? 0), 0);
  const groupCount = a.counts?.endpoint_groups ?? groups.length;
  const libs = of('dependency_group');
  const topics = a.nodes.filter((n) => n.layer === 'messaging');
  const data = a.nodes.filter((n) => n.layer === 'data');
  const repos = a.nodes.filter((n) => n.kind === 'repo' && n.repo_id && n.repo_id !== a.repo.id);
  const parts: string[] = [];
  if (endpoints) parts.push(`exposes ${endpoints} HTTP endpoint${endpoints === 1 ? '' : 's'} in ${groupCount} group${groupCount === 1 ? '' : 's'}`);
  if (topics.length) parts.push(`sends or receives messages on ${topics.map((t) => t.name).join(', ')}`);
  if (data.length) parts.push(`stores data in ${data.length} datastore${data.length === 1 ? '' : 's'}`);
  if (repos.length) parts.push(`talks to ${repos.map((r) => r.name).join(', ')}`);
  if (libs.length) parts.push(`uses ${libs.map((l) => `${l.count} ${l.name}`).join(' and ')}`);
  if (!parts.length) return null;
  const last = parts.pop();
  return (
    <p className="rounded-xl border border-slate-200/80 bg-white/70 px-4 py-3 text-sm text-slate-700 dark:border-white/[0.06] dark:bg-slate-900/40 dark:text-slate-300">
      <b className="font-medium">{a.repo.full_name}</b> {parts.length ? `${parts.join('; ')}; and ${last}` : last}. Read left to right: who calls it, what it exposes, its parts, then what it depends on.
    </p>
  );
}

export default function ArchitecturePage() {
  const { repoId } = useParams();
  return repoId ? <RepoArchitecture repoId={repoId} /> : <ArchitectureList />;
}

function ArchitectureList() {
  const list = useQuery({ queryKey: ['architecture'], queryFn: () => api.get<{ items: Summary[] }>('/architecture') });
  const [q, setQ] = useState('');
  const items = (list.data?.items ?? []).filter((s) => !q || s.full_name.toLowerCase().includes(q.toLowerCase()) || (s.service_name ?? '').toLowerCase().includes(q.toLowerCase()));
  return (
    <>
      <PageHeader
        title="Architecture"
        description="Every repository's architecture: generated from its code on each push, plus diagrams authored with archify and committed to the repository."
      />
      {list.isLoading && <Spinner />}
      <ErrorNote error={list.error} />
      {list.data && list.data.items.length === 0 && (
        <Empty title="No repositories yet" icon={Workflow} action={<Link to="/repos" className="text-sm font-medium text-brand-600 hover:underline dark:text-brand-300">Track a repository →</Link>}>
          Track a repository and its architecture appears here after the first push is processed.
        </Empty>
      )}
      {(list.data?.items.length ?? 0) > 0 && (
        <>
          <div className="relative mb-4 max-w-sm">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" aria-hidden />
            <Input aria-label="Filter repositories" placeholder="Filter repositories…" value={q} onChange={(e) => setQ(e.target.value)} className="pl-9" />
          </div>
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
            {items.map((s) => (
              <Link
                key={s.repo_id}
                to={`/architecture/${s.repo_id}`}
                className="group rounded-xl border border-slate-200/80 bg-white p-4 shadow-card transition hover:-translate-y-0.5 hover:border-brand-300 hover:shadow-md dark:border-white/[0.06] dark:bg-slate-900/60 dark:hover:border-brand-500/40"
              >
                <div className="flex items-start gap-3">
                  <span className="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-brand-50 text-brand-600 dark:bg-brand-500/15 dark:text-brand-300"><FolderGit2 className="h-[18px] w-[18px]" aria-hidden /></span>
                  <div className="min-w-0 flex-1">
                    <p className="truncate font-medium text-slate-900 group-hover:text-brand-700 dark:text-slate-100 dark:group-hover:text-brand-300">{s.full_name}</p>
                    <p className="truncate text-xs text-slate-500">{s.service_name ? `service ${s.service_name}` : 'repository'}</p>
                  </div>
                  {s.diagrams > 0 && <Badge tone="blue">{s.diagrams} diagram{s.diagrams === 1 ? '' : 's'}</Badge>}
                </div>
                <dl className="mt-4 grid grid-cols-4 gap-2 text-center">
                  {([['Endpoints', s.endpoints, 'endpoint'], ['Topics', s.topics, 'queue_topic'], ['Data', s.datastores, 'datastore'], ['Modules', s.modules, 'module']] as const).map(([label, n, kind]) => (
                    <div key={label} className="rounded-lg bg-slate-50 px-1 py-2 dark:bg-white/[0.03]">
                      <dd className="text-base font-semibold tabular-nums" style={{ color: n ? kindMeta(kind).color : undefined }}>{n}</dd>
                      <dt className="text-[11px] text-slate-500">{label}</dt>
                    </div>
                  ))}
                </dl>
              </Link>
            ))}
          </div>
          {items.length === 0 && <p className="text-sm text-slate-500">No repository matches “{q}”.</p>}
        </>
      )}
    </>
  );
}

function RepoArchitecture({ repoId }: { repoId: string }) {
  const nav = useNavigate();
  const qc = useQueryClient();
  const me = useMe();
  const [params, setParams] = useSearchParams();
  const view = params.get('view') ?? 'generated';
  const [selected, setSelected] = useState<string>();
  const arch = useQuery({ queryKey: ['architecture', repoId], queryFn: () => api.get<Architecture>(`/architecture/repos/${repoId}`) });
  const scan = useMutation({
    mutationFn: () => api.post<{ found: number; removed: number; checked: number; failed?: { path: string; error: string }[] }>(`/architecture/repos/${repoId}/scan`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['architecture'] }),
  });
  const a = arch.data;
  const canScan = me.data ? atLeast(me.data.role, 'editor') : false;
  const diagram = a?.diagrams.find((d) => d.id === view);
  const sel = a?.nodes.find((n) => n.id === selected);
  const names = useMemo(() => new Map((a?.nodes ?? []).map((n) => [n.id, n.name])), [a]);

  return (
    <>
      <Link to="/architecture" className="mb-3 inline-flex items-center gap-1 text-sm text-slate-500 hover:text-slate-800 dark:hover:text-slate-200">
        <ArrowLeft className="h-4 w-4" aria-hidden /> All repositories
      </Link>
      <PageHeader
        title={a?.repo.full_name ?? 'Architecture'}
        description={a ? `${a.repo.service_name ? `Runs as ${a.repo.service_name}. ` : ''}Generated from the code on every push${a.updated_at ? `; last change ${new Date(a.updated_at).toLocaleString()}` : ''}.` : undefined}
        actions={canScan ? (
          <Button variant="secondary" disabled={scan.isPending} onClick={() => scan.mutate()} title="Look for archify diagrams on the tracked branch">
            <RefreshCw className={cx('h-4 w-4', scan.isPending && 'animate-spin')} aria-hidden />Find diagrams
          </Button>
        ) : undefined}
      />
      {arch.isLoading && <Spinner />}
      <ErrorNote error={arch.error ?? scan.error} />
      {scan.data && <p className="mb-3 text-sm text-slate-600 dark:text-slate-400" role="status">Checked {scan.data.checked} file(s): {scan.data.found} diagram(s) found{scan.data.removed ? `, ${scan.data.removed} removed` : ''}.</p>}
      {!!scan.data?.failed?.length && (
        <div className="mb-3 rounded-lg border border-amber-300 bg-amber-50/70 p-3 text-sm dark:border-amber-500/30 dark:bg-amber-500/10" role="alert">
          <p className="font-medium">{scan.data.failed.length} file(s) could not be read; any copy found earlier is kept. Try again later.</p>
          <ul className="mt-1 space-y-0.5 text-xs text-amber-800 dark:text-amber-200">
            {scan.data.failed.map((f) => <li key={f.path} className="break-all"><code>{f.path}</code>: {f.error}</li>)}
          </ul>
        </div>
      )}
      {a && (
        <>
          <div role="tablist" aria-label="Views" className="mb-4 flex flex-wrap gap-1 border-b border-slate-200/80 dark:border-white/[0.06]">
            <Tab active={view === 'generated'} onClick={() => setParams({})} icon={Network}>Generated</Tab>
            {a.diagrams.map((d) => (
              <Tab key={d.id} active={view === d.id} onClick={() => setParams({ view: d.id })} icon={FileCode2}>{d.title}</Tab>
            ))}
          </div>

          {view === 'generated' && (
            <div className="grid gap-6 min-[1800px]:grid-cols-[minmax(0,1fr)_20rem]">
              <div className="min-w-0 space-y-3">
                {a.nodes.length <= 1 ? (
                  <Empty title="Nothing extracted yet" icon={Workflow}>
                    The architecture fills in from the code: endpoints, topics, datastores, and calls between repositories, as pushes are processed.
                  </Empty>
                ) : (
                  <>
                    <ArchSummary a={a} />
                    <ArchitectureDiagram nodes={a.nodes} links={a.links} hidden={a.hidden} selected={selected} onSelect={setSelected} />
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <ArchitectureLegend />
                      <span className="text-xs text-slate-500">Hover to trace · click for details</span>
                    </div>
                    {a.restricted > 0 && <p className="text-xs text-slate-500">{a.restricted} link(s) to repositories you don’t have access to are not shown.</p>}
                  </>
                )}
              </div>
              <div className="grid content-start gap-4 md:grid-cols-2 xl:grid-cols-3 min-[1800px]:grid-cols-1">
                {sel ? (
                  <Card title={sel.name}>
                    <p className="text-xs text-slate-500">{nodeSubtitle(sel, kindMeta(sel.kind).label)} · {LAYERS.find((l) => l.id === sel.layer)?.label}</p>
                    {sel.key && sel.key !== sel.name && <p className="mt-1 break-all font-mono text-[12px] text-slate-500">{sel.key}</p>}
                    {!!sel.items?.length && (
                      <ul className="mt-3 max-h-64 space-y-0.5 overflow-y-auto rounded-lg bg-slate-50 p-2 font-mono text-[12px] dark:bg-white/[0.04]">
                        {sel.items.map((it) => <li key={it} className="truncate" title={it}>{it}</li>)}
                        {(sel.count ?? 0) > sel.items.length && <li className="font-sans text-slate-400">+{(sel.count ?? 0) - sel.items.length} more</li>}
                      </ul>
                    )}
                    <ul className="mt-3 space-y-1 text-sm">
                      {a.links.filter((l) => l.src === sel.id || l.dst === sel.id).map((l) => (
                        <li key={l.src + l.kind + l.dst} className="text-slate-600 dark:text-slate-300">
                          {l.src === sel.id ? <>{l.kind.replace(/_/g, ' ')} → <b>{names.get(l.dst)}</b></> : <><b>{names.get(l.src)}</b> {l.kind.replace(/_/g, ' ')} →</>}
                          {l.weight > 1 && <span className="text-slate-400"> ×{l.weight}</span>}
                        </li>
                      ))}
                    </ul>
                    <div className="mt-3 flex flex-wrap gap-2">
                      {sel.entity_id && <Button size="sm" variant="secondary" onClick={() => nav(`/palace/${sel.entity_id}`)}>Open in Palace</Button>}
                      {sel.repo_id && sel.repo_id !== repoId && <Button size="sm" variant="secondary" onClick={() => { setSelected(undefined); nav(`/architecture/${sel.repo_id}`); }}>Its architecture</Button>}
                      <Button size="sm" variant="secondary" onClick={() => nav(`/ask?q=${encodeURIComponent(`How does ${sel.name} fit into ${a.repo.full_name}?`)}`)}>Ask about it</Button>
                    </div>
                  </Card>
                ) : null}
                <Card title="Configuration">
                  {a.env.length ? (
                    <div className="flex flex-wrap gap-1.5">{a.env.map((e) => <code key={e} className="rounded bg-slate-100 px-1.5 py-0.5 font-mono text-[12px] dark:bg-white/[0.06]">{e}</code>)}</div>
                  ) : <p className="text-sm text-slate-500">No environment variables read.</p>}
                </Card>
                <Card title="Documentation">
                  {a.docs.length ? (
                    <ul className="space-y-1.5 text-sm">
                      {a.docs.map((d) => (
                        <li key={d.entity_id}><Link to={`/palace/${d.entity_id}`} className="text-brand-600 hover:underline dark:text-brand-300">{d.name}</Link> <span className="text-xs text-slate-500">{d.relation === 'runbook_for' ? 'runbook' : kindMeta(d.kind).label}</span></li>
                      ))}
                    </ul>
                  ) : <p className="text-sm text-slate-500">No linked Confluence or Jira pages.</p>}
                  {a.owners.length > 0 && <p className="mt-3 text-sm text-slate-600 dark:text-slate-400">Owners: {a.owners.join(', ')}</p>}
                </Card>
                {a.diagrams.length === 0 && (
                  <Card title="Authored diagrams">
                    <p className="text-sm text-slate-600 dark:text-slate-400">
                      Diagrams made with <a href="https://github.com/tt-a1i/archify" target="_blank" rel="noreferrer" className="text-brand-600 hover:underline dark:text-brand-300">archify</a> and
                      committed under a path like <code className="font-mono text-[12px]">docs/architecture/*.html</code> show up here as tabs, and stay current with each push.
                    </p>
                  </Card>
                )}
              </div>
            </div>
          )}

          {diagram && (
            <div className="space-y-2">
              <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-slate-500">
                <span><span className="font-mono">{diagram.path}</span>{diagram.commit_sha && <> · {diagram.commit_sha.slice(0, 7)}</>} · {diagram.generator}</span>
                <a href={`/api/v1/architecture/diagrams/${diagram.id}`} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-brand-600 hover:underline dark:text-brand-300">
                  Open full screen <ExternalLink className="h-3.5 w-3.5" aria-hidden />
                </a>
              </div>
              <iframe
                key={diagram.id}
                title={diagram.title}
                src={`/api/v1/architecture/diagrams/${diagram.id}`}
                sandbox="allow-scripts allow-popups allow-popups-to-escape-sandbox allow-downloads"
                className="h-[78vh] w-full rounded-xl border border-slate-200/80 bg-white dark:border-white/[0.06] dark:bg-slate-950"
              />
            </div>
          )}
          {view !== 'generated' && !diagram && <p className="text-sm text-slate-500">That diagram is no longer in the repository.</p>}
        </>
      )}
    </>
  );
}

function Tab({ active, onClick, icon: Icon, children }: { active: boolean; onClick: () => void; icon: typeof Network; children: React.ReactNode }) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cx(
        '-mb-px inline-flex items-center gap-1.5 border-b-2 px-3 py-2 text-sm font-medium transition-colors',
        active ? 'border-brand-500 text-brand-700 dark:text-brand-300' : 'border-transparent text-slate-500 hover:text-slate-800 dark:hover:text-slate-200',
      )}
    >
      <Icon className="h-4 w-4" aria-hidden />
      {children}
    </button>
  );
}
