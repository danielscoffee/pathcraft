import { HIGHWAY_LEGEND_ORDER, highwayStyle } from '../lib/mapStyle'

export default function StreetLegend({ types }: { types: string[] }) {
  if (types.length === 0) return null

  const known = HIGHWAY_LEGEND_ORDER.filter((t) => types.includes(t))
  const unknown = types.filter((t) => !HIGHWAY_LEGEND_ORDER.includes(t))
  const ordered = [...known, ...unknown]

  return (
    <details className="mt-2.5 text-12px" open>
      <summary className="cursor-pointer text-ink-faint hover:text-ink-soft">Street types</summary>
      <div className="mt-1.5 grid grid-cols-2 gap-x-4 gap-y-1">
        {ordered.map((type) => {
          const style = highwayStyle(type)
          return (
            <div key={type} className="flex items-center gap-2 text-11px text-ink-soft">
              <span
                aria-hidden
                className="inline-block w-5.5 rounded-sm"
                style={{ background: style.color, height: Math.max(2, style.weight) }}
              />
              {type}
            </div>
          )
        })}
      </div>
    </details>
  )
}
