import type { RouteMode } from '../api/types'
import type { ModeResult } from '../hooks/useRouting'
import { formatDuration } from '../lib/geo'
import { DEFAULT_ROUTE_COLOR } from '../lib/mapStyle'
import { modeIcon } from '../lib/modeIcons'

export interface ModeTabsProps {
  modes: RouteMode[]
  selected: string
  results: Record<string, ModeResult>
  solving: boolean
  onSelect: (id: string) => void
}

/** Google-Maps-style mode row: icon tabs with a live ETA under each. */
export default function ModeTabs({ modes, selected, results, solving, onSelect }: ModeTabsProps) {
  return (
    <div className="flex gap-1" role="tablist" aria-label="Travel mode">
      {modes.map((mode) => {
        const Icon = modeIcon(mode.icon)
        const result = results[mode.id]
        const active = mode.id === selected
        const eta = result?.ok
          ? formatDuration(result.durationSeconds ?? 0)
          : result
            ? '—'
            : solving
              ? '…'
              : ''
        return (
          <button
            key={mode.id}
            type="button"
            role="tab"
            aria-selected={active}
            title={result?.ok === false ? `${mode.label}: ${result.error}` : mode.label}
            onClick={() => onSelect(mode.id)}
            className={`flex-1 flex flex-col items-center gap-0.5 px-1 py-1.5 rounded-lg border cursor-pointer transition-colors duration-150 ${
              active
                ? 'border-transparent text-paper'
                : 'bg-transparent border-transparent text-ink-faint hover:text-ink-soft hover:bg-paper-deep'
            }`}
            style={active ? { background: mode.color || DEFAULT_ROUTE_COLOR } : undefined}
          >
            <Icon size={19} />
            <span
              className={`font-mono text-10px leading-none tabular-nums ${
                active ? 'text-paper' : result?.ok ? 'text-ink-soft' : 'text-ink-faint'
              }`}
            >
              {eta || mode.label.split(' ')[0]}
            </span>
          </button>
        )
      })}
    </div>
  )
}
