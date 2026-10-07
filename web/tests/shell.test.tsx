import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { Route } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ErrorBoundary } from '@/components/ErrorBoundary';
import { Shell } from '@/layouts/Shell';
import { me, mockApi, renderAt } from './helpers';

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

describe('sidebar', () => {
  it('collapses to icons, remembers it, and toggles with Ctrl+B', async () => {
    mockApi({ 'GET /me': me('admin') });
    renderAt('/ask', <Route element={<Shell />}><Route path="/ask" element={<p>ask page</p>} /></Route>);
    expect(await screen.findByText('ask page')).toBeInTheDocument();
    expect(screen.getByText('New question')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: 'Collapse sidebar' }));
    expect(screen.queryByText('New question')).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Docs' })).toHaveAttribute('title', 'Docs'); // icon with a tooltip
    expect(localStorage.getItem('dth.sidebar-collapsed')).toBe('1');

    await userEvent.keyboard('{Control>}b{/Control}');
    expect(screen.getByText('New question')).toBeInTheDocument();
    expect(localStorage.getItem('dth.sidebar-collapsed')).toBe('0');
  });

  it('shows six sections, tabs for the current one, and Issues only when it applies', async () => {
    mockApi({ 'GET /me': { ...me('admin'), features: { issues: false } } });
    renderAt('/providers', <Route element={<Shell />}><Route path="/providers" element={<p>models page</p>} /></Route>);
    expect(await screen.findByText('models page')).toBeInTheDocument();
    const main = screen.getByRole('navigation', { name: 'Main' });
    for (const name of ['Docs', 'Repositories', 'Usage', 'Settings']) expect(within(main).getByRole('link', { name })).toBeInTheDocument();
    expect(within(main).queryByRole('link', { name: 'Issues' })).not.toBeInTheDocument(); // no alert source yet
    expect(within(main).queryByRole('link', { name: 'System' })).not.toBeInTheDocument(); // repositories do not talk
    expect(within(main).getByRole('link', { name: 'Settings' })).toHaveAttribute('aria-current', 'page');
    const tabs = screen.getByRole('navigation', { name: 'Settings pages' });
    expect(within(tabs).getAllByRole('link').map((l) => l.textContent)).toEqual(['Connections', 'AI models', 'People', 'Advanced']);
    expect(within(tabs).getByRole('link', { name: 'AI models' })).toHaveAttribute('aria-current', 'page');
  });

  it('shows Issues once an alert source is connected, and hides admin pages from viewers', async () => {
    mockApi({ 'GET /me': { ...me('viewer'), features: { issues: true, system: true } } });
    renderAt('/known-issues', <Route element={<Shell />}><Route path="/known-issues" element={<p>rules page</p>} /></Route>);
    expect(await screen.findByText('rules page')).toBeInTheDocument();
    const main = screen.getByRole('navigation', { name: 'Main' });
    expect(within(main).getByRole('link', { name: 'Issues' })).toHaveAttribute('aria-current', 'page');
    expect(within(main).queryByRole('link', { name: 'Settings' })).not.toBeInTheDocument();
    expect(within(main).getByRole('link', { name: 'System' })).toHaveAttribute('href', '/system');
    expect(within(screen.getByRole('navigation', { name: 'Issues pages' })).getByRole('link', { name: 'Known issues' })).toHaveAttribute('aria-current', 'page');
  });

  it('lists recent questions and searches with Ctrl+K', async () => {
    mockApi({
      'GET /me': me('admin'),
      'GET /threads': { items: [{ id: 't1', title: 'How does checkout retry?', scope: {}, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' }] },
    });
    renderAt('/ask', <Route element={<Shell />}><Route path="/ask" element={<p>ask page</p>} /><Route path="/ask/:id" element={<p>thread page</p>} /></Route>);
    expect(await screen.findByRole('link', { name: 'How does checkout retry?' })).toHaveAttribute('href', '/ask/t1');
    await userEvent.keyboard('{Control>}k{/Control}');
    await userEvent.type(await screen.findByRole('textbox', { name: 'Search pages and questions' }), 'checkout');
    const results = screen.getByRole('listbox', { name: 'Results' });
    expect(results).toHaveTextContent('Ask “checkout”');
    expect(results).toHaveTextContent('How does checkout retry?');
    await userEvent.keyboard('{ArrowDown}{Enter}');
    expect(await screen.findByText('thread page')).toBeInTheDocument();
  });
});

describe('ErrorBoundary', () => {
  it('clears on navigation without remounting what it wraps', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    let mounts = 0;
    function Stable() {
      useState(() => mounts++);
      return <p>stable</p>;
    }
    function Boom({ fail }: { fail: boolean }) {
      if (fail) throw new Error('boom');
      return <p>fine</p>;
    }
    function Harness() {
      const [path, setPath] = useState('/a');
      return (
        <>
          <button onClick={() => setPath('/b')}>go</button>
          <ErrorBoundary resetKey={path}>
            <Stable />
            <Boom fail={path === '/a'} />
          </ErrorBoundary>
        </>
      );
    }
    render(<Harness />);
    expect(screen.getByRole('alert')).toHaveTextContent('boom');
    expect(screen.getByText('Details for a bug report')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'go' }));
    expect(screen.getByText('fine')).toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });
});

describe('environment banner', () => {
  it('names a nonlive deployment on every page, and stays out of production', async () => {
    mockApi({ 'GET /me': me('admin'), 'GET /auth/config': { mode: 'local', sso: false, password: true, environment: 'nonlive' } });
    const { unmount } = renderAt('/ask', <Route element={<Shell />}><Route path="/ask" element={<p>ask page</p>} /></Route>);
    expect(await screen.findByText(/Nonlive environment: not production/)).toBeInTheDocument();
    expect(document.title).toBe('[nonlive] DocTheRepo');
    unmount();
    vi.unstubAllGlobals();
    mockApi({ 'GET /me': me('admin'), 'GET /auth/config': { mode: 'local', sso: false, password: true, environment: 'production' } });
    renderAt('/ask', <Route element={<Shell />}><Route path="/ask" element={<p>ask page</p>} /></Route>);
    expect(await screen.findByText('ask page')).toBeInTheDocument();
    expect(screen.queryByText(/environment: not production/)).not.toBeInTheDocument();
  });
});
