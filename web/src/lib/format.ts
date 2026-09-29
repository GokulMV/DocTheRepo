export function relTime(iso?: string, now = Date.now()): string {
  if (!iso) return '—';
  const s = Math.round((now - new Date(iso).getTime()) / 1000);
  if (s < 0) return 'just now';
  if (s < 60) return `${s}s ago`;
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}

export function num(n: number | undefined): string {
  if (n === undefined || n === null) return '—';
  if (Math.abs(n) >= 1e6) return `${(n / 1e6).toFixed(1)}M`;
  if (Math.abs(n) >= 1e3) return `${(n / 1e3).toFixed(1)}k`;
  return String(n);
}

export function usd(n: number | undefined): string {
  if (n === undefined || n === null) return '—';
  return n < 0.01 && n > 0 ? '<$0.01' : `$${n.toFixed(2)}`;
}

export function shortSha(sha?: string): string {
  return sha ? sha.slice(0, 8) : '—';
}

export function daysAgoISO(days: number, now = Date.now()): string {
  return new Date(now - days * 86400_000).toISOString().replace(/\.\d{3}Z$/, 'Z');
}
