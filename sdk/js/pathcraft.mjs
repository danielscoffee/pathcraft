function unwrap(raw) {
	if (typeof raw !== "string")
		throw new Error("PathCraft WASM returned a non-string response");
	let response;
	try {
		response = JSON.parse(raw);
	} catch (error) {
		throw new Error(`PathCraft WASM returned invalid JSON: ${error}`);
	}
	if (!response.ok)
		throw new Error(response.error || "PathCraft WASM call failed");
	return response.value;
}

async function loadBytes(source) {
	if (typeof source === "string" || source instanceof URL) {
		const response = await fetch(source);
		if (!response.ok)
			throw new Error(`failed to fetch PathCraft WASM: ${response.status}`);
		return response.arrayBuffer();
	}
	if (source instanceof ArrayBuffer || ArrayBuffer.isView(source))
		return source;
	throw new TypeError("WASM source must be a URL, ArrayBuffer, or typed array");
}

/**
 * Starts PathCraft's Go WebAssembly module.
 * Load matching Go wasm_exec.js before calling this function.
 */
export async function initPathcraft(source) {
	if (typeof globalThis.Go !== "function") {
		throw new Error(
			"Go WASM runtime missing; load matching wasm_exec.js first",
		);
	}

	const go = new globalThis.Go();
	const result = await WebAssembly.instantiate(
		await loadBytes(source),
		go.importObject,
	);
	const instance =
		result instanceof WebAssembly.Instance ? result : result.instance;
	void go.run(instance);

	const bridge = globalThis.pathcraftWasm;
	if (!bridge) throw new Error("PathCraft WASM bridge did not initialize");

	return Object.freeze({
		loadOSM(xml) {
			return unwrap(bridge.loadOSM(xml));
		},
		route(request) {
			return unwrap(bridge.route(JSON.stringify(request)));
		},
		stats() {
			return unwrap(bridge.stats());
		},
	});
}
