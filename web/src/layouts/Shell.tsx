import { NavLink, Navigate, Outlet, useLocation } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { api, ApiError } from '@/api/client';
import { useMe } from '@/api/hooks';
import { atLeast, type Role } from '@/api/types';
import { Button, Spinner, cx } from '@/components/ui';

interface NavItem {
  to: string;
  label: string;
  min: Role;
}

export const NAV: { section: string; items: NavItem[] }[] = [
  {
    section: 'Knowledge',
    items: [
      { to: '/ask', label: 'Ask', min: 'viewer' },
      { to: '/docs', label: 'Docs', min: 'viewer' },
      { to: '/palace', label: 'Palace', min: 'viewer' },
      { to: '/library', label: 'Library', min: 'viewer' },
    ],
  },
  {
    section: 'Operations',
    items: [
      { to: '/activity', label: 'Activity', min: 'viewer' },
      { to: '/analytics', label: 'Analytics', min: 'viewer' },
      { to: '/repos', label: 'Repositories', min: 'viewer' },
    ],
  },
  {
    section: 'Administration',
    items: [
      { to: '/connectors', label: 'Connectors', min: 'admin' },
      { to: '/providers', label: 'Providers & routing', min: 'admin' },
      { to: '/spend', label: 'Spend limits', min: 'admin' },
      { to: '/users', label: 'Users & access', min: 'admin' },
      { to: '/setup', label: 'Setup', min: 'admin' },
    ],
  },
];

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
  return (
    <div className="flex min-h-screen">
      <aside className="flex w-60 shrink-0 flex-col border-r border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
        <div className="px-4 py-4">
          <span className="text-base font-semibold">DocTheRepo</span>
          <span className="ml-1 text-xs text-slate-500">Hub</span>
        </div>
        <nav className="flex-1 space-y-5 px-2" aria-label="Main">
          {NAV.map((sec) => {
            const items = sec.items.filter((i) => atLeast(user.role, i.min));
            if (!items.length) return null;
            return (
              <div key={sec.section}>
                <p className="px-2 pb-1 text-xs font-semibold uppercase tracking-wide text-slate-400">{sec.section}</p>
                {items.map((i) => (
                  <NavLink
                    key={i.to}
                    to={i.to}
                    className={({ isActive }) =>
                      cx(
                        'block rounded-md px-2 py-1.5 text-sm',
                        isActive ? 'bg-brand-50 font-medium text-brand-700 dark:bg-brand-700/20 dark:text-brand-100' : 'text-slate-700 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800',
                      )
                    }
                  >
                    {i.label}
                  </NavLink>
                ))}
              </div>
            );
          })}
        </nav>
        <div className="border-t border-slate-200 p-3 text-xs dark:border-slate-800">
          <NavLink to="/account" className="block truncate font-medium hover:underline">
            {user.name || user.email}
          </NavLink>
          <p className="text-slate-500">{user.role}</p>
          <Button variant="ghost" size="sm" className="mt-2 w-full" onClick={logout}>
            Sign out
          </Button>
        </div>
      </aside>
      <main className="min-w-0 flex-1 p-6 lg:p-8">
        <Outlet context={user} />
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
