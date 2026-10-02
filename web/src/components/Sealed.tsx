import { LockKeyhole } from 'lucide-react';
import type { SecretMeta } from '@/api/types';
import { relTime } from '@/lib/format';

/** SealedBadge shows that a write-only secret is set, without ever showing it. */
export function SealedBadge({ meta, label = 'Sealed' }: { meta?: SecretMeta; label?: string }) {
  return (
    <span
      className="inline-flex items-center gap-1 rounded-md bg-emerald-50 px-1.5 py-0.5 text-xs font-medium text-emerald-800 ring-1 ring-emerald-200 dark:bg-emerald-500/10 dark:text-emerald-300 dark:ring-emerald-500/20"
      title="Write-only: encrypted at rest (AES-256-GCM under the Hub's key) and never returned by the API or shown in the UI. Replace it to change it."
    >
      <LockKeyhole className="h-3 w-3" aria-hidden />
      {label}
      {meta?.hint && <span className="font-mono">••••{meta.hint}</span>}
      {meta?.set_at && <span className="font-normal text-emerald-700/70 dark:text-emerald-300/60">· {relTime(meta.set_at)}</span>}
    </span>
  );
}

/** SealedHint explains, under a secret field, what happens to the value. */
export function SealedHint() {
  return (
    <span className="inline-flex items-start gap-1">
      <LockKeyhole className="mt-0.5 h-3 w-3 shrink-0 text-emerald-600" aria-hidden />
      <span>Sealed in this browser (X25519 + ML-KEM-768) before it is sent; stored encrypted; never shown again.</span>
    </span>
  );
}
