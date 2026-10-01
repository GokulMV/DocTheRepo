import { lazy, Suspense } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import { RequireRole, Shell } from '@/layouts/Shell';
import { Spinner } from '@/components/ui';
import Login from '@/pages/Login';

// Pages load on demand so the first paint ships only the shell and the current page.
const Ask = lazy(() => import('@/pages/Ask'));
const Docs = lazy(() => import('@/pages/Docs'));
const Palace = lazy(() => import('@/pages/Palace'));
const Library = lazy(() => import('@/pages/Library'));
const Repos = lazy(() => import('@/pages/Repos'));
const Connectors = lazy(() => import('@/pages/Connectors'));
const Providers = lazy(() => import('@/pages/Providers'));
const Spend = lazy(() => import('@/pages/Spend'));
const Analytics = lazy(() => import('@/pages/Analytics'));
const Activity = lazy(() => import('@/pages/Activity'));
const Users = lazy(() => import('@/pages/Users'));
const Account = lazy(() => import('@/pages/Account'));
const Setup = lazy(() => import('@/pages/Setup'));
const Inbox = lazy(() => import('@/pages/Inbox'));
const IssueDetail = lazy(() => import('@/pages/IssueDetail'));
const KnownIssues = lazy(() => import('@/pages/KnownIssues'));

const admin = (el: JSX.Element) => <RequireRole min="admin">{el}</RequireRole>;

export default function App() {
  return (
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
          <Route path="*" element={<p className="text-sm text-slate-500">Page not found.</p>} />
        </Route>
      </Routes>
    </Suspense>
  );
}
