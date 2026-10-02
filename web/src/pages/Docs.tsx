import { BookOpen, ChevronRight, FileText, Folder, FolderGit2, FolderOpen, Hash, Plug, Sparkles } from 'lucide-react';
import { useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { api } from '@/api/client';
import { useDocNode, useMe, useRepos, useRoutes, useTree } from '@/api/hooks';
import { atLeast, type TreeNode } from '@/api/types';
import { Markdown } from '@/components/Markdown';
import { Badge, Button, Card, ErrorNote, PageHeader, Spinner, cx } from '@/components/ui';
import { relTime, shortSha } from '@/lib/format';

const rowCls = 'group flex w-full items-center gap-1.5 rounded-md py-1 pr-2 text-left text-[13px] transition-colors';

function NodeIcon({ node, open }: { node: TreeNode; open: boolean }) {
  const cls = 'h-4 w-4 shrink-0';
  if (node.kind === 'repo') return <FolderGit2 className={cx(cls, 'text-brand-500')} aria-hidden />;
  if (node.kind === 'dir') return open ? <FolderOpen className={cx(cls, 'text-amber-500')} aria-hidden /> : <Folder className={cx(cls, 'text-amber-500')} aria-hidden />;
  if (node.kind === 'section') return <Hash className={cx(cls, 'text-slate-400')} aria-hidden />;
  return <FileText className={cx(cls, 'text-slate-400')} aria-hidden />;
}

/** Branch is one row of the explorer; folders load their children when opened. */
function Branch({ node, repoId, selected, onSelect, depth, defaultOpen = false }: {
  node: TreeNode;
  repoId: string;
  selected?: string;
  onSelect: (n: TreeNode) => void;
  depth: number;
  defaultOpen?: boolean;
}) {
  const [open, setOpen] = useState(defaultOpen);
  const isRepo = node.kind === 'repo';
  const expandable = isRepo || (node.has_children && node.kind !== 'file');
  const children = useTree(open && expandable ? repoId : undefined, open && expandable && !isRepo ? node.id : undefined);
  const active = selected === node.id;
  return (
    <li>
      <button
        type="button"
        className={cx(
          rowCls,
          isRepo && 'font-semibold',
          active ? 'bg-brand-50 font-medium text-brand-800 dark:bg-brand-500/15 dark:text-white' : 'text-slate-700 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-white/[0.05]',
        )}
        style={{ paddingLeft: depth * 14 + 6 }}
        onClick={() => (expandable ? setOpen(!open) : onSelect(node))}
        aria-expanded={expandable ? open : undefined}
        title={node.path || node.title}
      >
        <ChevronRight className={cx('h-3.5 w-3.5 shrink-0 text-slate-400 transition-transform', open && 'rotate-90', !expandable && 'invisible')} aria-hidden />
        <NodeIcon node={node} open={open} />
        <span className="truncate">{node.title}</span>
      </button>
      {open && expandable && (
        <ul className="relative">
          {/* indent guide */}
          <span aria-hidden className="absolute bottom-1 top-0 border-l border-slate-200 dark:border-white/[0.07]" style={{ left: depth * 14 + 13 }} />
          {children.isLoading && <li className="py-1 text-xs text-slate-400" style={{ paddingLeft: (depth + 1) * 14 + 26 }}>Loading…</li>}
          {children.data?.length === 0 && <li className="py-1 text-xs text-slate-400" style={{ paddingLeft: (depth + 1) * 14 + 26 }}>Empty</li>}
          {children.data?.map((c) => (
            <Branch key={c.id} node={c} repoId={repoId} selected={selected} onSelect={onSelect} depth={depth + 1} />
          ))}
        </ul>
      )}
    </li>
  );
}

/** NoDocs explains where documentation comes from, with the next step for this user. */
function NoDocs() {
  const me = useMe();
  const repos = useRepos();
  const routes = useRoutes();
  const admin = atLeast(me.data?.role, 'admin');
  const [queued, setQueued] = useState<string[]>([]);
  const [err, setErr] = useState<unknown>();
  const tracked = repos.data ?? [];
  const docgenRouted = routes.data?.items.some((r) => r.feature === 'docgen');
  if (tracked.length > 0) {
    const generate = async () => {
      setErr(undefined);
      try {
        for (const r of tracked) await api.post(`/repos/${r.id}/generate-docs`, {});
        setQueued(tracked.map((r) => r.full_name));
      } catch (e) {
        setErr(e);
      }
    };
    return (
      <div className="rounded-2xl border border-slate-200/80 bg-white/70 p-8 shadow-card dark:border-white/[0.06] dark:bg-slate-900/40">
        <div className="mx-auto max-w-2xl text-center">
          <span className="mx-auto grid h-12 w-12 place-items-center rounded-2xl bg-brand-50 text-brand-600 dark:bg-brand-500/15 dark:text-brand-300">
            <BookOpen className="h-6 w-6" aria-hidden />
          </span>
          <h2 className="mt-4 text-lg font-semibold">{queued.length ? 'Writing docs…' : 'No docs yet'}</h2>
          {queued.length > 0 ? (
            <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
              Docs are being written for {queued.join(', ')}. Files appear here as they finish (a large repository takes a while); progress is under <Link className="underline" to="/activity">Activity</Link>.
            </p>
          ) : admin && routes.data && !docgenRouted ? (
            <>
              <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
                You track {tracked.length} repositor{tracked.length === 1 ? 'y' : 'ies'}, but no model is set to write docs yet. Add a provider (or route “docgen” to one); docs are then written automatically.
              </p>
              <Link to="/providers" className="mt-4 inline-flex items-center gap-2 rounded-lg bg-brand-600 px-4 py-2 text-sm font-medium text-white hover:bg-brand-700">Set up a model</Link>
            </>
          ) : (
            <>
              <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
                You track {tracked.map((r) => r.full_name).join(', ')}. Docs are written on each push; code synced before a model was set up has no docs yet.
              </p>
              {atLeast(me.data?.role, 'admin') && <Button className="mt-4" onClick={() => void generate()}>Generate docs now</Button>}
              <ErrorNote error={err} />
            </>
          )}
        </div>
      </div>
    );
  }
  const steps = [
    { icon: Plug, title: 'Connect GitHub or GitLab', text: 'One click with “Connect with GitHub”.', to: '/connectors', show: admin },
    { icon: FolderGit2, title: 'Track a repository', text: 'Pick which repositories the Hub documents.', to: '/repos', show: true },
    { icon: Sparkles, title: 'Push, or import', text: 'Docs are written on the next push. Already have Markdown docs? Import them from the repository’s page.', to: '/repos', show: true },
  ].filter((x) => x.show);
  return (
    <div className="rounded-2xl border border-slate-200/80 bg-white/70 p-8 shadow-card dark:border-white/[0.06] dark:bg-slate-900/40">
      <div className="mx-auto max-w-2xl text-center">
        <span className="mx-auto grid h-12 w-12 place-items-center rounded-2xl bg-brand-50 text-brand-600 dark:bg-brand-500/15 dark:text-brand-300">
          <BookOpen className="h-6 w-6" aria-hidden />
        </span>
        <h2 className="mt-4 text-lg font-semibold">No docs yet</h2>
        <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
          Documentation appears here per repository, as files you can browse. It is written from your code and kept up to date on every push.
        </p>
      </div>
      <ol className="mx-auto mt-6 grid max-w-3xl gap-3 sm:grid-cols-3">
        {steps.map((st, i) => (
          <li key={st.title}>
            <Link to={st.to} className="block h-full rounded-xl border border-slate-200 p-4 transition-colors hover:border-brand-400 hover:bg-brand-50/40 dark:border-white/10 dark:hover:bg-brand-500/10">
              <span className="flex items-center gap-2 text-xs font-medium text-slate-400"><st.icon className="h-4 w-4 text-brand-500" aria-hidden />Step {i + 1}</span>
              <span className="mt-2 block text-sm font-medium">{st.title}</span>
              <span className="mt-1 block text-xs text-slate-500 dark:text-slate-400">{st.text}</span>
            </Link>
          </li>
        ))}
      </ol>
    </div>
  );
}

export default function Docs() {
  const { nodeId } = useParams();
  const nav = useNavigate();
  const roots = useTree();
  const node = useDocNode(nodeId);
  return (
    <>
      <PageHeader title="Docs" description="Generated and imported documentation, per repository. Edit generated files only inside dth:human blocks — those survive regeneration." />
      {roots.isLoading && <Spinner />}
      <ErrorNote error={roots.error} />
      {roots.data?.length === 0 && <NoDocs />}
      {!!roots.data?.length && (
        <div className="grid gap-6 lg:grid-cols-[17rem_minmax(0,1fr)]">
          <nav aria-label="Documentation" className="self-start rounded-2xl border border-slate-200/80 bg-white/70 shadow-card lg:sticky lg:top-6 dark:border-white/[0.06] dark:bg-slate-900/40">
            <div className="border-b border-slate-200/80 p-2 dark:border-white/[0.06]">
              <p className="px-2 pb-1.5 pt-1 text-[11px] font-semibold uppercase tracking-[0.1em] text-slate-400">Explorer</p>
            </div>
            <ul className="max-h-[calc(100vh-12rem)] overflow-y-auto p-2">
              {roots.data.map((r) => (
                <Branch key={r.id} node={r} repoId={r.repo_id!} selected={nodeId} onSelect={(n) => nav(`/docs/${n.id}`)} depth={0} defaultOpen={roots.data!.length === 1} />
              ))}
            </ul>
          </nav>
          <div className="min-w-0">
            {!nodeId && (
              <div className="grid min-h-[16rem] place-items-center rounded-2xl border border-slate-200/80 bg-white/40 p-8 text-center dark:border-white/[0.06] dark:bg-slate-900/20">
                <div>
                  <FileText className="mx-auto h-7 w-7 text-slate-300 dark:text-slate-600" aria-hidden />
                  <p className="mt-3 text-sm font-medium">Choose a page in the explorer</p>
                  <p className="mt-1 text-xs text-slate-500">Open a repository, then a folder, then a file.</p>
                </div>
              </div>
            )}
            {node.isLoading && <Spinner />}
            <ErrorNote error={node.error} />
            {node.data && (
              <Card
                title={
                  <span className="font-mono text-xs">
                    {node.data.repo} / {node.data.path}
                  </span>
                }
                actions={
                  <span className="text-xs text-slate-500">
                    {node.data.commit_sha && <>at {shortSha(node.data.commit_sha)} · </>}updated {relTime(node.data.updated_at)}
                  </span>
                }
              >
                {node.data.summary && <p className="mb-3 text-sm text-slate-600 dark:text-slate-400">{node.data.summary}</p>}
                {node.data.markdown ? <Markdown>{node.data.markdown}</Markdown> : <p className="text-sm text-slate-500">No content stored for this node.</p>}
                {node.data.chunks.length > 0 && (
                  <div className="mt-4 flex flex-wrap gap-1 border-t border-slate-200 pt-3 dark:border-slate-800">
                    {node.data.chunks.map((c) => (
                      <Badge key={c.chunk_id} tone="blue">
                        {c.symbol}
                      </Badge>
                    ))}
                  </div>
                )}
              </Card>
            )}
          </div>
        </div>
      )}
    </>
  );
}
