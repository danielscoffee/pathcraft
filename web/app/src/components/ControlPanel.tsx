import type { ReactNode } from 'react'
import type { SnapResult } from '../api/types'
import type { Status } from '../hooks/useRouting'

const TONE_CLASS: Record<Status['tone'], string> = {
  info: 'text-ink-faint',
  ok: 'text-ok',
  err: 'text-err',
}

function CoordRow({
  role,
  label,
  snap,
}: {
  role: 'from' | 'to'
  label: string
  snap: SnapResult | null
}) {
  return (
    <div className="flex items-center gap-2 text-12px">
      <span
        aria-hidden
        className={`w-3 h-3 rounded-full border-2 border-paper shrink-0 ${
          role === 'from' ? 'bg-ok' : 'bg-err'
        }`}
      />
      <span className="text-ink-soft">{label}</span>
      <span className="ml-auto font-mono text-11px text-ink-faint tabular-nums">
        {snap ? `${snap.lat.toFixed(5)}, ${snap.lon.toFixed(5)}` : '—'}
      </span>
    </div>
  )
}

export interface ControlPanelProps {
  from: SnapResult | null
  to: SnapResult | null
  status: Status
  onReset: () => void
  children: ReactNode
}

export default function ControlPanel({ from, to, status, onReset, children }: ControlPanelProps) {
  return (
    <aside className="pc-panel absolute top-4 left-4 z-[1000] w-84 max-h-[calc(100vh-2rem)] overflow-y-auto px-5 py-4.5 rounded-xl">
      <header className="mb-3 pc-reveal" style={{ animationDelay: '0ms' }}>
        <h1 className="font-display text-22px font-600 text-ink leading-tight">
          Path<em className="not-italic text-accent">Craft</em>
        </h1>
        <p className="text-11px text-ink-faint leading-snug mt-1">
          Field routing journal — click two points; each snaps to the nearest road and the engine
          solves the selected mode on demand.
        </p>
      </header>

      <div className="grid gap-1.5 mb-3 pc-reveal" style={{ animationDelay: '60ms' }}>
        <CoordRow role="from" label="A — origin" snap={from} />
        <CoordRow role="to" label="B — destination" snap={to} />
      </div>

      <div className="pc-reveal" style={{ animationDelay: '120ms' }}>{children}</div>

      <button
        type="button"
        onClick={onReset}
        className="mt-3 w-full px-3 py-2 text-12px font-500 tracking-wide text-ink-soft bg-transparent border border-paper-edge rounded-md cursor-pointer hover:border-accent hover:text-accent transition-colors"
      >
        Reset (R)
      </button>

      <p role="status" className={`mt-2.5 text-12px font-mono leading-snug ${TONE_CLASS[status.tone]}`}>
        ▸ {status.text}
      </p>
    </aside>
  )
}
