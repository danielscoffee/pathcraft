import type { RouteStats } from '../hooks/useRouting'

const ROWS: Array<{ key: keyof RouteStats; label: string }> = [
  { key: 'distance', label: 'Distance' },
  { key: 'time', label: 'Time' },
  { key: 'nodes', label: 'Nodes' },
  { key: 'solve', label: 'Solve' },
]

export default function Stats({ stats }: { stats: RouteStats }) {
  return (
    <dl className="grid gap-1">
      {ROWS.map(({ key, label }) => (
        <div key={key} className="flex items-baseline gap-2 text-13px">
          <dt className="text-ink-faint">{label}</dt>
          <span aria-hidden className="flex-1 border-b border-dotted border-paper-edge" />
          <dd className="font-mono text-ink tabular-nums">{stats[key]}</dd>
        </div>
      ))}
    </dl>
  )
}
