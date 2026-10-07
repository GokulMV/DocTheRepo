import { useQuery } from '@tanstack/react-query';
import { AlertTriangle, BookOpen, CheckCircle2, FileDown, Info, RefreshCw } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { api } from '@/api/client';
import { useInvalidating, useMe, useRepos, useRoutes } from '@/api/hooks';
import { atLeast, type ConfidenceLabel, type Repo, type RepoDoc, type RepoDocSummary, type RepoDocsList } from '@/api/types';
import { Markdown } from '@/components/Markdown';
import { Badge, Button, ErrorNote, Input, PageHeader, Select, Spinner, cx } from '@/components/ui';
import { relTime, shortSha, usd } from '@/lib/format';

const GROUPS = ['Basics', 'Modules', 'Interfaces', 'Engineering', 'Operations', 'People'];

const listKey = (repoId: string) => ['repo-docs', repoId];

export function useRepoDocs(repoId?: string) {
  return useQuery({
    queryKey: listKey(repoId ?? ''),
    queryFn: () => api.get<RepoDocsList>(`/repo-docs?repo_id=${repoId}`),
    enabled: !!repoId,
    // While documents are being written, keep the list fresh.
    refetchInterval: (q) => (q.state.data?.job && ['queued', 'processing'].includes(q.state.data.job.status) ? 3000 : false),
  });
}

function useRepoDoc(repoId?: string, type?: string, key?: string) {
  return useQuery({
    queryKey: ['repo-doc', repoId, type, key],
    queryFn: () => api.get<RepoDoc>(`/repo-docs/find?repo_id=${repoId}&type=${type}&key=${encodeURIComponent(key ?? type ?? '')}`),
    enabled: !!repoId && !!type,
  });
}

const TONE: Record<ConfidenceLabel, 'green' | 'amber' | 'red'> = { high: 'green', medium: 'amber', low: 'red' };
const DOT: Record<ConfidenceLabel, string> = { high: 'bg-emerald-500', medium: 'bg-amber-500', low: 'bg-red-500' };

/** ConfidenceBadge shows how far a document can be trusted, and why. */
export function ConfidenceBadge({ label, value, why, calibrated }: { label: ConfidenceLabel; value: number; why?: string[]; calibrated?: boolean }) {
  const [open, setOpen] = useState(false);
  const text = `${label[0].toUpperCase()}${label.slice(1)} confidence`;
  return (
    <span className="relative inline-flex">
      <button type="button" onClick={() => setOpen((o) => !o)} aria-expanded={open} aria-label={`${text}: ${Math.round(value * 100)}%`} className="inline-flex">
        <Badge tone={TONE[label]}>{text} · {Math.round(value * 100)}%</Badge>
      </button>
      {open && (
        <span role="dialog" aria-label="Why this confidence" className="absolute left-0 top-7 z-20 w-80 rounded-lg border border-slate-200 bg-white p-3 text-xs shadow-lg dark:border-white/10 dark:bg-slate-900">
          <span className="block font-medium">How this is scored</span>
          <span className="mt-1 block text-slate-600 dark:text-slate-400">
            Each section is checked: do its citations point at real code, do the names it mentions exist{calibrated ? ', and does the cited code support it (judged by Jev, calibrated)' : ''}. The document gets its weakest section’s score.
          </span>
          {why?.length ? (
            <ul className="mt-2 list-disc space-y-0.5 pl-4">{why.map((w) => <li key={w}>{w}</li>)}</ul>
          ) : <span className="mt-2 block text-emerald-700 dark:text-emerald-400">Nothing found to doubt.</span>}
        </span>
      )}
    </span>
  );
}

/** codeURL links a cited line to the code on the git host, when the host is known. */
function codeURL(repo: Repo | undefined, sha: string, path: string, line: string) {
  if (!repo) return undefined;
  if (repo.connector_type === 'github') return `https://github.com/${repo.full_name}/blob/${sha}/${path}#L${line}`;
  if (repo.connector_type === 'gitlab') return `https://gitlab.com/${repo.full_name}/-/blob/${sha}/${path}#L${line}`;
  return undefined;
}

const CITE = /\[([A-Za-z0-9_./@+-]+\.[A-Za-z0-9]+|[A-Za-z0-9_./@+-]*(?:Dockerfile|Makefile|Jenkinsfile|Procfile)):(\d+)(?:-(\d+))?\]/g;

/** withCitations turns [path:line] into links to the code (or inline code without a known host). */
export function withCitations(md: string, link: (path: string, line: string) => string | undefined) {
  return md.replace(CITE, (_m, p: string, l: string) => {
    const url = link(p, l);
    return url ? `[\`${p}:${l}\`](${url})` : `\`${p}:${l}\``;
  });
}

function DocView({ repo, type, docKey }: { repo?: Repo; type: string; docKey: string }) {
  const doc = useRepoDoc(repo?.id, type, docKey);
  const d = doc.data;
  if (doc.isLoading) return <Spinner />;
  if (doc.error) return <ErrorNote error={doc.error} />;
  if (!d) return null;
  const link = (p: string, l: string) => codeURL(repo, d.source_sha, p, l);
  const changed = Math.round((d.changed ?? 0) * 100);
  return (
    <article className="min-w-0">
      <div className="flex flex-wrap items-center gap-2 text-xs text-slate-500">
        <span>{d.group === 'Modules' ? 'Module guide' : d.group}</span>
        <span aria-hidden>·</span>
        <span>Written {relTime(d.updated_at)} from <code>{shortSha(d.source_sha)}</code></span>
      </div>
      <h1 className="mt-1 text-2xl font-semibold tracking-tight">{d.title}</h1>
      <div className="mt-2 flex flex-wrap items-center gap-2">
        <ConfidenceBadge label={d.label} value={d.confidence} why={d.why} calibrated={d.calibrated} />
        {changed >= 5 && (
          <span className="inline-flex items-center gap-1 text-xs text-amber-700 dark:text-amber-300"><Info className="h-3.5 w-3.5" aria-hidden />{changed}% of its code changed since it was written</span>
        )}
      </div>
      {d.status !== 'ok' ? (
        <p className="mt-4 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-800 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200">This document could not be written: {d.error}</p>
      ) : (
        <>
          <section aria-label="At a glance" className="mt-5 rounded-xl border border-brand-100 bg-brand-50/60 p-4 text-[15px] leading-relaxed dark:border-brand-400/20 dark:bg-brand-500/10">
            <p className="mb-1 text-xs font-semibold uppercase tracking-wide text-brand-700 dark:text-brand-300">At a glance</p>
            {d.at_a_glance}
          </section>
          <div className="mt-6 grid gap-8 xl:grid-cols-[minmax(0,1fr)_14rem]">
            <div className="min-w-0 space-y-8">
              {d.sections.map((s) => (
                <section key={s.key} id={s.key} aria-labelledby={`h-${s.key}`} className="scroll-mt-20">
                  <div className="mb-2 flex flex-wrap items-center gap-2">
                    <h2 id={`h-${s.key}`} className="text-lg font-semibold">{s.title}</h2>
                    {s.label !== 'high' && (
                      <span title={(s.why ?? []).join('\n')} className={cx('inline-flex items-center gap-1 text-xs', s.label === 'low' ? 'text-red-700 dark:text-red-300' : 'text-amber-700 dark:text-amber-300')}>
                        <AlertTriangle className="h-3.5 w-3.5" aria-hidden />Check against the code{s.why?.[0] ? `: ${s.why[0]}` : ''}
                      </span>
                    )}
                  </div>
                  <Markdown>{withCitations(s.markdown, link)}</Markdown>
                </section>
              ))}
              {d.gaps && d.gaps.length > 0 && (
                <section aria-label="Not determined from the code" className="rounded-lg border border-slate-200 p-4 text-sm dark:border-white/10">
                  <h2 className="font-semibold">Not determined from the code</h2>
                  <ul className="mt-2 list-disc space-y-1 pl-5 text-slate-600 dark:text-slate-400">{d.gaps.map((g) => <li key={g}>{g}</li>)}</ul>
                </section>
              )}
            </div>
            <nav aria-label="On this page" className="hidden xl:block">
              <div className="sticky top-4 text-sm">
                <p className="mb-2 text-xs font-semibold uppercase tracking-wide text-slate-500">On this page</p>
                <ul className="space-y-1">
                  {d.sections.map((s) => (
                    <li key={s.key}><a href={`#${s.key}`} className="flex items-center gap-2 text-slate-600 hover:text-brand-600 dark:text-slate-400"><span className={cx('h-1.5 w-1.5 rounded-full', DOT[s.label])} aria-hidden />{s.title}</a></li>
                  ))}
                </ul>
              </div>
            </nav>
          </div>
        </>
      )}
    </article>
  );
}

function DocNav({ items, repoId, active }: { items: RepoDocSummary[]; repoId: string; active?: string }) {
  const groups = useMemo(() => {
    const by: Record<string, RepoDocSummary[]> = {};
    for (const it of items) (by[it.group] ??= []).push(it);
    return GROUPS.filter((g) => by[g]?.length).map((g) => ({ group: g, items: by[g] }));
  }, [items]);
  return (
    <nav aria-label="Documents" className="space-y-5 text-sm">
      {groups.map(({ group, items: gi }) => (
        <div key={group}>
          <p className="mb-1 px-2 text-xs font-semibold uppercase tracking-wide text-slate-500">{group}</p>
          <ul className="space-y-0.5">
            {gi.map((it) => {
              const id = `${it.type}/${it.key}`;
              return (
                <li key={id}>
                  <Link to={`/docs/r/${repoId}/${it.type}/${encodeURIComponent(it.key)}`} aria-current={active === id ? 'page' : undefined}
                    className={cx('flex items-center gap-2 rounded-md px-2 py-1.5', active === id ? 'bg-brand-50 font-medium text-brand-700 dark:bg-brand-500/15 dark:text-brand-200' : 'text-slate-700 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-white/5')}>
                    <span className={cx('h-2 w-2 shrink-0 rounded-full', it.status === 'ok' ? DOT[it.label] : 'bg-slate-300')} title={it.status === 'ok' ? `${it.label} confidence` : 'Could not be written'} aria-hidden />
                    <span className="truncate">{it.title}</span>
                  </Link>
                </li>
              );
            })}
          </ul>
        </div>
      ))}
    </nav>
  );
}

function Writing({ list }: { list?: RepoDocsList }) {
  const job = list?.job;
  if (!job || !['queued', 'processing'].includes(job.status)) return null;
  const p = job.progress;
  return (
    <div role="status" className="mb-4 flex flex-wrap items-center gap-3 rounded-lg border border-brand-200 bg-brand-50/60 px-3 py-2 text-sm dark:border-brand-400/20 dark:bg-brand-500/10">
      <RefreshCw className="h-4 w-4 animate-spin text-brand-600" aria-hidden />
      <span>{job.status === 'queued' ? 'Documents are queued: they are written after the code is read.' : p ? `Writing ${p.stage} (${p.done} of ${p.total})` : 'Reading the code…'}</span>
    </div>
  );
}

function Actions({ repo, list }: { repo: Repo; list?: RepoDocsList }) {
  const me = useMe();
  const admin = atLeast(me.data?.role, 'admin');
  const [cap, setCap] = useState<string>();
  const [exported, setExported] = useState<{ url: string; number: number }>();
  const write = useInvalidating((full: boolean) => api.post(`/repos/${repo.id}/docs/write`, { full }), listKey(repo.id));
  const exp = useInvalidating(async () => setExported(await api.post<{ url: string; number: number }>(`/repos/${repo.id}/docs/export`)), listKey(repo.id));
  const budget = useInvalidating((c: number) => api.put(`/repos/${repo.id}/docs/budget`, { cap_usd: c }), listKey(repo.id));
  const b = list?.budget;
  const pct = b && b.cap_usd > 0 ? b.spent_usd / b.cap_usd : 0;
  return (
    <div className="flex flex-wrap items-center gap-2">
      {b && b.cap_usd > 0 && (
        <span className={cx('text-xs', pct >= 0.8 ? 'font-medium text-amber-700 dark:text-amber-300' : 'text-slate-500')} title="What writing this repository’s documents cost this month, and its monthly budget">
          Docs budget: {usd(b.spent_usd)} of {usd(b.cap_usd)} this month{pct >= 1 ? ' (paused)' : pct >= 0.8 ? ' (80% used)' : ''}
        </span>
      )}
      {admin && (
        <>
          {cap === undefined ? (
            <Button size="sm" variant="ghost" onClick={() => setCap(String(b?.cap_usd ?? ''))}>Change budget</Button>
          ) : (
            <span className="inline-flex items-center gap-1">
              <Input className="h-8 w-24" type="number" min={0} aria-label="Monthly docs budget in US dollars" value={cap} onChange={(e) => setCap(e.target.value)} />
              <Button size="sm" onClick={async () => { await budget.mutateAsync(Number(cap)); setCap(undefined); }}>Save</Button>
            </span>
          )}
          <Button size="sm" variant="secondary" disabled={write.isPending} onClick={() => write.mutate(false)}>Write changed docs now</Button>
          <Button size="sm" variant="ghost" disabled={write.isPending} onClick={() => { if (confirm('Rewrite every document? It costs about as much as the first run.')) write.mutate(true); }}>Rewrite all</Button>
          <Button size="sm" variant="ghost" disabled={exp.isPending || !list?.items.length} onClick={() => exp.mutate(undefined)}><FileDown className="mr-1 h-4 w-4" aria-hidden />Export to repository</Button>
        </>
      )}
      {exported && <a className="text-xs text-brand-600 underline" href={exported.url} target="_blank" rel="noreferrer">Pull request #{exported.number}</a>}
      <ErrorNote error={write.error ?? exp.error ?? budget.error} />
    </div>
  );
}

function Empty({ repo }: { repo: Repo }) {
  const me = useMe();
  const routes = useRoutes();
  const admin = atLeast(me.data?.role, 'admin');
  const [estimate, setEstimate] = useState<{ documents: number; estimated_usd: number; modules: number }>();
  const [err, setErr] = useState<unknown>();
  const write = useInvalidating(() => api.post(`/repos/${repo.id}/docs/write`, { full: true }), listKey(repo.id));
  const routed = routes.data?.items.some((r) => r.feature === 'docgen');
  useEffect(() => {
    if (!routed || !repo.last_processed_sha) return;
    api.post<{ documents: number; estimated_usd: number; modules: number }>(`/repos/${repo.id}/docs/estimate`).then(setEstimate, setErr);
  }, [repo.id, repo.last_processed_sha, routed]);
  return (
    <div className="rounded-2xl border border-slate-200/80 bg-white/70 p-8 text-center shadow-card dark:border-white/[0.06] dark:bg-slate-900/40">
      <span className="mx-auto grid h-12 w-12 place-items-center rounded-2xl bg-brand-50 text-brand-600 dark:bg-brand-500/15 dark:text-brand-300"><BookOpen className="h-6 w-6" aria-hidden /></span>
      <h2 className="mt-4 text-lg font-semibold">No documents for {repo.full_name} yet</h2>
      {!repo.last_processed_sha ? (
        <p className="mt-1 text-sm text-slate-500">The code has not been read yet. Documents follow the first sync.</p>
      ) : admin && routes.data && !routed ? (
        <>
          <p className="mt-1 text-sm text-slate-500">No model is set to write docs yet.</p>
          <Link to="/providers" className="mt-4 inline-flex rounded-lg bg-brand-600 px-4 py-2 text-sm font-medium text-white hover:bg-brand-700">Set up a model</Link>
        </>
      ) : (
        <>
          <p className="mx-auto mt-1 max-w-xl text-sm text-slate-500">
            The Hub writes an overview, the whole architecture, a guide per module, and the flows, API, data model, tests, runbooks and more that apply to this code. Every claim cites the code and gets a confidence score.
          </p>
          {estimate && <p className="mt-3 text-sm">About {estimate.documents} documents ({estimate.modules} modules){estimate.estimated_usd > 0 ? `, roughly ${usd(estimate.estimated_usd)}` : ''}.</p>}
          {admin && <Button className="mt-4" disabled={write.isPending} onClick={() => write.mutate(undefined)}>Write the documents</Button>}
          <ErrorNote error={err ?? write.error} />
        </>
      )}
    </div>
  );
}

/** RepoDocsPage is the Docs page when documents are written per repository (Docs v2). */
export default function RepoDocsPage() {
  const { repoId, type, key } = useParams();
  const nav = useNavigate();
  const repos = useRepos();
  const repo = repos.data?.find((r) => r.id === repoId) ?? repos.data?.[0];
  const list = useRepoDocs(repo?.id);
  const items = list.data?.items ?? [];
  const current = type ? `${type}/${key ?? type}` : undefined;
  useEffect(() => {
    if (repo && !type && items.length) {
      const first = items.find((i) => i.type === 'overview') ?? items[0];
      nav(`/docs/r/${repo.id}/${first.type}/${encodeURIComponent(first.key)}`, { replace: true });
    }
  }, [repo, type, items, nav]);
  return (
    <>
      <PageHeader title="Docs" description="Readable documentation for each repository: what it is, how it is built, and how each part works. Written from the code and checked against it."
        actions={repos.data && repos.data.length > 1 ? (
          <Select aria-label="Repository" value={repo?.id ?? ''} onChange={(e) => nav(`/docs/r/${e.target.value}`)}>
            {repos.data.map((r) => <option key={r.id} value={r.id}>{r.full_name}</option>)}
          </Select>
        ) : undefined} />
      {repos.isLoading && <Spinner />}
      {repos.data?.length === 0 && <p className="text-sm text-slate-500">Track a repository under <Link className="underline" to="/repos">Repositories</Link> and its documents appear here.</p>}
      {repo && (
        <>
          <div className="mb-4"><Actions repo={repo} list={list.data} /></div>
          <Writing list={list.data} />
          {list.isLoading ? <Spinner /> : items.length === 0 ? <Empty repo={repo} /> : (
            <div className="grid gap-8 lg:grid-cols-[15rem_minmax(0,1fr)]">
              <aside className="lg:sticky lg:top-4 lg:max-h-[calc(100vh-2rem)] lg:overflow-y-auto"><DocNav items={items} repoId={repo.id} active={current} /></aside>
              {type ? <DocView repo={repo} type={type} docKey={key ?? type} /> : <Spinner />}
            </div>
          )}
          {list.data?.job?.status === 'done' && items.some((i) => i.status !== 'ok') && (
            <p className="mt-4 inline-flex items-center gap-1 text-xs text-slate-500"><CheckCircle2 className="h-3.5 w-3.5" aria-hidden />Some documents could not be written; they are tried again when their code changes, or with “Write changed docs now”.</p>
          )}
          <ErrorNote error={list.error} />
        </>
      )}
    </>
  );
}
