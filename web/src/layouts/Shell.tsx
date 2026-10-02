import { Suspense, useEffect, useState } from 'react';
import { NavLink, Navigate, Outlet, useLocation } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { LogOut, Monitor, Moon, PanelLeftClose, PanelLeftOpen, Sun } from 'lucide-react';
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
export function ThemeSwitch({ compact = false }: { compact?: boolean }) {
  const { choice } = useTheme();
  if (compact) {
    const i = THEMES.findIndex((t) => t.value === choice);
    const cur = THEMES[i];
    const next = THEMES[(i + 1) % THEMES.length];
    return (
      <button type="button" onClick={() => setTheme(next.value)} title={`${cur.label} theme (switch to ${next.label.toLowerCase()})`} aria-label={`${cur.label} theme`}
        className="mx-auto grid h-8 w-8 place-items-center rounded-md text-slate-500 hover:bg-slate-100 hover:text-slate-800 dark:hover:bg-white/[0.06] dark:hover:text-slate-200">
        <cur.icon className="h-4 w-4" aria-hidden />
      </button>
    );
  }
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

const COLLAPSE_KEY = 'dth.sidebar-collapsed';

function readCollapsed(): boolean {
  try {
    const v = localStorage.getItem(COLLAPSE_KEY);
    if (v !== null) return v === '1';
  } catch {
    // no storage: fall through to the width default
  }
  return typeof window !== 'undefined' && window.innerWidth < 1024;
}

/** useCollapsed is the sidebar's collapsed state, remembered per browser and toggled with Ctrl/⌘+B. */
function useCollapsed(): [boolean, () => void] {
  const [collapsed, setCollapsed] = useState(readCollapsed);
  const toggle = () =>
    setCollapsed((c) => {
      try {
        localStorage.setItem(COLLAPSE_KEY, c ? '0' : '1');
      } catch {
        // not remembered; still toggles
      }
      return !c;
    });
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'b' && !(e.target instanceof HTMLTextAreaElement || e.target instanceof HTMLInputElement)) {
        e.preventDefault();
        toggle();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);
  return [collapsed, toggle];
}

/** Shell is the authenticated layout: sidebar navigation filtered by role, and the page outlet. */
export function Shell() {
  const me = useMe();
  const loc = useLocation();
  const qc = useQueryClient();
  const [collapsed, toggle] = useCollapsed();
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
      <aside
        className={cx(
          'sticky top-0 flex h-screen shrink-0 flex-col border-r border-slate-200/80 bg-white/80 backdrop-blur transition-[width] duration-200 dark:border-white/[0.06] dark:bg-slate-950/70',
          collapsed ? 'w-[68px]' : 'w-64',
        )}
        data-collapsed={collapsed || undefined}
      >
        <div className={cx('flex items-center py-5', collapsed ? 'flex-col gap-3 px-2' : 'gap-2.5 px-5')}>
          <Logo className="h-8 w-8" />
          {!collapsed && (
            <div className="min-w-0 flex-1 leading-tight">
              <span className="block text-[15px] font-semibold tracking-tight">DocTheRepo</span>
              <span className="block text-[11px] font-medium uppercase tracking-[0.14em] text-slate-400">Hub</span>
            </div>
          )}
          <button
            type="button"
            onClick={toggle}
            aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
            title={`${collapsed ? 'Expand' : 'Collapse'} sidebar (Ctrl/⌘+B)`}
            className="rounded-md p-1.5 text-slate-400 hover:bg-slate-100 hover:text-slate-700 dark:hover:bg-white/[0.06] dark:hover:text-slate-200"
          >
            {collapsed ? <PanelLeftOpen className="h-4 w-4" aria-hidden /> : <PanelLeftClose className="h-4 w-4" aria-hidden />}
          </button>
        </div>
        <nav className={cx('flex-1 overflow-y-auto pb-4', collapsed ? 'space-y-3 px-2' : 'space-y-6 px-3')} aria-label="Main">
          {NAV.map((sec) => {
            const items = sec.items.filter((i) => atLeast(user.role, i.min as Role));
            if (!items.length) return null;
            return (
              <div key={sec.section}>
                {collapsed ? (
                  <div className="mx-3 mb-2 border-t border-slate-200/80 dark:border-white/[0.06]" aria-hidden />
                ) : (
                  <p className="px-3 pb-1.5 text-[11px] font-semibold uppercase tracking-[0.12em] text-slate-400 dark:text-slate-500">{sec.section}</p>
                )}
                <div className="space-y-0.5">
                  {items.map((i) => (
                    <NavLink
                      key={i.to}
                      to={i.to}
                      title={collapsed ? i.label : undefined}
                      aria-label={collapsed ? i.label : undefined}
                      className={({ isActive }) =>
                        cx(
                          'group relative flex items-center rounded-lg py-2 text-sm transition-colors',
                          collapsed ? 'justify-center px-0' : 'gap-3 px-3',
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
                          {!collapsed && i.label}
                        </>
                      )}
                    </NavLink>
                  ))}
                </div>
              </div>
            );
          })}
        </nav>
        <div className={cx('border-t border-slate-200/80 pt-3 dark:border-white/[0.06]', collapsed ? 'px-2' : 'px-4')}>
          <ThemeSwitch compact={collapsed} />
        </div>
        <div className={cx('flex items-center py-3', collapsed ? 'flex-col gap-2 px-2' : 'gap-3 px-4')}>
          <span title={collapsed ? `${who} (${user.role})` : undefined} className="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-gradient-to-br from-slate-200 to-slate-300 text-xs font-semibold text-slate-700 dark:from-slate-700 dark:to-slate-800 dark:text-slate-200">
            {initials(who)}
          </span>
          {!collapsed && (
            <NavLink to="/account" className="min-w-0 flex-1 text-xs">
              <span className="block truncate font-medium text-slate-800 hover:underline dark:text-slate-100">{who}</span>
              <span className="block capitalize text-slate-500">{user.role}</span>
            </NavLink>
          )}
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
        <div className="mx-auto max-w-[1600px] px-6 py-8 lg:px-10">
          {/* Reset on navigation (not remounted): a failed page does not block the next, and switching never flashes. */}
          <ErrorBoundary resetKey={loc.pathname}>
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
