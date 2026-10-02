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
});
