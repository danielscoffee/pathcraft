import type { ReactNode } from 'react'
import type { SnapResult } from '../api/types'
import type { Status } from '../hooks/useRouting'
import { ClearIcon, SwapIcon } from './Icons'

const TONE_CLASS: Record<Status['tone'], string> = {
  info: 'text-ink-faint',
  ok: 'text-ok',
  err: 'text-err',
}

function EndpointRow({
  role,
  snap,
  placeholder,
  onClear,
}: {
  role: 'from' | 'to'
  snap: SnapResult | null
  placeholder: string
  onClear: () => void
}) {
  return (
    <div className="flex items-center gap-2 min-h-7">
      {role === 'from' ? (
        <span aria-hidden className="w-2.5 h-2.5 rounded-full border-2 border-ok bg-paper shrink-0" />
      ) : (
        <span aria-hidden className="w-2.5 h-2.5 rounded-sm rotate-45 bg-err shrink-0" />
      )}
      <div className="flex-1 px-2.5 py-1.5 bg-paper-deep border border-paper-edge rounded-md">
        {snap ? (
          <span className="font-mono text-11px text-ink tabular-nums">
            {snap.lat.toFixed(5)}, {snap.lon.toFixed(5)}
          </span>
        ) : (
          <span className="text-12px text-ink-faint">{placeholder}</span>
        )}
      </div>
      <button
        type="button"
        aria-label={`Clear ${role === 'from' ? 'origin' : 'destination'}`}
        onClick={onClear}
        disabled={!snap}
        className={`p-1 rounded-full transition-colors ${
          snap ? 'text-ink-faint hover:text-err cursor-pointer' : 'text-transparent'
        }`}
      >
        <ClearIcon size={13} />
      </button>
    </div>
  )
}

export interface DirectionsCardProps {
  from: SnapResult | null
  to: SnapResult | null
  status: Status
  onClearPoint: (role: 'from' | 'to') => void
  onSwap: () => void
  children: ReactNode
}

/**
 * Google-Maps-style directions card: A/B endpoint rows joined by a dotted
 * rail with a swap control, then mode tabs / summary passed as children.
 */
export default function DirectionsCard({
  from,
  to,
  status,
  onClearPoint,
  onSwap,
  children,
}: DirectionsCardProps) {
  return (
    <section className="pc-panel absolute top-4 left-4 z-[1000] w-88 max-h-[calc(100vh-2rem)] overflow-y-auto px-4 py-3.5 rounded-xl">
      <header className="mb-2.5 flex items-baseline justify-between pc-reveal">
        <h1 className="font-display text-18px font-600 text-ink leading-tight">
          Path<em className="not-italic text-accent">Craft</em>
        </h1>
        <span className="font-mono text-9px uppercase tracking-widest text-ink-faint">
          directions
        </span>
      </header>

      <div className="flex items-stretch gap-1 pc-reveal" style={{ animationDelay: '60ms' }}>
        <div className="relative flex-1 grid gap-1.5">
          {/* dotted rail joining A to B, Google-Maps style */}
          <span
            aria-hidden
            className="absolute left-1.25 top-6.5 bottom-6.5 w-0 border-l-2 border-dotted border-paper-edge -translate-x-1/2"
          />
          <EndpointRow
            role="from"
            snap={from}
            placeholder="Click the map — origin"
            onClear={() => onClearPoint('from')}
          />
          <EndpointRow
            role="to"
            snap={to}
            placeholder={from ? 'Click again — destination' : 'Destination'}
            onClear={() => onClearPoint('to')}
          />
        </div>
        <button
          type="button"
          aria-label="Swap origin and destination"
          title="Swap A and B"
          onClick={onSwap}
          disabled={!from && !to}
          className={`self-center p-1.5 rounded-full border border-paper-edge transition-colors ${
            from || to
              ? 'text-ink-soft hover:text-accent hover:border-accent cursor-pointer'
              : 'text-paper-edge'
          }`}
        >
          <SwapIcon size={15} />
        </button>
      </div>

      <div className="mt-2.5 pc-reveal" style={{ animationDelay: '120ms' }}>{children}</div>

      <p role="status" className={`mt-2.5 text-11px font-mono leading-snug ${TONE_CLASS[status.tone]}`}>
        ▸ {status.text}
      </p>
    </section>
  )
}
