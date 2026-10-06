import { Link } from 'react-router-dom';
import { ArrowRight, BookOpen, ChevronDown, CircleCheck, Cpu, FolderGit2, KeyRound, Plug, type LucideIcon } from 'lucide-react';
import { useState } from 'react';
import { useAuthConfig, useConnectors, useLimits, useMe, useProviders, useRepos, useRoutes, useTree } from '@/api/hooks';
import { PageHeader, Spinner, cx } from '@/components/ui';
import { featureLabel } from '@/lib/labels';

interface Step {
  title: string;
  done: boolean;
  /** What it does, in a sentence. */
  why: string;
  /** What you need, and where to get it. */
  need: string;
  status?: string;
  to: string;
  action: string;
  icon: LucideIcon;
  /** A secondary way to mark the step done. */
  skip?: { label: string; onClick: () => void };
}

const SIGNIN_OK = 'dth.setup.passwords-enough';
const readFlag = (k: string) => { try { return localStorage.getItem(k) === '1'; } catch { return false; } };

/**
 * Setup is the first-run checklist: three steps to useful docs and answers, each saying what you need and
 * where to get it. Every step reads live state, so the page doubles as a health view.
 */
export default function Setup() {
  const conns = useConnectors();
  const providers = useProviders();
  const routes = useRoutes();
  const repos = useRepos();
  const limits = useLimits();
  const docs = useTree();
  const me = useMe();
  const authCfg = useAuthConfig();
  const [more, setMore] = useState(false);
  const [passwordsEnough, setPasswordsEnough] = useState(() => readFlag(SIGNIN_OK));
  if (conns.isLoading || providers.isLoading || routes.isLoading || repos.isLoading) return <Spinner />;
  const routed = new Set(routes.data?.items.map((r) => r.feature));
  const git = conns.data?.filter((c) => c.type === 'github' || c.type === 'gitlab') ?? [];
  const tracked = repos.data ?? [];
  const hasDocs = (docs.data?.length ?? 0) > 0;
  const owner = me.data?.role === 'owner';
  const sso = !!authCfg.data?.sso;
  const signIn: Step[] = owner ? [{
    title: 'Choose how your team signs in',
    done: sso || passwordsEnough,
    why: 'Single sign-on lets people use their company account (Google, Microsoft, Okta, Keycloak). It can be set up before anything else.',
    need: 'An app in your identity provider: the page shows the steps for each one and the callback URL to register. Or keep email and password and send people links.',
    status: sso ? 'Single sign-on is on' : passwordsEnough ? 'Email and password' : undefined,
    to: '/sign-in', action: 'Set up sign-in', icon: KeyRound,
    skip: sso ? undefined : { label: 'Passwords are enough', onClick: () => { try { localStorage.setItem(SIGNIN_OK, '1'); } catch { /* private mode */ } setPasswordsEnough(true); } },
  }] : [];
  const steps: Step[] = [
    ...signIn,
    {
      title: 'Connect GitHub',
      done: git.length > 0,
      why: 'So the Hub can read your code and keep docs up to date on every push.',
      need: 'Nothing to copy: “Connect with GitHub” creates a private GitHub App and you pick the repositories. GitLab works with a token.',
      status: git.length ? `Connected: ${git.map((c) => c.name).join(', ')}` : undefined,
      to: '/connectors', action: 'Connect', icon: Plug,
    },
    {
      title: 'Add a model',
      done: routed.has('docgen') && routed.has('qa'),
      why: 'The model writes the docs and answers questions. You use your own account; nothing is shared.',
      need: 'An API key, e.g. from console.anthropic.com → API keys (or OpenAI, Azure, Bedrock, Vertex, or local Ollama with no key). The form links to the right page.',
      status: routed.size ? `Used for: ${[...routed].map(featureLabel).join(', ')}` : (providers.data?.items.length ? 'A provider is added, but not yet used for docs and answers' : undefined),
      to: '/providers', action: 'Add a model', icon: Cpu,
    },
    {
      title: 'Choose repositories',
      done: tracked.length > 0,
      why: 'Only the repositories you choose are documented and searchable.',
      need: 'Pick them right after connecting GitHub, or here.',
      status: tracked.length ? `${tracked.length} tracked: ${tracked.slice(0, 3).map((r) => r.full_name).join(', ')}${tracked.length > 3 ? '…' : ''}` : undefined,
      to: '/repos', action: 'Choose', icon: FolderGit2,
    },
    {
      title: 'Docs are written',
      done: hasDocs,
      why: 'The first docs are written automatically once the steps above are done; then on every push.',
      need: tracked.length && routed.has('docgen') && !hasDocs ? 'Nothing yet? Use “Generate docs” on the Repositories page.' : 'Nothing to do.',
      status: hasDocs ? `${docs.data?.length} repositor${docs.data?.length === 1 ? 'y has' : 'ies have'} docs` : undefined,
      to: hasDocs ? '/docs' : '/repos', action: hasDocs ? 'Open Docs' : 'Repositories', icon: BookOpen,
    },
  ];
  const done = steps.filter((s) => s.done).length;
  const next = steps.findIndex((s) => !s.done);
  const optional = [
    { title: 'Monthly budget', text: limits.data?.length ? 'A spending limit is set.' : 'Set the most the Hub may spend on models each month.', to: '/providers' },
    { title: 'Search by meaning', text: routed.has('embedding') ? 'On.' : 'Optional: choose a search model (OpenAI, Azure, Ollama…) under AI models. Word search works without it.', to: '/providers' },
    { title: 'Errors and alerts', text: 'Connect Sentry, Datadog, PagerDuty and others to explain incidents in the Inbox.', to: '/connectors' },
    { title: 'Confluence and Jira', text: 'Include wiki pages and tickets in answers.', to: '/connectors' },
    { title: 'Settings as code', text: 'Keep all of this in a YAML file with secret references.', to: '/settings-file' },
  ];
  return (
    <>
      <PageHeader title="Setup" description={done === steps.length ? 'Everything is set up. This page stays as a health check.' : `A few steps to docs and answers. ${done} of ${steps.length} done.`} />
      <div className="max-w-3xl">
        <ol className="space-y-3">
          {steps.map((s, i) => {
            const isNext = i === next;
            return (
              <li
                key={s.title}
                className={cx(
                  'flex gap-4 rounded-xl border bg-white p-4 shadow-card transition-colors dark:bg-slate-900/60',
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
                  {s.done ? <CircleCheck className="h-5 w-5" aria-label="Done" /> : <s.icon className="h-5 w-5" aria-hidden />}
                </span>
                <div className="min-w-0 flex-1">
                  <p className="font-medium">
                    <span className="mr-1.5 text-slate-400">{i + 1}.</span>
                    {s.title}
                    {s.done && <span className="ml-2 rounded-md bg-emerald-100 px-1.5 py-0.5 text-xs font-medium text-emerald-800 dark:bg-emerald-500/15 dark:text-emerald-300">Done</span>}
                    {isNext && <span className="ml-2 rounded-md bg-brand-100 px-1.5 py-0.5 text-xs font-medium text-brand-700 dark:bg-brand-500/20 dark:text-brand-200">Next</span>}
                  </p>
                  <p className="mt-0.5 text-sm text-slate-600 dark:text-slate-400">{s.why}</p>
                  {!s.done && <p className="mt-1.5 text-xs text-slate-500"><b className="font-medium text-slate-600 dark:text-slate-300">You need:</b> {s.need}</p>}
                  {s.status && <p className="mt-1.5 text-xs text-slate-500">{s.status}</p>}
                  {!s.done && s.skip && <button type="button" onClick={s.skip.onClick} className="mt-1.5 text-xs font-medium text-slate-500 underline hover:text-slate-700 dark:hover:text-slate-300">{s.skip.label}</button>}
                </div>
                <Link
                  to={s.to}
                  className={cx(
                    'inline-flex shrink-0 items-center gap-1.5 self-center rounded-lg px-3 py-1.5 text-sm font-medium transition-colors',
                    isNext ? 'bg-brand-600 text-white hover:bg-brand-700 dark:bg-brand-500 dark:hover:bg-brand-400' : 'text-brand-600 hover:bg-brand-50 dark:text-brand-300 dark:hover:bg-brand-500/10',
                  )}
                >
                  {s.action} <ArrowRight className="h-4 w-4" aria-hidden />
                </Link>
              </li>
            );
          })}
        </ol>
        <button type="button" onClick={() => setMore((m) => !m)} aria-expanded={more} className="mt-6 inline-flex items-center gap-1.5 text-sm font-medium text-slate-600 hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100">
          <ChevronDown className={cx('h-4 w-4 transition-transform', more && 'rotate-180')} aria-hidden />Optional, any time
        </button>
        {more && (
          <ul className="mt-3 grid gap-2 sm:grid-cols-2">
            {optional.map((o) => (
              <li key={o.title}>
                <Link to={o.to} className="block h-full rounded-lg border border-slate-200 p-3 hover:border-brand-400 dark:border-white/10">
                  <span className="text-sm font-medium">{o.title}</span>
                  <span className="mt-0.5 block text-xs text-slate-500">{o.text}</span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </div>
    </>
  );
}
