// A short in-memory log of uncaught errors, attached to the error card's bug-report details.
const MAX = 5;
const entries: string[] = [];

function push(s: string) {
  entries.push(`${new Date().toISOString()} ${s}`);
  if (entries.length > MAX) entries.shift();
}

export function installErrorLog() {
  window.addEventListener('error', (e) => push(`error: ${e.message} @ ${e.filename}:${e.lineno}:${e.colno}${e.error?.stack ? `\n${e.error.stack}` : ''}`));
  window.addEventListener('unhandledrejection', (e) => {
    const r = e.reason;
    push(`unhandled rejection: ${r instanceof Error ? r.stack ?? r.message : String(r)}`);
  });
}

export function recentErrors(): string {
  return entries.join('\n\n');
}
