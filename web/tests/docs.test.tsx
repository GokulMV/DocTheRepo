import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route } from 'react-router-dom';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import Docs from '@/pages/Docs';
import { me, mockApi, renderAt } from './helpers';

// Tree: repo r1 → dir "src" (d1) → dir "api" (d2) → file "server.go" (f1); and dir "docs" (d3) → file "guide.md" (f2).
const tree: Record<string, object[]> = {
  '': [{ id: 'd1', kind: 'dir', title: 'src', path: 'src', has_children: true }, { id: 'd3', kind: 'dir', title: 'docs', path: 'docs', has_children: true }],
  d1: [{ id: 'd2', kind: 'dir', title: 'api', path: 'src/api', has_children: true }],
  d2: [{ id: 'f1', kind: 'file', title: 'server.go', path: 'src/api/server.go', has_children: false }],
  d3: [{ id: 'f2', kind: 'file', title: 'guide.md', path: 'docs/guide.md', has_children: false }],
};

function api() {
  return mockApi({
    'GET /me': me('viewer'),
    'GET /docs/tree': (url: string) => {
      const q = new URL(url, 'http://x').searchParams;
      if (!q.get('repo_id')) return { nodes: [{ id: 'r1', kind: 'repo', title: 'acme/shop', path: '', repo_id: 'repo-1', has_children: true }, { id: 'r2', kind: 'repo', title: 'acme/other', path: '', repo_id: 'repo-2', has_children: false }] };
      return { nodes: q.get('repo_id') === 'repo-1' ? tree[q.get('parent_id') ?? ''] : [] };
    },
    'GET /docs/node/f1': { id: 'f1', kind: 'file', title: 'server.go', path: 'src/api/server.go', repo: 'acme/shop', repo_id: 'repo-1', summary: '', markdown: '# Server', updated_at: new Date().toISOString(), ancestors: ['r1', 'd1', 'd2'], chunks: [] },
  });
}

const row = (name: string) => screen.queryByRole('button', { name });

describe('Docs explorer', () => {
  beforeAll(() => { Element.prototype.scrollIntoView = vi.fn(); });
  afterEach(() => vi.unstubAllGlobals());

  it('expands and collapses every folder at once', async () => {
    api();
    renderAt('/docs', <Route path="/docs" element={<Docs />} />);
    await userEvent.click(await screen.findByRole('button', { name: 'Expand all folders' }));
    expect(await screen.findByRole('button', { name: 'server.go' })).toBeInTheDocument();
    expect(await screen.findByRole('button', { name: 'guide.md' })).toBeInTheDocument();
    // A folder closed by hand stays closed until the next Expand all.
    await userEvent.click(row('docs')!);
    await waitFor(() => expect(row('guide.md')).not.toBeInTheDocument());
    await userEvent.click(screen.getByRole('button', { name: 'Collapse all folders' }));
    await waitFor(() => expect(row('src')).not.toBeInTheDocument());
    expect(row('acme/shop')).toHaveAttribute('aria-expanded', 'false');
  });

  it('reveals the open file in the tree', async () => {
    api();
    renderAt('/docs/f1', <Route path="/docs/:nodeId" element={<Docs />} />);
    // Opening a link reveals the file.
    expect(await screen.findByRole('button', { name: 'server.go' })).toHaveAttribute('aria-current', 'page');
    await userEvent.click(screen.getByRole('button', { name: 'Collapse all folders' }));
    await waitFor(() => expect(row('server.go')).not.toBeInTheDocument());
    await userEvent.click(screen.getByRole('button', { name: 'Show this file in the tree' }));
    expect(await screen.findByRole('button', { name: 'server.go' })).toBeInTheDocument();
    expect(row('guide.md')).not.toBeInTheDocument(); // only the path to the file opens
    expect(Element.prototype.scrollIntoView).toHaveBeenCalled();
  });
});
