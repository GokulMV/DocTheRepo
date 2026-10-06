import { screen } from '@testing-library/react';
import { Route } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import Analytics from '@/pages/Analytics';
import { me, mockApi, renderAt } from './helpers';

// Charts measure their container; jsdom has no ResizeObserver.
globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;

const empty = { series: [], totals: { calls: 0, tokens: 0, cost_usd: 0, cache_read_tokens: 0, cached_calls: 0, blocked: 0 } };
const base = {
  'GET /me': me('viewer'),
  'GET /analytics/usage': empty,
  'GET /analytics/savings': { by_kind: [{ kind: 'evidence_sifted', events: 2, tokens_avoided: 15500, cost_avoided_usd: 0.07 }], total: { kind: 'total', events: 2, tokens_avoided: 15500, cost_avoided_usd: 0.07 } },
  'GET /analytics/pipeline': { jobs: [], triage_abort_rate: 0, freshness: [] },
};

describe('Analytics', () => {
  it('shows what Ask source picking trimmed and saved', async () => {
    mockApi({
      ...base,
      'GET /analytics/sift': { answers: 10, picked: 8, trimmed: 6, candidates: 160, kept: 40, tokens_saved: 52000, judge_tokens: 20000, judge_cost_usd: 0.02, saved_usd: 0.24, explored: 3,
        daily: [{ day: '2026-10-02T00:00:00Z', picked: 3, tokens_saved: 20000, saved_usd: 0.1 }, { day: '2026-10-03T00:00:00Z', picked: 5, tokens_saved: 32000, saved_usd: 0.14 }] },
    });
    renderAt('/analytics', <Route path="/analytics" element={<Analytics />} />);
    expect(await screen.findByText(/It ran on 8 of 10 answers and trimmed 6/)).toBeInTheDocument();
    expect(screen.getByText('52.0k')).toBeInTheDocument();
    expect(screen.getByText('$0.24')).toBeInTheDocument();
    expect(screen.getByText('25%')).toBeInTheDocument();
    expect(screen.getByText('40 of 160 retrieved')).toBeInTheDocument();
    expect(screen.getByText('20.0k tokens, 3 files explored')).toBeInTheDocument();
    expect(screen.getByText('Ask sources trimmed before answering')).toBeInTheDocument();
  });

  it('says how to turn source picking on when it never ran', async () => {
    mockApi({ ...base, 'GET /analytics/sift': { answers: 4, picked: 0, trimmed: 0, candidates: 0, kept: 0, tokens_saved: 0, judge_tokens: 0, judge_cost_usd: 0, saved_usd: 0, explored: 0, daily: [] } });
    renderAt('/analytics', <Route path="/analytics" element={<Analytics />} />);
    expect(await screen.findByText(/No answers used the source picker in this period/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Settings → AI models' })).toHaveAttribute('href', '/providers');
  });
});
