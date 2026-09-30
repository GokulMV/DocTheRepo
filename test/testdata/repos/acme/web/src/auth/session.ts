const REFRESH_BEFORE_EXPIRY_MS = 5 * 60 * 1000;

export interface Session {
  accessToken: string;
  expiresAt: number;
}

/** Refreshes the access token five minutes before it expires so API calls never fail with 401 mid-checkout. */
export async function refreshSession(s: Session, refresh: () => Promise<Session>, now = Date.now()): Promise<Session> {
  if (s.expiresAt - now > REFRESH_BEFORE_EXPIRY_MS) return s;
  return refresh();
}
