import { Check, Copy } from 'lucide-react';
import { useState } from 'react';

/** CopyField is a read-only value (a link, an ID) with a copy button. */
export function CopyField({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex items-stretch overflow-hidden rounded-lg border border-slate-300 dark:border-white/10">
      <input readOnly aria-label={label} value={value} onFocus={(e) => e.target.select()}
        className="min-w-0 flex-1 bg-slate-50 px-3 py-2 font-mono text-xs text-slate-700 outline-hidden dark:bg-white/3 dark:text-slate-200" />
      <button
        type="button"
        aria-label={`Copy ${label}`}
        onClick={async () => {
          await navigator.clipboard?.writeText(value).catch(() => undefined);
          setCopied(true);
          setTimeout(() => setCopied(false), 2000);
        }}
        className="inline-flex items-center gap-1.5 border-l border-slate-300 px-3 text-xs font-medium text-slate-600 hover:bg-slate-100 dark:border-white/10 dark:text-slate-300 dark:hover:bg-white/6"
      >
        {copied ? <><Check className="h-3.5 w-3.5" aria-hidden />Copied</> : <><Copy className="h-3.5 w-3.5" aria-hidden />Copy</>}
      </button>
    </div>
  );
}
