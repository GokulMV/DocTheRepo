// Thin fetch wrapper for the Hub API: same-origin cookies, the CSRF header on mutations, and the
// server's error contract surfaced as ApiError.

export interface ErrorBody {
  error: { code: string; message: string; details?: Record<string, unknown>; correlation_id: string };
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public correlationId?: string,
    public details?: Record<string, unknown>,
  ) {
    super(message);
  }
}

let csrfToken = '';

/** setCSRF stores the session's CSRF token (from login or GET /me). */
export function setCSRF(token: string | undefined) {
  csrfToken = token ?? '';
}

/** getCSRF returns the token for raw fetch calls (the Ask stream). */
export function getCSRF(): string {
  return csrfToken;
}

export const API = '/api/v1';

function isMutation(method: string) {
  return !['GET', 'HEAD', 'OPTIONS'].includes(method.toUpperCase());
}

export async function toApiError(res: Response): Promise<ApiError> {
  let body: ErrorBody | undefined;
  try {
    body = (await res.json()) as ErrorBody;
  } catch {
    /* non-JSON error */
  }
  const e = body?.error;
  return new ApiError(res.status, e?.code ?? `HTTP_${res.status}`, e?.message ?? res.statusText, e?.correlation_id, e?.details);
}

export async function request<T>(method: string, path: string, body?: unknown, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (body !== undefined) headers.set('Content-Type', 'application/json');
  if (isMutation(method) && csrfToken) headers.set('X-CSRF-Token', csrfToken);
  const res = await fetch(API + path, {
    ...init,
    method,
    headers,
    credentials: 'same-origin',
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) throw await toApiError(res);
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}

/** postForm sends multipart form data (file uploads); the browser sets the boundary. */
export async function postForm<T>(path: string, form: FormData): Promise<T> {
  const headers = new Headers();
  if (csrfToken) headers.set('X-CSRF-Token', csrfToken);
  const res = await fetch(API + path, { method: 'POST', headers, credentials: 'same-origin', body: form });
  if (!res.ok) throw await toApiError(res);
  return (await res.json()) as T;
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body ?? {}),
  put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body ?? {}),
  patch: <T>(path: string, body?: unknown) => request<T>('PATCH', path, body ?? {}),
  del: <T = void>(path: string) => request<T>('DELETE', path),
};

/** qs builds a query string from defined, non-empty values. */
export function qs(params: Record<string, string | number | boolean | undefined | null>): string {
  const u = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== '') u.set(k, String(v));
  }
  const s = u.toString();
  return s ? `?${s}` : '';
}
