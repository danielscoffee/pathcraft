import { useEffect, useState } from 'react'
import { fetchTrip, fetchTripIDs } from '../api/client'
import type { TripDetail } from '../api/types'
import type { Status } from '../hooks/useRouting'

export interface TripOverlayProps {
  onTrip: (trip: TripDetail | null) => void
  onStatus: (status: Status) => void
}

export default function TripOverlay({ onTrip, onStatus }: TripOverlayProps) {
  const [tripIDs, setTripIDs] = useState<string[]>([])
  const [selected, setSelected] = useState('')

  useEffect(() => {
    fetchTripIDs()
      .then(setTripIDs)
      .catch(() => {
        // GTFS not loaded — leave the overlay empty, same as the legacy page.
      })
  }, [])

  if (tripIDs.length === 0) return null

  const render = () => {
    if (!selected) return
    fetchTrip(selected)
      .then(onTrip)
      .catch((err: Error) => onStatus({ text: `Trip load failed: ${err.message}`, tone: 'err' }))
  }

  return (
    <details className="mt-2.5 text-12px">
      <summary className="cursor-pointer text-ink-faint hover:text-ink-soft">
        GTFS trip overlay
      </summary>
      <div className="grid gap-1.5 mt-1.5">
        <select
          value={selected}
          onChange={(e) => setSelected(e.target.value)}
          className="w-full px-2 py-1.5 text-12px font-mono bg-paper-deep text-ink border border-paper-edge rounded-md outline-none focus:border-accent"
        >
          <option value="">Select a trip</option>
          {tripIDs.map((id) => (
            <option key={id} value={id}>
              {id}
            </option>
          ))}
        </select>
        <div className="flex gap-1.5">
          <button
            type="button"
            onClick={render}
            className="flex-1 px-2 py-1.5 text-12px font-500 bg-ink text-paper rounded-md cursor-pointer hover:bg-ink-soft transition-colors"
          >
            Render trip
          </button>
          <button
            type="button"
            onClick={() => onTrip(null)}
            className="px-2 py-1.5 text-12px text-ink-soft border border-paper-edge rounded-md cursor-pointer hover:border-ink-soft transition-colors"
          >
            Clear
          </button>
        </div>
      </div>
    </details>
  )
}
