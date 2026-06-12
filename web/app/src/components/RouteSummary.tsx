import type { RouteMode } from '../api/types'
import type { ModeResult } from '../hooks/useRouting'
import { formatDistance, formatDuration, formatNumber } from '../lib/geo'
import { modeColor } from '../lib/mapStyle'
import Itinerary from './Itinerary'

export interface RouteSummaryProps {
  mode: RouteMode
  result: ModeResult
  busTime: string
  onBusTime: (value: string) => void
}

/** Google-Maps-style result block: big ETA, distance, transit schedule. */
export default function RouteSummary({ mode, result, busTime, onBusTime }: RouteSummaryProps) {
  if (!result.ok) {
    return (
      <p className="mt-3 pt-3 border-t border-paper-edge text-12px text-err leading-snug">
        {mode.label} failed: {result.error}
      </p>
    )
  }

  const journey = result.journey
  return (
    <section className="mt-3 pt-3 border-t border-paper-edge">
      <div className="flex items-baseline gap-2.5">
        <span
          aria-hidden
          className="self-stretch w-1 rounded-full"
          style={{ background: modeColor(mode.id) }}
        />
        <p className="font-display text-22px font-600 text-ink leading-none">
          {formatDuration(result.durationSeconds ?? 0)}
        </p>
        <p className="text-13px text-ink-soft">
          {result.distanceMeters ? formatDistance(result.distanceMeters) : ''}
          {journey?.walking_distance_meters ? ' walking' : ''}
        </p>
      </div>

      {mode.kind === 'gtfs' && (
        <div className="mt-2 flex items-center gap-2 text-12px text-ink-soft">
          <label className="flex items-center gap-1.5">
            <span className="text-11px uppercase tracking-widest text-ink-faint">Depart</span>
            <input
              type="time"
              step={1}
              value={busTime}
              onChange={(e) => onBusTime(e.target.value)}
              className="px-1.5 py-0.5 font-mono text-12px text-ink bg-paper-deep border border-paper-edge rounded outline-none focus:border-accent"
            />
          </label>
          {journey && (
            <span className="font-mono text-11px tabular-nums text-ink-faint">
              {journey.departure_time} → {journey.arrival_time}
            </span>
          )}
        </div>
      )}

      {journey &&
        (journey.mode === 'multimodal' ? (
          <Itinerary journey={journey} />
        ) : (
          <p className="mt-1.5 text-11px text-ink-faint leading-snug">
            No faster transit found — direct walk shown.
          </p>
        ))}

      <p className="mt-2 font-mono text-10px text-ink-faint tabular-nums">
        {result.nodeCount ?? 0} nodes · solved in {formatNumber(result.solveMs)} ms
      </p>
    </section>
  )
}
