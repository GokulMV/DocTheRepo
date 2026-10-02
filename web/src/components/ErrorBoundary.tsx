import { Component, type ReactNode } from 'react';
import { RefreshCw, TriangleAlert } from 'lucide-react';

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
}

/**
 * ErrorBoundary keeps one broken page from blanking the whole app: a stale chunk reloads the page once
 * (picking up the new build); anything else shows what failed, with a way out.
 */
export class ErrorBoundary extends Component<{ children: ReactNode }, State> {
  state: State = {};

  static getDerivedStateFromError(error: unknown): State {
    return { error };
  }

  componentDidCatch(error: unknown) {
    if (isChunkError(error) && reloadOnce()) this.setState({ reloading: true });
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
      </div>
    );
  }
}
