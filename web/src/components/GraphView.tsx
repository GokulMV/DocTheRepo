import cytoscape, { type Core, type ElementDefinition } from 'cytoscape';
import { useEffect, useRef } from 'react';
import { kindMeta } from './palaceKinds';

export interface GNode {
  id: string;
  kind: string;
  name: string;
  key?: string;
  degree?: number;
}

export interface GEdge {
  src: string;
  dst: string;
  kind: string;
  weight?: number;
}

export type LayoutName = 'force' | 'concentric' | 'hierarchy' | 'circle' | 'grid';

function layoutOptions(name: LayoutName, n: number, focus?: string): cytoscape.LayoutOptions {
  switch (name) {
    case 'concentric':
      return { name: 'concentric', concentric: (node: cytoscape.NodeSingular) => node.degree(false), levelWidth: () => 2, minNodeSpacing: 24, animate: true, animationDuration: 400 } as cytoscape.LayoutOptions;
    case 'hierarchy':
      return { name: 'breadthfirst', directed: true, spacingFactor: 1.15, roots: focus ? `#${CSS.escape(focus)}` : undefined, animate: true, animationDuration: 400 } as cytoscape.LayoutOptions;
    case 'circle':
      return { name: 'circle', animate: true, animationDuration: 400 } as cytoscape.LayoutOptions;
    case 'grid':
      return { name: 'grid', avoidOverlap: true, animate: true, animationDuration: 400 } as cytoscape.LayoutOptions;
  }
  return {
    name: 'cose', animate: n < 250, animationDuration: 600, nodeRepulsion: () => 9000, idealEdgeLength: () => 90, gravity: 0.25,
    numIter: n > 400 ? 400 : 1000, padding: 30, randomize: true,
  } as cytoscape.LayoutOptions;
}

/**
 * GraphView draws Palace entities with Cytoscape: shape and colour per kind, size by connections, edge
 * width by how many underlying links it stands for. Hovering a node highlights its neighbourhood;
 * clicking selects it. `query` highlights matching nodes.
 */
export function GraphView({ nodes, edges, focus, selected, layout = 'force', query = '', fitSignal = 0, onSelect, height = 'h-[620px]' }: {
  nodes: GNode[];
  edges: GEdge[];
  focus?: string;
  selected?: string;
  layout?: LayoutName;
  query?: string;
  fitSignal?: number;
  onSelect: (id: string | undefined) => void;
  height?: string;
}) {
  const el = useRef<HTMLDivElement>(null);
  const cy = useRef<Core>();
  const select = useRef(onSelect);
  select.current = onSelect;

  useEffect(() => {
    if (!el.current) return;
    const dark = document.documentElement.classList.contains('dark');
    const ink = dark ? '#cbd5e1' : '#334155';
    const paper = dark ? '#0b1120' : '#f8fafc';
    const maxDeg = Math.max(1, ...nodes.map((n) => n.degree ?? 0));
    const ids = new Set(nodes.map((n) => n.id));
    const elements: ElementDefinition[] = [
      ...nodes.map((n) => {
        const m = kindMeta(n.kind);
        const size = 18 + 26 * Math.sqrt((n.degree ?? 1) / maxDeg);
        return { data: { id: n.id, label: n.name || n.key || n.id, kind: n.kind, color: m.color, shape: m.shape, size } };
      }),
      ...edges
        .filter((e) => ids.has(e.src) && ids.has(e.dst))
        .map((e) => ({ data: { id: `${e.src}|${e.kind}|${e.dst}`, source: e.src, target: e.dst, label: e.kind.replace(/_/g, ' '), weight: e.weight ?? 1 } })),
    ];
    cy.current?.destroy();
    const c = cytoscape({
      container: el.current,
      elements,
      minZoom: 0.15,
      maxZoom: 3,
      wheelSensitivity: 0.25,
      style: [
        {
          selector: 'node',
          style: {
            label: 'data(label)',
            'background-color': 'data(color)',
            shape: 'data(shape)' as never,
            width: 'data(size)',
            height: 'data(size)',
            'font-size': 10,
            'font-weight': 500,
            color: ink,
            'text-valign': 'bottom',
            'text-margin-y': 5,
            'text-outline-color': paper,
            'text-outline-width': 2,
            'text-max-width': '140px',
            'text-wrap': 'ellipsis',
            'min-zoomed-font-size': 7,
            'border-width': 2,
            'border-color': paper,
            'transition-property': 'opacity, border-width, border-color',
            'transition-duration': 150,
          },
        },
        { selector: 'node.focus', style: { 'border-width': 4, 'border-color': dark ? '#ffffff' : '#0f172a' } },
        { selector: 'node:selected', style: { 'border-width': 4, 'border-color': '#f59e0b', 'overlay-opacity': 0 } },
        { selector: 'node.match', style: { 'border-width': 4, 'border-color': '#22d3ee' } },
        {
          selector: 'edge',
          style: {
            width: 'mapData(weight, 1, 25, 1.2, 5)',
            'line-color': dark ? '#334155' : '#cbd5e1',
            'target-arrow-color': dark ? '#475569' : '#94a3b8',
            'target-arrow-shape': 'triangle',
            'arrow-scale': 0.8,
            'curve-style': 'bezier',
            opacity: 0.8,
            'font-size': 9,
            color: dark ? '#94a3b8' : '#64748b',
            'text-outline-color': paper,
            'text-outline-width': 2,
            'text-rotation': 'autorotate',
          },
        },
        { selector: 'edge.hl', style: { label: 'data(label)', 'line-color': '#6090fa', 'target-arrow-color': '#6090fa', opacity: 1, 'z-index': 9 } },
        { selector: '.faded', style: { opacity: 0.12 } },
      ],
      layout: layoutOptions(layout, nodes.length, focus),
    });
    if (focus) c.getElementById(focus).addClass('focus');
    c.on('tap', 'node', (e) => select.current(e.target.id()));
    c.on('tap', (e) => {
      if (e.target === c) select.current(undefined);
    });
    c.on('mouseover', 'node', (e) => {
      const hood = e.target.closedNeighborhood();
      c.elements().not(hood).addClass('faded');
      hood.edges().addClass('hl');
      if (el.current) el.current.style.cursor = 'pointer';
    });
    c.on('mouseout', 'node', () => {
      c.elements().removeClass('faded').removeClass('hl');
      if (el.current) el.current.style.cursor = '';
    });
    cy.current = c;
    return () => c.destroy();
  }, [nodes, edges, focus, layout]);

  useEffect(() => {
    const c = cy.current;
    if (!c) return;
    c.nodes().unselect();
    if (selected) c.getElementById(selected).select();
  }, [selected, nodes]);

  useEffect(() => {
    const c = cy.current;
    if (!c) return;
    c.nodes().removeClass('match');
    const q = query.trim().toLowerCase();
    if (q.length < 2) return;
    const hits = c.nodes().filter((n) => String(n.data('label')).toLowerCase().includes(q));
    hits.addClass('match');
    if (hits.length) c.animate({ fit: { eles: hits, padding: 80 }, duration: 300 });
  }, [query, nodes]);

  useEffect(() => {
    if (fitSignal) cy.current?.animate({ fit: { eles: cy.current.elements(), padding: 30 }, duration: 300 });
  }, [fitSignal]);

  return (
    <div
      ref={el}
      className={`${height} w-full rounded-xl bg-[radial-gradient(circle_at_1px_1px,rgba(100,116,139,0.18)_1px,transparent_0)] [background-size:22px_22px]`}
      aria-label="Knowledge graph"
      role="img"
    />
  );
}
