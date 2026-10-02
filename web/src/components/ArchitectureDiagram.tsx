import { useMemo, useState } from 'react';
import { cx } from '@/components/ui';
import { kindMeta } from './palaceKinds';

export interface ArchNode {
  id: string;
  kind: string;
  name: string;
  key?: string;
  layer: string;
  entity_id?: string;
  repo_id?: string;
  degree: number;
}

export interface ArchLink {
  src: string;
  dst: string;
  kind: string;
  weight: number;
}

export const LAYERS: { id: string; label: string; hint: string }[] = [
  { id: 'upstream', label: 'Upstream', hint: 'Callers and producers in other repositories' },
  { id: 'interface', label: 'Interface', hint: 'Endpoints this repository exposes' },
  { id: 'core', label: 'Core', hint: 'Service and main modules' },
  { id: 'messaging', label: 'Messaging', hint: 'Topics and queues' },
  { id: 'data', label: 'Data', hint: 'Datastores' },
  { id: 'downstream', label: 'Downstream', hint: 'What it calls, and who consumes its topics' },
];

const LINK_COLOR: Record<string, string> = {
  calls: '#94a3b8',
  exposes: '#f97316',
  contains: '#06b6d4',
  publishes: '#ec4899',
  subscribes: '#ec4899',
  'consumed by': '#ec4899',
  uses_datastore: '#14b8a6',
};

const W = 184;
const H = 52;
const GAP = 14;
const COL_GAP = 70;
const PAD = 28;
const HEAD = 44;
const LANE = 6;
const MAX_LANES = 14;

interface Placed extends ArchNode {
  x: number;
  y: number;
  col: number;
}

const truncate = (s: string, n: number) => (s.length > n ? s.slice(0, n - 1) + '…' : s);

/** ArchitectureDiagram lays the components out in layers, left to right, with arrows between them. */
export function ArchitectureDiagram({ nodes, links, selected, onSelect, hidden = {} }: {
  nodes: ArchNode[];
  links: ArchLink[];
  selected?: string;
  onSelect: (id: string | undefined) => void;
  hidden?: Record<string, number>;
}) {
  const [hover, setHover] = useState<string>();
  const [fit, setFit] = useState(true);
  const { placed, cols, width, height, lanes, laneTop } = useMemo(() => {
    const present = LAYERS.filter((l) => nodes.some((n) => n.layer === l.id));
    const colOf = new Map(present.map((l, i) => [l.id, i]));
    const layerOf = new Map(nodes.map((n) => [n.id, n.layer]));
    // Links that skip a column run in lanes above the boxes, so they never pass behind a box they don't touch.
    const long = links
      .filter((l) => Math.abs((colOf.get(layerOf.get(l.src) ?? '') ?? 0) - (colOf.get(layerOf.get(l.dst) ?? '') ?? 0)) > 1)
      .sort((a, b) => (colOf.get(layerOf.get(a.src) ?? '') ?? 0) - (colOf.get(layerOf.get(b.src) ?? '') ?? 0) || a.dst.localeCompare(b.dst));
    const lanes = new Map(long.map((l, i) => [`${l.src}|${l.kind}|${l.dst}`, Math.min(i, MAX_LANES - 1)]));
    const laneH = long.length ? 10 + Math.min(long.length, MAX_LANES) * LANE : 0;
    const tallest = Math.max(1, ...present.map((l) => nodes.filter((n) => n.layer === l.id).length + (hidden[l.id] ? 1 : 0)));
    const innerH = tallest * (H + GAP) - GAP;
    const placed = new Map<string, Placed>();
    present.forEach((l, col) => {
      const ns = nodes.filter((n) => n.layer === l.id);
      const colH = (ns.length + (hidden[l.id] ? 1 : 0)) * (H + GAP) - GAP;
      const top = PAD + HEAD + laneH + (innerH - colH) / 2;
      ns.forEach((n, i) => placed.set(n.id, { ...n, col, x: PAD + col * (W + COL_GAP), y: top + i * (H + GAP) }));
    });
    return {
      placed,
      lanes,
      laneTop: PAD + HEAD - 4,
      cols: present.map((l, col) => ({ ...l, x: PAD + col * (W + COL_GAP), more: hidden[l.id] ?? 0, count: nodes.filter((n) => n.layer === l.id).length })),
      width: PAD * 2 + present.length * W + Math.max(0, present.length - 1) * COL_GAP,
      height: PAD * 2 + HEAD + laneH + innerH,
    };
  }, [nodes, links, hidden]);

  const focus = hover ?? selected;
  const linked = useMemo(() => {
    if (!focus) return undefined;
    const s = new Set([focus]);
    for (const l of links) {
      if (l.src === focus) s.add(l.dst);
      if (l.dst === focus) s.add(l.src);
    }
    return s;
  }, [focus, links]);

  const path = (a: Placed, b: Placed, lane?: number) => {
    const ay = a.y + H / 2;
    const by = b.y + H / 2;
    if (a.col === b.col) {
      const x = a.x;
      const bend = 34 + Math.min(40, Math.abs(by - ay) / 6);
      return `M ${x} ${ay} C ${x - bend} ${ay}, ${x - bend} ${by}, ${x} ${by}`;
    }
    const forward = a.col < b.col;
    const x1 = forward ? a.x + W : a.x;
    const x2 = forward ? b.x : b.x + W;
    const s = forward ? 1 : -1;
    if (lane !== undefined) {
      // Up into the lane in the gap after the source column, across, and down in the gap before the target.
      const ly = laneTop + 10 + lane * LANE;
      const t = COL_GAP * 0.45;
      return `M ${x1} ${ay} C ${x1 + s * t} ${ay}, ${x1 + s * t * 0.4} ${ly}, ${x1 + s * t} ${ly} L ${x2 - s * t} ${ly} C ${x2 - s * t * 0.4} ${ly}, ${x2 - s * t} ${by}, ${x2} ${by}`;
    }
    const dx = Math.max(40, Math.abs(x2 - x1) / 2);
    return `M ${x1} ${ay} C ${x1 + s * dx} ${ay}, ${x2 - s * dx} ${by}, ${x2} ${by}`;
  };

  if (nodes.length === 0) return null;
  return (
    <div className="relative overflow-x-auto rounded-xl border border-slate-200/80 bg-[radial-gradient(circle,rgba(148,163,184,0.18)_1px,transparent_1px)] [background-size:18px_18px] dark:border-white/[0.06]">
      <div className="absolute right-2 top-2 z-10 flex rounded-md bg-white/90 p-0.5 text-[11px] shadow-sm ring-1 ring-slate-200 dark:bg-slate-900/90 dark:ring-white/10" role="group" aria-label="Zoom">
        {([['Fit', true], ['100%', false]] as const).map(([label, v]) => (
          <button key={label} type="button" aria-pressed={fit === v} onClick={() => setFit(v)}
            className={cx('rounded px-2 py-0.5 font-medium', fit === v ? 'bg-slate-900 text-white dark:bg-white/15' : 'text-slate-500 hover:text-slate-800 dark:hover:text-slate-200')}>
            {label}
          </button>
        ))}
      </div>
      <svg
        width={fit ? '100%' : width}
        height={fit ? undefined : height}
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-label="Architecture diagram"
        className={cx('block', fit && 'h-auto')}
        style={fit ? { maxWidth: width } : undefined}
      >
        <defs>
          {Object.entries(LINK_COLOR).map(([k, c]) => (
            <marker key={k} id={`arr-${k.replace(/\s/g, '-')}`} viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
              <path d="M0,0 L10,5 L0,10 z" fill={c} />
            </marker>
          ))}
        </defs>
        {cols.map((c) => (
          <g key={c.id}>
            <text x={c.x} y={PAD + 12} className="fill-slate-500 text-[11px] font-semibold uppercase tracking-[0.12em] dark:fill-slate-400">
              {c.label} · {c.count + c.more}
            </text>
            <title>{c.hint}</title>
            <line x1={c.x} x2={c.x + W} y1={PAD + 22} y2={PAD + 22} className="stroke-slate-200 dark:stroke-white/10" />
          </g>
        ))}
        {links.map((l) => {
          const a = placed.get(l.src);
          const b = placed.get(l.dst);
          if (!a || !b) return null;
          const on = !linked || (linked.has(l.src) && linked.has(l.dst) && (l.src === focus || l.dst === focus));
          const color = LINK_COLOR[l.kind] ?? '#94a3b8';
          return (
            <path
              key={`${l.src}|${l.kind}|${l.dst}`}
              d={path(a, b, lanes.get(`${l.src}|${l.kind}|${l.dst}`))}
              fill="none"
              stroke={color}
              strokeWidth={Math.min(4, 1.3 + Math.log2(l.weight + 1) * 0.6)}
              strokeDasharray={l.kind === 'publishes' || l.kind === 'subscribes' || l.kind === 'consumed by' ? '6 4' : undefined}
              markerEnd={`url(#arr-${(LINK_COLOR[l.kind] ? l.kind : 'calls').replace(/\s/g, '-')})`}
              opacity={on ? 0.9 : 0.12}
              className="transition-opacity"
            >
              <title>{`${a.name} — ${l.kind.replace(/_/g, ' ')}${l.weight > 1 ? ` (${l.weight})` : ''} → ${b.name}`}</title>
            </path>
          );
        })}
        {[...placed.values()].map((n) => {
          const m = kindMeta(n.kind);
          const Icon = m.icon;
          const dim = linked && !linked.has(n.id);
          return (
            <g
              key={n.id}
              transform={`translate(${n.x} ${n.y})`}
              onMouseEnter={() => setHover(n.id)}
              onMouseLeave={() => setHover(undefined)}
              onClick={() => onSelect(selected === n.id ? undefined : n.id)}
              className="cursor-pointer transition-opacity"
              opacity={dim ? 0.3 : 1}
              role="button"
              aria-label={`${m.label} ${n.name}`}
            >
              <title>{n.key && n.key !== n.name ? `${n.name}\n${n.key}` : n.name}</title>
              <rect
                width={W}
                height={H}
                rx={10}
                className={cx('fill-white dark:fill-slate-900', selected === n.id ? 'stroke-amber-400' : 'stroke-slate-200 dark:stroke-white/10')}
                strokeWidth={selected === n.id ? 2.5 : 1}
              />
              <rect width={4} height={H - 16} x={0} y={8} rx={2} fill={m.color} />
              <rect x={12} y={12} width={28} height={28} rx={7} fill={m.color} opacity={0.14} />
              <Icon x={18} y={18} width={16} height={16} color={m.color} strokeWidth={2} />
              <text x={50} y={23} className="fill-slate-800 text-[12.5px] font-medium dark:fill-slate-100">{truncate(n.name, 21)}</text>
              <text x={50} y={39} className="fill-slate-500 text-[10.5px] dark:fill-slate-400">{m.label}{n.degree > 0 ? ` · ${n.degree} links` : ''}</text>
            </g>
          );
        })}
        {cols.filter((c) => c.more > 0).map((c) => {
          const last = [...placed.values()].filter((n) => n.layer === c.id).reduce((y, n) => Math.max(y, n.y), 0);
          return (
            <text key={c.id + '-more'} x={c.x + W / 2} y={last + H + GAP + 22} textAnchor="middle" className="fill-slate-500 text-[11px] dark:fill-slate-400">
              + {c.more} more
            </text>
          );
        })}
      </svg>
    </div>
  );
}

/** ArchitectureLegend explains the arrow colours. */
export function ArchitectureLegend() {
  const items: [string, string, boolean][] = [
    ['exposes', '#f97316', false],
    ['calls', '#94a3b8', false],
    ['publishes / subscribes', '#ec4899', true],
    ['uses datastore', '#14b8a6', false],
    ['contains', '#06b6d4', false],
  ];
  return (
    <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-slate-500">
      {items.map(([label, color, dashed]) => (
        <span key={label} className="inline-flex items-center gap-1.5">
          <svg width="22" height="8" aria-hidden><line x1="0" y1="4" x2="22" y2="4" stroke={color} strokeWidth="2" strokeDasharray={dashed ? '5 3' : undefined} /></svg>
          {label}
        </span>
      ))}
    </div>
  );
}
