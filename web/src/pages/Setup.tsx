import { Link } from 'react-router-dom';
import { ArrowRight, CircleCheck, Cpu, FileSearch, FolderGit2, Plug, Route, Wallet, type LucideIcon } from 'lucide-react';
import { useConnectors, useLimits, useProviders, useRepos, useRoutes } from '@/api/hooks';
import { PageHeader, Spinner, cx } from '@/components/ui';

interface Step {
  title: string;
  done: boolean;
  detail: string;
  to: string;
  action: string;
  icon: LucideIcon;
}

/** Setup is the first-run checklist: each step reads live state, so it doubles as a health view. */
export default function Setup() {
  const conns = useConnectors();
  const providers = useProviders();
  const routes = useRoutes();
  const repos = useRepos();
  const limits = useLimits();
  if (conns.isLoading || providers.isLoading || routes.isLoading || repos.isLoading) return <Spinner />;
  const routed = new Set(routes.data?.items.map((r) => r.feature));
  const git = conns.data?.filter((c) => c.type === 'github' || c.type === 'gitlab') ?? [];
  const steps: Step[] = [
    { title: 'Connect GitHub or GitLab', done: git.length > 0, detail: git.length ? `${git.length} git connector(s)` : 'Token or GitHub App; webhooks or polling.', to: '/connectors', action: 'Connectors', icon: Plug },
    { title: 'Add your LLM provider', done: (providers.data?.items.length ?? 0) > 0, detail: 'Your own key: Anthropic, OpenAI, Azure, Bedrock, Vertex, Ollama, or compatible.', to: '/providers', action: 'Providers', icon: Cpu },
    { title: 'Route docgen, Q&A, and embeddings', done: ['docgen', 'qa', 'embedding'].every((f) => routed.has(f)), detail: `Routed: ${[...routed].join(', ') || 'none'}`, to: '/providers', action: 'Routing', icon: Route },
    { title: 'Set spend limits', done: (limits.data?.length ?? 0) > 0, detail: 'A global daily ceiling is seeded; tune per feature or repository.', to: '/spend', action: 'Spend', icon: Wallet },
    { title: 'Track repositories', done: (repos.data?.length ?? 0) > 0, detail: `${repos.data?.length ?? 0} tracked`, to: '/repos', action: 'Repositories', icon: FolderGit2 },
    { title: 'Dry run, then import existing docs', done: (repos.data ?? []).some((r) => !!r.last_processed_sha), detail: 'See the cost of the first push before it runs; import Markdown so Ask works on day one.', to: '/repos', action: 'Repositories', icon: FileSearch },
  ];
  const done = steps.filter((s) => s.done).length;
  const next = steps.findIndex((s) => !s.done);
  const pct = Math.round((done / steps.length) * 100);
  return (
    <>
      <PageHeader title="Setup" description={`${done} of ${steps.length} steps done.`} />
      {done === steps.length && (
        <p className="-mt-4 mb-6 inline-flex items-center gap-2 rounded-lg bg-emerald-50 px-3 py-1.5 text-sm text-emerald-800 dark:bg-emerald-500/10 dark:text-emerald-300">
          <CircleCheck className="h-4 w-4" aria-hidden /> Everything is set up — this page stays as a health check.
        </p>
      )}
      <div className="max-w-3xl">
        <div className="mb-6" aria-label={`Setup progress ${pct}%`}>
          <div className="mb-2 flex justify-between text-xs font-medium text-slate-500"><span>Progress</span><span>{pct}%</span></div>
          <div className="h-2 overflow-hidden rounded-full bg-slate-200 dark:bg-white/[0.06]">
            <div className="h-full rounded-full bg-gradient-to-r from-brand-500 to-violet-500 transition-all" style={{ width: `${pct}%` }} />
          </div>
        </div>
        <ol className="space-y-3">
          {steps.map((s, i) => {
            const isNext = i === next;
            return (
              <li
                key={s.title}
                className={cx(
                  'flex items-center gap-4 rounded-xl border bg-white p-4 shadow-card transition-colors dark:bg-slate-900/60',
                  isNext ? 'border-brand-300 ring-1 ring-brand-200 dark:border-brand-400/40 dark:ring-brand-400/20' : 'border-slate-200/80 dark:border-white/[0.07]',
                )}
              >
                <span
                  className={cx(
                    'grid h-10 w-10 shrink-0 place-items-center rounded-xl ring-1 ring-inset',
                    s.done ? 'bg-emerald-50 text-emerald-600 ring-emerald-100 dark:bg-emerald-500/15 dark:text-emerald-300 dark:ring-emerald-400/20'
                      : isNext ? 'bg-brand-50 text-brand-600 ring-brand-100 dark:bg-brand-500/15 dark:text-brand-300 dark:ring-brand-400/20'
                        : 'bg-slate-100 text-slate-400 ring-slate-200 dark:bg-white/[0.05] dark:text-slate-500 dark:ring-white/10',
                  )}
                >
                  {s.done ? <CircleCheck className="h-5 w-5" aria-label="done" /> : <s.icon className="h-5 w-5" aria-hidden />}
                </span>
                <div className="min-w-0 flex-1">
                  <p className="font-medium">
                    <span className="mr-1.5 text-slate-400">{i + 1}.</span>
                    {s.title}
                    {s.done && <span className="ml-2 rounded-md bg-emerald-100 px-1.5 py-0.5 text-xs font-medium text-emerald-800 dark:bg-emerald-500/15 dark:text-emerald-300">done</span>}
                    {isNext && <span className="ml-2 rounded-md bg-brand-100 px-1.5 py-0.5 text-xs font-medium text-brand-700 dark:bg-brand-500/20 dark:text-brand-200">next</span>}
                  </p>
                  <p className="mt-0.5 text-sm text-slate-500 dark:text-slate-400">{s.detail}</p>
                </div>
                <Link
                  to={s.to}
                  className={cx(
                    'inline-flex shrink-0 items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium transition-colors',
                    isNext ? 'bg-brand-600 text-white hover:bg-brand-700 dark:bg-brand-500 dark:hover:bg-brand-400' : 'text-brand-600 hover:bg-brand-50 dark:text-brand-300 dark:hover:bg-brand-500/10',
                  )}
                >
                  {s.action} <ArrowRight className="h-4 w-4" aria-hidden />
                </Link>
              </li>
            );
          })}
        </ol>
      </div>
    </>
  );
}
