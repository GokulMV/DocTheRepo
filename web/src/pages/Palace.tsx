import { useCallback, useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { ArrowLeft, Crosshair, Info, Maximize2, MessageSquareText, Network, Search, X } from 'lucide-react';
import { useEntities, useGraph, useOverview } from '@/api/hooks';
import type { Entity } from '@/api/types';
import { GraphView, type GEdge, type GNode, type LayoutName } from '@/components/GraphView';
import { COUNTED_KINDS, kindMeta, KINDS, OVERVIEW_KINDS } from '@/components/palaceKinds';
import { capFirst } from '@/lib/labels';
import { Badge, Button, Card, Empty, ErrorNote, Input, PageHeader, Select, Spinner, cx } from '@/components/ui';

const LAYOUTS: [LayoutName, string][] = [['force', 'Force'], ['concentric', 'Concentric'], ['hierarchy', 'Hierarchy'], ['circle', 'Circle'], ['grid', 'Grid']];

function KindDot({ kind, className }: { kind: string; className?: string }) {
  return <span className={cx('inline-block h-2.5 w-2.5 shrink-0 rounded-full', className)} style={{ background: kindMeta(kind).color }} aria-hidden />;
}

/** Details shows the selected entity: what it is, its attributes, and what it connects to on the map. */
function Details({ node, nodes, edges, onSelect, onClose }: {
  node: GNode & Partial<Entity>;
  nodes: Map<string, GNode>;
  edges: GEdge[];
  onSelect: (id: string) => void;
  onClose: () => void;
}) {
  const nav = useNavigate();
  const m = kindMeta(node.kind);
  const links = edges
    .filter((e) => e.src === node.id || e.dst === node.id)
    .map((e) => ({ kind: e.kind, out: e.src === node.id, other: nodes.get(e.src === node.id ? e.dst : e.src), weight: e.weight }))
    .filter((l) => l.other);
  const grouped = new Map<string, typeof links>();
  for (const l of links) {
    const k = `${l.out ? '' : '← '}${l.kind.replace(/_/g, ' ')}${l.out ? ' →' : ''}`;
    grouped.set(k, [...(grouped.get(k) ?? []), l]);
  }
  return (
    <Card
      title={
        <span className="flex items-center gap-2">
          <span className="grid h-7 w-7 place-items-center rounded-lg text-white" style={{ background: m.color }}>
            <m.icon className="h-4 w-4" aria-hidden />
          </span>
          {m.label}
        </span>
      }
      actions={<button type="button" onClick={onClose} aria-label="Close details" className="rounded p-1 text-slate-400 hover:bg-slate-100 dark:hover:bg-white/[0.06]"><X className="h-4 w-4" /></button>}
    >
      <p className="break-words text-base font-semibold">{node.name}</p>
      {node.key && node.key !== node.name && <p className="mt-0.5 break-all font-mono text-xs text-slate-500">{node.key}</p>}
      <div className="mt-3 flex flex-wrap gap-2">
        <Button size="sm" onClick={() => nav(`/palace/${node.id}`)}><Crosshair className="h-3.5 w-3.5" aria-hidden /> Focus</Button>
        <Button size="sm" variant="secondary" onClick={() => nav(`/ask?q=${encodeURIComponent(`What does ${m.label.toLowerCase()} ${node.name} do, and what depends on it?`)}`)}>
          <MessageSquareText className="h-3.5 w-3.5" aria-hidden /> Ask about it
        </Button>
      </div>
      {node.attrs && Object.keys(node.attrs).length > 0 && (
        <dl className="mt-4 grid grid-cols-[6.5rem_1fr] gap-x-3 gap-y-1 text-xs">
          {Object.entries(node.attrs).map(([k, v]) => (
            <div key={k} className="contents">
              <dt className="text-slate-500">{k}</dt>
              <dd className="break-all font-mono">{k === 'url' ? <a className="text-brand-600 hover:underline dark:text-brand-300" href={v} target="_blank" rel="noreferrer">{v}</a> : v}</dd>
            </div>
          ))}
        </dl>
      )}
      <div className="mt-4 space-y-3">
        <p className="text-xs font-semibold uppercase tracking-wide text-slate-400">Connections on the map · {links.length}</p>
        {links.length === 0 && <p className="text-xs text-slate-500">None visible. Focus it to see its full neighbourhood.</p>}
        {[...grouped.entries()].map(([k, ls]) => (
          <div key={k}>
            <p className="mb-1 text-xs font-medium text-slate-500">{k}</p>
            <ul className="space-y-0.5">
              {ls.slice(0, 12).map((l) => (
                <li key={l.other!.id}>
                  <button type="button" onClick={() => onSelect(l.other!.id)} className="flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-sm hover:bg-slate-100 dark:hover:bg-white/[0.05]">
                    <KindDot kind={l.other!.kind} />
                    <span className="truncate">{l.other!.name}</span>
                    {(l.weight ?? 1) > 1 && <span className="ml-auto text-xs text-slate-400">×{l.weight}</span>}
                  </button>
                </li>
              ))}
              {ls.length > 12 && <li className="px-2 text-xs text-slate-400">+{ls.length - 12} more</li>}
            </ul>
          </div>
        ))}
      </div>
    </Card>
  );
}

const VERBS: Record<string, string> = {
  publishes: 'publishes to', subscribes: 'listens to', uses_datastore: 'uses', calls: 'calls', depends_on: 'depends on', deployed_as: 'runs as',
};

/** Summary describes each repository and service in a sentence, from the links on the map. */
function Summary({ nodes, edges, endpoints, onSelect }: {
  nodes: GNode[];
  edges: GEdge[];
  endpoints: Map<string, number>;
  onSelect: (id: string) => void;
}) {
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const subjects = nodes.filter((n) => n.kind === 'service' || n.kind === 'repo');
  const rows = subjects
    .map((n) => {
      const parts: { verb: string; names: string[] }[] = [];
      for (const [kind, verb] of Object.entries(VERBS)) {
        const names = edges.filter((e) => e.src === n.id && e.kind === kind).map((e) => byId.get(e.dst)?.name).filter((x): x is string => !!x);
        if (names.length) parts.push({ verb, names });
      }
      return { n, parts, eps: endpoints.get(n.id) ?? 0 };
    })
    .sort((a, b) => b.parts.length + b.eps / 100 - (a.parts.length + a.eps / 100));
  if (!rows.length) return null;
  return (
    <Card title="What the map shows">
      <ul className="space-y-3 text-sm">
        {rows.slice(0, 12).map(({ n, parts, eps }) => (
          <li key={n.id}>
            <button type="button" onClick={() => onSelect(n.id)} className="inline-flex items-center gap-2 font-medium hover:underline">
              <KindDot kind={n.kind} />{n.name}
            </button>
            <p className="mt-0.5 text-slate-600 dark:text-slate-400">
              {eps > 0 && <>Exposes <b className="font-medium text-slate-800 dark:text-slate-200">{eps} endpoint{eps === 1 ? '' : 's'}</b>{parts.length ? '; ' : '.'}</>}
              {parts.map((pt, i) => (
                <span key={pt.verb}>
                  {i === 0 && !eps ? capFirst(pt.verb) : pt.verb} {pt.names.slice(0, 3).join(', ')}{pt.names.length > 3 ? ` and ${pt.names.length - 3} more` : ''}{i < parts.length - 1 ? '; ' : '.'}
                </span>
              ))}
              {!eps && !parts.length && <span className="text-slate-400">No links found in its code yet.</span>}
            </p>
          </li>
        ))}
      </ul>
    </Card>
  );
}

const EXPLAINER_KEY = 'dth.palace-explainer-hidden';

/** HowToRead explains the map in three lines; it can be dismissed. */
function HowToRead() {
  const [hidden, setHidden] = useState(() => {
    try {
      return localStorage.getItem(EXPLAINER_KEY) === '1';
    } catch {
      return false;
    }
  });
  if (hidden) return null;
  const hide = () => {
    setHidden(true);
    try {
      localStorage.setItem(EXPLAINER_KEY, '1');
    } catch {
      // shown again next visit
    }
  };
  return (
    <div className="mb-4 flex gap-3 rounded-xl border border-brand-200 bg-brand-50/60 p-4 text-sm dark:border-brand-500/30 dark:bg-brand-500/10">
      <Info className="mt-0.5 h-4 w-4 shrink-0 text-brand-600 dark:text-brand-300" aria-hidden />
      <div className="flex-1 space-y-1 text-slate-700 dark:text-slate-300">
        <p><b className="font-medium">How to read this map.</b> The Hub reads your code and finds the moving parts: repositories, the services they run, the topics and queues they send messages through, and the databases they use.</p>
        <p>A line means one uses the other: a service <i>publishes to</i> a topic, <i>uses</i> a datastore, or <i>calls</i> another service. Endpoints are counted on their service rather than drawn one by one.</p>
        <p>Click anything for details, its endpoints, and “Ask about it”. <b className="font-medium">Focus</b> shows just its neighbourhood.</p>
      </div>
      <button type="button" onClick={hide} aria-label="Hide the explanation" className="self-start rounded p-1 text-slate-400 hover:bg-white/60 hover:text-slate-700 dark:hover:bg-white/10"><X className="h-4 w-4" aria-hidden /></button>
    </div>
  );
}

export default function Palace() {
  const { entityId } = useParams();
  const nav = useNavigate();
  const [kinds, setKinds] = useState<string[]>(OVERVIEW_KINDS);
  const [layout, setLayout] = useState<LayoutName>('force');
  const [depth, setDepth] = useState(1);
  const [q, setQ] = useState('');
  const [selected, setSelected] = useState<string>();
  const [fit, setFit] = useState(0);
  // Endpoints are always fetched (to count them per service) but only drawn when turned on.
  const overview = useOverview([...new Set([...kinds, ...COUNTED_KINDS])]);
  const graph = useGraph(entityId, depth);
  const search = useEntities('', q.trim().length >= 2 ? q.trim() : '');

  const focusMode = !!entityId;
  const data = focusMode ? graph.data : overview.data;
  const { nodes, edges, allNodes, allEdges, endpoints } = useMemo(() => {
    const none = { nodes: [] as GNode[], edges: [] as GEdge[], allNodes: [] as GNode[], allEdges: [] as GEdge[], endpoints: new Map<string, number>() };
    if (!data) return none;
    const deg = new Map<string, number>();
    for (const e of data.edges) {
      deg.set(e.src, (deg.get(e.src) ?? 0) + 1);
      deg.set(e.dst, (deg.get(e.dst) ?? 0) + 1);
    }
    const all = data.nodes.map((n) => ({ ...n, degree: 'degree' in n ? Math.max(Number(n.degree), deg.get(n.id) ?? 0) : deg.get(n.id) ?? 0 })) as GNode[];
    const allE = data.edges as GEdge[];
    const kindOf = new Map(all.map((n) => [n.id, n.kind]));
    const eps = new Map<string, number>();
    for (const e of allE) if (e.kind === 'exposes' && kindOf.get(e.dst) === 'endpoint') eps.set(e.src, (eps.get(e.src) ?? 0) + 1);
    // In the overview, collapse endpoints into a count on their owner unless they are turned on.
    const drawEndpoints = focusMode || kinds.includes('endpoint');
    const shown = all
      .filter((n) => drawEndpoints || n.kind !== 'endpoint')
      .map((n) => (!drawEndpoints && eps.get(n.id) ? { ...n, name: `${n.name} · ${eps.get(n.id)} endpoints` } : n));
    const ids = new Set(shown.map((n) => n.id));
    return { nodes: shown, edges: allE.filter((e) => ids.has(e.src) && ids.has(e.dst)), allNodes: all, allEdges: allE, endpoints: eps };
  }, [data, focusMode, kinds]);
  const byId = useMemo(() => new Map(nodes.map((n) => [n.id, n])), [nodes]);
  const allById = useMemo(() => new Map(allNodes.map((n) => [n.id, n])), [allNodes]);
  const sel = selected ? allById.get(selected) : entityId ? allById.get(entityId) : undefined;
  const counts = overview.data?.counts ?? {};
  const kindList = Object.keys(KINDS).filter((k) => (counts[k] ?? 0) > 0);
  const onSelect = useCallback((id: string | undefined) => setSelected(id), []);
  const focusNode = entityId ? byId.get(entityId) : undefined;
  const loading = focusMode ? graph.isLoading : overview.isLoading;
  const empty = !focusMode && overview.data && overview.data.nodes.length === 0;
  const total = Object.values(counts).reduce((a, b) => a + b, 0);

  return (
    <>
      <PageHeader
        title="Palace"
        description="The knowledge graph of your estate: repositories, services, endpoints, topics, datastores, and docs — extracted from code and linked across repositories."
        actions={focusMode && <Button variant="secondary" onClick={() => { setSelected(undefined); nav('/palace'); }}><ArrowLeft className="h-4 w-4" aria-hidden /> Overview</Button>}
      />
      {total > 0 && !focusMode && <HowToRead />}
      {total > 0 && (
        <div className="mb-4 flex flex-wrap gap-2" role="group" aria-label="Entity kinds on the map">
          {kindList.map((k) => {
            const m = kindMeta(k);
            const on = kinds.includes(k);
            return (
              <button
                key={k}
                type="button"
                aria-pressed={on}
                disabled={focusMode}
                onClick={() => setKinds((cur) => (on ? cur.filter((x) => x !== k) : [...cur, k]))}
                className={cx(
                  'inline-flex items-center gap-2 rounded-full border px-3 py-1 text-xs font-medium transition disabled:cursor-default disabled:opacity-60',
                  on ? 'border-transparent bg-white shadow-card dark:bg-white/[0.08]' : 'border-slate-200 text-slate-400 hover:text-slate-600 dark:border-white/10 dark:hover:text-slate-200',
                )}
                title={focusMode ? 'Kinds apply to the overview' : on ? `Hide ${m.plural.toLowerCase()}` : `Show ${m.plural.toLowerCase()}`}
              >
                <KindDot kind={k} className={cx(!on && 'opacity-40')} />
                {m.plural}
                <span className="tabular-nums text-slate-400">{counts[k].toLocaleString()}</span>
              </button>
            );
          })}
        </div>
      )}
      {empty ? (
        <Empty icon={Network} title={total ? 'Nothing of these kinds yet' : 'The Palace is empty'} action={!total && <Link to="/repos" className="text-sm font-medium text-brand-600 dark:text-brand-300">Track a repository →</Link>}>
          {total ? 'Turn on more kinds above.' : 'It fills in from code as tracked repositories are processed, and from Confluence and Jira once they sync.'}
        </Empty>
      ) : (
        <div className={cx('grid gap-4', (sel || !focusMode) ? 'xl:grid-cols-[minmax(0,1fr)_22rem]' : '')}>
          <Card className="min-w-0 overflow-hidden" title={
            <span className="flex items-center gap-2">
              {focusMode && focusNode ? <><KindDot kind={focusNode.kind} /> {focusNode.name}</> : 'Overview'}
              {data && <span className="font-normal text-slate-400">· {nodes.length} items · {edges.length} links{overview.data?.truncated && !focusMode ? ' · most connected shown' : ''}</span>}
            </span>
          } actions={
            <div className="flex flex-wrap items-center gap-2">
              <div className="relative w-48">
                <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-slate-400" aria-hidden />
                <Input aria-label="Search the graph" placeholder="Find…" className="h-8 pl-8 text-xs" value={q} onChange={(e) => setQ(e.target.value)} />
                {q.trim().length >= 2 && !!search.data?.items.length && (
                  <ul className="absolute right-0 z-20 mt-1 max-h-72 w-72 overflow-y-auto rounded-xl border border-slate-200 bg-white p-1 shadow-xl dark:border-white/10 dark:bg-slate-900">
                    {search.data.items.slice(0, 20).map((e) => (
                      <li key={e.id}>
                        <button type="button" onClick={() => { setQ(''); setSelected(undefined); nav(`/palace/${e.id}`); }}
                          className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-xs hover:bg-slate-100 dark:hover:bg-white/[0.06]">
                          <KindDot kind={e.kind} />
                          <span className="truncate font-medium">{e.name}</span>
                          <span className="ml-auto shrink-0 text-slate-400">{kindMeta(e.kind).label}</span>
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
              <div className="w-32">
                <Select aria-label="Layout" className="h-8 py-0 text-xs" value={layout} onChange={(e) => setLayout(e.target.value as LayoutName)}>
                  {LAYOUTS.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
                </Select>
              </div>
              {focusMode && (
                <div className="w-24">
                  <Select aria-label="Depth" className="h-8 py-0 text-xs" value={depth} onChange={(e) => setDepth(Number(e.target.value))}>
                    {[1, 2, 3].map((d) => <option key={d} value={d}>{d} hop{d > 1 ? 's' : ''}</option>)}
                  </Select>
                </div>
              )}
              <Button size="sm" variant="secondary" onClick={() => setFit((f) => f + 1)} aria-label="Fit to screen"><Maximize2 className="h-3.5 w-3.5" aria-hidden /></Button>
            </div>
          }>
            {loading && <Spinner label="Laying out the graph" />}
            <ErrorNote error={focusMode ? graph.error : overview.error} />
            {data && (
              <div className="relative">
                <GraphView nodes={nodes} edges={edges} focus={entityId} selected={sel?.id} layout={layout} query={q} fitSignal={fit} onSelect={onSelect} />
                <div className="pointer-events-none absolute bottom-2 left-2 flex max-w-[70%] flex-wrap gap-x-3 gap-y-1 rounded-lg bg-white/85 px-2.5 py-1.5 text-[11px] text-slate-600 backdrop-blur dark:bg-slate-950/70 dark:text-slate-300">
                  {[...new Set(nodes.map((n) => n.kind))].map((k) => (
                    <span key={k} className="inline-flex items-center gap-1.5"><KindDot kind={k} />{kindMeta(k).label}</span>
                  ))}
                </div>
                <p className="pointer-events-none absolute right-3 top-2 text-[11px] text-slate-400">Hover to trace · click for details · scroll to zoom</p>
              </div>
            )}
          </Card>
          {sel ? (
            <Details node={sel as GNode & Partial<Entity>} nodes={allById} edges={allEdges} onSelect={(id) => setSelected(id)} onClose={() => setSelected(undefined)} />
          ) : !focusMode && data ? (
            <Summary nodes={allNodes} edges={allEdges} endpoints={endpoints} onSelect={(id) => setSelected(id)} />
          ) : null}
        </div>
      )}
      {focusMode && graph.data && graph.data.nodes.length <= 1 && (
        <p className="mt-3 text-sm text-slate-500">No links from this entity yet. <Badge>{focusNode ? kindMeta(focusNode.kind).label : 'entity'}</Badge></p>
      )}
    </>
  );
}
