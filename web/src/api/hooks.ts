import { useMutation, useQuery, useQueryClient, type QueryKey } from '@tanstack/react-query';
import { api, qs, setCSRF } from './client';
import type {
  AuthConfig,
  Activity,
  Connector,
  DocNode,
  Entity,
  Issue,
  IssueDetail,
  Job,
  KnownIssue,
  Me,
  Neighbourhood,
  Overview,
  Page,
  Provider,
  Repo,
  Route,
  SavingsKind,
  Shelf,
  SpendLimit,
  Suggestion,
  Thread,
  Token,
  TreeNode,
  UsageReport,
  User,
} from './types';

export const keys = {
  me: ['me'] as const,
  repos: ['repos'] as const,
  connectors: ['connectors'] as const,
  providers: ['providers'] as const,
  routes: ['routes'] as const,
  limits: ['spend-limits'] as const,
  shelves: ['shelves'] as const,
  users: ['users'] as const,
  tokens: ['tokens'] as const,
  threads: ['threads'] as const,
};

export function useMe() {
  return useQuery({
    queryKey: keys.me,
    queryFn: async () => {
      const me = await api.get<Me>('/me');
      setCSRF(me.csrf_token);
      return me;
    },
    retry: false,
    staleTime: 60_000,
  });
}

/** useInvalidating wraps a mutation that refreshes the given queries on success. */
export function useInvalidating<TVars, TResult = unknown>(fn: (v: TVars) => Promise<TResult>, ...invalidate: QueryKey[]) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => Promise.all(invalidate.map((k) => qc.invalidateQueries({ queryKey: k }))),
  });
}

// --- docs, palace, library ---

export const useTree = (repoId?: string, parentId?: string) =>
  useQuery({
    queryKey: ['tree', repoId ?? '', parentId ?? ''],
    queryFn: () => api.get<{ nodes: TreeNode[] }>(`/docs/tree${qs({ repo_id: repoId, parent_id: parentId })}`).then((r) => r.nodes),
  });

export const useDocNode = (id?: string) =>
  useQuery({ queryKey: ['doc', id], queryFn: () => api.get<DocNode>(`/docs/node/${id}`), enabled: !!id });

export const useEntities = (kind: string, q: string) =>
  useQuery({
    queryKey: ['entities', kind, q],
    queryFn: () => api.get<Page<Entity>>(`/palace/entities${qs({ kind, q, limit: 100 })}`),
  });

export const useOverview = (kinds: string[]) =>
  useQuery({
    queryKey: ['palace-overview', kinds],
    queryFn: () => api.get<Overview>(`/palace/overview${qs({ kinds: kinds.join(','), limit: 300 })}`),
  });

export const useGraph = (id?: string, depth = 1) =>
  useQuery({
    queryKey: ['graph', id, depth],
    queryFn: () => api.get<Neighbourhood>(`/palace/entities/${id}/graph${qs({ depth })}`),
    enabled: !!id,
  });

export const useShelves = () =>
  useQuery({ queryKey: keys.shelves, queryFn: () => api.get<{ shelves: Shelf[] }>('/library/shelves').then((r) => r.shelves) });

export const useShelf = (slug?: string) =>
  useQuery({ queryKey: ['shelf', slug], queryFn: () => api.get<Shelf>(`/library/shelves/${slug}`), enabled: !!slug });

// --- ask ---

export const useThreads = () => useQuery({ queryKey: keys.threads, queryFn: () => api.get<Page<Thread>>('/threads?limit=50') });

export const useThread = (id?: string) =>
  useQuery({ queryKey: ['thread', id], queryFn: () => api.get<Thread>(`/threads/${id}`), enabled: !!id });

// --- administration ---

export const useRepos = () => useQuery({ queryKey: keys.repos, queryFn: () => api.get<{ items: Repo[] }>('/repos').then((r) => r.items) });

export const useConnectors = (enabled = true) =>
  useQuery({
    queryKey: keys.connectors,
    queryFn: () => api.get<{ items: Connector[] }>('/connectors').then((r) => r.items),
    enabled,
  });

export const useProviders = () =>
  useQuery({ queryKey: keys.providers, queryFn: () => api.get<{ items: Provider[]; kinds: string[] }>('/providers') });

export const useRoutes = () =>
  useQuery({ queryKey: keys.routes, queryFn: () => api.get<{ items: Route[]; features: string[] }>('/routes') });

export const useLimits = () =>
  useQuery({ queryKey: keys.limits, queryFn: () => api.get<{ items: SpendLimit[] }>('/spend/limits').then((r) => r.items) });

export const useAuthConfig = () => useQuery({ queryKey: ['auth-config'], queryFn: () => api.get<AuthConfig>('/auth/config') });

export const useUsers = () => useQuery({ queryKey: keys.users, queryFn: () => api.get<Page<User>>('/users?limit=200') });

export const useTokens = () =>
  useQuery({ queryKey: keys.tokens, queryFn: () => api.get<{ items: Token[] }>('/tokens').then((r) => r.items) });

// --- jobs, activity, analytics ---

export const useJobs = (filter: { status?: string; type?: string; repo_id?: string }) =>
  useQuery({
    queryKey: ['jobs', filter],
    queryFn: () => api.get<Page<Job>>(`/jobs${qs({ ...filter, limit: 50 })}`),
    refetchInterval: 10_000,
  });

export const useActivity = (types: string) =>
  useQuery({
    queryKey: ['activity', types],
    queryFn: () => api.get<Page<Activity>>(`/activity${qs({ types, limit: 100 })}`),
    refetchInterval: 15_000,
  });

export const useUsage = (groupBy: string, granularity: string, from: string) =>
  useQuery({
    queryKey: ['usage', groupBy, granularity, from],
    queryFn: () => api.get<UsageReport>(`/analytics/usage${qs({ group_by: groupBy, granularity, from })}`),
  });

export const useSavings = (from: string) =>
  useQuery({
    queryKey: ['savings', from],
    queryFn: () => api.get<{ by_kind: SavingsKind[]; total: SavingsKind }>(`/analytics/savings${qs({ from })}`),
  });

export const usePipelineStats = () =>
  useQuery({
    queryKey: ['pipeline-stats'],
    queryFn: () =>
      api.get<{
        jobs: { type: string; status: string; jobs: number; p50_seconds: number; p95_seconds: number }[];
        triage_abort_rate: number;
        freshness: { repo_id: string; repo: string; last_processed_sha: string; last_success?: string }[];
      }>('/analytics/pipeline'),
  });

// --- signals ---

export interface IssueFilters {
  status?: string;
  severity?: string;
  source?: string;
  service?: string;
  env?: string;
  kind?: string;
  q?: string;
  since?: string;
}

export const useIssues = (f: IssueFilters) =>
  useQuery({
    queryKey: ['issues', f],
    queryFn: () => api.get<Page<Issue>>(`/issues${qs({ ...f, limit: 100 })}`),
    refetchInterval: 30_000,
  });

export const useIssue = (id?: string) =>
  useQuery({ queryKey: ['issue', id], queryFn: () => api.get<IssueDetail>(`/issues/${id}`), enabled: !!id });

export const useKnownIssues = () =>
  useQuery({ queryKey: ['known-issues'], queryFn: () => api.get<{ items: KnownIssue[] }>('/known-issues').then((r) => r.items) });

export const useSuggestions = (status = 'pending', enabled = true) =>
  useQuery({
    queryKey: ['suggestions', status],
    queryFn: () => api.get<{ items: Suggestion[] }>(`/known-issues/suggestions${qs({ status })}`).then((r) => r.items),
    enabled,
  });
