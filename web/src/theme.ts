import { useSyncExternalStore } from 'react';

export type ThemeChoice = 'light' | 'dark' | 'system';

const KEY = 'dth.theme';
const media = typeof window !== 'undefined' ? window.matchMedia?.('(prefers-color-scheme: dark)') : undefined;
const listeners = new Set<() => void>();

function read(): ThemeChoice {
  try {
    const v = localStorage.getItem(KEY);
    if (v === 'light' || v === 'dark') return v;
  } catch {
    // Storage can be unavailable (private windows, blocked site data); fall back to the OS.
  }
  return 'system';
}

let choice: ThemeChoice = read();

/** isDark reports what is on screen now, after resolving "system" against the OS setting. */
export function isDark(): boolean {
  return choice === 'dark' || (choice === 'system' && !!media?.matches);
}

function apply() {
  document.documentElement.classList.toggle('dark', isDark());
  document.documentElement.style.colorScheme = isDark() ? 'dark' : 'light';
  listeners.forEach((l) => l());
}

/** initTheme applies the saved choice before the first render and follows OS changes while on "system". */
export function initTheme() {
  apply();
  media?.addEventListener?.('change', () => choice === 'system' && apply());
}

export function setTheme(next: ThemeChoice) {
  choice = next;
  try {
    if (next === 'system') localStorage.removeItem(KEY);
    else localStorage.setItem(KEY, next);
  } catch {
    // Not saved; the choice still holds for this page load.
  }
  apply();
}

function subscribe(l: () => void) {
  listeners.add(l);
  return () => listeners.delete(l);
}

/** useTheme returns the saved choice and whether the page is dark, re-rendering when either changes. */
export function useTheme(): { choice: ThemeChoice; dark: boolean } {
  const c = useSyncExternalStore(subscribe, () => choice);
  const dark = useSyncExternalStore(subscribe, isDark);
  return { choice: c, dark };
}
