// Shapes of the Hub API responses (see /api/v1/openapi.json).

export type Role = 'viewer' | 'editor' | 'admin' | 'owner';
export const roleRank: Record<Role, number> = { viewer: 1, editor: 2, admin: 3, owner: 4 };
export const atLeast = (r: Role | undefined, min: Role) => !!r && roleRank[r] >= roleRank[min];

export interface Page<T> {
  items: T[];
  next_cursor: string | null;
}

export interface Me {
  id: string;
  email: string;
  name: string;
  role: Role;
  repo_access: { all: boolean; repo_ids: string[] };
  csrf_token?: string;
}

export interface User {
  id: string;
  email: string;
  name: string;
  role: Role;
  disabled: boolean;
  sso: boolean;
  created_at: string;
  last_login_at?: string;
}

export interface Token {
  id: string;
  name: string;
  scopes: string[];
  expires_at?: string;
  last_used_at?: string;
  created_at: string;
}

export interface Citation {
  n: number;
  type: 'code' | 'doc' | 'confluence' | 'issue';
  title: string;
  url?: string;
  repo?: string;
  repo_id?: string;
  path?: string;
  chunk_id: string;
}

export interface AskResponse {
  thread_id: string;
  message_id: string;
  answer: string;
  citations: Citation[];
  cached: boolean;
  usage: { input_tokens: number; output_tokens: number; cost_usd: number };
}

export interface Thread {
  id: string;
  title: string;
  scope: unknown;
  created_at: string;
  updated_at: string;
  messages?: Message[];
}

export interface Message {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  citations: Citation[];
  model?: string;
  usage?: { input_tokens: number; output_tokens: number; cost_usd: number };
  cached: boolean;
  feedback?: 'up' | 'down';
  created_at: string;
}

export interface TreeNode {
  id: string;
  kind: 'repo' | 'dir' | 'file' | 'section';
  title: string;
  path: string;
  summary?: string;
  repo_id?: string;
  has_children: boolean;
  updated_at?: string;
}

export interface DocNode {
  id: string;
  kind: string;
  title: string;
  path: string;
  repo: string;
  repo_id: string;
  summary: string;
  markdown: string;
  commit_sha?: string;
  updated_at: string;
  parent_id?: string;
  chunks: { chunk_id: string; path: string; symbol: string; language: string; signature: string }[];
}

export interface Entity {
  id: string;
  kind: string;
  key: string;
  name: string;
  repo_id?: string;
  attrs?: Record<string, string>;
  last_seen: string;
}

export interface GraphEdge {
  src: string;
  dst: string;
  kind: string;
}

export interface Neighbourhood {
  nodes: Entity[];
  edges: GraphEdge[];
}

export interface Overview {
  nodes: (Entity & { degree: number })[];
  edges: (GraphEdge & { weight: number })[];
  counts: Record<string, number>;
  truncated: boolean;
}

export interface Shelf {
  id: string;
  slug: string;
  title: string;
  description: string;
  curated: boolean;
  order: number;
  item_count: number;
  items?: ShelfEntry[];
}

export interface ShelfEntry {
  type: string;
  id: string;
  title: string;
  path: string;
  summary?: string;
  repo_id?: string;
  pinned: boolean;
  note?: string;
}

export type PushMode = 'direct' | 'pr_auto_merge' | 'pr_with_approver';

export interface Repo {
  id: string;
  connector_id: string;
  connector_type: string;
  full_name: string;
  default_branch: string;
  tracked_branch?: string;
  docs_path: string;
  last_processed_sha?: string;
  service_name?: string;
  enabled: boolean;
  push: {
    mode: PushMode;
    on_reject: string;
    approver?: string;
    conflict_strategy: string;
    stale_after_ns: number;
  };
}

export interface Connector {
  id: string;
  type: string;
  name: string;
  config: Record<string, string>;
  mode: 'webhook' | 'poll' | 'both';
  poll_seconds: number;
  enabled: boolean;
  health: 'ok' | 'degraded' | 'failing' | 'unknown';
  last_error?: string;
  last_sync_at?: string;
  has_credentials: boolean;
  has_webhook_secret: boolean;
  created_at: string;
  credentials_meta?: SecretMeta;
}

/** What a disable, enable or remove did on GitHub (GitHub App connectors only). */
export interface RemoteResult {
  action: 'suspended' | 'resumed' | 'uninstalled';
  ok: boolean;
  error?: string;
  settings_url?: string;
}

export interface Check {
  name: string;
  ok: boolean;
  detail?: string;
}

export interface Provider {
  id: string;
  kind: string;
  name: string;
  base_url?: string;
  extra: Record<string, string>;
  has_key: boolean;
  redact_pii: boolean;
  enabled: boolean;
  /** The write-only key's hint (last 4 characters of a long key) and when it was set. */
  key_meta?: SecretMeta;
}

export interface SecretMeta {
  hint?: string;
  set_at?: string;
}

export interface Route {
  feature: string;
  provider_id: string;
  model: string;
  max_output_tokens: number;
  context_token_budget: number;
  temperature?: number | null;
  effort: string;
  fallback_provider_id?: string | null;
  fallback_model: string;
  updated_at: string;
}

export interface SpendLimit {
  scope: 'global' | 'feature' | 'provider' | 'repo';
  scope_key: string;
  window: 'day' | 'month';
  max_tokens: number | null;
  max_cost_usd: number | null;
  on_breach: 'block' | 'block_and_alert';
  alert_url?: string;
}

export type JobStatus =
  | 'queued'
  | 'processing'
  | 'done'
  | 'failed'
  | 'aborted'
  | 'spend_blocked'
  | 'pending_approval'
  | 'needs_human'
  | 'dead';

export interface Job {
  job_id: string;
  seq: number;
  type: string;
  repo_id?: string;
  status: JobStatus;
  attempts: number;
  max_attempts: number;
  correlation_id: string;
  error?: string;
  result?: unknown;
  payload: unknown;
  replayed_from?: string;
  created_at: string;
  updated_at: string;
}

export interface Activity {
  kind: 'job' | 'pr' | 'audit';
  ref_id: string;
  action: string;
  repo_id?: string;
  detail?: string;
  at: string;
}

export interface UsagePoint {
  t: string;
  calls: number;
  tokens: number;
  cost_usd: number;
  cached_calls: number;
  blocked: number;
}

export interface UsageReport {
  series: { key: string; points: UsagePoint[] }[];
  totals: UsagePoint;
}

export interface SavingsKind {
  kind: string;
  events: number;
  tokens_avoided: number;
  cost_avoided_usd: number;
}

// --- signals: Inbox, known issues, suggestions (plan § 7.5) ---

export type Severity = 'info' | 'warning' | 'error' | 'critical';
export type IssueStatus = 'new' | 'decoded' | 'suppressed' | 'acknowledged' | 'resolved' | 'regressed';

export interface Issue {
  id: string;
  fingerprint: string;
  kind: 'error' | 'alert' | 'security_finding' | 'log_match' | 'event_bus';
  title: string;
  service: string;
  environment: string;
  status: IssueStatus;
  severity: Severity;
  occurrences: number;
  suppressed_count: number;
  sources: string[];
  first_seen: string;
  last_seen: string;
  repo_id?: string;
  known_issue_id?: string;
  assignee_user_id?: string;
  resolved_at?: string;
  decode_summary?: string;
  decode_confidence?: 'high' | 'medium' | 'low';
  is_actionable?: boolean;
  sparkline: number[];
}

export interface AffectedCode {
  chunk_id: string;
  repo_id?: string;
  path: string;
  symbol?: string;
  start_line?: number;
  end_line?: number;
  reason: string;
}

export interface Commit {
  sha: string;
  author: string;
  message: string;
  at: string;
  url?: string;
}

export interface Decode {
  id: string;
  summary: string;
  probable_cause: string;
  impact: string;
  affected_code: AffectedCode[];
  related_commits: Commit[];
  related_docs: { chunk_id: string; path: string; symbol?: string; source: string }[];
  next_steps: string[];
  confidence: 'high' | 'medium' | 'low';
  is_actionable: boolean;
  suggest_known_issue: boolean;
  provider: string;
  model: string;
  tokens: number;
  cost_usd: number;
  created_at: string;
}

export interface StackFrame {
  module?: string;
  function?: string;
  file?: string;
  line?: number;
  in_app?: boolean;
}

export interface SignalEvent {
  connector_id?: string;
  source: string;
  external_id: string;
  occurred_at: string;
  severity: Severity;
  kind: string;
  service: string;
  environment: string;
  title: string;
  message: string;
  exception_type?: string;
  stack?: StackFrame[];
  attrs?: Record<string, string>;
}

export interface Match {
  fingerprints?: string[];
  message_regex?: string;
  sources?: string[];
  services?: string[];
  environments?: string[];
  attrs?: Record<string, string>;
  min_severity?: Severity | '';
  max_severity?: Severity | '';
}

export interface KnownIssue {
  id: string;
  title: string;
  description: string;
  explanation: string;
  reason: string;
  match: Match;
  action: 'suppress' | 'label_only';
  enabled: boolean;
  expires_at?: string;
  source: string;
  owner_user_id?: string;
  ticket_url?: string;
  jira_key?: string;
  confluence_page_id?: string;
  /** Linked Jira issue's status at the last sync. */
  upstream_status?: string;
  /** Why a sync changed the rule (e.g. "Fixed upstream … verify"). */
  upstream_note?: string;
  /** Imported from a labelled Jira issue / Confluence page; follows the label. */
  label_managed?: boolean;
  hits: number;
  last_hit_at?: string;
  created_at: string;
  updated_at: string;
}

export interface IssueDetail extends Issue {
  decode?: Decode;
  events: SignalEvent[];
  hourly: { t: string; count: number; suppressed: number }[];
  known_issue?: KnownIssue;
  similar_issues: { id: string; title: string; status: IssueStatus }[];
}

export interface Suggestion {
  id: string;
  issue_ids: string[];
  proposed_match: Match;
  rationale: string;
  status: 'pending' | 'accepted' | 'rejected';
  decided_by?: string;
  decided_at?: string;
  created_at: string;
  known_issue_id?: string;
}

export interface RuleTest {
  would_match_last_7d: number;
  sample_issue_ids: string[];
}

export interface Candidate {
  issue_id: string;
  fingerprint: string;
  kind: string;
  title: string;
  service: string;
  environment: string;
  status: string;
  occurrences: number;
  last_seen: string;
  decode_summary?: string;
}

export interface TextSuggestion {
  explanation: string;
  reason: string;
  proposed_match: Match;
  confidence: 'high' | 'medium' | 'low';
  candidates: Candidate[];
  extracted: { exceptions?: string[]; error_codes?: string[]; quoted_messages?: string[]; services?: string[]; resources?: string[] };
  matching_issues_last_7d: number;
  sample_issue_ids: string[];
  /** Set by from-link: the Jira issue or Confluence page that was fetched. */
  link?: LinkedDoc;
}

export interface LinkedDoc {
  source: 'jira' | 'confluence';
  external_id: string;
  title: string;
  url: string;
  status?: string;
  done: boolean;
  jira_key?: string;
  confluence_page_id?: string;
  source_text: string;
}

export const KNOWN_REASONS = ['known_bug', 'wont_fix', 'third_party', 'expected_noise', 'cannot_action', 'in_progress'] as const;
