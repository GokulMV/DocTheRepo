import { screen } from '@testing-library/react';
import { Route } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import System from '@/pages/System';
import { me, mockApi, renderAt } from './helpers';

const repos = [
  { id: 'w', connector_id: 'c', connector_type: 'github', full_name: 'acme/web', default_branch: 'main', last_processed_sha: 'aaa111', docs_path: '', enabled: true, push: {} },
  { id: 'l', connector_id: 'c', connector_type: 'github', full_name: 'acme/lib', default_branch: 'main', last_processed_sha: 'bbb222', docs_path: '', enabled: true, push: {} },
];
const link = { from_repo: 'w', from_name: 'acme/web', to_repo: 'l', to_name: 'acme/lib', kind: 'library', via: 'github.com/acme/lib', path: 'go.mod', line: 5, n: 1 };

describe('System architecture', () => {
  it('shows the map, the write-up and the links found in the code', async () => {
    mockApi({
      'GET /me': me('viewer'), 'GET /repos': { items: repos },
      'GET /system': { links: [link], repos: [{ id: 'w', name: 'acme/web' }, { id: 'l', name: 'acme/lib' }], complete: true, diagram: 'flowchart LR\n  r0 --> r1\n',
        doc: { id: 's', title: 'System architecture', at_a_glance: 'The web app uses the shared money library.', confidence: 0.82, label: 'high', status: 'ok', updated_at: new Date().toISOString(),
          sections: [{ key: 'interactions', title: 'How the parts talk', markdown: 'web formats money with lib [acme/web/main.go:5].', score: 0.9, label: 'high' }] } },
    });
    renderAt('/system', <Route path="/system" element={<System />} />);
    expect(await screen.findByRole('region', { name: 'At a glance' })).toHaveTextContent('shared money library');
    expect(screen.getByRole('link', { name: 'acme/web/main.go:5' })).toHaveAttribute('href', 'https://github.com/acme/web/blob/aaa111/main.go#L5');
    expect(screen.getByRole('cell', { name: 'Package' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'go.mod:5' })).toHaveAttribute('href', 'https://github.com/acme/web/blob/aaa111/go.mod#L5');
  });

  it('explains when the write-up covers repositories the reader cannot see', async () => {
    mockApi({ 'GET /me': me('viewer'), 'GET /repos': { items: repos }, 'GET /system': { links: [link], repos: [], complete: false, diagram: 'flowchart LR\n' } });
    renderAt('/system', <Route path="/system" element={<System />} />);
    expect(await screen.findByText(/covers repositories you cannot read/)).toBeInTheDocument();
  });
});
