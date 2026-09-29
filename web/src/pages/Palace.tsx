import { useCallback, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useEntities, useGraph } from '@/api/hooks';
import { GraphView } from '@/components/GraphView';
import { Badge, Card, Empty, ErrorNote, Input, PageHeader, Select, Spinner } from '@/components/ui';

const KINDS = ['', 'repo', 'service', 'module', 'file', 'symbol', 'endpoint', 'env_var', 'dependency', 'datastore', 'queue_topic', 'team', 'person'];

export default function Palace() {
  const { entityId } = useParams();
  const nav = useNavigate();
  const [kind, setKind] = useState('service');
  const [q, setQ] = useState('');
  const [depth, setDepth] = useState(1);
  const ents = useEntities(kind, q);
  const graph = useGraph(entityId, depth);
  const select = useCallback((id: string) => nav(`/palace/${id}`), [nav]);
  const focus = graph.data?.nodes.find((n) => n.id === entityId);
  return (
    <>
      <PageHeader title="Palace" description="The knowledge graph: services, endpoints, symbols, env vars, dependencies, topics, and owners — extracted from code and linked across repositories." />
      <div className="grid gap-6 lg:grid-cols-[20rem_1fr]">
        <Card title="Find">
          <div className="space-y-2">
            <Select aria-label="Kind" value={kind} onChange={(e) => setKind(e.target.value)}>
              {KINDS.map((k) => (
                <option key={k} value={k}>
                  {k || 'all kinds'}
                </option>
              ))}
            </Select>
            <Input aria-label="Search entities" placeholder="name or key…" value={q} onChange={(e) => setQ(e.target.value)} />
          </div>
          {ents.isLoading && <Spinner />}
          <ErrorNote error={ents.error} />
          <ul className="mt-3 max-h-[28rem] space-y-0.5 overflow-y-auto text-sm">
            {ents.data?.items.map((e) => (
              <li key={e.id}>
                <button className="w-full truncate rounded px-2 py-1 text-left hover:bg-slate-100 dark:hover:bg-slate-800" onClick={() => select(e.id)} title={e.key}>
                  <Badge>{e.kind}</Badge> {e.name}
                </button>
              </li>
            ))}
            {ents.data?.items.length === 0 && <li className="text-xs text-slate-500">No matches.</li>}
          </ul>
        </Card>
        <div className="min-w-0">
          {!entityId && <Empty title="Select an entity">Its neighbourhood appears here; click nodes to walk the graph.</Empty>}
          {graph.isLoading && <Spinner />}
          <ErrorNote error={graph.error} />
          {graph.data && (
            <Card
              title={focus ? `${focus.kind}: ${focus.name}` : 'Graph'}
              actions={
                <Select aria-label="Depth" value={depth} onChange={(e) => setDepth(Number(e.target.value))} className="w-28">
                  {[1, 2, 3].map((d) => (
                    <option key={d} value={d}>
                      {d} hop{d > 1 ? 's' : ''}
                    </option>
                  ))}
                </Select>
              }
            >
              <GraphView graph={graph.data} focus={entityId} onSelect={select} />
              {focus?.attrs && Object.keys(focus.attrs).length > 0 && (
                <dl className="mt-3 grid grid-cols-[8rem_1fr] gap-x-3 text-xs">
                  {Object.entries(focus.attrs).map(([k, v]) => (
                    <div key={k} className="contents">
                      <dt className="text-slate-500">{k}</dt>
                      <dd className="font-mono">{v}</dd>
                    </div>
                  ))}
                </dl>
              )}
            </Card>
          )}
        </div>
      </div>
    </>
  );
}
