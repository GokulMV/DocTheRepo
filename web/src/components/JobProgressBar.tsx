import type { Job } from '@/api/types';

const STAGES: Record<string, string> = { documenting: 'Documenting', writing: 'Writing docs' };

/** JobProgressBar shows how far a running job is ("Documenting 120 of 450 files"). */
export function JobProgressBar({ job }: { job: Job }) {
  const p = job.progress;
  if (job.status !== 'processing' || !p || !p.total) return null;
  const pct = Math.min(100, Math.round((p.done / p.total) * 100));
  const what = p.stage === 'writing' ? `${p.total} file${p.total === 1 ? '' : 's'}` : `${p.done} of ${p.total} files`;
  return (
    <div className="mt-1 min-w-40" aria-label={`${STAGES[p.stage] ?? p.stage}: ${what}`}>
      <div className="flex justify-between gap-2 text-[11px] text-slate-500">
        <span>{STAGES[p.stage] ?? p.stage} · {what}</span>
        {p.stage !== 'writing' && <span className="tabular-nums">{pct}%</span>}
      </div>
      <div className="mt-0.5 h-1.5 overflow-hidden rounded-full bg-slate-200 dark:bg-white/8">
        <div className={p.stage === 'writing' ? 'h-full w-full animate-pulse rounded-full bg-brand-500' : 'h-full rounded-full bg-brand-500 transition-all'} style={p.stage === 'writing' ? undefined : { width: `${pct}%` }} />
      </div>
    </div>
  );
}
