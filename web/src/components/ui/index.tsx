// Small, accessible UI kit on Tailwind (shadcn/ui-style primitives; Radix for the dialog).
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { forwardRef, type ButtonHTMLAttributes, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes, type TextareaHTMLAttributes } from 'react';
import { ApiError } from '@/api/client';

const cx = (...c: (string | false | undefined | null)[]) => c.filter(Boolean).join(' ');
export { cx };

type Variant = 'primary' | 'secondary' | 'ghost' | 'danger';
const variants: Record<Variant, string> = {
  primary: 'bg-brand-600 text-white hover:bg-brand-700 disabled:bg-brand-600/50',
  secondary: 'border border-slate-300 bg-white text-slate-800 hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100 dark:hover:bg-slate-800',
  ghost: 'text-slate-700 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800',
  danger: 'bg-red-600 text-white hover:bg-red-700 disabled:bg-red-600/50',
};

export const Button = forwardRef<HTMLButtonElement, ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; size?: 'sm' | 'md' }>(
  ({ variant = 'primary', size = 'md', className, type = 'button', ...p }, ref) => (
    <button
      ref={ref}
      type={type}
      className={cx(
        'inline-flex items-center justify-center gap-1.5 rounded-md font-medium transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500 disabled:cursor-not-allowed',
        size === 'sm' ? 'h-8 px-2.5 text-xs' : 'h-9 px-3.5 text-sm',
        variants[variant],
        className,
      )}
      {...p}
    />
  ),
);
Button.displayName = 'Button';

const field =
  'w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm shadow-sm focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500 dark:border-slate-700 dark:bg-slate-900';

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(({ className, ...p }, ref) => (
  <input ref={ref} className={cx(field, 'h-9', className)} {...p} />
));
Input.displayName = 'Input';

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement>>(({ className, ...p }, ref) => (
  <textarea ref={ref} className={cx(field, className)} {...p} />
));
Textarea.displayName = 'Textarea';

export const Select = forwardRef<HTMLSelectElement, SelectHTMLAttributes<HTMLSelectElement>>(({ className, ...p }, ref) => (
  <select ref={ref} className={cx(field, 'h-9', className)} {...p} />
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
    <section className={cx('rounded-lg border border-slate-200 bg-white shadow-sm dark:border-slate-800 dark:bg-slate-900', className)}>
      {(title || actions) && (
        <header className="flex items-center justify-between gap-2 border-b border-slate-200 px-4 py-3 dark:border-slate-800">
          <h2 className="text-sm font-semibold">{title}</h2>
          <div className="flex items-center gap-2">{actions}</div>
        </header>
      )}
      <div className="p-4">{children}</div>
    </section>
  );
}

const tones = {
  gray: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300',
  green: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-300',
  amber: 'bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300',
  red: 'bg-red-100 text-red-800 dark:bg-red-900/40 dark:text-red-300',
  blue: 'bg-brand-100 text-brand-700 dark:bg-brand-700/30 dark:text-brand-100',
};
export type Tone = keyof typeof tones;

export function Badge({ tone = 'gray', children }: { tone?: Tone; children: ReactNode }) {
  return <span className={cx('inline-flex items-center rounded px-1.5 py-0.5 text-xs font-medium', tones[tone])}>{children}</span>;
}

export function statusTone(status: string): Tone {
  if (['done', 'ok', 'merged', 'success'].includes(status)) return 'green';
  if (['failed', 'dead', 'failing', 'needs_human', 'spend_blocked'].includes(status)) return 'red';
  if (['queued', 'processing', 'pending_approval', 'degraded', 'open', 'awaiting_review'].includes(status)) return 'amber';
  return 'gray';
}

export function PageHeader({ title, description, actions }: { title: string; description?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
        {description && <p className="mt-1 max-w-3xl text-sm text-slate-500">{description}</p>}
      </div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </div>
  );
}

export function Spinner({ label = 'Loading' }: { label?: string }) {
  return (
    <div role="status" className="flex items-center gap-2 py-6 text-sm text-slate-500">
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

export function Empty({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="rounded-lg border border-dashed border-slate-300 p-8 text-center dark:border-slate-700">
      <p className="text-sm font-medium">{title}</p>
      {children && <div className="mt-1 text-sm text-slate-500">{children}</div>}
    </div>
  );
}

export function Table({ head, children }: { head: ReactNode[]; children: ReactNode }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left text-sm">
        <thead className="border-b border-slate-200 text-xs uppercase text-slate-500 dark:border-slate-800">
          <tr>
            {head.map((h, i) => (
              <th key={i} className="px-3 py-2 font-medium">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-slate-100 dark:divide-slate-800">{children}</tbody>
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
        <DialogPrimitive.Overlay className="fixed inset-0 z-40 bg-black/40" />
        <DialogPrimitive.Content className="fixed left-1/2 top-1/2 z-50 max-h-[90vh] w-[min(36rem,95vw)] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-lg bg-white p-5 shadow-xl dark:bg-slate-900">
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

export function Toggle({ checked, onChange, label }: { checked: boolean; onChange: (v: boolean) => void; label: string }) {
  return (
    <label className="inline-flex cursor-pointer items-center gap-2 text-sm">
      <input type="checkbox" className="h-4 w-4 accent-brand-600" checked={checked} onChange={(e) => onChange(e.target.checked)} />
      {label}
    </label>
  );
}
