// Small, accessible UI kit on Tailwind (shadcn/ui-style primitives; Radix for the dialog).
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { forwardRef, type ButtonHTMLAttributes, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes, type TextareaHTMLAttributes } from 'react';
import { useInRouterContext, useLocation } from 'react-router-dom';
import { CircleDashed, type LucideIcon } from 'lucide-react';
import { ApiError } from '@/api/client';
import { navItemFor } from '@/layouts/nav';
import { capFirst, sentence } from '@/lib/labels';

const cx = (...c: (string | false | undefined | null)[]) => c.filter(Boolean).join(' ');
export { cx };

type Variant = 'primary' | 'secondary' | 'ghost' | 'danger';
const variants: Record<Variant, string> = {
  primary: 'bg-brand-600 text-white shadow-sm shadow-brand-600/20 hover:bg-brand-700 disabled:bg-brand-600/50 dark:bg-brand-500 dark:hover:bg-brand-400',
  secondary: 'border border-slate-200 bg-white text-slate-800 shadow-sm hover:bg-slate-50 dark:border-white/10 dark:bg-white/[0.04] dark:text-slate-100 dark:hover:bg-white/[0.08]',
  ghost: 'text-slate-700 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-white/[0.06]',
  danger: 'bg-red-600 text-white hover:bg-red-700 disabled:bg-red-600/50',
};

export const Button = forwardRef<HTMLButtonElement, ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; size?: 'sm' | 'md' }>(
  ({ variant = 'primary', size = 'md', className, type = 'button', ...p }, ref) => (
    <button
      ref={ref}
      type={type}
      className={cx(
        'inline-flex items-center justify-center gap-1.5 rounded-lg font-medium transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500 disabled:cursor-not-allowed',
        size === 'sm' ? 'h-8 px-2.5 text-xs' : 'h-9 px-3.5 text-sm',
        variants[variant],
        className,
      )}
      {...p}
    />
  ),
);
Button.displayName = 'Button';

const fieldBase =
  'rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm shadow-sm placeholder:text-slate-400 focus:border-brand-500 focus:outline-none focus:ring-2 focus:ring-brand-500/20 dark:border-white/10 dark:bg-white/[0.03] dark:placeholder:text-slate-500';

// field is full width unless the caller sets a width (w-48, min-w-…): Tailwind cannot tell which of two
// width classes was meant, so only one may be present.
const field = (className?: string) => cx(fieldBase, !/(^|\s)w-/.test(className ?? '') && 'w-full');

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(({ className, ...p }, ref) => (
  <input ref={ref} className={cx(field(className), 'h-9', className)} {...p} />
));
Input.displayName = 'Input';

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement>>(({ className, ...p }, ref) => (
  <textarea ref={ref} className={cx(field(className), className)} {...p} />
));
Textarea.displayName = 'Textarea';

export const Select = forwardRef<HTMLSelectElement, SelectHTMLAttributes<HTMLSelectElement>>(({ className, ...p }, ref) => (
  <select ref={ref} className={cx(field(className), 'h-9', className)} {...p} />
));
Select.displayName = 'Select';

export function Field({ label, hint, children, htmlFor }: { label: string; hint?: ReactNode; children: ReactNode; htmlFor?: string }) {
  // Wrapping the control associates the label implicitly (screen readers, getByLabel) without ids.
  if (htmlFor) {
    return (
      <div className="space-y-1">
        <label htmlFor={htmlFor} className="block text-sm font-medium text-slate-700 dark:text-slate-300">
          {label}
        </label>
        {children}
        {hint && <p className="text-xs text-slate-500">{hint}</p>}
      </div>
    );
  }
  return (
    <div className="space-y-1">
      <label className="block space-y-1">
        <span className="block text-sm font-medium text-slate-700 dark:text-slate-300">{label}</span>
        {children}
      </label>
      {hint && <p className="text-xs text-slate-500">{hint}</p>}
    </div>
  );
}

export function Card({ title, actions, children, className }: { title?: ReactNode; actions?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={cx('rounded-xl border border-slate-200/80 bg-white shadow-card dark:border-white/[0.07] dark:bg-slate-900/60', className)}>
      {(title || actions) && (
        <header className="flex items-center justify-between gap-2 border-b border-slate-200/80 px-4 py-3 dark:border-white/[0.06]">
          <h2 className="text-sm font-semibold">{title}</h2>
          <div className="flex items-center gap-2">{actions}</div>
        </header>
      )}
      <div className="p-4">{children}</div>
    </section>
  );
}

const tones = {
  gray: 'bg-slate-100 text-slate-700 dark:bg-white/[0.06] dark:text-slate-300',
  green: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-500/15 dark:text-emerald-300',
  amber: 'bg-amber-100 text-amber-800 dark:bg-amber-500/15 dark:text-amber-300',
  red: 'bg-red-100 text-red-800 dark:bg-red-500/15 dark:text-red-300',
  blue: 'bg-brand-100 text-brand-700 dark:bg-brand-500/20 dark:text-brand-200',
};
export type Tone = keyof typeof tones;

/** Badge capitalizes its text ("spend_blocked" reads "Spend blocked"); values shown stay identifiers elsewhere. */
export function Badge({ tone = 'gray', children }: { tone?: Tone; children: ReactNode }) {
  let content = children;
  if (typeof children === 'string') content = sentence(children);
  else if (Array.isArray(children) && typeof children[0] === 'string') content = [capFirst(children[0]), ...children.slice(1)];
  return <span className={cx('inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-xs font-medium', tones[tone])}>{content}</span>;
}

export function statusTone(status: string): Tone {
  if (['done', 'ok', 'merged', 'success'].includes(status)) return 'green';
  if (['failed', 'dead', 'failing', 'needs_human', 'spend_blocked'].includes(status)) return 'red';
  if (['queued', 'processing', 'pending_approval', 'degraded', 'open', 'awaiting_review'].includes(status)) return 'amber';
  return 'gray';
}

/** IconTile is an icon in a tinted rounded square. */
export function IconTile({ icon: Icon, tone = 'brand', size = 'md' }: { icon: LucideIcon; tone?: 'brand' | 'green' | 'amber' | 'slate' | 'violet'; size?: 'sm' | 'md' | 'lg' }) {
  const t = {
    brand: 'bg-brand-50 text-brand-600 ring-brand-100 dark:bg-brand-500/15 dark:text-brand-300 dark:ring-brand-400/20',
    green: 'bg-emerald-50 text-emerald-600 ring-emerald-100 dark:bg-emerald-500/15 dark:text-emerald-300 dark:ring-emerald-400/20',
    amber: 'bg-amber-50 text-amber-600 ring-amber-100 dark:bg-amber-500/15 dark:text-amber-300 dark:ring-amber-400/20',
    violet: 'bg-violet-50 text-violet-600 ring-violet-100 dark:bg-violet-500/15 dark:text-violet-300 dark:ring-violet-400/20',
    slate: 'bg-slate-100 text-slate-500 ring-slate-200 dark:bg-white/[0.06] dark:text-slate-400 dark:ring-white/10',
  }[tone];
  const box = { sm: 'h-8 w-8 rounded-lg', md: 'h-10 w-10 rounded-xl', lg: 'h-12 w-12 rounded-2xl' }[size];
  const ic = { sm: 'h-4 w-4', md: 'h-5 w-5', lg: 'h-6 w-6' }[size];
  return (
    <span className={cx('grid shrink-0 place-items-center ring-1 ring-inset', t, box)}>
      <Icon className={ic} aria-hidden />
    </span>
  );
}

function usePageIcon(): LucideIcon | undefined {
  const inRouter = useInRouterContext();
  // Hooks run unconditionally; outside a router (some tests) there is no location to read.
  // eslint-disable-next-line react-hooks/rules-of-hooks
  const path = inRouter ? useLocation().pathname : '';
  return navItemFor(path)?.icon;
}

export function PageHeader({ title, description, actions, icon }: { title: string; description?: ReactNode; actions?: ReactNode; icon?: LucideIcon }) {
  const fromNav = usePageIcon();
  const Icon = icon ?? fromNav;
  return (
    <div className="mb-8 flex flex-wrap items-start justify-between gap-4">
      <div className="flex items-start gap-4">
        {Icon && <IconTile icon={Icon} size="lg" />}
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
          {description && <p className="mt-1 max-w-3xl text-sm leading-relaxed text-slate-500 dark:text-slate-400">{description}</p>}
        </div>
      </div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </div>
  );
}

/** Spinner appears only after 300 ms, so a quick load shows the content without a loading flash. */
export function Spinner({ label = 'Loading' }: { label?: string }) {
  return (
    <div role="status" className="dth-appear-late flex items-center gap-2 py-6 text-sm text-slate-500">
      <span className="h-4 w-4 animate-spin rounded-full border-2 border-slate-300 border-t-brand-600" />
      {label}…
    </div>
  );
}

export function ErrorNote({ error }: { error: unknown }) {
  if (!error) return null;
  const e = error instanceof ApiError ? error : undefined;
  return (
    <div role="alert" className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-200">
      <strong>{e?.code ?? 'Error'}:</strong> {error instanceof Error ? error.message : String(error)}
      {e?.correlationId && <span className="ml-2 font-mono text-xs opacity-70">ref {e.correlationId.slice(0, 8)}</span>}
    </div>
  );
}

export function Empty({ title, children, icon: Icon = CircleDashed, action }: { title: string; children?: ReactNode; icon?: LucideIcon; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center rounded-xl border border-dashed border-slate-300 bg-white/50 px-8 py-12 text-center dark:border-white/10 dark:bg-white/[0.02]">
      <span className="grid h-12 w-12 place-items-center rounded-2xl bg-slate-100 text-slate-400 dark:bg-white/[0.05] dark:text-slate-500">
        <Icon className="h-6 w-6" aria-hidden />
      </span>
      <p className="mt-4 text-sm font-semibold">{title}</p>
      {children && <div className="mt-1 max-w-md text-sm text-slate-500 dark:text-slate-400">{children}</div>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}

export function Table({ head, children }: { head: ReactNode[]; children: ReactNode }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left text-sm">
        <thead className="border-b border-slate-200/80 text-[11px] uppercase tracking-wide text-slate-500 dark:border-white/[0.06]">
          <tr>
            {head.map((h, i) => (
              <th key={i} className="px-3 py-2 font-medium">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-slate-100 dark:divide-white/[0.05]">{children}</tbody>
      </table>
    </div>
  );
}

export function Td({ children, className }: { children?: ReactNode; className?: string }) {
  return <td className={cx('px-3 py-2 align-top', className)}>{children}</td>;
}

export function Dialog({ open, onOpenChange, title, description, children }: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  title: string;
  description?: string;
  children: ReactNode;
}) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-40 bg-slate-950/50 backdrop-blur-sm" />
        <DialogPrimitive.Content className="fixed left-1/2 top-1/2 z-50 max-h-[90vh] w-[min(36rem,95vw)] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-2xl border border-slate-200/80 bg-white p-6 shadow-2xl dark:border-white/10 dark:bg-slate-900">
          <DialogPrimitive.Title className="text-base font-semibold">{title}</DialogPrimitive.Title>
          {description ? (
            <DialogPrimitive.Description className="mt-1 text-sm text-slate-500">{description}</DialogPrimitive.Description>
          ) : (
            <DialogPrimitive.Description className="sr-only">{title}</DialogPrimitive.Description>
          )}
          <div className="mt-4">{children}</div>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

/** DialogFooter ends a dialog form: actions on the right, separated from the fields. */
export function DialogFooter({ children, note }: { children: ReactNode; note?: ReactNode }) {
  return (
    <div className="-mx-6 -mb-6 mt-5 flex flex-wrap items-center justify-end gap-2 rounded-b-2xl border-t border-slate-200/80 bg-slate-50/80 px-6 py-3 dark:border-white/[0.06] dark:bg-white/[0.02]">
      {note && <div className="mr-auto text-xs text-slate-500">{note}</div>}
      {children}
    </div>
  );
}

export function Toggle({ checked, onChange, label }: { checked: boolean; onChange: (v: boolean) => void; label: string }) {
  return (
    <label className="inline-flex cursor-pointer items-center gap-2 text-sm">
      <input type="checkbox" className="h-4 w-4 accent-brand-600" checked={checked} onChange={(e) => onChange(e.target.checked)} />
      {label}
    </label>
  );
}
