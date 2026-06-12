import type { RouteMode } from '../api/types'

export interface ModeControlsProps {
  modes: RouteMode[]
  selected: string
  busTime: string
  onSelect: (id: string) => void
  onBusTime: (value: string) => void
}

export default function ModeControls({
  modes,
  selected,
  busTime,
  onSelect,
  onBusTime,
}: ModeControlsProps) {
  const active = modes.find((m) => m.id === selected)
  return (
    <div className="grid gap-2">
      <div className="flex flex-wrap gap-1.5" role="radiogroup" aria-label="Routing mode">
        {modes.map((mode) => (
          <button
            key={mode.id}
            type="button"
            role="radio"
            aria-checked={mode.id === selected}
            onClick={() => onSelect(mode.id)}
            className={`px-3 py-1.5 text-12px font-500 tracking-wide rounded-full border transition-colors duration-150 cursor-pointer ${
              mode.id === selected
                ? 'bg-ink text-paper border-ink'
                : 'bg-transparent text-ink-soft border-paper-edge hover:border-ink-soft'
            }`}
          >
            {mode.label}
          </button>
        ))}
      </div>
      {active?.kind === 'gtfs' && (
        <label className="grid gap-1 text-11px uppercase tracking-widest text-ink-faint">
          Departure
          <input
            type="time"
            step={1}
            value={busTime}
            onChange={(e) => onBusTime(e.target.value)}
            className="w-full box-border px-2.5 py-1.5 font-mono text-13px text-ink bg-paper-deep border border-paper-edge rounded-md outline-none focus:border-accent"
          />
        </label>
      )}
    </div>
  )
}
