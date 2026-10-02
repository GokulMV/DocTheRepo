import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { API, ApiError, api, getCSRF, toApiError } from '@/api/client';
import { keys, useRepos, useThread, useThreads } from '@/api/hooks';
import type { AskResponse, Citation, Message } from '@/api/types';
import { Bug, Check, ChevronDown, FolderGit2, GitBranch, History, Network, Send, ShieldCheck, Sparkles, ThumbsDown, ThumbsUp } from 'lucide-react';
import { Markdown } from '@/components/Markdown';
import { Badge, Button, ErrorNote, cx } from '@/components/ui';
import { readSSE } from '@/lib/sse';
import { usd } from '@/lib/format';

const INCLUDES = [
  { id: 'code', label: 'Code' },
  { id: 'docs', label: 'Docs' },
] as const;

export function Citations({ items }: { items: Citation[] }) {
  if (!items?.length) return null;
  return (
    <ol className="mt-3 space-y-1 border-t border-slate-200 pt-2 text-xs dark:border-slate-800">
      {items.map((c) => (
        <li key={c.n} className="flex items-baseline gap-2">
          <span className="font-mono text-slate-400">[{c.n}]</span>
          <Badge tone={c.type === 'code' ? 'blue' : 'gray'}>{c.type}</Badge>
          <span className="truncate">
            {c.repo && <span className="text-slate-500">{c.repo} · </span>}
            {c.url ? (
              <a className="text-brand-600 underline" href={c.url} target="_blank" rel="noreferrer">
                {c.path ?? c.title}
              </a>
            ) : (
              <span className="font-mono">{c.path ?? c.title}</span>
            )}
            {c.title && c.title !== c.path && <span className="text-slate-500"> — {c.title}</span>}
          </span>
        </li>
      ))}
    </ol>
  );
}

const NOT_FOUND = 'I could not find this in the connected sources.';

/** NotFoundTips says what to try when no source answered the question. */
function NotFoundTips() {
  return (
    <div className="mt-3 rounded-lg bg-slate-50 p-3 text-xs text-slate-600 dark:bg-white/[0.04] dark:text-slate-400">
      <p className="font-medium text-slate-700 dark:text-slate-300">Nothing in your sources answered this. Things that help:</p>
      <ul className="mt-1 list-disc space-y-0.5 pl-4">
        <li>Name what you mean: a file, function, endpoint, service or error message.</li>
        <li>Check the repository has docs under <Link className="underline" to="/docs">Docs</Link>; answers draw on code, docs and READMEs.</li>
        <li>Search by meaning, not just words, by routing <b>embedding</b> to a provider under <Link className="underline" to="/providers">Providers &amp; routing</Link>.</li>
      </ul>
    </div>
  );
}

function Feedback({ m }: { m: Message }) {
  const [value, setValue] = useState(m.feedback);
  const send = async (v: 'up' | 'down') => {
    setValue(v);
    await api.post(`/messages/${m.id}/feedback`, { value: v }).catch(() => setValue(m.feedback));
  };
  return (
    <div className="mt-2 flex items-center gap-1 text-xs text-slate-500">
      <span className="mr-1">{value ? 'Thanks for the feedback' : 'Was this helpful?'}</span>
      <button type="button" aria-label="Helpful" aria-pressed={value === 'up'} title="Helpful"
        className={cx('rounded-md p-1.5 hover:bg-slate-100 dark:hover:bg-white/[0.06]', value === 'up' ? 'text-emerald-600 dark:text-emerald-400' : 'text-slate-400')} onClick={() => send('up')}>
        <ThumbsUp className={cx('h-3.5 w-3.5', value === 'up' && 'fill-current')} aria-hidden />
      </button>
      <button type="button" aria-label="Not helpful" aria-pressed={value === 'down'} title="Not helpful"
        className={cx('rounded-md p-1.5 hover:bg-slate-100 dark:hover:bg-white/[0.06]', value === 'down' ? 'text-red-600 dark:text-red-400' : 'text-slate-400')} onClick={() => send('down')}>
        <ThumbsDown className={cx('h-3.5 w-3.5', value === 'down' && 'fill-current')} aria-hidden />
      </button>
      {m.cached && <Badge>cached</Badge>}
      {m.usage && m.usage.cost_usd > 0 && <span>{usd(m.usage.cost_usd)}</span>}
    </div>
  );
}

const EXAMPLES = [
  { q: 'How does checkout handle a failed payment?', icon: GitBranch },
  { q: 'Which services publish to the orders topic?', icon: Network },
  { q: 'What is the runbook for a database failover?', icon: ShieldCheck },
  { q: 'Why is the refund job throwing NullPointerException?', icon: Bug },
];

/** ScopePicker chooses repositories (none = all you can read) and sources, in a small popover. */
function ScopePicker({ repos, repoIds, setRepoIds, include, setInclude }: {
  repos: { id: string; full_name: string }[];
  repoIds: string[];
  setRepoIds: (v: string[]) => void;
  include: string[];
  setInclude: (v: string[]) => void;
}) {
  const [open, setOpen] = useState(false);
  const label = repoIds.length === 0 ? 'All repositories' : repoIds.length === 1 ? repos.find((r) => r.id === repoIds[0])?.full_name ?? '1 repository' : `${repoIds.length} repositories`;
  const toggle = (list: string[], v: string) => (list.includes(v) ? list.filter((x) => x !== v) : [...list, v]);
  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className="inline-flex items-center gap-1.5 rounded-lg border border-slate-200 px-2.5 py-1.5 text-xs font-medium text-slate-600 hover:bg-slate-50 dark:border-white/10 dark:text-slate-300 dark:hover:bg-white/[0.04]"
      >
        <FolderGit2 className="h-3.5 w-3.5" aria-hidden />
        {label}
        {include.length > 0 && <span className="text-slate-400">· {include.join(' + ')}</span>}
        <ChevronDown className="h-3.5 w-3.5 text-slate-400" aria-hidden />
      </button>
      {open && (
        <>
          <div className="fixed inset-0 z-20" onClick={() => setOpen(false)} aria-hidden />
          <div className="absolute bottom-full left-0 z-30 mb-2 w-72 rounded-xl border border-slate-200 bg-white p-2 shadow-xl dark:border-white/10 dark:bg-slate-900" role="dialog" aria-label="Scope">
            <p className="px-2 pb-1 pt-1 text-[11px] font-semibold uppercase tracking-wider text-slate-400">Sources</p>
            <div className="flex gap-1.5 px-2 pb-2">
              {INCLUDES.map((i) => (
                <button key={i.id} type="button" aria-pressed={include.includes(i.id)} onClick={() => setInclude(toggle(include, i.id))}
                  className={cx('rounded-full border px-2.5 py-0.5 text-xs', include.includes(i.id) ? 'border-brand-400 bg-brand-50 text-brand-700 dark:bg-brand-500/15 dark:text-brand-200' : 'border-slate-200 text-slate-600 dark:border-white/10 dark:text-slate-300')}>
                  {i.label}
                </button>
              ))}
              <span className="self-center text-[11px] text-slate-400">{include.length ? '' : 'all sources'}</span>
            </div>
            <p className="px-2 pb-1 pt-1 text-[11px] font-semibold uppercase tracking-wider text-slate-400">Repositories</p>
            <div className="max-h-56 overflow-y-auto">
              <button type="button" onClick={() => setRepoIds([])} className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-slate-100 dark:hover:bg-white/[0.05]">
                <Check className={cx('h-3.5 w-3.5', repoIds.length ? 'invisible' : 'text-brand-600')} aria-hidden />All you can read
              </button>
              {repos.map((r) => (
                <button key={r.id} type="button" role="menuitemcheckbox" aria-checked={repoIds.includes(r.id)} onClick={() => setRepoIds(toggle(repoIds, r.id))}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-slate-100 dark:hover:bg-white/[0.05]">
                  <Check className={cx('h-3.5 w-3.5', repoIds.includes(r.id) ? 'text-brand-600' : 'invisible')} aria-hidden />
                  <span className="truncate">{r.full_name}</span>
                </button>
              ))}
            </div>
          </div>
        </>
      )}
    </div>
  );
}

export default function Ask() {
  const { threadId } = useParams();
  const nav = useNavigate();
  const qc = useQueryClient();
  const threads = useThreads();
  const thread = useThread(threadId);
  const repos = useRepos();
  const [params] = useSearchParams();
  const [question, setQuestion] = useState(() => params.get('q') ?? '');
  // Search (Ctrl/⌘+K) can hand over a question while Ask is already open.
  const handedOver = params.get('q');
  useEffect(() => {
    if (handedOver) setQuestion(handedOver);
  }, [handedOver]);
  const [repoIds, setRepoIds] = useState<string[]>([]);
  const [include, setInclude] = useState<string[]>([]);
  const [streaming, setStreaming] = useState('');
  const [pending, setPending] = useState<string>();
  const [error, setError] = useState<unknown>();
  const abort = useRef<AbortController>();
  const bottom = useRef<HTMLDivElement>(null);

  // A block body on purpose: newer browsers return a Promise from scrollIntoView, and an effect that returns
  // anything but a function makes React call it as a cleanup ("… is not a function" on the next page).
  useEffect(() => {
    bottom.current?.scrollIntoView?.({ behavior: 'smooth' });
  }, [thread.data, streaming]);
  useEffect(() => () => abort.current?.abort(), []);

  const ask = async (e: FormEvent) => {
    e.preventDefault();
    const q = question.trim();
    if (!q) return;
    setError(undefined);
    setPending(q);
    setStreaming('');
    setQuestion('');
    abort.current = new AbortController();
    try {
      const res = await fetch(`${API}/ask`, {
        method: 'POST',
        credentials: 'same-origin',
        signal: abort.current.signal,
        headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream', 'X-CSRF-Token': getCSRF() },
        body: JSON.stringify({ question: q, thread_id: threadId ?? null, scope: { repo_ids: repoIds, include } }),
      });
      if (!res.ok) throw await toApiError(res);
      let done: AskResponse | undefined;
      await readSSE(
        res,
        (ev) => {
          const data = JSON.parse(ev.data);
          if (ev.event === 'delta') setStreaming((s) => s + data.text);
          if (ev.event === 'done') done = data;
          if (ev.event === 'error') throw new ApiError(500, data.error.code, data.error.message, data.error.correlation_id);
        },
        abort.current.signal,
      );
      await qc.invalidateQueries({ queryKey: keys.threads });
      if (done) {
        await qc.invalidateQueries({ queryKey: ['thread', done.thread_id] });
        if (done.thread_id !== threadId) nav(`/ask/${done.thread_id}`);
      }
    } catch (e) {
      if (!(e instanceof DOMException && e.name === 'AbortError')) setError(e);
    } finally {
      setPending(undefined);
      setStreaming('');
    }
  };

  const messages = threadId ? thread.data?.messages ?? [] : [];
  const empty = !threadId && !pending;
  const composer = (
    <form onSubmit={ask} className="rounded-2xl border border-slate-200/80 bg-white p-2 shadow-card focus-within:border-brand-300 dark:border-white/[0.08] dark:bg-slate-900/80 dark:focus-within:border-brand-500/40">
      <textarea
        aria-label="Question"
        rows={empty ? 3 : 2}
        maxLength={4000}
        placeholder={empty ? 'Ask about your code, docs, incidents…' : 'Ask a follow-up…'}
        value={question}
        onChange={(e) => setQuestion(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) ask(e as unknown as FormEvent);
        }}
        className="block w-full resize-none bg-transparent px-2 py-1.5 text-[15px] outline-none placeholder:text-slate-400"
      />
      <div className="flex items-center gap-2 px-1 pt-1">
        <ScopePicker repos={repos.data ?? []} repoIds={repoIds} setRepoIds={setRepoIds} include={include} setInclude={setInclude} />
        <span className="hidden text-[11px] text-slate-400 sm:inline">Enter to send · Shift+Enter for a new line</span>
        <Button type="submit" size="sm" className="ml-auto" disabled={!!pending || !question.trim()} aria-label="Ask">
          <Send className="h-3.5 w-3.5" aria-hidden /> {pending ? 'Answering…' : 'Ask'}
        </Button>
      </div>
    </form>
  );
  return (
    <div className="-my-4 grid h-[calc(100vh-4rem)]">
      <div className="relative flex min-h-0 min-w-0 flex-col">
        <div className="min-h-0 flex-1 overflow-y-auto">
          <div className="mx-auto max-w-3xl space-y-4 px-1 pb-4">
            {empty && (
              <div className="pt-[8vh] text-center">
                <span className="mx-auto grid h-14 w-14 place-items-center rounded-2xl bg-gradient-to-br from-brand-500 to-violet-600 text-white shadow-lg shadow-brand-500/30">
                  <Sparkles className="h-7 w-7" aria-hidden />
                </span>
                <h1 className="mt-5 text-2xl font-semibold tracking-tight">Ask about your systems</h1>
                <p className="mt-2 text-sm text-slate-500 dark:text-slate-400">
                  Answers come from your connected repositories and docs, with citations. Nothing outside your access is used.
                </p>
                <div className="mt-6 text-left">{composer}</div>
                <div className="mt-4 grid gap-2 text-left sm:grid-cols-2">
                  {EXAMPLES.map((ex) => (
                    <button
                      key={ex.q}
                      type="button"
                      onClick={() => setQuestion(ex.q)}
                      className="group flex items-start gap-3 rounded-xl border border-slate-200/80 bg-white/70 p-3 text-sm transition hover:border-brand-300 hover:bg-white dark:border-white/[0.07] dark:bg-slate-900/40 dark:hover:border-brand-400/40"
                    >
                      <ex.icon className="mt-0.5 h-4 w-4 shrink-0 text-brand-500" aria-hidden />
                      <span className="text-left text-slate-700 group-hover:text-slate-900 dark:text-slate-300 dark:group-hover:text-white">{ex.q}</span>
                    </button>
                  ))}
                </div>
                <button type="button" onClick={() => nav(threads.data?.items?.[0] ? `/ask/${threads.data.items[0].id}` : '/ask')} className="mt-6 inline-flex items-center gap-1.5 text-xs text-slate-500 hover:text-slate-800 lg:hidden dark:hover:text-slate-200">
                  <History className="h-3.5 w-3.5" aria-hidden /> {threads.data?.items?.length ? `${threads.data.items.length} earlier question(s)` : 'No earlier questions'}
                </button>
              </div>
            )}
            {threadId && thread.data && <h2 className="pt-2 text-lg font-semibold tracking-tight">{thread.data.title}</h2>}
            {messages.map((m) => (
              <div key={m.id} className={cx(m.role === 'user' ? 'flex justify-end' : '')}>
                {m.role === 'user' ? (
                  <p className="max-w-[85%] rounded-2xl rounded-br-md bg-brand-600 px-4 py-2.5 text-sm font-medium text-white dark:bg-brand-500">{m.content}</p>
                ) : (
                  <div className="rounded-2xl border border-slate-200/80 bg-white p-4 shadow-card dark:border-white/[0.07] dark:bg-slate-900/60">
                    <Markdown>{m.content}</Markdown>
                    {m.content.trim() === NOT_FOUND && <NotFoundTips />}
                    <Citations items={m.citations} />
                    <Feedback m={m} />
                  </div>
                )}
              </div>
            ))}
            {pending && (
              <>
                <div className="flex justify-end">
                  <p className="max-w-[85%] rounded-2xl rounded-br-md bg-brand-600 px-4 py-2.5 text-sm font-medium text-white dark:bg-brand-500">{pending}</p>
                </div>
                <div className="rounded-2xl border border-slate-200/80 bg-white p-4 shadow-card dark:border-white/[0.07] dark:bg-slate-900/60" aria-live="polite">
                  {streaming ? <Markdown>{streaming}</Markdown> : <p className="animate-pulse text-sm text-slate-500">Searching your sources…</p>}
                </div>
              </>
            )}
            <ErrorNote error={error} />
            <div ref={bottom} />
          </div>
        </div>
        {!empty && <div className="mx-auto w-full max-w-3xl px-1 pb-4 pt-2">{composer}</div>}
      </div>
    </div>
  );
}
