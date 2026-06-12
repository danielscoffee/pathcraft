export interface TogglesProps {
  streets: boolean
  nodes: boolean
  stops: boolean
  onStreets: (v: boolean) => void
  onNodes: (v: boolean) => void
  onStops: (v: boolean) => void
}

const ITEMS = [
  { key: 'streets', label: 'Street graph' },
  { key: 'nodes', label: 'Test nodes' },
  { key: 'stops', label: 'Bus stops' },
] as const

export default function Toggles(props: TogglesProps) {
  const values = { streets: props.streets, nodes: props.nodes, stops: props.stops }
  const setters = { streets: props.onStreets, nodes: props.onNodes, stops: props.onStops }

  return (
    <fieldset className="mt-3 pt-3 border-t border-paper-edge">
      <legend className="sr-only">Debug overlays</legend>
      <p className="text-11px font-600 uppercase tracking-widest text-ink-faint mb-1.5">Overlays</p>
      <div className="flex flex-wrap gap-x-4 gap-y-1">
        {ITEMS.map(({ key, label }) => (
          <label
            key={key}
            className="flex items-center gap-1.5 text-12px text-ink-soft cursor-pointer select-none"
          >
            <input
              type="checkbox"
              checked={values[key]}
              onChange={(e) => setters[key](e.target.checked)}
              className="accent-[#c2451e] cursor-pointer"
            />
            {label}
          </label>
        ))}
      </div>
    </fieldset>
  )
}
