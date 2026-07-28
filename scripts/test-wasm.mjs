import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import vm from "node:vm";

import { initPathcraft } from "../sdk/js/pathcraft.mjs";

const [wasmPath, wasmExecPath, osmPath] = process.argv.slice(2);
if (!wasmPath || !wasmExecPath || !osmPath) {
	throw new Error(
		"usage: node scripts/test-wasm.mjs <pathcraft.wasm> <wasm_exec.js> <fixture.osm>",
	);
}

vm.runInThisContext(await readFile(wasmExecPath, "utf8"), {
	filename: wasmExecPath,
});

const pathcraft = await initPathcraft(await readFile(wasmPath));
assert.deepEqual(pathcraft.stats(), {
	nodes: 0,
	edges: 0,
	contractedNodes: 0,
	contractionChains: 0,
});

const loadStats = pathcraft.loadOSM(await readFile(osmPath, "utf8"));
assert.equal(loadStats.nodes, 6);
assert.equal(loadStats.contractedNodes, 4);

const route = pathcraft.route({
	from: { lat: -8.05428, lon: -34.8813 },
	to: { lat: -8.0552, lon: -34.8797 },
	mode: "walk",
	includeCoordinates: true,
});
assert.deepEqual(route.nodes, [1, 2, 3, 4, 5, 6]);
assert.equal(route.coordinates.length, 6);
assert.throws(
	() => pathcraft.route({ mode: "flight" }),
	/from and to coordinates are required|unsupported mode/,
);
let lowLevelError;
try {
	lowLevelError = JSON.parse(globalThis.pathcraftWasm.route());
} catch (error) {
	assert.fail(`low-level error response is not JSON: ${error}`);
}
assert.equal(lowLevelError.ok, false);

process.stdout.write("WASM smoke test passed\n");
process.exit(0);
