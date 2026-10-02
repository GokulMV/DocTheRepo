import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ErrorBoundary, isChunkError } from '@/components/ErrorBoundary';
import Palace from '@/pages/Palace';
import { me, mockApi, renderAt } from './helpers';

// Cytoscape needs a real canvas; the graph itself is exercised in the browser E2E run.
vi.mock('cytoscape', () => {
  const coll = { addClass: () => coll, removeClass: () => coll, filter: () => coll, unselect: () => coll, select: () => coll, length: 0, not: () => coll, edges: () => coll };
  const cy = { on: () => undefined, destroy: () => undefined, getElementById: () => coll, nodes: () => coll, elements: () => coll, animate: () => undefined };
  return { default: () => cy };
});

afterEach(() => vi.unstubAllGlobals());

const overview = {
  nodes: [
    { id: 'r1', kind: 'repo', key: 'acme/checkout', name: 'acme/checkout', degree: 5, last_seen: '' },
    { id: 't1', kind: 'queue_topic', key: 'orders.created', name: 'orders.created', degree: 2, last_seen: '' },
  ],
  edges: [{ src: 'r1', dst: 't1', kind: 'publishes', weight: 3 }],
  counts: { repo: 1, queue_topic: 1, symbol: 40 },
  truncated: false,
};

describe('Palace overview', () => {
  it('shows kinds with counts and refetches when a kind is toggled', async () => {
    const { calls } = mockApi({ 'GET /me': me('viewer'), 'GET /palace/overview': overview });
    renderAt('/palace', <Route path="/palace" element={<Palace />} />);
    expect(await screen.findByText('2 nodes · 1 links', { exact: false })).toBeInTheDocument();
    const symbols = screen.getByRole('button', { name: /Symbols\s*40/ });
    expect(symbols).toHaveAttribute('aria-pressed', 'false');
    await userEvent.click(symbols);
    await waitFor(() => expect(calls.some((c) => c.url.includes('/palace/overview') && decodeURIComponent(c.url).includes('symbol'))).toBe(true));
    expect(screen.getByRole('img', { name: 'Knowledge graph' })).toBeInTheDocument();
    expect(screen.getByText('Repository')).toBeInTheDocument(); // legend
  });

  it('explains an empty Palace', async () => {
    mockApi({ 'GET /me': me('viewer'), 'GET /palace/overview': { nodes: [], edges: [], counts: {}, truncated: false } });
    renderAt('/palace', <Route path="/palace" element={<Palace />} />);
    expect(await screen.findByText('The Palace is empty')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /Track a repository/ })).toHaveAttribute('href', '/repos');
  });
});

describe('ErrorBoundary', () => {
  it('reloads once for a stale chunk and explains anything else', async () => {
    const reload = vi.fn();
    vi.stubGlobal('location', { ...window.location, reload });
    sessionStorage.clear();
    const Stale = () => {
      throw new TypeError('Failed to fetch dynamically imported module: /assets/Docs-old.js');
    };
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    const { unmount } = render(<ErrorBoundary><Stale /></ErrorBoundary>);
    expect(await screen.findByText('The Hub was updated')).toBeInTheDocument();
    expect(reload).toHaveBeenCalledTimes(1);
    unmount();
    render(<ErrorBoundary><Stale /></ErrorBoundary>);
    expect(reload).toHaveBeenCalledTimes(1); // not again within a minute: no reload loop

    const Broken = () => {
      throw new Error('boom');
    };
    render(<ErrorBoundary><Broken /></ErrorBoundary>);
    expect(await screen.findByText('This page failed to load')).toBeInTheDocument();
    expect(isChunkError(new Error('Importing a module script failed.'))).toBe(true);
  });
});
