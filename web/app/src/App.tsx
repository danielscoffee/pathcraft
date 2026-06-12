import { useCallback, useEffect, useRef, useState } from 'react'
import { fetchConfig, fetchModes } from './api/client'
import type { MapConfig, RouteMode, TripDetail } from './api/types'
import ControlPanel from './components/ControlPanel'
import Itinerary from './components/Itinerary'
import MapView from './components/MapView'
import ModeControls from './components/ModeControls'
import Stats from './components/Stats'
import StreetLegend from './components/StreetLegend'
import Toggles from './components/Toggles'
import TripOverlay from './components/TripOverlay'
import { useRouting, type Status } from './hooks/useRouting'

// Recife fallbacks, mirroring the server-side defaults in handlers_config.go.
const FALLBACK_CONFIG: MapConfig = {
  center_lat: -8.054,
  center_lon: -34.88,
  zoom: 16,
  tile_url: 'https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png',
}

const FALLBACK_MODES: RouteMode[] = [
  { id: 'walk', label: 'Walk', kind: 'standard', endpoint: '/route' },
  { id: 'bus', label: 'Bus / GTFS', kind: 'gtfs', endpoint: '/journey' },
  { id: 'car', label: 'Car', kind: 'standard', endpoint: '/route' },
  { id: 'bike', label: 'Bike', kind: 'standard', endpoint: '/route' },
]

export default function App() {
  const [config, setConfig] = useState<MapConfig | null>(null)
  const [modes, setModes] = useState<RouteMode[]>(FALLBACK_MODES)
  const [modeID, setModeID] = useState('walk')
  const [busTime, setBusTime] = useState('05:00:00')
  const [showStreets, setShowStreets] = useState(false)
  const [showNodes, setShowNodes] = useState(false)
  const [showStops, setShowStops] = useState(false)
  const [streetTypes, setStreetTypes] = useState<string[]>([])
  const [trip, setTrip] = useState<TripDetail | null>(null)

  const mode = modes.find((m) => m.id === modeID) ?? modes[0]
  const routing = useRouting(mode, busTime)
  const { status, setStatus, reset, resolve, placePoint } = routing

  useEffect(() => {
    fetchConfig()
      .then(setConfig)
      .catch(() => setConfig(FALLBACK_CONFIG))
    fetchModes()
      .then((loaded) => {
        setModes(loaded)
        setModeID((current) => (loaded.some((m) => m.id === current) ? current : loaded[0].id))
      })
      .catch(() =>
        setStatus({ text: 'Mode catalog unavailable; using built-in modes.', tone: 'info' }),
      )
  }, [setStatus])

  // Re-solve when the mode or departure time changes mid-session.
  const resolveRef = useRef(resolve)
  useEffect(() => {
    resolveRef.current = resolve
  }, [resolve])
  useEffect(() => {
    resolveRef.current()
  }, [modeID, busTime])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'r' || e.key === 'R') reset()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [reset])

  const onStatus = useCallback((s: Status) => setStatus(s), [setStatus])

  if (!config) {
    return (
      <main className="h-screen grid place-items-center bg-paper">
        <p className="font-mono text-13px text-ink-faint">▸ Loading PathCraft…</p>
      </main>
    )
  }

  return (
    <main className="relative h-screen w-full overflow-hidden">
      <MapView
        config={config}
        from={routing.from}
        to={routing.to}
        route={routing.route}
        trip={trip}
        showStreets={showStreets}
        showNodes={showNodes}
        showStops={showStops}
        onMapClick={placePoint}
        onStatus={onStatus}
        onStreetTypes={setStreetTypes}
      />

      <ControlPanel from={routing.from} to={routing.to} status={status} onReset={() => reset()}>
        <ModeControls
          modes={modes}
          selected={mode?.id ?? ''}
          busTime={busTime}
          onSelect={setModeID}
          onBusTime={setBusTime}
        />
        <div className="mt-3">
          <Stats stats={routing.stats} />
        </div>
        {routing.route?.journey && <Itinerary journey={routing.route.journey} />}
        <Toggles
          streets={showStreets}
          nodes={showNodes}
          stops={showStops}
          onStreets={setShowStreets}
          onNodes={setShowNodes}
          onStops={setShowStops}
        />
        {showStreets && <StreetLegend types={streetTypes} />}
        <TripOverlay onTrip={setTrip} onStatus={onStatus} />
      </ControlPanel>
    </main>
  )
}
