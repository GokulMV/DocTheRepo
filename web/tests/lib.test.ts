import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, api, qs, setCSRF } from '@/api/client';
import { parseSSE } from '@/lib/sse';
import { num, relTime, shortSha, usd } from '@/lib/format';
import { atLeast } from '@/api/types';
import { mockApi } from './helpers';

afterEach(() => vi.unstubAllGlobals());

describe('api client', () => {
  it('sends the CSRF header only on mutations', async () => {
    const { calls } = mockApi({ 'GET /me': { ok: 1 }, 'POST /tokens': { token: 't' } });
    setCSRF('abc');
    await api.get('/me');
    await api.post('/tokens', { name: 'x' });
    const h0 = new Headers(calls[0].init?.headers);
    const h1 = new Headers(calls[1].init?.headers);
    expect(h0.get('X-CSRF-Token')).toBeNull();
    expect(h1.get('X-CSRF-Token')).toBe('abc');
    expect(h1.get('Content-Type')).toBe('application/json');
    expect(calls[1].init?.credentials).toBe('same-origin');
  });

  it('surfaces the error contract', async () => {
    mockApi({
      'POST /ask': () =>
        new Response(JSON.stringify({ error: { code: 'SPEND_BLOCKED', message: 'ceiling', correlation_id: 'cid' } }), { status: 402 }),
    });
    await expect(api.post('/ask', {})).rejects.toMatchObject({ status: 402, code: 'SPEND_BLOCKED', correlationId: 'cid' });
    await expect(api.post('/ask', {})).rejects.toBeInstanceOf(ApiError);
  });

  it('handles 204 and builds query strings', async () => {
    mockApi({ 'DELETE /tokens/1': () => new Response(null, { status: 204 }) });
    await expect(api.del('/tokens/1')).resolves.toBeUndefined();
    expect(qs({ a: 'x y', b: undefined, c: '', d: 3 })).toBe('?a=x+y&d=3');
    expect(qs({})).toBe('');
  });
});

describe('sse', () => {
  it('parses complete events and keeps the partial remainder', () => {
    const { events, rest } = parseSSE('event: delta\ndata: {"text":"a"}\n\n: comment\n\nevent: done\ndata: {"x":1}\n\nevent: delt');
    expect(events).toEqual([
      { event: 'delta', data: '{"text":"a"}' },
      { event: 'done', data: '{"x":1}' },
    ]);
    expect(rest).toBe('event: delt');
  });
  it('joins multi-line data and normalises CRLF', () => {
    expect(parseSSE('data: a\r\ndata: b\r\n\r\n').events).toEqual([{ event: 'message', data: 'a\nb' }]);
  });
});

describe('format', () => {
  it('formats numbers, money, times, shas', () => {
    expect(num(1234)).toBe('1.2k');
    expect(num(2_500_000)).toBe('2.5M');
    expect(usd(0.001)).toBe('<$0.01');
    expect(usd(3)).toBe('$3.00');
    expect(relTime(new Date(Date.now() - 90_000).toISOString())).toBe('1m ago');
    expect(relTime(undefined)).toBe('—');
    expect(shortSha('0123456789abcdef')).toBe('01234567');
  });
  it('orders roles', () => {
    expect(atLeast('admin', 'editor')).toBe(true);
    expect(atLeast('viewer', 'admin')).toBe(false);
    expect(atLeast(undefined, 'viewer')).toBe(false);
  });
});
