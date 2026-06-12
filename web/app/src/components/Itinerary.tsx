import type { Journey, JourneyLeg } from '../api/types'
import { formatNumber } from '../lib/geo'
import { modeColor } from '../lib/mapStyle'

function legTitle(leg: JourneyLeg): string {
  if (leg.mode === 'transit') {
    const line = [leg.route_name || leg.route_id || leg.trip_id || '', leg.route_long_name || '']
      .filter(Boolean)
      .join(' — ')
    return `Bus ${line}`
  }
  if (leg.mode === 'transfer') return 'Transfer'
  if (leg.mode === 'walk') return 'Walk'
  return leg.mode || 'Leg'
}

function legDetail(leg: JourneyLeg): string {
  const fromTo = `${leg.from_name || leg.from_stop_id || ''} → ${leg.to_name || leg.to_stop_id || ''}`
  if (leg.mode === 'transit') return fromTo
  const distance = `${formatNumber(leg.distance_meters ?? 0)} m`
  const minutes = `${formatNumber((leg.duration_seconds ?? 0) / 60)} min`
  return `${fromTo} · ${distance} · ${minutes}`
}

export default function Itinerary({ journey }: { journey: Journey }) {
  const legs = journey.legs ?? []
  if (legs.length === 0) return null

  return (
    <section className="mt-3 pt-3 border-t border-paper-edge">
      <h2 className="text-11px font-600 uppercase tracking-widest text-ink-faint mb-2">
        Journey · {journey.departure_time} → {journey.arrival_time}
      </h2>
      <ol className="grid">
        {legs.map((leg, i) => (
          <li key={i} className="relative pl-5 pb-2.5 last:pb-0">
            {/* ticket-style connector line + mode dot */}
            {i < legs.length - 1 && (
              <span
                aria-hidden
                className="absolute left-1.25 top-3 bottom--1 w-2px"
                style={{ background: modeColor(leg.mode) }}
              />
            )}
            <span
              aria-hidden
              className="absolute left-0 top-1 w-2.75 h-2.75 rounded-full border-2 border-paper"
              style={{ background: modeColor(leg.mode) }}
            />
            <p className="text-12px font-600" style={{ color: modeColor(leg.mode) }}>
              {legTitle(leg)}
            </p>
            <p className="text-12px text-ink-soft leading-snug">{legDetail(leg)}</p>
          </li>
        ))}
      </ol>
    </section>
  )
}
