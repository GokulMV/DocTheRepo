import { cx } from '@/components/ui';

// The "Code D": five lines of code whose ends trace a D, each split into a highlighted token and the rest.
const LINES: { y: number; first: number; end: number; key: string }[] = [
  { y: 48, first: 40, end: 168, key: '#F59E0B' },
  { y: 88, first: 64, end: 187, key: '#EC4899' },
  { y: 128, first: 28, end: 196, key: '#14B8A6' },
  { y: 168, first: 48, end: 187, key: '#EC4899' },
  { y: 208, first: 72, end: 168, key: '#F59E0B' },
];
const X0 = 52;
const GAP = 10;

/** Logo is the DocTheRepo mark: colour by default, or one colour (currentColor) with mono. */
export function Logo({ className, mono = false, title = 'DocTheRepo' }: { className?: string; mono?: boolean; title?: string }) {
  return (
    <svg viewBox="0 0 256 256" className={cx('shrink-0', className)} role="img" aria-label={title}>
      {LINES.map((l) => (
        <g key={l.y}>
          <rect x={X0} y={l.y - 14} width={l.first} height={28} rx={14} fill={mono ? 'currentColor' : l.key} />
          <rect x={X0 + l.first + GAP} y={l.y - 14} width={l.end - X0 - l.first - GAP} height={28} rx={14} fill={mono ? 'currentColor' : '#3B6EF6'} className={mono ? undefined : 'dark:fill-[#5B8BFF]'} />
        </g>
      ))}
    </svg>
  );
}
