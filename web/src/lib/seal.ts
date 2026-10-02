// Sealing: secrets are encrypted in the browser to the Hub's hybrid public key (X25519 + ML-KEM-768) before
// they are sent, so they never travel or sit in a request body in the clear. Same format as
// internal/secrets/seal.go:
//   "dthseal1:" + base64url(version | kidLen | kid | x25519 ephemeral(32) | ML-KEM-768 ct(1088) | nonce(12) | AES-256-GCM)
import { gcm } from '@noble/ciphers/aes.js';
import { x25519 } from '@noble/curves/ed25519.js';
import { hkdf } from '@noble/hashes/hkdf.js';
import { sha256 } from '@noble/hashes/sha2.js';
import { randomBytes } from '@noble/hashes/utils.js';
import { ml_kem768 } from '@noble/post-quantum/ml-kem.js';
import { api } from '@/api/client';

export const SEAL_PREFIX = 'dthseal1:';
export type Purpose = 'provider.api_key' | 'connector.credentials' | 'connector.webhook_secret';

export interface PublicSealKey {
  kid: string;
  alg: string;
  x25519: string; // base64
  mlkem768: string; // base64
}

const enc = new TextEncoder();
const b64 = (s: string) => Uint8Array.from(atob(s), (c) => c.charCodeAt(0));
const b64url = (b: Uint8Array) => {
  let s = '';
  for (const x of b) s += String.fromCharCode(x);
  return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
};
const concat = (...parts: Uint8Array[]) => {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let o = 0;
  for (const p of parts) {
    out.set(p, o);
    o += p.length;
  }
  return out;
};

/** sealWith encrypts value to key for purpose. */
export function sealWith(key: PublicSealKey, value: string, purpose: Purpose): string {
  const serverX = b64(key.x25519);
  const ek = b64(key.mlkem768);
  const kid = enc.encode(key.kid);
  if (serverX.length !== 32 || ek.length !== 1184 || kid.length > 255) throw new Error('invalid sealing key');
  const ephPriv = x25519.utils.randomSecretKey();
  const eph = x25519.getPublicKey(ephPriv);
  const ssX = x25519.getSharedSecret(ephPriv, serverX);
  const { cipherText, sharedSecret } = ml_kem768.encapsulate(ek);
  const info = enc.encode(`dthseal-v1|${key.kid}|${purpose}`);
  const aesKey = hkdf(sha256, concat(sharedSecret, ssX), concat(eph, serverX), info, 32);
  const nonce = randomBytes(12);
  const body = gcm(aesKey, nonce, info).encrypt(enc.encode(value));
  aesKey.fill(0);
  ephPriv.fill(0);
  return SEAL_PREFIX + b64url(concat(Uint8Array.of(1, kid.length), kid, eph, cipherText, nonce, body));
}

let cached: { key: PublicSealKey; at: number } | undefined;

/** seal fetches the Hub's sealing key (cached for 10 minutes) and seals value; empty stays empty. */
export async function seal(value: string, purpose: Purpose): Promise<string> {
  if (!value) return value;
  if (!cached || Date.now() - cached.at > 600_000) cached = { key: await api.get<PublicSealKey>('/seal/key'), at: Date.now() };
  return sealWith(cached.key, value, purpose);
}
