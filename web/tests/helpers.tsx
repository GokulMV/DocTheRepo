import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render } from '@testing-library/react';
import type { ReactNode } from 'react';
import { MemoryRouter, Routes } from 'react-router-dom';
import { vi } from 'vitest';

type Handler = (url: string, init?: RequestInit) => unknown;

/** mockApi stubs fetch: routes "METHOD /path" (path without /api/v1 and query) to handlers. */
export function mockApi(routes: Record<string, Handler | object>) {
  const calls: { method: string; url: string; init?: RequestInit }[] = [];
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = (init?.method ?? 'GET').toUpperCase();
    calls.push({ method, url, init });
    const path = url.replace(/^\/api\/v1/, '').split('?')[0];
    const key = `${method} ${path}`;
    const h = routes[key];
    if (h === undefined) {
      return new Response(JSON.stringify({ error: { code: 'NOT_FOUND', message: key, correlation_id: 'c' } }), { status: 404 });
    }
    const body = typeof h === 'function' ? (h as Handler)(url, init) : h;
    if (body instanceof Response) return body;
    return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });
  });
  vi.stubGlobal('fetch', fetchMock);
  return { fetchMock, calls };
}

export function renderAt(path: string, routes: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]} future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
        <Routes>{routes}</Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

export const me = (role: string) => ({ id: 'u1', email: 'ann@acme.com', name: 'Ann', role, repo_access: { all: true, repo_ids: [] }, csrf_token: 'csrf-1' });
