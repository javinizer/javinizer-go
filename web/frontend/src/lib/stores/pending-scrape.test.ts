import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { PendingScrape } from './pending-scrape.svelte';

// Every test here isolates module state via vi.resetModules() + a fresh
// dynamic import of the store graph — which now includes the paraglide
// message barrel (apply-plan.ts is localized). Each re-import re-loads ~2k
// compiled locale modules, so the stock 5s test timeout is insufficient;
// the timeout is NOT a correctness signal for these tests.
vi.setConfig({ testTimeout: 20_000 });

function makeSnapshot(overrides: Partial<PendingScrape> = {}): PendingScrape {
	return {
		version: 2,
		files: ['/a.mp4', '/b.mp4'],
		applyPlan: {
			version: 1,
			video_operation: 'organize',
			destination: '/out',
			nfo_output: 'write',
			media_policy: 'missing',
		},
		showScraperSelector: true,
		selectedScrapers: ['javdb', 'r18dev'],
		force: true,
		...overrides,
	};
}

async function loadStore() {
	vi.resetModules();
	return await import('./pending-scrape.svelte');
}

describe('buildPendingScrapeSnapshot', () => {
	it('normalizes a canonical version-2 plan', async () => {
		const s = await loadStore();
		const snap = s.buildPendingScrapeSnapshot({
			files: ['/a.mp4'],
			applyPlan: {
				version: 1,
				video_operation: 'organize',
				destination: ' /lib ',
				nfo_output: 'write',
				media_policy: 'missing',
			},
			showScraperSelector: false,
			selectedScrapers: [],
			force: false,
		});
		expect(snap).toMatchObject({
			version: 2,
			applyPlan: { video_operation: 'organize', destination: '/lib' },
		});
	});

	it('migrates compatible legacy update state', async () => {
		const s = await loadStore();
		const snap = s.buildPendingScrapeSnapshot({
			files: ['/a.mp4'],
			browseMode: 'update',
			update: true,
			effectiveOperationMode: 'organize',
			scalarStrategy: 'prefer-scraper',
			arrayStrategy: 'replace',
			showScraperSelector: false,
			selectedScrapers: [],
			force: false,
		});
		expect(snap.applyPlan).toMatchObject({
			video_operation: 'leave-in-place',
			merge: { scalar_strategy: 'prefer-scraper', array_strategy: 'replace' },
		});
	});

	it.each([
		{
			version: 1,
			video_operation: 'organize',
			destination: '',
			nfo_output: 'write',
			media_policy: 'missing',
		},
		{ version: 1, video_operation: 'rename-file', nfo_output: 'write', media_policy: 'replace' },
		{
			version: 1,
			video_operation: 'leave-in-place',
			nfo_output: 'skip',
			media_policy: 'skip',
			merge: { scalar_strategy: 'prefer-nfo', array_strategy: 'merge' },
		},
		{
			version: 1,
			video_operation: 'leave-in-place',
			nfo_output: 'write',
			media_policy: 'missing',
			merge: {
				scalar_strategy: 'prefer-nfo',
				array_strategy: 'merge',
				source_preset: 'aggressive',
			},
		},
	] as const)('clears an invalid version-2 persisted plan', async (applyPlan) => {
		const s = await loadStore();
		s.setPendingScrape({ ...makeSnapshot(), applyPlan: applyPlan as never });
		expect(s.getPendingScrape()?.applyPlan).toBeNull();
		expect(s.getPendingScrape()?.migrationWarning).toBeTruthy();
	});

	it('requires reselection for unsupported legacy operation modes', async () => {
		const s = await loadStore();
		const snap = s.buildPendingScrapeSnapshot({
			files: ['/a.mp4'],
			browseMode: 'scrape',
			effectiveOperationMode: 'preview',
			showScraperSelector: false,
			selectedScrapers: [],
			force: false,
		});
		expect(snap.applyPlan).toBeNull();
		expect(snap.migrationWarning).toBeTruthy();
	});
});

describe('pendingScrape store', () => {
	beforeEach(() => {
		if (typeof sessionStorage !== 'undefined') sessionStorage.clear();
		vi.resetModules();
	});

	it('sets, gets, and clears the canonical snapshot', async () => {
		const s = await loadStore();
		const snap = makeSnapshot();
		s.setPendingScrape(snap);
		expect(s.getPendingScrape()).toEqual(snap);
		s.clearPendingScrape();
		expect(s.getPendingScrape()).toBeNull();
	});

	it('survives refresh through sessionStorage', async () => {
		const before = await loadStore();
		before.setPendingScrape(makeSnapshot());
		const after = await loadStore();
		expect(after.getPendingScrape()).toEqual(makeSnapshot());
	});

	it('clear prevents refresh resurrection', async () => {
		const before = await loadStore();
		before.setPendingScrape(makeSnapshot());
		before.clearPendingScrape();
		const after = await loadStore();
		expect(after.getPendingScrape()).toBeNull();
	});

	it('preserves an empty scraper selection', async () => {
		const s = await loadStore();
		const snap = makeSnapshot({ showScraperSelector: false, selectedScrapers: [] });
		s.setPendingScrape(snap);
		expect(s.getPendingScrape()).toEqual(snap);
	});
});
