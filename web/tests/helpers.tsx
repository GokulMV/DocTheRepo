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

// A sealing key made fresh for each test run, in the Hub's layout: X25519 private (32) | ML-KEM-768 seed (64).
// No key is kept in the repository.
const sealPriv = crypto.getRandomValues(new Uint8Array(96));
const b64 = (b: Uint8Array) => Buffer.from(b).toString('base64');
export const sealKey = {
  kid: 'test-key',
  alg: 'X25519+ML-KEM-768/HKDF-SHA256/AES-256-GCM',
  x25519: b64(x25519.getPublicKey(sealPriv.subarray(0, 32))),
  mlkem768: b64(ml_kem768.keygen(sealPriv.subarray(32)).publicKey),
};

/** writeSealVector saves this run's key and a value sealed to it, for the Go side to open (CI: seal interop). */
export function writeSealVector(dir: string, vector: { purpose: string; plaintext: string; sealed: string }) {
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(path.join(dir, 'seal_key.json'), JSON.stringify({ kid: sealKey.kid, private_hex: Buffer.from(sealPriv).toString('hex') }) + '\n');
  fs.writeFileSync(path.join(dir, 'browser_sealed.json'), JSON.stringify(vector) + '\n');
}

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
