import { useCallback, useEffect, useMemo, useState } from 'react'
import { fetchConfig, fetchModes } from './api/client'
import type { MapConfig, RouteMode, TripDetail } from './api/types'
import DirectionsCard from './components/DirectionsCard'
import LayersMenu from './components/LayersMenu'
import MapView from './components/MapView'
import ModeTabs from './components/ModeTabs'
import RouteSummary from './components/RouteSummary'
import { useRouting, type ModeOptions, type SolvedRoute, type Status } from './hooks/useRouting'
import { defaultModeOptions, supportsGeographicMap } from './lib/modeResult'

// Recife fallbacks, mirroring the server-side defaults in handlers_config.go.
const FALLBACK_CONFIG: MapConfig = {
  center_lat: -8.054,
  center_lon: -34.88,
  zoom: 16,
  tile_url: 'https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png',
}

export default function App() {
  const [config, setConfig] = useState<MapConfig | null>(null)
  const [modes, setModes] = useState<RouteMode[]>([])
  const [modeID, setModeID] = useState('')
  const [modeOptions, setModeOptions] = useState<ModeOptions>({})
  const [showStreets, setShowStreets] = useState(false)
  const [showNodes, setShowNodes] = useState(false)
  const [showStops, setShowStops] = useState(false)
  const [streetTypes, setStreetTypes] = useState<string[]>([])
  const [trip, setTrip] = useState<TripDetail | null>(null)

  const routing = useRouting(modes, modeOptions)
  const { setStatus, reset, resolveMode } = routing

  const activeMode = modes.find((m) => m.id === modeID) ?? modes[0]
  const activeResult = routing.results[activeMode?.id ?? '']

  // The drawn route derives from the active tab's cached result.
  const route: SolvedRoute | null = useMemo(() => {
    if (!activeMode || !activeResult?.ok || !activeResult.geojson) return null
    return {
      geojson: activeResult.geojson,
      modeID: activeMode.id,
      generation: routing.generation,
    }
  }, [activeMode, activeResult, routing.generation])

  useEffect(() => {
    fetchConfig()
      .then(setConfig)
      .catch(() => setConfig(FALLBACK_CONFIG))
    fetchModes()
      .then((loaded) => {
        const supported = loaded.filter(supportsGeographicMap)
        if (supported.length === 0) throw new Error('no modes support this map renderer')
        setModes(supported)
        setModeOptions(Object.fromEntries(supported.map((mode) => [mode.id, defaultModeOptions(mode)])))
        setModeID((current) =>
          supported.some((mode) => mode.id === current) ? current : supported[0].id,
        )
      })
      .catch(() => setStatus({ text: 'Mode catalog unavailable.', tone: 'err' }))
  }, [setStatus])

  const onModeOption = useCallback(
    (name: string, value: string) => {
      if (!activeMode) return
      const options = { ...(modeOptions[activeMode.id] ?? {}), [name]: value }
      setModeOptions((previous) => ({ ...previous, [activeMode.id]: options }))
      resolveMode(activeMode.id, options)
    },
    [activeMode, modeOptions, resolveMode],
  )

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
        route={route}
        trip={trip}
        showStreets={showStreets}
        showNodes={showNodes}
        showStops={showStops}
        onMapClick={routing.placePoint}
        onStatus={onStatus}
        onStreetTypes={setStreetTypes}
      />

      <DirectionsCard
        from={routing.from}
        to={routing.to}
        status={routing.status}
        onClearPoint={routing.clearPoint}
        onSwap={routing.swap}
      >
        <ModeTabs
          modes={modes}
          selected={activeMode?.id ?? ''}
          results={routing.results}
          solving={routing.solving}
          onSelect={setModeID}
        />
        {activeMode && activeResult && (
          <RouteSummary
            mode={activeMode}
            result={activeResult}
            options={modeOptions[activeMode.id] ?? {}}
            onOption={onModeOption}
          />
        )}
      </DirectionsCard>

      <LayersMenu
        streets={showStreets}
        nodes={showNodes}
        stops={showStops}
        streetTypes={streetTypes}
        onStreets={setShowStreets}
        onNodes={setShowNodes}
        onStops={setShowStops}
        onTrip={setTrip}
        onStatus={onStatus}
      />
    </main>
  )
}
