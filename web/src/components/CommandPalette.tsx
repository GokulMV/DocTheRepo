import * as DialogPrimitive from '@radix-ui/react-dialog';
import { CornerDownLeft, MessageSquareText, Search, Sparkles } from 'lucide-react';
import { useMemo, useState, type KeyboardEvent } from 'react';
import { useNavigate } from 'react-router-dom';
import { useThreads } from '@/api/hooks';
import { atLeast, type Role } from '@/api/types';
import { NAV, OPT_IN_FEATURES } from '@/layouts/nav';
import { useMe } from '@/api/hooks';
import { cx } from '@/components/ui';

interface Entry {
  key: string;
  label: string;
  hint: string;
  icon: typeof Search;
  to: string;
}

/**
 * CommandPalette (Ctrl/⌘+K) jumps to any page or earlier question, or asks a new question with what was typed.
 */
export function CommandPalette({ open, onOpenChange, role }: { open: boolean; onOpenChange: (o: boolean) => void; role: Role }) {
  const [q, setQ] = useState('');
  const [active, setActive] = useState(0);
  const nav = useNavigate();
  const threads = useThreads();
  const features = useMe().data?.features;
  const entries = useMemo(() => {
    const needle = q.trim().toLowerCase();
    const shown = (f?: 'issues' | 'system') => !f || (OPT_IN_FEATURES.includes(f) ? features?.[f] === true : features?.[f] !== false);
    const pages: Entry[] = NAV.filter((i) => atLeast(role, i.min) && shown(i.feature)).flatMap((i) =>
      (i.tabs ?? [{ to: i.to, label: i.label }])
        .filter((t) => atLeast(role, t.min ?? i.min))
        .map((t) => ({ key: t.to, label: t.label, hint: t.label === i.label ? 'Page' : i.label, icon: i.icon, to: t.to })),
    );
    const questions: Entry[] = (threads.data?.items ?? []).map((t) => ({ key: t.id, label: t.title, hint: 'Question', icon: MessageSquareText, to: `/ask/${t.id}` }));
    const match = (e: Entry) => !needle || e.label.toLowerCase().includes(needle);
    const out = [...pages.filter(match), ...questions.filter(match).slice(0, needle ? 8 : 5)];
    if (needle) out.unshift({ key: 'ask', label: `Ask “${q.trim()}”`, hint: 'New question', icon: Sparkles, to: `/ask?q=${encodeURIComponent(q.trim())}` });
    return out;
  }, [q, role, threads.data, features]);
  const go = (e: Entry | undefined) => {
    if (!e) return;
    onOpenChange(false);
    setQ('');
    nav(e.to);
  };
  const onKey = (e: KeyboardEvent) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setActive((a) => Math.min(a + 1, entries.length - 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setActive((a) => Math.max(a - 1, 0));
    } else if (e.key === 'Enter') {
      e.preventDefault();
      go(entries[active]);
    }
  };
  return (
    <DialogPrimitive.Root open={open} onOpenChange={(o) => { onOpenChange(o); if (!o) setQ(''); }}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-40 bg-slate-950/40 backdrop-blur-sm" />
        <DialogPrimitive.Content className="fixed left-1/2 top-[12vh] z-50 w-[min(38rem,94vw)] -translate-x-1/2 overflow-hidden rounded-2xl border border-slate-200/80 bg-white shadow-2xl dark:border-white/10 dark:bg-slate-900">
          <DialogPrimitive.Title className="sr-only">Search</DialogPrimitive.Title>
          <DialogPrimitive.Description className="sr-only">Find a page or an earlier question, or ask something new.</DialogPrimitive.Description>
          <div className="flex items-center gap-3 border-b border-slate-200/80 px-4 dark:border-white/[0.06]">
            <Search className="h-4 w-4 shrink-0 text-slate-400" aria-hidden />
            <input
              autoFocus
              aria-label="Search pages and questions"
              placeholder="Search pages and questions, or type a question…"
              value={q}
              onChange={(e) => { setQ(e.target.value); setActive(0); }}
              onKeyDown={onKey}
              className="h-12 w-full bg-transparent text-sm outline-none placeholder:text-slate-400"
            />
          </div>
          <ul role="listbox" aria-label="Results" className="max-h-[50vh] overflow-y-auto p-2">
            {entries.length === 0 && <li className="px-3 py-6 text-center text-sm text-slate-500">Nothing matches.</li>}
            {entries.map((e, i) => (
              <li key={e.key} role="option" aria-selected={i === active}>
                <button
                  type="button"
                  onMouseEnter={() => setActive(i)}
                  onClick={() => go(e)}
                  className={cx('flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm', i === active ? 'bg-slate-100 dark:bg-white/[0.06]' : '')}
                >
                  <e.icon className="h-4 w-4 shrink-0 text-slate-400" aria-hidden />
                  <span className="min-w-0 flex-1 truncate">{e.label}</span>
                  <span className="shrink-0 text-[11px] text-slate-400">{e.hint}</span>
                  {i === active && <CornerDownLeft className="h-3.5 w-3.5 shrink-0 text-slate-400" aria-hidden />}
                </button>
              </li>
            ))}
          </ul>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
