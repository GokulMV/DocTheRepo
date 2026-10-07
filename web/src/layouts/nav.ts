import { BookOpen, ChartColumn, FolderGit2, Inbox, Network, Settings, type LucideIcon } from 'lucide-react';
import type { Role } from '@/api/types';

/** Tab is one page inside a section, shown as a tab above the page. */
export interface Tab {
  to: string;
  label: string;
  min?: Role;
  /** Other paths that belong to this tab (detail pages, old addresses). */
  also?: string[];
}

/** Features shown only when the Hub says so (others are shown unless it says no). */
export const OPT_IN_FEATURES = ['system'];

export interface NavItem {
  to: string;
  label: string;
  min: Role;
  icon: LucideIcon;
  /** One line shown under the page title. */
  blurb?: string;
  /** Pages in this section; the first is the section's own address. */
  tabs?: Tab[];
  /** Shown only when this feature applies (the Hub reports it in /me). */
  feature?: 'issues' | 'system';
}

/** NAV is the sidebar: Ask (the "New question" row) plus these sections. */
export const NAV: NavItem[] = [
  {
    to: '/docs', label: 'Docs', min: 'viewer', icon: BookOpen,
    tabs: [
      { to: '/docs', label: 'Docs' },
      { to: '/architecture', label: 'Architecture' },
      { to: '/library', label: 'Team docs' },
    ],
  },
  // Shown only when tracked repositories talk to each other (APIs, events, shared packages).
  { to: '/system', label: 'System', min: 'viewer', icon: Network, feature: 'system' },
  {
    to: '/inbox', label: 'Issues', min: 'viewer', icon: Inbox, feature: 'issues',
    tabs: [
      { to: '/inbox', label: 'Inbox' },
      { to: '/known-issues', label: 'Known issues' },
    ],
  },
  {
    to: '/repos', label: 'Repositories', min: 'viewer', icon: FolderGit2,
    tabs: [
      { to: '/repos', label: 'Repositories' },
      { to: '/activity', label: 'Activity' },
      { to: '/security', label: 'Security', min: 'editor' },
    ],
  },
  { to: '/analytics', label: 'Usage', min: 'viewer', icon: ChartColumn },
  {
    to: '/connectors', label: 'Settings', min: 'admin', icon: Settings,
    tabs: [
      { to: '/connectors', label: 'Connections' },
      { to: '/providers', label: 'AI models' },
      { to: '/users', label: 'People', also: ['/sign-in'] },
      { to: '/settings-file', label: 'Advanced', also: ['/spend', '/setup'] },
    ],
  },
];

const under = (pathname: string, to: string) => pathname === to || pathname.startsWith(to + '/');

/** tabFor finds the tab a path belongs to (/inbox/123 → Inbox). */
export function tabFor(item: NavItem, pathname: string): Tab | undefined {
  return item.tabs?.find((t) => under(pathname, t.to) || t.also?.some((a) => under(pathname, a)));
}

/** navItemFor finds the section a path belongs to (/known-issues → Issues). */
export function navItemFor(pathname: string): NavItem | undefined {
  return NAV.find((i) => under(pathname, i.to) || !!tabFor(i, pathname));
}
