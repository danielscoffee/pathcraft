# JavaScript SDK (WebAssembly)

PathCraft's pre-release JavaScript binding runs street routing in browser-compatible Go WebAssembly. It loads plain OSM XML and supports walk, bike, and car coordinate routes. GTFS and multimodal routing are not included in this binding yet.

## Build

```bash
GOOS=js GOARCH=wasm go build -o pathcraft.wasm ./cmd/pathcraft-wasm
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" ./wasm_exec.js
```

Do not commit generated `pathcraft.wasm`. Compile module and copy `wasm_exec.js` from same Go major version; Go's runtime ABI is version-coupled.

Serve files over HTTP. Browser `file://` URLs cannot reliably load Wasm.

## Browser use

Load Go runtime before ES module:

```html
<script src="/wasm_exec.js"></script>
<script type="module">
  import { initPathcraft } from '/sdk/js/pathcraft.mjs'

  const pathcraft = await initPathcraft('/pathcraft.wasm')
  const osm = await fetch('/data/city.osm').then((response) => response.text())
  pathcraft.loadOSM(osm)

  const route = pathcraft.route({
    from: { lat: -8.05428, lon: -34.88130 },
    to: { lat: -8.05520, lon: -34.87970 },
    mode: 'walk',
    includeCoordinates: true,
  })

  console.log(route.distanceMeters, route.durationSeconds)
  console.log(route.coordinates)
</script>
```

`initPathcraft` also accepts an `ArrayBuffer` or typed array, useful when application code already fetched module bytes.

## API

### `loadOSM(xml)`

Loads plain OSM XML, builds street graph, and publishes degree-two contraction index. Returns:

```js
{
  nodes: 6,
  edges: 10,
  contractedNodes: 4,
  contractionChains: 2,
}
```

Use `LoadOSM` in Go CLI/SDK for `.osm.gz`; browser binding accepts uncompressed XML strings only.

### `route(request)`

Request:

```js
{
  from: { lat: number, lon: number },
  to: { lat: number, lon: number },
  mode: 'walk' | 'bike' | 'car', // omitted means walk
  includeCoordinates: boolean,
}
```

Result:

```js
{
  nodes: [1, 2, 3],
  coordinates: [{ lat: -8.05, lon: -34.88 }],
  distanceMeters: 120.5,
  durationSeconds: 86.1,
  fromNodeId: 1,
  toNodeId: 3,
  fromSnapDistanceMeters: 2.4,
  toSnapDistanceMeters: 5.1,
}
```

Invalid JSON, modes, coordinates, unloaded graphs, and unsolved routes become JavaScript `Error` values.

### `stats()`

Returns current graph counts. Before loading, all counts are zero.

## Runtime limits

Calls are synchronous. OSM parsing and route search block current browser thread or Web Worker. Fetch data before calling binding; starting asynchronous browser APIs from a Go callback can deadlock JavaScript event loop. Put PathCraft in Worker when dataset size causes visible UI stalls.

Current binding has no cancellation, streaming ingestion, GTFS, npm package, or compatibility guarantee before v1.

## Smoke test

```bash
GOOS=js GOARCH=wasm go build -o /tmp/pathcraft.wasm ./cmd/pathcraft-wasm
node scripts/test-wasm.mjs \
  /tmp/pathcraft.wasm \
  "$(go env GOROOT)/lib/wasm/wasm_exec.js" \
  testdata/example.osm
```
