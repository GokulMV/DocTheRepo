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
    expect(screen.getByText('Knowledge')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: 'Collapse sidebar' }));
    expect(screen.queryByText('Knowledge')).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Docs' })).toHaveAttribute('title', 'Docs'); // icon with a tooltip
    expect(localStorage.getItem('dth.sidebar-collapsed')).toBe('1');

    await userEvent.keyboard('{Control>}b{/Control}');
    expect(screen.getByText('Knowledge')).toBeInTheDocument();
    expect(localStorage.getItem('dth.sidebar-collapsed')).toBe('0');
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
