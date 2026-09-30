export interface Flag {
  name: string;
  enabled: boolean;
  /** Percentage rollout, 0–100. */
  rolloutPercent: number;
}

/**
 * Feature flag check with a sticky percentage rollout: the user id is hashed (FNV-1a) into a 0–99 bucket,
 * so the same user always gets the same answer while the rollout percentage grows.
 */
export function isEnabled(flag: Flag, userId: string): boolean {
  if (!flag.enabled) return false;
  let h = 0x811c9dc5;
  for (const ch of flag.name + ':' + userId) {
    h ^= ch.charCodeAt(0);
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return h % 100 < flag.rolloutPercent;
}
