# React Frontend Migration — Design Spec

Date: 2026-06-11
Status: approved (autonomous mode)

## Goal

Replace the Go-templated `web/template/map.html` demo page with a modern
single-page application built on React + Vite + UnoCSS, served by the
existing Go HTTP server. Full feature parity with the current page.

## Context

- Backend: Go engine with a compile-time plugin registry (algorithms,
  loaders, exporters, cost models, loggers).
- HTTP API (JSON/GeoJSON): `/route`, `/journey`, `/modes`, `/nearest`,
  `/graph`, `/nodes`, `/transit/stops`, `/transit/trips`, `/transit/trip`,
  `/health`, `/status`.
- Current UI: one 718-line `map.html` rendered through `html/template`
  with injected page data (center, zoom, tile URL, route style). Leaflet
  from a CDN, all logic in inline vanilla JS.

## Decisions

1. **Stack**: Vite + React 19 + TypeScript + UnoCSS (presetWind). Leaflet
   via `leaflet` + `react-leaflet`. No global state library — hooks and
   one context are enough for this app size.
2. **Template data → API**: new `GET /config` endpoint returns
   `{ center_lat, center_lon, zoom, tile_url }` (computed the same way
   `handleGraphVisual` does today). The SPA fetches it at startup.
3. **Location**: `web/app/` (Vite project). Build output `web/app/dist/`.
4. **Serving**:
   - Dev: `vite dev` on :5173 with proxy of all API paths to :8080.
   - Prod: `//go:embed all:app/dist` in `web/embed.go`; Go mux serves the
     SPA at `/` with index fallback. `make web` builds the frontend before
     `make build`.
5. **Removal**: `web/template/map.html`, `handlers_demo.go`, `PageData`,
   and the `/graph-visual` route die. `/graph-visual` becomes a redirect
   to `/` so old bookmarks keep working.

## Component breakdown

```
web/app/src/
  main.tsx              entry, UnoCSS reset, App mount
  App.tsx               config fetch, layout (map + panel)
  api/client.ts         typed fetchers for all endpoints
  api/types.ts          TS mirrors of Go response structs
  hooks/useRouting.ts   A/B click state machine, snap, solve
  hooks/useOverlays.ts  street graph / nodes / stops debug layers
  components/MapView.tsx       Leaflet map, markers, route layers
  components/ControlPanel.tsx  panel shell
  components/ModeControls.tsx  mode select + bus time
  components/Stats.tsx         distance/time/nodes/solve grid
  components/Itinerary.tsx     journey legs list
  components/Toggles.tsx       debug checkboxes
  components/TripOverlay.tsx   GTFS trip select + render
  components/StreetLegend.tsx  highway type legend
  lib/geo.ts            haversine, length, time estimate
  lib/journey.ts        journey → GeoJSON conversion
  lib/mapStyle.ts       mode colors, highway styles
```

## Data flow

App fetches `/config` and `/modes` on mount. Map clicks drive a small
state machine (idle → A placed → B placed → solved); each placement
calls `/nearest` to snap. Solving picks endpoint by mode kind
(`standard` → `/route` GeoJSON, `gtfs` → `/journey` legs). Debug
overlays are lazy: fetched on first toggle, cached. Node overlay
refetches on map move with 200 ms debounce, zoom ≥ 16, abortable.

## Error handling

Every fetch failure lands in the status line (same UX as today).
`/modes` failure falls back to built-in mode list. `/config` failure
falls back to Recife defaults (-8.0540, -34.8800, zoom 16).

## Testing

- Go: handler test for `/config`; existing router tests keep passing.
- Frontend: `vite build` + `tsc --noEmit` as the CI gate;
  vitest unit tests for `lib/` (geo math, journey→GeoJSON).

## Visual checkpoints

After parity build: run `pathcraft server` with the Recife example
datasets, build SPA, screenshot with Playwright, present to user.
