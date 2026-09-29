import { Link } from 'react-router-dom';
import { useConnectors, useLimits, useProviders, useRepos, useRoutes } from '@/api/hooks';
import { Badge, Card, PageHeader, Spinner } from '@/components/ui';

interface Step {
  title: string;
  done: boolean;
  detail: string;
  to: string;
  action: string;
}

/** Setup is the first-run checklist: each step reads live state, so it doubles as a health view. */
export default function Setup() {
  const conns = useConnectors();
  const providers = useProviders();
  const routes = useRoutes();
  const repos = useRepos();
  const limits = useLimits();
  if (conns.isLoading || providers.isLoading || routes.isLoading || repos.isLoading) return <Spinner />;
  const routed = new Set(routes.data?.items.map((r) => r.feature));
  const git = conns.data?.filter((c) => c.type === 'github' || c.type === 'gitlab') ?? [];
  const steps: Step[] = [
    { title: 'Connect GitHub or GitLab', done: git.length > 0, detail: git.length ? `${git.length} git connector(s)` : 'Token or GitHub App; webhooks or polling.', to: '/connectors', action: 'Connectors' },
    { title: 'Add your LLM provider', done: (providers.data?.items.length ?? 0) > 0, detail: 'Your own key: Anthropic, OpenAI, Azure, Bedrock, Vertex, Ollama, or compatible.', to: '/providers', action: 'Providers' },
    { title: 'Route docgen, Q&A, and embeddings', done: ['docgen', 'qa', 'embedding'].every((f) => routed.has(f)), detail: `Routed: ${[...routed].join(', ') || 'none'}`, to: '/providers', action: 'Routing' },
    { title: 'Set spend limits', done: (limits.data?.length ?? 0) > 0, detail: 'A global daily ceiling is seeded; tune per feature or repository.', to: '/spend', action: 'Spend' },
    { title: 'Track repositories', done: (repos.data?.length ?? 0) > 0, detail: `${repos.data?.length ?? 0} tracked`, to: '/repos', action: 'Repositories' },
    { title: 'Dry run, then import existing docs', done: (repos.data ?? []).some((r) => !!r.last_processed_sha), detail: 'See the cost of the first push before it runs; import Markdown so Ask works on day one.', to: '/repos', action: 'Repositories' },
  ];
  const done = steps.filter((s) => s.done).length;
  return (
    <>
      <PageHeader title="Setup" description={`${done} of ${steps.length} steps done.`} />
      <div className="max-w-3xl space-y-3">
        {steps.map((s, i) => (
          <Card key={s.title}>
            <div className="flex items-start justify-between gap-4">
              <div>
                <p className="font-medium">{i + 1}. {s.title} {s.done && <Badge tone="green">done</Badge>}</p>
                <p className="text-sm text-slate-500">{s.detail}</p>
              </div>
              <Link to={s.to} className="shrink-0 text-sm font-medium text-brand-600">{s.action} →</Link>
            </div>
          </Card>
        ))}
      </div>
    </>
  );
}
