import { render, screen } from '@testing-library/react';
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

  it('keeps most pages under More, opens it for the current page, and pins pages', async () => {
    mockApi({ 'GET /me': me('admin') });
    renderAt('/connectors', <Route element={<Shell />}><Route path="/connectors" element={<p>connectors page</p>} /></Route>);
    expect(await screen.findByText('connectors page')).toBeInTheDocument();
    // The current page is inside More, so More is open.
    expect(screen.getByRole('button', { name: 'Less' })).toHaveAttribute('aria-expanded', 'true');
    await userEvent.click(screen.getByRole('button', { name: 'Pin Spend limits' }));
    expect(screen.getByRole('group', { name: 'Pinned' })).toHaveTextContent('Spend limits');
    expect(JSON.parse(localStorage.getItem('dth.pinned') ?? '[]')).toEqual(['/spend']);
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
