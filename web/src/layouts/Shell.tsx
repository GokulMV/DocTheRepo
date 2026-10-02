import { Suspense, useEffect, useState } from 'react';
import { NavLink, Navigate, Outlet, useLocation, useNavigate } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { ChevronDown, Ellipsis, LogOut, Monitor, Moon, PanelLeftClose, PanelLeftOpen, Pin, PinOff, Plus, Search, Sun, Trash2 } from 'lucide-react';
import { api, ApiError } from '@/api/client';
import { keys, useMe, useThreads } from '@/api/hooks';
import { CommandPalette } from '@/components/CommandPalette';
import { atLeast, type Role } from '@/api/types';
import { ErrorBoundary } from '@/components/ErrorBoundary';
import { Logo } from '@/components/Logo';
import { Spinner, cx } from '@/components/ui';
import { setTheme, useTheme, type ThemeChoice } from '@/theme';
import { NAV, PRIMARY, type NavItem } from './nav';

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

const PINS_KEY = 'dth.pinned';

/** usePins is the user's pinned pages, remembered per browser. */
function usePins(): [string[], (to: string) => void] {
  const [pins, setPins] = useState<string[]>(() => {
    try {
      const v = JSON.parse(localStorage.getItem(PINS_KEY) ?? '[]');
      return Array.isArray(v) ? v.filter((x) => typeof x === 'string') : [];
    } catch {
      return [];
    }
  });
  const toggle = (to: string) =>
    setPins((p) => {
      const next = p.includes(to) ? p.filter((x) => x !== to) : [...p, to];
      try {
        localStorage.setItem(PINS_KEY, JSON.stringify(next));
      } catch {
        // remembered for this visit only
      }
      return next;
    });
  return [pins, toggle];
}

const rowBase = 'group relative flex w-full items-center rounded-lg text-sm transition-colors';
const rowIdle = 'text-slate-600 hover:bg-slate-100 hover:text-slate-900 dark:text-slate-400 dark:hover:bg-white/[0.05] dark:hover:text-slate-100';
const rowActive = 'bg-slate-100 font-medium text-slate-900 dark:bg-white/[0.08] dark:text-white';

/** SideLink is one sidebar row: icon and label (icon with a tooltip when collapsed), with an optional pin toggle. */
function SideLink({ item, collapsed, pinned, onPin, end }: { item: NavItem; collapsed: boolean; pinned?: boolean; onPin?: () => void; end?: boolean }) {
  return (
    <div className="group/row relative">
      <NavLink
        to={item.to}
        end={end}
        title={collapsed ? item.label : undefined}
        aria-label={collapsed ? item.label : undefined}
        className={({ isActive }) => cx(rowBase, collapsed ? 'h-9 justify-center' : 'gap-3 px-3 py-2', isActive ? rowActive : rowIdle)}
      >
        {({ isActive }) => (
          <>
            <item.icon className={cx('h-[18px] w-[18px] shrink-0', isActive ? 'text-brand-600 dark:text-brand-300' : 'text-slate-400 group-hover:text-slate-600 dark:group-hover:text-slate-300')} aria-hidden />
            {!collapsed && <span className="truncate pr-5">{item.label}</span>}
          </>
        )}
      </NavLink>
      {!collapsed && onPin && (
        <button
          type="button"
          onClick={onPin}
          aria-label={pinned ? `Unpin ${item.label}` : `Pin ${item.label}`}
          title={pinned ? 'Unpin' : 'Pin to the top'}
          className={cx('absolute right-1.5 top-1/2 -translate-y-1/2 rounded p-1 text-slate-400 hover:bg-slate-200 hover:text-slate-700 dark:hover:bg-white/10 dark:hover:text-slate-200',
            pinned ? 'hidden group-hover/row:block' : 'hidden group-hover/row:block')}
        >
          {pinned ? <PinOff className="h-3.5 w-3.5" aria-hidden /> : <Pin className="h-3.5 w-3.5" aria-hidden />}
        </button>
      )}
    </div>
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return <p className="px-3 pb-1 pt-4 text-[11px] font-medium text-slate-400 dark:text-slate-500">{children}</p>;
}

/** Recents lists the latest questions, like a chat app's history. */
function Recents({ current }: { current?: string }) {
  const threads = useThreads();
  const nav = useNavigate();
  const qc = useQueryClient();
  const items = (threads.data?.items ?? []).slice(0, 15);
  if (!items.length) return null;
  const remove = async (id: string, title: string) => {
    if (!confirm(`Delete “${title}”?`)) return;
    await api.del(`/threads/${id}`).catch(() => undefined);
    await qc.invalidateQueries({ queryKey: keys.threads });
    if (id === current) nav('/ask');
  };
  return (
    <div aria-label="Recents" role="group">
      <SectionLabel>Recents</SectionLabel>
      <ul className="space-y-px">
        {items.map((t) => (
          <li key={t.id} className="group/row relative">
            <NavLink
              to={`/ask/${t.id}`}
              title={t.title}
              className={({ isActive }) => cx(rowBase, 'px-3 py-1.5 pr-8', isActive ? rowActive : rowIdle)}
            >
              <span className="truncate">{t.title}</span>
            </NavLink>
            <button
              type="button"
              aria-label={`Delete “${t.title}”`}
              title="Delete"
              onClick={() => remove(t.id, t.title)}
              className="absolute right-1.5 top-1/2 hidden -translate-y-1/2 rounded p-1 text-slate-400 hover:bg-slate-200 hover:text-red-600 group-hover/row:block dark:hover:bg-white/10"
            >
              <Trash2 className="h-3.5 w-3.5" aria-hidden />
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

/** Shell is the authenticated layout: sidebar navigation filtered by role, and the page outlet. */
export function Shell() {
  const me = useMe();
  const loc = useLocation();
  const qc = useQueryClient();
  const [collapsed, toggle] = useCollapsed();
  const [pins, togglePin] = usePins();
  const [moreOpen, setMoreOpen] = useState(false);
  const [search, setSearch] = useState(false);
  // A new page starts at the top, like a page load would, without the reload.
  useEffect(() => {
    window.scrollTo(0, 0);
  }, [loc.pathname]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setSearch(true);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);
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
  const allowed = (i: NavItem) => atLeast(user.role, i.min as Role);
  const all = NAV.flatMap((s) => s.items).filter(allowed);
  const primary = PRIMARY.map((to) => all.find((i) => i.to === to)).filter((i): i is NavItem => !!i);
  const moreSections = NAV.map((s) => ({ section: s.section, items: s.items.filter((i) => allowed(i) && !PRIMARY.includes(i.to) && i.to !== '/ask') })).filter((s) => s.items.length);
  const activeInMore = moreSections.some((s) => s.items.some((i) => loc.pathname === i.to || loc.pathname.startsWith(i.to + '/')));
  const showMore = moreOpen || activeInMore;
  const pinned = pins.map((to) => all.find((i) => i.to === to)).filter((i): i is NavItem => !!i);
  const threadId = loc.pathname.startsWith('/ask/') ? loc.pathname.slice(5) : undefined;
  const newQuestion: NavItem = { to: '/ask', label: 'New question', min: 'viewer', icon: Plus };
  return (
    <div className="flex min-h-screen">
      <aside
        className={cx(
          'sticky top-0 flex h-screen shrink-0 flex-col border-r border-slate-200/80 bg-slate-50/90 backdrop-blur transition-[width] duration-200 dark:border-white/[0.06] dark:bg-slate-950/80',
          collapsed ? 'w-[64px]' : 'w-[272px]',
        )}
        data-collapsed={collapsed || undefined}
      >
        <div className={cx('flex items-center pb-3 pt-4', collapsed ? 'flex-col gap-3 px-2' : 'gap-2.5 px-4')}>
          {collapsed ? null : (
            <>
              <Logo className="h-7 w-7" />
              <span className="min-w-0 flex-1 truncate text-[15px] font-semibold tracking-tight">DocTheRepo</span>
            </>
          )}
          <button
            type="button"
            onClick={toggle}
            aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
            title={`${collapsed ? 'Expand' : 'Collapse'} sidebar (Ctrl/⌘+B)`}
            className="rounded-md p-1.5 text-slate-400 hover:bg-slate-200/70 hover:text-slate-700 dark:hover:bg-white/[0.06] dark:hover:text-slate-200"
          >
            {collapsed ? <PanelLeftOpen className="h-[18px] w-[18px]" aria-hidden /> : <PanelLeftClose className="h-[18px] w-[18px]" aria-hidden />}
          </button>
        </div>
        <div className={cx(collapsed ? 'px-2' : 'px-3')}>
          <button
            type="button"
            onClick={() => setSearch(true)}
            aria-label="Search"
            title={collapsed ? 'Search (Ctrl/⌘+K)' : undefined}
            className={cx(
              'flex w-full items-center rounded-lg border border-slate-200 bg-white text-sm text-slate-400 hover:border-slate-300 dark:border-white/10 dark:bg-white/[0.03] dark:hover:border-white/20',
              collapsed ? 'h-9 justify-center' : 'gap-2.5 px-3 py-2',
            )}
          >
            <Search className="h-4 w-4 shrink-0" aria-hidden />
            {!collapsed && (
              <>
                <span className="flex-1 text-left">Search</span>
                <kbd className="rounded border border-slate-200 px-1.5 text-[10px] font-medium text-slate-400 dark:border-white/10">⌘K</kbd>
              </>
            )}
          </button>
        </div>
        <nav className={cx('mt-2 flex-1 overflow-y-auto pb-4', collapsed ? 'px-2' : 'px-3')} aria-label="Main">
          <div className="space-y-px">
            <NavLink
              to="/ask"
              end
              title={collapsed ? 'New question' : undefined}
              aria-label={collapsed ? 'New question' : undefined}
              className={({ isActive }) => cx(rowBase, collapsed ? 'h-9 justify-center' : 'gap-3 px-3 py-2', isActive ? rowActive : rowIdle)}
            >
              <span className="grid h-[22px] w-[22px] shrink-0 place-items-center rounded-full bg-brand-600 text-white shadow-sm shadow-brand-600/30">
                <newQuestion.icon className="h-3.5 w-3.5" aria-hidden />
              </span>
              {!collapsed && <span>New question</span>}
            </NavLink>
            {primary.map((i) => (
              <SideLink key={i.to} item={i} collapsed={collapsed} pinned={pins.includes(i.to)} onPin={() => togglePin(i.to)} />
            ))}
            <button
              type="button"
              onClick={() => (collapsed ? (toggle(), setMoreOpen(true)) : setMoreOpen((o) => !o))}
              aria-expanded={showMore}
              title={collapsed ? 'More' : undefined}
              aria-label={collapsed ? 'More' : undefined}
              className={cx(rowBase, rowIdle, collapsed ? 'h-9 justify-center' : 'gap-3 px-3 py-2')}
            >
              {collapsed ? <Ellipsis className="h-[18px] w-[18px] text-slate-400" aria-hidden /> : <ChevronDown className={cx('h-[18px] w-[18px] text-slate-400 transition-transform', showMore ? '' : '-rotate-90')} aria-hidden />}
              {!collapsed && <span>{showMore ? 'Less' : 'More'}</span>}
            </button>
            {showMore && !collapsed && (
              <div className="ml-3 border-l border-slate-200 pl-2 dark:border-white/[0.08]">
                {moreSections.map((s) => (
                  <div key={s.section}>
                    <p className="px-3 pb-0.5 pt-2.5 text-[10.5px] font-semibold uppercase tracking-[0.1em] text-slate-400 dark:text-slate-500">{s.section}</p>
                    {s.items.map((i) => (
                      <SideLink key={i.to} item={i} collapsed={false} pinned={pins.includes(i.to)} onPin={() => togglePin(i.to)} />
                    ))}
                  </div>
                ))}
              </div>
            )}
          </div>
          {pinned.length > 0 && (
            <div role="group" aria-label="Pinned">
              {collapsed ? <div className="mx-2 my-3 border-t border-slate-200 dark:border-white/[0.08]" aria-hidden /> : <SectionLabel>Pinned</SectionLabel>}
              <div className="space-y-px">
                {pinned.map((i) => (
                  <SideLink key={i.to} item={i} collapsed={collapsed} pinned onPin={() => togglePin(i.to)} />
                ))}
              </div>
            </div>
          )}
          {!collapsed && <Recents current={threadId} />}
        </nav>
        <div className={cx('border-t border-slate-200/80 pt-3 dark:border-white/[0.06]', collapsed ? 'px-2' : 'px-4')}>
          <ThemeSwitch compact={collapsed} />
        </div>
        <div className={cx('flex items-center py-3', collapsed ? 'flex-col gap-2 px-2' : 'gap-3 px-4')}>
          <NavLink to="/account" title={`${who} (${user.role}): your account`} className="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-gradient-to-br from-brand-500 to-violet-600 text-xs font-semibold text-white">
            {initials(who)}
          </NavLink>
          {!collapsed && (
            <NavLink to="/account" className="min-w-0 flex-1 text-xs">
              <span className="block truncate font-medium text-slate-800 hover:underline dark:text-slate-100">{who}</span>
              <span className="block capitalize text-slate-500">{user.role}</span>
            </NavLink>
          )}
          <button
            type="button"
            onClick={logout}
            className="rounded-md p-1.5 text-slate-400 hover:bg-slate-200/70 hover:text-slate-700 dark:hover:bg-white/[0.06] dark:hover:text-slate-200"
            aria-label="Sign out"
            title="Sign out"
          >
            <LogOut className="h-4 w-4" aria-hidden />
          </button>
        </div>
      </aside>
      <CommandPalette open={search} onOpenChange={setSearch} role={user.role} />
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
