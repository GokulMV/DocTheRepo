const MAX_ATTEMPTS = 3;

/**
 * fetchWithRetry retries HTTP 429 and 503 responses up to three attempts, waiting for the Retry-After
 * header (seconds) when the server sends one and 500 ms × attempt otherwise.
 */
export async function fetchWithRetry(url: string, init?: RequestInit): Promise<Response> {
  for (let attempt = 1; ; attempt++) {
    const res = await fetch(url, init);
    if ((res.status !== 429 && res.status !== 503) || attempt === MAX_ATTEMPTS) return res;
    const retryAfter = Number(res.headers.get('Retry-After'));
    const waitMs = retryAfter > 0 ? retryAfter * 1000 : 500 * attempt;
    await new Promise((r) => setTimeout(r, waitMs));
  }
}
