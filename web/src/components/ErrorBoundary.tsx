import { Component, type ErrorInfo, type ReactNode } from 'react';
import { RefreshCw, TriangleAlert } from 'lucide-react';
import { recentErrors } from '@/lib/errorLog';

// Messages browsers use when a lazily loaded page chunk cannot be fetched — typically a tab left open
// across a Hub upgrade, whose old chunk names no longer exist.
const chunkError = /dynamically imported module|Importing a module script failed|error loading dynamically|ChunkLoadError|Loading chunk|module script/i;
const RELOAD_KEY = 'dth.chunk-reload';

export function isChunkError(e: unknown): boolean {
  return chunkError.test(e instanceof Error ? `${e.name} ${e.message}` : String(e));
}

/** reloadOnce reloads the page for a stale chunk, at most once a minute (so a real outage cannot loop). */
function reloadOnce(): boolean {
  try {
    const last = Number(sessionStorage.getItem(RELOAD_KEY) ?? 0);
    if (Date.now() - last < 60_000) return false;
    sessionStorage.setItem(RELOAD_KEY, String(Date.now()));
  } catch {
    return false;
  }
  window.location.reload();
  return true;
}

interface State {
  error?: unknown;
  reloading?: boolean;
  where?: string;
}

/**
 * ErrorBoundary keeps one broken page from blanking the whole app: a stale chunk reloads the page once
 * (picking up the new build); anything else shows what failed, with a way out.
 */
export class ErrorBoundary extends Component<{ children: ReactNode; resetKey?: string }, State> {
  state: State = {};

  // Navigating away clears the error without remounting the subtree, so a page switch never flashes.
  componentDidUpdate(prev: { resetKey?: string }) {
    if (this.state.error && prev.resetKey !== this.props.resetKey) this.setState({ error: undefined, reloading: false, where: undefined });
  }

  static getDerivedStateFromError(error: unknown): State {
    return { error };
  }

  componentDidCatch(error: unknown, info: ErrorInfo) {
    this.setState({ where: info.componentStack ?? undefined });
    if (isChunkError(error) && reloadOnce()) this.setState({ reloading: true });
  }

  details(): string {
    const e = this.state.error;
    const stack = e instanceof Error ? e.stack ?? `${e.name}: ${e.message}` : String(e);
    const comp = (this.state.where ?? '').trim().split('\n').slice(0, 8).join('\n');
    const recent = recentErrors();
    return `${window.location.pathname}\n${navigator.userAgent}\n\n${stack}\n\nComponents:\n${comp}${recent ? `\n\nRecent errors:\n${recent}` : ''}`;
  }

  render() {
    const { error, reloading } = this.state;
    if (!error) return this.props.children;
    const stale = isChunkError(error);
    return (
      <div role="alert" className="mx-auto mt-16 max-w-lg rounded-2xl border border-amber-300/60 bg-amber-50 p-6 text-center dark:border-amber-500/30 dark:bg-amber-500/10">
        <TriangleAlert className="mx-auto h-8 w-8 text-amber-500" aria-hidden />
        <h2 className="mt-3 text-base font-semibold">{stale ? 'The Hub was updated' : 'This page failed to load'}</h2>
        <p className="mt-1 text-sm text-slate-600 dark:text-slate-300">
          {reloading ? 'Reloading to pick up the new version…' : stale ? 'Reload to get the latest version of the app.' : error instanceof Error ? error.message : String(error)}
        </p>
        {!reloading && (
          <button
            type="button"
            onClick={() => window.location.reload()}
            className="mt-4 inline-flex items-center gap-2 rounded-lg bg-brand-600 px-4 py-2 text-sm font-medium text-white hover:bg-brand-700"
          >
            <RefreshCw className="h-4 w-4" aria-hidden /> Reload
          </button>
        )}
        {!stale && (
          <details className="mt-4 text-left text-xs text-slate-600 dark:text-slate-300">
            <summary className="cursor-pointer select-none text-center">Details for a bug report</summary>
            <pre className="mt-2 max-h-60 overflow-auto whitespace-pre-wrap rounded-lg bg-white/70 p-2 font-mono text-[11px] dark:bg-black/30">{this.details()}</pre>
            <button type="button" className="mt-2 text-brand-600 hover:underline dark:text-brand-300" onClick={() => navigator.clipboard?.writeText(this.details()).catch(() => undefined)}>
              Copy details
            </button>
          </details>
        )}
      </div>
    );
  }
}
