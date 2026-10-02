import {
  BookOpen, Box, Boxes, Braces, Database, FileCode2, FolderGit2, Globe, KeyRound, Package, Radio, Server, Ticket, UserRound, Users, type LucideIcon,
} from 'lucide-react';

export interface KindMeta {
  label: string;
  plural: string;
  color: string;
  /** Cytoscape node shape. */
  shape: string;
  icon: LucideIcon;
}

// One look per entity kind, shared by the graph, the legend, and the details panel.
export const KINDS: Record<string, KindMeta> = {
  repo: { label: 'Repository', plural: 'Repositories', color: '#3b6ef6', shape: 'round-rectangle', icon: FolderGit2 },
  service: { label: 'Service', plural: 'Services', color: '#8b5cf6', shape: 'ellipse', icon: Server },
  endpoint: { label: 'Endpoint', plural: 'Endpoints', color: '#f97316', shape: 'round-tag', icon: Globe },
  queue_topic: { label: 'Topic', plural: 'Topics & queues', color: '#ec4899', shape: 'diamond', icon: Radio },
  datastore: { label: 'Datastore', plural: 'Datastores', color: '#14b8a6', shape: 'barrel', icon: Database },
  env_var: { label: 'Env var', plural: 'Env vars', color: '#eab308', shape: 'round-triangle', icon: KeyRound },
  dependency: { label: 'Dependency', plural: 'Dependencies', color: '#a855f7', shape: 'hexagon', icon: Package },
  module: { label: 'Module', plural: 'Modules', color: '#06b6d4', shape: 'round-rectangle', icon: Boxes },
  file: { label: 'File', plural: 'Files', color: '#64748b', shape: 'rectangle', icon: FileCode2 },
  symbol: { label: 'Symbol', plural: 'Symbols', color: '#10b981', shape: 'ellipse', icon: Braces },
  confluence_page: { label: 'Confluence page', plural: 'Confluence pages', color: '#0ea5e9', shape: 'round-rectangle', icon: BookOpen },
  jira_issue: { label: 'Jira issue', plural: 'Jira issues', color: '#2563eb', shape: 'round-rectangle', icon: Ticket },
  endpoint_group: { label: 'Endpoints', plural: 'Endpoint groups', color: '#f97316', shape: 'round-tag', icon: Globe },
  dependency_group: { label: 'Libraries', plural: 'Libraries', color: '#a855f7', shape: 'hexagon', icon: Package },
  team: { label: 'Team', plural: 'Teams', color: '#22c55e', shape: 'octagon', icon: Users },
  person: { label: 'Person', plural: 'People', color: '#22c55e', shape: 'ellipse', icon: UserRound },
};

const fallback: KindMeta = { label: 'Entity', plural: 'Other', color: '#94a3b8', shape: 'ellipse', icon: Box };

export const kindMeta = (k: string): KindMeta => KINDS[k] ?? { ...fallback, label: k.replace(/_/g, ' '), plural: k.replace(/_/g, ' ') };

/** Kinds shown on the overview by default (high-level architecture, not every symbol). */
export const OVERVIEW_KINDS = ['repo', 'service', 'queue_topic', 'datastore', 'confluence_page'];

/** Endpoints are many: the overview counts them on their repository or service instead of drawing each. */
export const COUNTED_KINDS = ['endpoint'];
