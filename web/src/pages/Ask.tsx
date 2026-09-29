import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { API, ApiError, api, getCSRF, toApiError } from '@/api/client';
import { keys, useRepos, useThread, useThreads } from '@/api/hooks';
import type { AskResponse, Citation, Message } from '@/api/types';
import { Markdown } from '@/components/Markdown';
import { Badge, Button, ErrorNote, Textarea, Toggle, cx } from '@/components/ui';
import { readSSE } from '@/lib/sse';
import { relTime, usd } from '@/lib/format';

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

function Feedback({ m }: { m: Message }) {
  const [value, setValue] = useState(m.feedback);
  const send = async (v: 'up' | 'down') => {
    setValue(v);
    await api.post(`/messages/${m.id}/feedback`, { value: v }).catch(() => setValue(m.feedback));
  };
  return (
    <div className="mt-2 flex items-center gap-1 text-xs text-slate-500">
      <button aria-label="Helpful" className={cx('rounded px-1.5 hover:bg-slate-100', value === 'up' && 'text-emerald-600')} onClick={() => send('up')}>
        ▲
      </button>
      <button aria-label="Not helpful" className={cx('rounded px-1.5 hover:bg-slate-100', value === 'down' && 'text-red-600')} onClick={() => send('down')}>
        ▼
      </button>
      {m.cached && <Badge>cached</Badge>}
      {m.usage && m.usage.cost_usd > 0 && <span>{usd(m.usage.cost_usd)}</span>}
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
  const [question, setQuestion] = useState('');
  const [repoIds, setRepoIds] = useState<string[]>([]);
  const [include, setInclude] = useState<string[]>([]);
  const [streaming, setStreaming] = useState('');
  const [pending, setPending] = useState<string>();
  const [error, setError] = useState<unknown>();
  const abort = useRef<AbortController>();
  const bottom = useRef<HTMLDivElement>(null);

  useEffect(() => bottom.current?.scrollIntoView?.({ behavior: 'smooth' }), [thread.data, streaming]);
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
  return (
    <div className="flex h-[calc(100vh-4rem)] gap-6">
      <aside className="hidden w-60 shrink-0 overflow-y-auto lg:block">
        <Button variant="secondary" className="mb-3 w-full" onClick={() => nav('/ask')}>
          New question
        </Button>
        <ul className="space-y-0.5 text-sm">
          {threads.data?.items.map((t) => (
            <li key={t.id}>
              <Link
                to={`/ask/${t.id}`}
                className={cx('block truncate rounded px-2 py-1.5 hover:bg-slate-100 dark:hover:bg-slate-800', t.id === threadId && 'bg-slate-100 font-medium dark:bg-slate-800')}
                title={t.title}
              >
                {t.title}
                <span className="block text-xs font-normal text-slate-400">{relTime(t.updated_at)}</span>
              </Link>
            </li>
          ))}
        </ul>
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex-1 space-y-4 overflow-y-auto pb-4">
          {!threadId && !pending && (
            <div className="mx-auto mt-16 max-w-xl text-center">
              <h1 className="text-xl font-semibold">Ask about your systems</h1>
              <p className="mt-2 text-sm text-slate-500">
                Answers come from your connected repositories and docs, with citations. Nothing outside your access is used.
              </p>
            </div>
          )}
          {messages.map((m) => (
            <div key={m.id} className={cx('rounded-lg p-4', m.role === 'user' ? 'bg-brand-50 dark:bg-brand-700/20' : 'border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900')}>
              {m.role === 'user' ? (
                <p className="text-sm font-medium">{m.content}</p>
              ) : (
                <>
                  <Markdown>{m.content}</Markdown>
                  <Citations items={m.citations} />
                  <Feedback m={m} />
                </>
              )}
            </div>
          ))}
          {pending && (
            <>
              <div className="rounded-lg bg-brand-50 p-4 text-sm font-medium dark:bg-brand-700/20">{pending}</div>
              <div className="rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900" aria-live="polite">
                {streaming ? <Markdown>{streaming}</Markdown> : <p className="text-sm text-slate-500">Searching your sources…</p>}
              </div>
            </>
          )}
          <ErrorNote error={error} />
          <div ref={bottom} />
        </div>
        <form onSubmit={ask} className="space-y-2 border-t border-slate-200 pt-3 dark:border-slate-800">
          <Textarea
            aria-label="Question"
            rows={3}
            maxLength={4000}
            placeholder="How does checkout handle a failed payment?"
            value={question}
            onChange={(e) => setQuestion(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) ask(e as unknown as FormEvent);
            }}
          />
          <div className="flex flex-wrap items-center gap-3 text-sm">
            {!!repos.data?.length && <select
              aria-label="Repositories"
              multiple
              className="h-16 min-w-48 rounded-md border border-slate-300 bg-white px-2 text-sm dark:border-slate-700 dark:bg-slate-900"
              value={repoIds}
              onChange={(e) => setRepoIds(Array.from(e.target.selectedOptions, (o) => o.value))}
            >
              {repos.data?.map((r) => (
                <option key={r.id} value={r.id}>
                  {r.full_name}
                </option>
              ))}
            </select>}
            {INCLUDES.map((i) => (
              <Toggle
                key={i.id}
                label={i.label}
                checked={include.includes(i.id)}
                onChange={(v) => setInclude((cur) => (v ? [...cur, i.id] : cur.filter((x) => x !== i.id)))}
              />
            ))}
            <span className="text-xs text-slate-400">No selection = all you can read. Ctrl/⌘+Enter to send.</span>
            <Button type="submit" className="ml-auto" disabled={!!pending || !question.trim()}>
              {pending ? 'Answering…' : 'Ask'}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
}
