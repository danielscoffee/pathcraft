import { useState } from 'react'
import type { TripDetail } from '../api/types'
import type { Status } from '../hooks/useRouting'
import { LayersIcon } from './Icons'
import StreetLegend from './StreetLegend'
import TripOverlay from './TripOverlay'

export interface LayersMenuProps {
  streets: boolean
  nodes: boolean
  stops: boolean
  streetTypes: string[]
  onStreets: (v: boolean) => void
  onNodes: (v: boolean) => void
  onStops: (v: boolean) => void
  onTrip: (trip: TripDetail | null) => void
  onStatus: (status: Status) => void
}

const TOGGLES = [
  { key: 'streets', label: 'Street graph' },
  { key: 'nodes', label: 'Intersections' },
  { key: 'stops', label: 'Bus stops' },
] as const

/** Google-Maps-style layers button (bottom-left) with a popover menu. */
export default function LayersMenu(props: LayersMenuProps) {
  const [open, setOpen] = useState(false)
  const values = { streets: props.streets, nodes: props.nodes, stops: props.stops }
  const setters = { streets: props.onStreets, nodes: props.onNodes, stops: props.onStops }

  return (
    <div className="absolute bottom-6 left-4 z-[1000]">
      {open && (
        <div className="pc-panel mb-2 w-60 px-3.5 py-3 rounded-xl pc-reveal">
          <p className="text-11px font-600 uppercase tracking-widest text-ink-faint mb-2">
            Map layers
          </p>
          <div className="grid gap-1.5">
            {TOGGLES.map(({ key, label }) => (
              <label
                key={key}
                className="flex items-center gap-2 text-12px text-ink-soft cursor-pointer select-none"
              >
                <input
                  type="checkbox"
                  checked={values[key]}
                  onChange={(e) => setters[key](e.target.checked)}
                  className="accent-[color:var(--accent,#c2451e)] cursor-pointer"
                />
                {label}
              </label>
            ))}
          </div>
          {props.streets && <StreetLegend types={props.streetTypes} />}
          <TripOverlay onTrip={props.onTrip} onStatus={props.onStatus} />
        </div>
      )}
      <button
        type="button"
        aria-label="Map layers"
        aria-expanded={open}
        title="Map layers"
        onClick={() => setOpen((v) => !v)}
        className={`pc-panel p-2.5 rounded-xl cursor-pointer transition-colors ${
          open ? 'text-accent' : 'text-ink-soft hover:text-ink'
        }`}
      >
        <LayersIcon size={20} />
      </button>
    </div>
  )
}
