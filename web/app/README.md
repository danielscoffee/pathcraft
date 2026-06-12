# PathCraft Frontend

React + Vite + UnoCSS single-page app for the PathCraft routing demo.
Replaces the old Go-templated `map.html`.

## Development

Start the Go API first, then the dev server with hot reload:

```bash
make demo            # or: ./bin/pathcraft serve --file ... --addr :8080
cd web/app && npm run dev   # http://localhost:5173, proxies API to :8080
```

Set `PATHCRAFT_API` to proxy to a non-default backend address.

## Production

`npm run build` writes to `dist/`, which the Go server embeds via
`web/web.go` (`go:embed`) and serves at `/`. From the repo root:

```bash
make release   # build frontend + Go binary
```

## Layout

- `src/api/` — typed client for the Go JSON/GeoJSON endpoints
- `src/hooks/useRouting.ts` — A/B click → snap → solve state machine
- `src/components/` — map, control panel, overlays, itinerary
- `src/lib/` — geo math, journey→GeoJSON conversion, map styling (unit-tested)

## Checks

```bash
npm run lint && npm run build && npm test
```
