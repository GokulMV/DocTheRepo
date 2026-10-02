import { Suspense } from 'react';
import { NavLink, Navigate, Outlet, useLocation } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { LogOut, Monitor, Moon, Sun } from 'lucide-react';
import { api, ApiError } from '@/api/client';
import { useMe } from '@/api/hooks';
import { atLeast, type Role } from '@/api/types';
import { ErrorBoundary } from '@/components/ErrorBoundary';
import { Logo } from '@/components/Logo';
import { Spinner, cx } from '@/components/ui';
import { setTheme, useTheme, type ThemeChoice } from '@/theme';
import { NAV } from './nav';

export { NAV } from './nav';

function initials(s: string) {
  const parts = s.replace(/@.*/, '').split(/[\s._-]+/).filter(Boolean);
  return ((parts[0]?.[0] ?? '?') + (parts[1]?.[0] ?? '')).toUpperCase();
}

const THEMES: { value: ThemeChoice; label: string; icon: typeof Sun }[] = [
  { value: 'light', label: 'Light', icon: Sun },
  { value: 'dark', label: 'Dark', icon: Moon },
  { value: 'system', label: 'System', icon: Monitor },
];

/** ThemeSwitch is a three-way light / dark / follow-the-OS control. */
export function ThemeSwitch() {
  const { choice } = useTheme();
  return (
    <div role="radiogroup" aria-label="Theme" className="flex rounded-lg bg-slate-100 p-0.5 dark:bg-white/[0.05]">
      {THEMES.map((t) => (
        <button
          key={t.value}
          type="button"
          role="radio"
          aria-checked={choice === t.value}
          aria-label={`${t.label} theme`}
          title={`${t.label} theme`}
          onClick={() => setTheme(t.value)}
          className={cx(
            'flex flex-1 items-center justify-center gap-1.5 rounded-md px-2 py-1 text-[11px] font-medium transition-colors',
            choice === t.value
              ? 'bg-white text-slate-900 shadow-sm dark:bg-white/[0.12] dark:text-white'
              : 'text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200',
          )}
        >
          <t.icon className="h-3.5 w-3.5" aria-hidden />
          {t.label}
        </button>
      ))}
    </div>
  );
}

/** Shell is the authenticated layout: sidebar navigation filtered by role, and the page outlet. */
export function Shell() {
  const me = useMe();
  const loc = useLocation();
  const qc = useQueryClient();
  if (me.isLoading) return <Spinner />;
  if (me.error instanceof ApiError && me.error.status === 401) {
    return <Navigate to={`/login?return=${encodeURIComponent(loc.pathname + loc.search)}`} replace />;
  }
  if (me.error || !me.data) return <div className="p-8 text-sm text-red-700">Could not reach the Hub API.</div>;
  const user = me.data;
  const logout = async () => {
    await api.post('/auth/logout').catch(() => undefined);
    qc.clear();
    window.location.assign('/login');
  };
  const who = user.name || user.email;
  return (
    <div className="flex min-h-screen">
      <aside className="sticky top-0 flex h-screen w-64 shrink-0 flex-col border-r border-slate-200/80 bg-white/80 backdrop-blur dark:border-white/[0.06] dark:bg-slate-950/70">
        <div className="flex items-center gap-2.5 px-5 py-5">
          <Logo className="h-8 w-8" />
          <div className="leading-tight">
            <span className="block text-[15px] font-semibold tracking-tight">DocTheRepo</span>
            <span className="block text-[11px] font-medium uppercase tracking-[0.14em] text-slate-400">Hub</span>
          </div>
        </div>
        <nav className="flex-1 space-y-6 overflow-y-auto px-3 pb-4" aria-label="Main">
          {NAV.map((sec) => {
            const items = sec.items.filter((i) => atLeast(user.role, i.min as Role));
            if (!items.length) return null;
            return (
              <div key={sec.section}>
                <p className="px-3 pb-1.5 text-[11px] font-semibold uppercase tracking-[0.12em] text-slate-400 dark:text-slate-500">{sec.section}</p>
                <div className="space-y-0.5">
                  {items.map((i) => (
                    <NavLink
                      key={i.to}
                      to={i.to}
                      className={({ isActive }) =>
                        cx(
                          'group relative flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors',
                          isActive
                            ? 'bg-brand-50 font-medium text-brand-700 dark:bg-brand-500/15 dark:text-white'
                            : 'text-slate-600 hover:bg-slate-100 hover:text-slate-900 dark:text-slate-400 dark:hover:bg-white/[0.04] dark:hover:text-slate-100',
                        )
                      }
                    >
                      {({ isActive }) => (
                        <>
                          {isActive && <span className="absolute inset-y-1.5 left-0 w-0.5 rounded-full bg-brand-500" aria-hidden />}
                          <i.icon
                            className={cx('h-4 w-4 shrink-0', isActive ? 'text-brand-600 dark:text-brand-300' : 'text-slate-400 group-hover:text-slate-600 dark:group-hover:text-slate-300')}
                            aria-hidden
                          />
                          {i.label}
                        </>
                      )}
                    </NavLink>
                  ))}
                </div>
              </div>
            );
          })}
        </nav>
        <div className="border-t border-slate-200/80 px-4 pt-3 dark:border-white/[0.06]">
          <ThemeSwitch />
        </div>
        <div className="flex items-center gap-3 px-4 py-3">
          <span className="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-gradient-to-br from-slate-200 to-slate-300 text-xs font-semibold text-slate-700 dark:from-slate-700 dark:to-slate-800 dark:text-slate-200">
            {initials(who)}
          </span>
          <NavLink to="/account" className="min-w-0 flex-1 text-xs">
            <span className="block truncate font-medium text-slate-800 hover:underline dark:text-slate-100">{who}</span>
            <span className="block capitalize text-slate-500">{user.role}</span>
          </NavLink>
          <button
            type="button"
            onClick={logout}
            className="rounded-md p-1.5 text-slate-400 hover:bg-slate-100 hover:text-slate-700 dark:hover:bg-white/[0.06] dark:hover:text-slate-200"
            aria-label="Sign out"
            title="Sign out"
          >
            <LogOut className="h-4 w-4" aria-hidden />
          </button>
        </div>
      </aside>
      <main className="min-w-0 flex-1">
        <div className="mx-auto max-w-7xl px-6 py-8 lg:px-10">
          {/* Keyed by path: a page that failed does not keep the next one from rendering. */}
          <ErrorBoundary key={loc.pathname}>
            <Suspense fallback={<Spinner />}>
              <Outlet context={user} />
            </Suspense>
          </ErrorBoundary>
        </div>
      </main>
    </div>
  );
}

/** RequireRole hides a page from roles below min. */
export function RequireRole({ min, children }: { min: Role; children: React.ReactNode }) {
  const me = useMe();
  if (!atLeast(me.data?.role, min)) {
    return <p className="text-sm text-slate-500">This page needs the {min} role.</p>;
  }
  return <>{children}</>;
}
