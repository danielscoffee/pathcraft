import { describe, expect, it } from "vitest";
import type { RouteMode } from "../api/types";
import { solveModesProgressively, type ModeResult } from "./useRouting";

const modes: RouteMode[] = [
	{ id: "walk", label: "Walk" },
	{ id: "space", label: "Space", dimensions: [3] },
];

describe("solveModesProgressively", () => {
	it("publishes a fast mode before slower modes finish", async () => {
		const resolvers: Record<string, (result: ModeResult) => void> = {};
		const published: string[] = [];
		let finished = false;

		const solving = solveModesProgressively(
			modes,
			(mode) =>
				new Promise<ModeResult>((resolve) => {
					resolvers[mode.id] = resolve;
				}),
			(id) => published.push(id),
		).then(() => {
			finished = true;
		});

		resolvers.walk({ ok: true, solveMs: 2 });
		await Promise.resolve();

		expect(published).toEqual(["walk"]);
		expect(finished).toBe(false);

		resolvers.space({ ok: true, solveMs: 9_800 });
		await solving;

		expect(published).toEqual(["walk", "space"]);
		expect(finished).toBe(true);
	});
});
