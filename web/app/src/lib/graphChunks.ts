import type { GraphChunkConfig } from "../api/types.ts";

export const MAX_VISIBLE_CHUNKS = 256;
export const DEFAULT_CHUNK_CONCURRENCY = 6;
export const DEFAULT_CHUNK_CACHE_SIZE = 64;
const MAX_MERCATOR_LATITUDE = 85.05112878;

export function graphChunkLoadStatus(edges: number, failures: number) {
	if (failures > 0) {
		return {
			text: `Street graph: ${edges} edges shown; ${failures} chunk${failures === 1 ? "" : "s"} failed.`,
			tone: "err" as const,
		};
	}
	return { text: `Street graph: ${edges} edges shown.`, tone: "info" as const };
}

export function pruneGraphChunkFailures<T>(
	failures: Map<string, T>,
	desired: ReadonlySet<string>,
) {
	let changed = false;
	for (const id of failures.keys()) {
		if (!desired.has(id)) {
			failures.delete(id);
			changed = true;
		}
	}
	return changed;
}

export interface GraphChunkBounds {
	west: number;
	south: number;
	east: number;
	north: number;
}

export interface GraphChunkViewport {
	config: GraphChunkConfig | null;
	enabled: boolean;
	bounds: GraphChunkBounds;
	mapZoom: number;
}

interface GraphChunkManagerOptions<T> {
	load: (
		config: GraphChunkConfig,
		id: string,
		signal: AbortSignal,
	) => Promise<T>;
	publish: (id: string, value: T) => void;
	remove: (id: string) => void;
	error?: (error: Error, id: string) => void;
	maxConcurrent?: number;
	maxCache?: number;
}

interface ChunkFlight {
	controller: AbortController;
	signature: string;
}

const clamp = (value: number, minimum: number, maximum: number) =>
	Math.max(minimum, Math.min(maximum, value));

const normalizeLongitude = (longitude: number) => {
	const normalized = ((((longitude + 180) % 360) + 360) % 360) - 180;
	return Object.is(normalized, -0) ? 0 : normalized;
};

const longitudeTile = (longitude: number, count: number) =>
	clamp(Math.floor(((longitude + 180) / 360) * count), 0, count - 1);

const latitudeTile = (latitude: number, count: number) => {
	const radians =
		(clamp(latitude, -MAX_MERCATOR_LATITUDE, MAX_MERCATOR_LATITUDE) * Math.PI) /
		180;
	return clamp(
		Math.floor(((1 - Math.asinh(Math.tan(radians)) / Math.PI) / 2) * count),
		0,
		count - 1,
	);
};

export function visibleChunkIDs(
	bounds: GraphChunkBounds,
	zoom: number,
): string[] {
	if (
		!Number.isInteger(zoom) ||
		zoom < 0 ||
		zoom > 30 ||
		!Object.values(bounds).every(Number.isFinite) ||
		bounds.south > bounds.north
	) {
		return [];
	}

	const count = 2 ** zoom;
	const minimumY = latitudeTile(bounds.north, count);
	const maximumY = latitudeTile(bounds.south, count);
	const longitudeSpan = Math.abs(bounds.east - bounds.west);
	let xRanges: Array<[number, number]>;
	if (longitudeSpan >= 360) {
		xRanges = [[0, count - 1]];
	} else {
		const west = normalizeLongitude(bounds.west);
		let east = normalizeLongitude(bounds.east);
		if (east === -180 && bounds.east > bounds.west) east = 180 - Number.EPSILON;
		const minimumX = longitudeTile(west, count);
		const maximumX = longitudeTile(east, count);
		xRanges =
			west <= east
				? [[minimumX, maximumX]]
				: [
						[minimumX, count - 1],
						[0, maximumX],
					];
	}

	const ids: string[] = [];
	for (let y = minimumY; y <= maximumY; y++) {
		for (const [minimumX, maximumX] of xRanges) {
			for (let x = minimumX; x <= maximumX; x++) {
				ids.push(`${zoom}/${x}/${y}`);
				if (ids.length === MAX_VISIBLE_CHUNKS) return ids;
			}
		}
	}
	return ids;
}

const configSignature = (config: GraphChunkConfig) =>
	`${config.generation}\u0000${config.zoom}\u0000${config.min_render_zoom}\u0000${config.url}`;

export class GraphChunkManager<T> {
	private readonly maxConcurrent: number;
	private readonly maxCache: number;
	private readonly cache = new Map<string, T>();
	private readonly flights = new Map<string, ChunkFlight>();
	private readonly published = new Set<string>();
	private desired = new Set<string>();
	private queue: string[] = [];
	private config: GraphChunkConfig | null = null;
	private signature = "";
	private enabled = false;
	private disposed = false;
	private readonly options: GraphChunkManagerOptions<T>;

	constructor(options: GraphChunkManagerOptions<T>) {
		this.options = options;
		this.maxConcurrent = Math.max(
			1,
			Math.floor(options.maxConcurrent ?? DEFAULT_CHUNK_CONCURRENCY),
		);
		this.maxCache = Math.max(
			1,
			Math.floor(options.maxCache ?? DEFAULT_CHUNK_CACHE_SIZE),
		);
	}

	update(viewport: GraphChunkViewport) {
		if (this.disposed) return;
		const nextSignature = viewport.config
			? configSignature(viewport.config)
			: "";
		if (nextSignature !== this.signature) {
			this.abortAll();
			this.removeAll();
			this.cache.clear();
			this.queue = [];
			this.desired.clear();
			this.signature = nextSignature;
		}
		this.config = viewport.config;

		if (
			!viewport.enabled ||
			!viewport.config ||
			viewport.mapZoom < viewport.config.min_render_zoom
		) {
			this.enabled = false;
			this.abortAll();
			this.queue = [];
			this.desired.clear();
			this.removeAll();
			return;
		}

		this.enabled = true;
		const nextDesired = new Set(
			visibleChunkIDs(viewport.bounds, viewport.config.zoom),
		);
		this.desired = nextDesired;
		this.queue = this.queue.filter((id) => nextDesired.has(id));
		for (const [id, flight] of this.flights) {
			if (!nextDesired.has(id)) flight.controller.abort();
		}
		for (const id of [...this.published]) {
			if (!nextDesired.has(id)) this.unpublish(id);
		}
		for (const id of nextDesired) {
			if (this.published.has(id)) continue;
			const cached = this.touch(id);
			if (cached !== undefined) {
				this.publish(id, cached);
			} else if (!this.flights.has(id) && !this.queue.includes(id)) {
				this.queue.push(id);
			}
		}
		this.pump();
	}

	dispose() {
		if (this.disposed) return;
		this.disposed = true;
		this.enabled = false;
		this.abortAll();
		this.removeAll();
		this.cache.clear();
		this.queue = [];
		this.desired.clear();
	}

	private pump() {
		while (
			this.enabled &&
			this.config &&
			this.flights.size < this.maxConcurrent &&
			this.queue.length > 0
		) {
			const id = this.queue.shift();
			if (!id || !this.desired.has(id) || this.flights.has(id)) continue;
			const controller = new AbortController();
			const signature = this.signature;
			const config = this.config;
			this.flights.set(id, { controller, signature });
			let request: Promise<T>;
			try {
				request = this.options.load(config, id, controller.signal);
			} catch (reason) {
				request = Promise.reject(reason);
			}
			request
				.then((value) => {
					if (
						controller.signal.aborted ||
						signature !== this.signature ||
						this.disposed
					)
						return;
					this.put(id, value);
					if (this.enabled && this.desired.has(id)) this.publish(id, value);
				})
				.catch((reason: unknown) => {
					if (
						controller.signal.aborted ||
						signature !== this.signature ||
						this.disposed
					)
						return;
					const error =
						reason instanceof Error ? reason : new Error(String(reason));
					this.options.error?.(error, id);
				})
				.finally(() => {
					if (this.flights.get(id)?.controller === controller) {
						this.flights.delete(id);
						if (
							controller.signal.aborted &&
							this.enabled &&
							this.desired.has(id) &&
							!this.published.has(id) &&
							!this.cache.has(id) &&
							!this.queue.includes(id)
						) {
							this.queue.push(id);
						}
					}
					this.pump();
				});
		}
	}

	private publish(id: string, value: T) {
		if (this.published.has(id)) return;
		this.published.add(id);
		this.options.publish(id, value);
	}

	private unpublish(id: string) {
		if (!this.published.delete(id)) return;
		this.options.remove(id);
	}

	private removeAll() {
		for (const id of [...this.published]) this.unpublish(id);
	}

	private abortAll() {
		for (const flight of this.flights.values()) flight.controller.abort();
	}

	private touch(id: string): T | undefined {
		const value = this.cache.get(id);
		if (value === undefined) return undefined;
		this.cache.delete(id);
		this.cache.set(id, value);
		return value;
	}

	private put(id: string, value: T) {
		this.cache.delete(id);
		this.cache.set(id, value);
		while (this.cache.size > this.maxCache) {
			const oldest = this.cache.keys().next().value;
			if (oldest === undefined) break;
			this.cache.delete(oldest);
		}
	}
}

const parseChunkID = (
	config: GraphChunkConfig,
	id: string,
): [number, number, number] => {
	const match = /^(0|[1-9]\d*)\/(0|[1-9]\d*)\/(0|[1-9]\d*)$/.exec(id);
	if (!match) throw new Error(`invalid graph chunk id ${id}`);
	const values = match.slice(1).map(Number);
	const [z, x, y] = values;
	if (
		!values.every(Number.isSafeInteger) ||
		z !== config.zoom ||
		z < 0 ||
		z > 30
	) {
		throw new Error(`invalid graph chunk id ${id}`);
	}
	const count = 2 ** z;
	if (x < 0 || x >= count || y < 0 || y >= count)
		throw new Error(`invalid graph chunk id ${id}`);
	return [z, x, y];
};

const substitute = (template: string, token: string, value: string) => {
	if (template.split(token).length !== 2)
		throw new Error("invalid graph chunk URL template");
	return template.replace(token, value);
};

const hasUnsafeURLCharacter = (value: string) =>
	value.includes("\\") ||
	[...value].some((character) => {
		const code = character.charCodeAt(0);
		return code <= 31 || code === 127;
	});

export function graphChunkURL(config: GraphChunkConfig, id: string): string {
	if (
		!config.generation ||
		!config.url.startsWith("/") ||
		config.url.startsWith("//") ||
		hasUnsafeURLCharacter(config.url)
	) {
		throw new Error("invalid graph chunk config");
	}
	const [z, x, y] = parseChunkID(config, id);
	let url = substitute(
		config.url,
		"{generation}",
		encodeURIComponent(config.generation),
	);
	url = substitute(url, "{z}", String(z));
	url = substitute(url, "{x}", String(x));
	url = substitute(url, "{y}", String(y));
	if (url.includes("{") || url.includes("}"))
		throw new Error("invalid graph chunk URL template");
	return url;
}
