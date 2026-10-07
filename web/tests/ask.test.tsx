import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import Ask from '@/pages/Ask';
import { me, mockApi, renderAt } from './helpers';

const sse = (name: string, data: unknown) => `event: ${name}\ndata: ${JSON.stringify(data)}\n\n`;

describe('Ask', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('shows how it looked further, and replaces a withdrawn answer', async () => {
    const enc = new TextEncoder();
    let push!: (s: string) => void;
    let end!: () => void;
    const body = new ReadableStream<Uint8Array>({
      start(c) {
        push = (s) => c.enqueue(enc.encode(s));
        end = () => c.close();
      },
    });
    mockApi({
      'GET /me': me('viewer'),
      'GET /repos': { items: [] },
      'GET /threads': { items: [], next_cursor: null },
      'POST /ask': () => new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }),
      'GET /threads/t1': { id: 't1', title: 'q', messages: [] },
    });
    renderAt('/ask', <><Route path="/ask" element={<Ask />} /><Route path="/ask/:threadId" element={<Ask />} /></>);
    await userEvent.type(await screen.findByLabelText('Question'), 'how often do keys rotate?');
    await userEvent.click(screen.getByRole('button', { name: 'Ask' }));

    push(sse('delta', { text: 'I think maybe…' }));
    expect(await screen.findByText('I think maybe…')).toBeInTheDocument();
    push(sse('reset', {}));
    push(sse('status', { step: 1, action: 'search', input: 'key rotation', reason: 'looking for rotation code' }));
    push(sse('status', { step: 2, action: 'read_file', input: 'keys/rotation.go', reason: 'reading it' }));
    expect(await screen.findByText(/The first search found little/)).toBeInTheDocument();
    expect(screen.getByText('key rotation')).toBeInTheDocument();
    expect(screen.getByText('keys/rotation.go')).toBeInTheDocument();
    expect(screen.queryByText('I think maybe…')).not.toBeInTheDocument();
    push(sse('delta', { text: 'Every 24 hours [1].' }));
    expect(await screen.findByText(/Every 24 hours/)).toBeInTheDocument();
    push(sse('done', { thread_id: 't1', message_id: 'm1', answer: 'Every 24 hours [1].', citations: [], cached: false, investigated: true, usage: {} }));
    end();
    await waitFor(() => expect(screen.queryByText(/The first search found little/)).not.toBeInTheDocument());
  });

  it('shows what source picking saved, per answer and for the conversation', async () => {
    const sift = (kept: number, saved: number, usd: number) => ({ candidates: 20, kept, tokens_saved: saved, judge_tokens: 3000, cost_usd: 0.001, saved_usd: usd, calibrated: true, model: 'jev-latest' });
    const answer = (id: string, content: string, s?: unknown) => ({ id, role: 'assistant', content, citations: [], cached: false, sift: s, usage: { input_tokens: 900, output_tokens: 40, cost_usd: 0.01 }, created_at: '2026-10-03T10:00:00Z' });
    mockApi({
      'GET /me': me('viewer'),
      'GET /repos': { items: [] },
      'GET /threads': { items: [], next_cursor: null },
      'GET /threads/t1': { id: 't1', title: 'Key rotation', messages: [
        { id: 'u1', role: 'user', content: 'how do keys rotate?', citations: [], cached: false, created_at: '2026-10-03T10:00:00Z' },
        answer('a1', 'Every 24 hours [1].', sift(3, 9400, 0.046)),
        { id: 'u2', role: 'user', content: 'and who runs it?', citations: [], cached: false, created_at: '2026-10-03T10:01:00Z' },
        answer('a2', 'A cron job [1].', sift(2, 6100, 0.03)),
        { id: 'u3', role: 'user', content: 'thanks', citations: [], cached: false, created_at: '2026-10-03T10:02:00Z' },
        answer('a3', 'You are welcome.'),
      ] },
    });
    renderAt('/ask/t1', <Route path="/ask/:threadId" element={<Ask />} />);
    expect(await screen.findByText('Read 3 of 20 sources · 9.4k tokens saved ($0.05)')).toBeInTheDocument();
    expect(screen.getByText('Read 2 of 20 sources · 6.1k tokens saved ($0.03)')).toBeInTheDocument();
    expect(screen.getByText('Source picking saved 15.5k tokens ($0.08) in this conversation')).toBeInTheDocument();
    expect(screen.getByText('Read 3 of 20 sources · 9.4k tokens saved ($0.05)').parentElement).toHaveAttribute('title', expect.stringContaining('Judge (jev-latest): 3,000 tokens'));
  });

  it('shows how far each answer can be trusted, and why', async () => {
    mockApi({
      'GET /me': me('viewer'),
      'GET /repos': { items: [] },
      'GET /threads': { items: [], next_cursor: null },
      'GET /threads/t1': { id: 't1', title: 'Refunds', messages: [
        { id: 'u1', role: 'user', content: 'how are refunds approved?', citations: [], cached: false, created_at: '2026-10-03T10:00:00Z' },
        { id: 'a1', role: 'assistant', content: 'Finance approves them [1].', citations: [], cached: false, created_at: '2026-10-03T10:00:00Z',
          confidence: { score: 0.52, label: 'low', why: ['it cites a document of low confidence', 'it rests on a single source'] } },
      ] },
    });
    renderAt('/ask/t1', <Route path="/ask/:threadId" element={<Ask />} />);
    const badge = await screen.findByText('Low confidence');
    expect(badge.parentElement).toHaveAttribute('title', 'Why: it cites a document of low confidence; it rests on a single source');
  });
});
