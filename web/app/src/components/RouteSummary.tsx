import type { RouteMode } from '../api/types'
import type { ModeResult } from '../hooks/useRouting'
import { formatDistance, formatDuration, formatNumber } from '../lib/geo'
import { DEFAULT_ROUTE_COLOR } from '../lib/mapStyle'
import Itinerary from './Itinerary'

export interface RouteSummaryProps {
  mode: RouteMode
  result: ModeResult
  options: Record<string, string>
  onOption: (name: string, value: string) => void
}

const metaString = (value: unknown) => (typeof value === 'string' ? value : '')

export default function RouteSummary({
  mode,
  result,
  options,
  onOption,
}: RouteSummaryProps) {
  if (!result.ok) {
    return (
      <p className="mt-3 pt-3 border-t border-paper-edge text-12px text-err leading-snug">
        {mode.label} failed: {result.error}
      </p>
    )
  }

  const declaredOptions = mode.options ?? []
  const departure = metaString(result.route?.meta?.departure_time)
  const arrival = metaString(result.route?.meta?.arrival_time)
  const segments = result.route?.segments ?? []
  return (
    <section className="mt-3 pt-3 border-t border-paper-edge">
      <div className="flex items-baseline gap-2.5">
        <span
          aria-hidden
          className="self-stretch w-1 rounded-full"
          style={{ background: mode.color || DEFAULT_ROUTE_COLOR }}
        />
        <p className="font-display text-22px font-600 text-ink leading-none">
          {formatDuration(result.durationSeconds ?? 0)}
        </p>
        <p className="text-13px text-ink-soft">
          {result.distanceMeters ? formatDistance(result.distanceMeters) : ''}
        </p>
      </div>

      {declaredOptions.length > 0 && (
        <div className="mt-2 grid gap-1.5 text-12px text-ink-soft">
          {declaredOptions.map((option) => (
            <label key={option.name} className="flex items-center gap-1.5">
              <span className="text-11px uppercase tracking-widest text-ink-faint">
                {option.label}
              </span>
              <input
                type={option.kind === 'time' ? 'time' : option.kind === 'number' ? 'number' : 'text'}
                step={option.kind === 'time' ? 1 : 'any'}
                value={options[option.name] ?? option.default ?? ''}
                required={option.required}
                onChange={(event) => onOption(option.name, event.target.value)}
                className="min-w-0 flex-1 px-1.5 py-0.5 font-mono text-12px text-ink bg-paper-deep border border-paper-edge rounded outline-none focus:border-accent"
              />
            </label>
          ))}
          {departure && arrival && (
            <span className="font-mono text-11px tabular-nums text-ink-faint">
              {departure} → {arrival}
            </span>
          )}
        </div>
      )}

      {segments.length > 1 && <Itinerary segments={segments} />}

      <p className="mt-2 font-mono text-10px text-ink-faint tabular-nums">
        {result.nodeCount ?? 0} positions · solved in {formatNumber(result.solveMs)} ms
      </p>
    </section>
  )
}
