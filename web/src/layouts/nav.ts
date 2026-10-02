import {
  Activity, BookOpen, ChartColumn, FileCog, FolderGit2, Inbox, KeyRound, Library, MessageSquareText, Network, Plug, Rocket, ShieldCheck, SlidersHorizontal,
  Users, Wallet, Workflow, type LucideIcon,
} from 'lucide-react';
import type { Role } from '@/api/types';

export interface NavItem {
  to: string;
  label: string;
  min: Role;
  icon: LucideIcon;
  /** One line shown under the page title. */
  blurb?: string;
}

export const NAV: { section: string; items: NavItem[] }[] = [
  {
    section: 'Knowledge',
    items: [
      { to: '/ask', label: 'Ask', min: 'viewer', icon: MessageSquareText },
      { to: '/docs', label: 'Docs', min: 'viewer', icon: BookOpen },
      { to: '/palace', label: 'Palace', min: 'viewer', icon: Network },
      { to: '/architecture', label: 'Architecture', min: 'viewer', icon: Workflow },
      { to: '/library', label: 'Library', min: 'viewer', icon: Library },
    ],
  },
  {
    section: 'Signals',
    items: [
      { to: '/inbox', label: 'Inbox', min: 'viewer', icon: Inbox },
      { to: '/known-issues', label: 'Known issues', min: 'viewer', icon: ShieldCheck },
    ],
  },
  {
    section: 'Operations',
    items: [
      { to: '/activity', label: 'Activity', min: 'viewer', icon: Activity },
      { to: '/analytics', label: 'Analytics', min: 'viewer', icon: ChartColumn },
      { to: '/repos', label: 'Repositories', min: 'viewer', icon: FolderGit2 },
    ],
  },
  {
    section: 'Administration',
    items: [
      { to: '/connectors', label: 'Connectors', min: 'admin', icon: Plug },
      { to: '/providers', label: 'Providers & routing', min: 'admin', icon: SlidersHorizontal },
      { to: '/spend', label: 'Spend limits', min: 'admin', icon: Wallet },
      { to: '/users', label: 'Users & access', min: 'admin', icon: Users },
      { to: '/sign-in', label: 'Sign-in & SSO', min: 'owner', icon: KeyRound },
      { to: '/settings-file', label: 'Settings file', min: 'admin', icon: FileCog },
      { to: '/setup', label: 'Setup', min: 'admin', icon: Rocket },
    ],
  },
];

/** navItemFor finds the section a path belongs to (/inbox/123 → Inbox). */
export function navItemFor(pathname: string): NavItem | undefined {
  for (const s of NAV) {
    for (const i of s.items) {
      if (pathname === i.to || pathname.startsWith(i.to + '/')) return i;
    }
  }
  return undefined;
}

/** The pages shown at the top of the sidebar; the rest sit under "More". */
export const PRIMARY = ['/docs', '/architecture', '/inbox'];
