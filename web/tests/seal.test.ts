import { describe, expect, it } from 'vitest';
import { SEAL_PREFIX, sealWith } from '@/lib/seal';

import { openSealed as unseal, sealKey as key, writeSealVector } from './helpers';

describe('sealing', () => {
  it('seals to the Hub key in the documented format', () => {
    const v = sealWith(key, 'sk-ant-from-the-browser', 'provider.api_key');
    expect(v.startsWith(SEAL_PREFIX)).toBe(true);
    expect(v).not.toContain('sk-ant');
    expect(unseal(v, 'provider.api_key')).toBe('sk-ant-from-the-browser');
    expect(() => unseal(v, 'connector.credentials')).toThrow(); // bound to its field
    expect(sealWith(key, 'same', 'provider.api_key')).not.toBe(sealWith(key, 'same', 'provider.api_key'));
    // CI hands this to the Go tests, which must open it (internal/secrets TestUnsealsWhatTheBrowserSealed).
    if (process.env.SEAL_VECTOR_DIR) writeSealVector(process.env.SEAL_VECTOR_DIR, { purpose: 'provider.api_key', plaintext: 'sk-ant-from-the-browser', sealed: v });
  });

  it('rejects a malformed key', () => {
    expect(() => sealWith({ ...key, x25519: btoa('short') }, 'x', 'provider.api_key')).toThrow('invalid sealing key');
  });
});
