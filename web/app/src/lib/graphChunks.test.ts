import { describe, expect, it } from "vitest";
import type { GraphChunkConfig } from "../api/types";
import {
	DEFAULT_CHUNK_CACHE_SIZE,
	GraphChunkManager,
	graphChunkLoadStatus,
	graphChunkURL,
	pruneGraphChunkFailures,
	visibleChunkIDs,
	type GraphChunkBounds,
} from "./graphChunks.ts";

const config = (generation = "generation-a"): GraphChunkConfig => ({
	generation,
	zoom: 3,
	min_render_zoom: 2,
	url: "/graph/chunks/{generation}/{z}/{x}/{y}",
});

const ordinaryBounds: GraphChunkBounds = {
	west: -10,
	south: -10,
	east: 10,
	north: 10,
};

const chunk = (id: string): GeoJSON.FeatureCollection => ({
	type: "FeatureCollection",
	features: [],
	bbox: [id.length, 0, 0, 0],
});

const settle = async () => {
	for (let index = 0; index < 8; index++) await Promise.resolve();
};

function deferred<T>() {
	let resolve!: (value: T) => void;
	let reject!: (reason?: unknown) => void;
	const promise = new Promise<T>((yes, no) => {
		resolve = yes;
		reject = no;
	});
	return { promise, resolve, reject };
}

describe("visibleChunkIDs", () => {
	it("returns ordinary viewport tiles in deterministic row-major order", () => {
		expect(visibleChunkIDs(ordinaryBounds, 2)).toEqual([
			"2/1/1",
			"2/2/1",
			"2/1/2",
			"2/2/2",
		]);
		expect(visibleChunkIDs(ordinaryBounds, 2)).toEqual(
			visibleChunkIDs(ordinaryBounds, 2),
		);
	});

	it("wraps across the antimeridian", () => {
		expect(
			visibleChunkIDs({ west: 170, south: -10, east: -170, north: 10 }, 2),
		).toEqual(["2/3/1", "2/0/1", "2/3/2", "2/0/2"]);
	});

	it("clamps polar bounds to Web Mercator", () => {
		expect(
			visibleChunkIDs({ west: -10, south: -90, east: 10, north: 90 }, 1),
		).toEqual(["1/0/0", "1/1/0", "1/0/1", "1/1/1"]);
	});

	it("caps pathological viewports before allocating an unbounded tile list", () => {
		expect(
			visibleChunkIDs({ west: -180, south: -90, east: 180, north: 90 }, 20),
		).toHaveLength(256);
	});
});

describe("graphChunkURL", () => {
	it("substitutes only validated tile integers and an encoded server generation", () => {
		expect(
			graphChunkURL({ ...config(), generation: "generation/a" }, "3/4/2"),
		).toBe("/graph/chunks/generation%2Fa/3/4/2");
		expect(() => graphChunkURL(config(), "3/../../secret")).toThrow(
			"invalid graph chunk id",
		);
		expect(() =>
			graphChunkURL(
				{ ...config(), url: "javascript:{generation}/{z}/{x}/{y}" },
				"3/4/2",
			),
		).toThrow("invalid graph chunk config");
		for (const url of [
			"/\\evil.example/{generation}/{z}/{x}/{y}",
			"/\n/evil.example/{generation}/{z}/{x}/{y}",
			"/\r/evil.example/{generation}/{z}/{x}/{y}",
			"/\t/evil.example/{generation}/{z}/{x}/{y}",
			"//evil.example/{generation}/{z}/{x}/{y}",
			"relative/{generation}/{z}/{x}/{y}",
		]) {
			expect(() => graphChunkURL({ ...config(), url }, "3/4/2")).toThrow(
				"invalid graph chunk config",
			);
		}
	});
});

describe("graphChunkLoadStatus", () => {
	it("keeps partial failures visible after successful chunks publish", () => {
		expect(graphChunkLoadStatus(12, 1)).toEqual({
			text: "Street graph: 12 edges shown; 1 chunk failed.",
			tone: "err",
		});
		expect(graphChunkLoadStatus(12, 0).tone).toBe("info");
	});

	it("reports when offscreen failures were pruned", () => {
		const failures = new Map([
			["3/1/1", new Error("gone")],
			["3/2/1", new Error("visible")],
		]);
		expect(pruneGraphChunkFailures(failures, new Set(["3/2/1"]))).toBe(true);
		expect([...failures.keys()]).toEqual(["3/2/1"]);
		expect(pruneGraphChunkFailures(failures, new Set(["3/2/1"]))).toBe(false);
	});
});

describe("GraphChunkManager", () => {
	it("limits concurrent loads to six and publishes each completed chunk immediately", async () => {
		const pending: Array<
			ReturnType<typeof deferred<GeoJSON.FeatureCollection>>
		> = [];
		const published: string[] = [];
		let active = 0;
		let maximum = 0;
		const manager = new GraphChunkManager<GeoJSON.FeatureCollection>({
			load: async () => {
				active++;
				maximum = Math.max(maximum, active);
				const request = deferred<GeoJSON.FeatureCollection>();
				pending.push(request);
				return request.promise.finally(() => active--);
			},
			publish: (id) => published.push(id),
			remove: () => undefined,
		});

		manager.update({
			config: config(),
			enabled: true,
			bounds: { west: -179, south: -80, east: 179, north: 80 },
			mapZoom: 3,
		});
		expect(pending).toHaveLength(6);
		pending[0].resolve(chunk("first"));
		await settle();
		expect(published).toHaveLength(1);
		expect(pending).toHaveLength(7);
		expect(maximum).toBe(6);
		manager.dispose();
	});

	it("keeps aborted requests in the concurrency budget until they settle", async () => {
		const pending: Array<
			ReturnType<typeof deferred<GeoJSON.FeatureCollection>>
		> = [];
		const manager = new GraphChunkManager<GeoJSON.FeatureCollection>({
			load: async (_config, id) => {
				const request = deferred<GeoJSON.FeatureCollection>();
				pending.push(request);
				return request.promise.then(() => chunk(id));
			},
			publish: () => undefined,
			remove: () => undefined,
		});
		manager.update({
			config: config(),
			enabled: true,
			bounds: { west: -179, south: -80, east: -1, north: 80 },
			mapZoom: 3,
		});
		expect(pending).toHaveLength(6);
		manager.update({
			config: config(),
			enabled: true,
			bounds: { west: 1, south: -80, east: 179, north: 80 },
			mapZoom: 3,
		});
		expect(pending).toHaveLength(6);
		pending[0].resolve(chunk("aborted"));
		await settle();
		expect(pending).toHaveLength(7);
		manager.dispose();
	});

	it("waits for an aborted same-ID flight before loading the new generation", async () => {
		const pending: Array<
			ReturnType<typeof deferred<GeoJSON.FeatureCollection>>
		> = [];
		const manager = new GraphChunkManager<GeoJSON.FeatureCollection>({
			load: async (_config, id) => {
				const request = deferred<GeoJSON.FeatureCollection>();
				pending.push(request);
				return request.promise.then(() => chunk(id));
			},
			publish: () => undefined,
			remove: () => undefined,
		});
		const viewport = {
			config: config(),
			enabled: true,
			bounds: ordinaryBounds,
			mapZoom: 3,
		};
		manager.update(viewport);
		const firstGenerationLoads = pending.length;
		manager.update({ ...viewport, config: config("generation-b") });
		expect(pending).toHaveLength(firstGenerationLoads);
		pending[0].resolve(chunk("aborted"));
		await settle();
		expect(pending).toHaveLength(firstGenerationLoads + 1);
		manager.dispose();
	});

	it("aborts in-flight work when disabled or generation changes", () => {
		const createManager = (signals: AbortSignal[]) =>
			new GraphChunkManager<GeoJSON.FeatureCollection>({
				load: (_config, _id, signal) => {
					signals.push(signal);
					return new Promise(() => undefined);
				},
				publish: () => undefined,
				remove: () => undefined,
			});
		const viewport = {
			config: config(),
			enabled: true,
			bounds: ordinaryBounds,
			mapZoom: 3,
		};

		const disabledSignals: AbortSignal[] = [];
		const disabledManager = createManager(disabledSignals);
		disabledManager.update(viewport);
		expect(disabledSignals.length).toBeGreaterThan(0);
		disabledManager.update({ ...viewport, enabled: false });
		expect(disabledSignals.every((signal) => signal.aborted)).toBe(true);
		disabledManager.dispose();

		const staleSignals: AbortSignal[] = [];
		const staleManager = createManager(staleSignals);
		staleManager.update(viewport);
		expect(staleSignals.length).toBeGreaterThan(0);
		staleManager.update({ ...viewport, config: config("generation-b") });
		expect(staleSignals.every((signal) => signal.aborted)).toBe(true);
		staleManager.dispose();
	});

	it("removes offscreen publications and skips requests below minimum zoom", async () => {
		const published: string[] = [];
		const removed: string[] = [];
		let loads = 0;
		const manager = new GraphChunkManager<GeoJSON.FeatureCollection>({
			load: async (_config, id) => {
				loads++;
				return chunk(id);
			},
			publish: (id) => published.push(id),
			remove: (id) => removed.push(id),
		});
		manager.update({
			config: config(),
			enabled: true,
			bounds: ordinaryBounds,
			mapZoom: 1,
		});
		expect(loads).toBe(0);

		manager.update({
			config: config(),
			enabled: true,
			bounds: ordinaryBounds,
			mapZoom: 3,
		});
		await settle();
		expect(published.length).toBeGreaterThan(0);
		const first = published[0];
		manager.update({
			config: config(),
			enabled: true,
			bounds: { west: 100, south: -10, east: 110, north: 10 },
			mapZoom: 3,
		});
		expect(removed).toContain(first);
		manager.dispose();
	});

	it("uses a bounded LRU and retries evicted chunks", async () => {
		expect(DEFAULT_CHUNK_CACHE_SIZE).toBe(64);
		const loads = new Map<string, number>();
		const manager = new GraphChunkManager<GeoJSON.FeatureCollection>({
			maxCache: 2,
			maxConcurrent: 6,
			load: async (_config, id) => {
				loads.set(id, (loads.get(id) ?? 0) + 1);
				return chunk(id);
			},
			publish: () => undefined,
			remove: () => undefined,
		});
		const firstViewport = { west: -170, south: 1, east: 80, north: 10 };
		const firstID = visibleChunkIDs(firstViewport, 3)[0];
		manager.update({
			config: config(),
			enabled: true,
			bounds: firstViewport,
			mapZoom: 3,
		});
		await settle();
		manager.update({
			config: config(),
			enabled: true,
			bounds: firstViewport,
			mapZoom: 3,
		});
		await settle();
		expect(loads.get(firstID)).toBe(1);
		manager.update({
			config: config(),
			enabled: true,
			bounds: { west: 140, south: 1, east: 150, north: 10 },
			mapZoom: 3,
		});
		await settle();
		manager.update({
			config: config(),
			enabled: true,
			bounds: firstViewport,
			mapZoom: 3,
		});
		await settle();
		expect(loads.get(firstID)).toBe(2);
		manager.dispose();
	});
});
