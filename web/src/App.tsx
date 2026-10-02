import { lazy, Suspense, useEffect } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import { RequireRole, Shell } from '@/layouts/Shell';
import { ErrorBoundary } from '@/components/ErrorBoundary';
import { Spinner } from '@/components/ui';
import Login from '@/pages/Login';

// Pages ship with the app, so switching pages never waits on the network (and never meets a stale chunk
// after an upgrade). Only the two heavy ones — the graph and the charts — load separately, prefetched once
// the app is idle.
import Ask from '@/pages/Ask';
import Docs from '@/pages/Docs';
import Architecture from '@/pages/Architecture';
import Library from '@/pages/Library';
import Repos from '@/pages/Repos';
import Connectors from '@/pages/Connectors';
import Providers from '@/pages/Providers';
import Spend from '@/pages/Spend';
import Activity from '@/pages/Activity';
import Users from '@/pages/Users';
import Account from '@/pages/Account';
import Setup from '@/pages/Setup';
import SettingsFile from '@/pages/SettingsFile';
import Inbox from '@/pages/Inbox';
import IssueDetail from '@/pages/IssueDetail';
import KnownIssues from '@/pages/KnownIssues';
const loadPalace = () => import('@/pages/Palace');
const loadAnalytics = () => import('@/pages/Analytics');
const Palace = lazy(loadPalace);
const Analytics = lazy(loadAnalytics);

function usePrefetchHeavyPages() {
  useEffect(() => {
    const run = () => void Promise.all([loadPalace(), loadAnalytics()]).catch(() => undefined);
    const w = window as Window & { requestIdleCallback?: (cb: () => void) => number };
    if (w.requestIdleCallback) w.requestIdleCallback(run);
    else setTimeout(run, 1500);
  }, []);
}

const admin = (el: JSX.Element) => <RequireRole min="admin">{el}</RequireRole>;

export default function App() {
  usePrefetchHeavyPages();
  return (
    <ErrorBoundary>
    <Suspense fallback={<Spinner />}>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route element={<Shell />}>
          <Route index element={<Navigate to="/ask" replace />} />
          <Route path="/ask" element={<Ask />} />
          <Route path="/ask/:threadId" element={<Ask />} />
          <Route path="/docs" element={<Docs />} />
          <Route path="/docs/:nodeId" element={<Docs />} />
          <Route path="/palace" element={<Palace />} />
          <Route path="/palace/:entityId" element={<Palace />} />
          <Route path="/architecture" element={<Architecture />} />
          <Route path="/architecture/:repoId" element={<Architecture />} />
          <Route path="/library" element={<Library />} />
          <Route path="/library/:slug" element={<Library />} />
          <Route path="/inbox" element={<Inbox />} />
          <Route path="/inbox/:id" element={<IssueDetail />} />
          <Route path="/known-issues" element={<KnownIssues />} />
          <Route path="/repos" element={<Repos />} />
          <Route path="/activity" element={<Activity />} />
          <Route path="/analytics" element={<Analytics />} />
          <Route path="/account" element={<Account />} />
          <Route path="/connectors" element={admin(<Connectors />)} />
          <Route path="/providers" element={admin(<Providers />)} />
          <Route path="/spend" element={admin(<Spend />)} />
          <Route path="/users" element={admin(<Users />)} />
          <Route path="/setup" element={admin(<Setup />)} />
          <Route path="/settings-file" element={admin(<SettingsFile />)} />
          <Route path="*" element={<p className="text-sm text-slate-500">Page not found.</p>} />
        </Route>
      </Routes>
    </Suspense>
    </ErrorBoundary>
  );
}
