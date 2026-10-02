import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render } from '@testing-library/react';
import type { ReactNode } from 'react';
import { MemoryRouter, Routes } from 'react-router-dom';
import fs from 'node:fs';
import path from 'node:path';
import { gcm } from '@noble/ciphers/aes.js';
import { x25519 } from '@noble/curves/ed25519.js';
import { hkdf } from '@noble/hashes/hkdf.js';
import { sha256 } from '@noble/hashes/sha2.js';
import { ml_kem768 } from '@noble/post-quantum/ml-kem.js';
import { vi } from 'vitest';

// The sealing key internal/secrets generated for tests (its private half is test-only).
export const sealFixtureDir = path.resolve(__dirname, '../../internal/secrets/testdata');
const sealFixture = JSON.parse(fs.readFileSync(path.join(sealFixtureDir, 'seal_key.json'), 'utf8'));
export const sealKey = { kid: sealFixture.kid, alg: 'X25519+ML-KEM-768/HKDF-SHA256/AES-256-GCM', x25519: sealFixture.x25519, mlkem768: sealFixture.mlkem768 };
const sealPriv = Uint8Array.from(Buffer.from(sealFixture.private_hex, 'hex'));

/** openSealed decodes a sealed value with the test key, independently of the app's code (from the format spec). */
export function openSealed(v: string, purpose: string): string {
  if (!v.startsWith('dthseal1:')) throw new Error(`not sealed: ${v}`);
  const raw = Uint8Array.from(Buffer.from(v.slice('dthseal1:'.length), 'base64url'));
  const kl = raw[1];
  const kid = new TextDecoder().decode(raw.subarray(2, 2 + kl));
  let p = 2 + kl;
  const eph = raw.subarray(p, (p += 32));
  const ct = raw.subarray(p, (p += 1088));
  const nonce = raw.subarray(p, (p += 12));
  const body = raw.subarray(p);
  const xPriv = sealPriv.subarray(0, 32);
  const { secretKey } = ml_kem768.keygen(sealPriv.subarray(32));
  const ssKEM = ml_kem768.decapsulate(ct, secretKey);
  const ssX = x25519.getSharedSecret(xPriv, eph);
  const info = new TextEncoder().encode(`dthseal-v1|${kid}|${purpose}`);
  const salt = new Uint8Array([...eph, ...x25519.getPublicKey(xPriv)]);
  const k = hkdf(sha256, new Uint8Array([...ssKEM, ...ssX]), salt, info, 32);
  return new TextDecoder().decode(gcm(k, nonce, info).decrypt(body));
}

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
    const h = routes[key] ?? (key === 'GET /seal/key' ? sealKey : undefined);
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
      <MemoryRouter initialEntries={[path]}>
        <Routes>{routes}</Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

export const me = (role: string) => ({ id: 'u1', email: 'ann@acme.com', name: 'Ann', role, repo_access: { all: true, repo_ids: [] }, csrf_token: 'csrf-1' });
