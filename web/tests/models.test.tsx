import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import Providers from '@/pages/Providers';
import { mockApi, renderAt } from './helpers';

const anthropic = { id: 'p1', kind: 'anthropic', name: 'anthropic', extra: {}, has_key: true, redact_pii: false, enabled: true };
const openai = { id: 'p2', kind: 'openai', name: 'openai', extra: {}, has_key: true, redact_pii: false, enabled: true };
const features = ['docgen', 'docgen_fast', 'qa', 'decode', 'triage', 'suggest', 'decide', 'security', 'sift', 'embedding'];
const put = (feature: string) => ({ [`PUT /routes/${feature}`]: {} });

describe('AI models', () => {
  it('routes every feature from a main, a fast and a search model', async () => {
    const { calls } = mockApi({
      'GET /providers': { items: [anthropic, openai], kinds: ['anthropic', 'openai'] },
      'GET /routes': { items: [], features },
      'GET /spend/limits': { items: [] },
      'GET /analytics/usage': { series: [], totals: { calls: 0, tokens: 0, cost_usd: 0, cache_read_tokens: 0, cached_calls: 0, blocked: 0 } },
      'GET /docs/mode': { mode: 'balanced', source: 'default' },
      ...Object.assign({}, ...features.map(put)),
    });
    renderAt('/providers', <Route path="/providers" element={<Providers />} />);
    const card = (await screen.findByRole('heading', { name: 'Models' })).closest('section')!;
    await userEvent.selectOptions(within(card).getByLabelText('Main model: provider'), 'p1');
    await userEvent.type(within(card).getByLabelText('Main model: model'), 'claude-sonnet-5-5');
    await userEvent.selectOptions(within(card).getByLabelText('Fast model (optional): provider'), 'p1');
    await userEvent.type(within(card).getByLabelText('Fast model (optional): model'), 'claude-haiku-4-5');
    await userEvent.selectOptions(within(card).getByLabelText('Search model (optional): provider'), 'p2');
    await userEvent.type(within(card).getByLabelText('Search model (optional): model'), 'text-embedding-3-small');
    await userEvent.click(within(card).getByRole('button', { name: 'Save models' }));
    await waitFor(() => expect(calls.filter((c) => c.method === 'PUT').length).toBe(9));
    const routed = Object.fromEntries(calls.filter((c) => c.method === 'PUT').map((c) => [c.url.split('/routes/')[1], JSON.parse(String(c.init?.body)).model]));
    expect(routed).toEqual({
      docgen: 'claude-sonnet-5-5', qa: 'claude-sonnet-5-5', decode: 'claude-sonnet-5-5', suggest: 'claude-sonnet-5-5', security: 'claude-sonnet-5-5',
      docgen_fast: 'claude-haiku-4-5', triage: 'claude-haiku-4-5', sift: 'claude-haiku-4-5', embedding: 'text-embedding-3-small',
    });
  });

  it('keeps one monthly budget among the other limits', async () => {
    const daily = { scope: 'global', scope_key: '', window: 'day', max_tokens: 2000000, max_cost_usd: null, on_breach: 'block' };
    const { calls } = mockApi({
      'GET /providers': { items: [anthropic], kinds: ['anthropic'] },
      'GET /routes': { items: [], features },
      'GET /spend/limits': { items: [daily] },
      'PUT /spend/limits': {},
      'GET /analytics/usage': { series: [], totals: { calls: 3, tokens: 9000, cost_usd: 12.5, cache_read_tokens: 0, cached_calls: 0, blocked: 0 } },
      'GET /docs/mode': { mode: 'balanced', source: 'default' },
    });
    renderAt('/providers', <Route path="/providers" element={<Providers />} />);
    expect(await screen.findByText('$12.50')).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText('Monthly budget in US dollars'), '200');
    await userEvent.click(within(screen.getByRole('heading', { name: 'Monthly budget' }).closest('section')!).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true));
    const sent = JSON.parse(String(calls.find((c) => c.method === 'PUT')!.init?.body)).items;
    expect(sent).toEqual([daily, { scope: 'global', scope_key: '', window: 'month', max_tokens: null, max_cost_usd: 200, on_breach: 'block' }]);
  });
});
