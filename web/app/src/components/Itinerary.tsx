import type { ModeRouteSegment } from '../api/types'
import { formatNumber } from '../lib/geo'
import { DEFAULT_ROUTE_COLOR } from '../lib/mapStyle'

const metaString = (value: unknown) => (typeof value === 'string' ? value : '')

function segmentDetail(segment: ModeRouteSegment): string {
  const from = metaString(segment.meta?.from) || metaString(segment.meta?.from_stop_id)
  const to = metaString(segment.meta?.to) || metaString(segment.meta?.to_stop_id)
  const fromTo = from || to ? `${from} → ${to}` : ''
  const distance = segment.distance_meters ? `${formatNumber(segment.distance_meters)} m` : ''
  const minutes = segment.duration_seconds
    ? `${formatNumber(segment.duration_seconds / 60)} min`
    : ''
  return [fromTo, distance, minutes].filter(Boolean).join(' · ')
}

export default function Itinerary({ segments }: { segments: ModeRouteSegment[] }) {
  if (segments.length === 0) return null

  return (
    <section className="mt-2.5">
      <h2 className="text-11px font-600 uppercase tracking-widest text-ink-faint mb-2">Steps</h2>
      <ol className="grid">
        {segments.map((segment, index) => {
          const color = segment.color || DEFAULT_ROUTE_COLOR
          return (
            <li key={index} className="relative pl-5 pb-2.5 last:pb-0">
              {index < segments.length - 1 && (
                <span
                  aria-hidden
                  className="absolute left-1.25 top-3 bottom--1 w-2px"
                  style={{ background: color }}
                />
              )}
              <span
                aria-hidden
                className="absolute left-0 top-1 w-2.75 h-2.75 rounded-full border-2 border-paper"
                style={{ background: color }}
              />
              <p className="text-12px font-600" style={{ color }}>
                {segment.label || segment.kind || 'Segment'}
              </p>
              <p className="text-12px text-ink-soft leading-snug">{segmentDetail(segment)}</p>
            </li>
          )
        })}
      </ol>
    </section>
  )
}
