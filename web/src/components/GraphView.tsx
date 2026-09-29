import cytoscape, { type Core } from 'cytoscape';
import { useEffect, useRef } from 'react';
import type { Neighbourhood } from '@/api/types';

const kindColor: Record<string, string> = {
  repo: '#2f6fed',
  service: '#7c3aed',
  module: '#0891b2',
  file: '#64748b',
  symbol: '#0f766e',
  endpoint: '#ea580c',
  env_var: '#ca8a04',
  dependency: '#9333ea',
  queue_topic: '#db2777',
  team: '#16a34a',
  person: '#16a34a',
};

/** GraphView renders a Palace neighbourhood with Cytoscape; clicking a node selects it. */
export function GraphView({ graph, focus, onSelect }: { graph: Neighbourhood; focus?: string; onSelect: (id: string) => void }) {
  const el = useRef<HTMLDivElement>(null);
  const cy = useRef<Core>();
  useEffect(() => {
    if (!el.current) return;
    cy.current?.destroy();
    cy.current = cytoscape({
      container: el.current,
      elements: [
        ...graph.nodes.map((n) => ({ data: { id: n.id, label: n.name || n.key, kind: n.kind } })),
        ...graph.edges.map((e) => ({ data: { id: `${e.src}-${e.kind}-${e.dst}`, source: e.src, target: e.dst, label: e.kind } })),
      ],
      style: [
        {
          selector: 'node',
          style: {
            label: 'data(label)',
            'font-size': 9,
            'background-color': (n: cytoscape.NodeSingular) => kindColor[n.data('kind')] ?? '#475569',
            width: 18,
            height: 18,
            color: '#334155',
            'text-valign': 'bottom',
            'text-margin-y': 4,
          },
        },
        { selector: `node[id = "${focus}"]`, style: { width: 28, height: 28, 'border-width': 3, 'border-color': '#0f172a' } },
        {
          selector: 'edge',
          style: {
            width: 1,
            'line-color': '#cbd5e1',
            'target-arrow-color': '#cbd5e1',
            'target-arrow-shape': 'triangle',
            'curve-style': 'bezier',
            label: 'data(label)',
            'font-size': 7,
            color: '#94a3b8',
          },
        },
      ],
      layout: { name: graph.nodes.length > 60 ? 'concentric' : 'cose', animate: false } as cytoscape.LayoutOptions,
    });
    cy.current.on('tap', 'node', (e) => onSelect(e.target.id()));
    return () => cy.current?.destroy();
  }, [graph, focus, onSelect]);
  return <div ref={el} className="h-[32rem] w-full rounded-md bg-slate-50 dark:bg-slate-950" aria-label="Knowledge graph" />;
}
