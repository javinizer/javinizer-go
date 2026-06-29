import { describe, it, expect, beforeEach, vi } from 'vitest';
import type { PendingScrape } from './pending-scrape.svelte';

function makeSnapshot(overrides: Partial<PendingScrape> = {}): PendingScrape {
	return {
		files: ['/a.mp4', '/b.mp4'],
		browseMode: 'scrape',
		update: false,
		effectiveOperationMode: 'organize',
		isInPlaceImplied: false,
		showScraperSelector: true,
		destination: '/out',
		selectedScrapers: ['javdb', 'r18dev'],
		force: true,
		preset: 'gap-fill',
		scalarStrategy: 'prefer-scraper',
		arrayStrategy: 'replace',
		...overrides
	};
}

// loadStore returns a FRESH module instance (state re-inits to null) so a test
// can simulate a page refresh by calling it twice: the first sets + serializes,
// the second hydrates from sessionStorage.
async function loadStore() {
	vi.resetModules();
	return await import('./pending-scrape.svelte');
}

describe('buildPendingScrapeSnapshot', () => {
	it("derives update=true from browseMode 'update' (SageEagle must-fix #4)", async () => {
		const s = await loadStore();
		const snap = s.buildPendingScrapeSnapshot({
			files: ['/a.mp4'],
			browseMode: 'update',
			effectiveOperationMode: 'in-place',
			isInPlaceImplied: false,
			showScraperSelector: false,
			destination: '/out',
			selectedScrapers: [],
			force: false,
			preset: 'gap-fill',
			scalarStrategy: 'prefer-scraper',
			arrayStrategy: 'replace'
		});
		expect(snap.update).toBe(true);
		expect(snap.browseMode).toBe('update');
	});

	it("derives update=false from browseMode 'scrape'", async () => {
		const s = await loadStore();
		const snap = s.buildPendingScrapeSnapshot({
			files: ['/a.mp4'],
			browseMode: 'scrape',
			effectiveOperationMode: 'organize',
			isInPlaceImplied: false,
			showScraperSelector: true,
			destination: '/out',
			selectedScrapers: ['javdb'],
			force: true
		});
		expect(snap.update).toBe(false);
		expect(snap.preset).toBeUndefined();
	});

	it('round-trips through the store (set snapshot → get)', async () => {
		const s = await loadStore();
		const snap = s.buildPendingScrapeSnapshot({
			files: ['/a.mp4', '/b.mp4'],
			browseMode: 'scrape',
			effectiveOperationMode: 'organize',
			isInPlaceImplied: true,
			showScraperSelector: false,
			destination: '/lib',
			selectedScrapers: [],
			force: false
		});
		s.setPendingScrape(snap);
		expect(s.getPendingScrape()).toEqual(snap);
	});
});

describe('pendingScrape store', () => {
	beforeEach(() => {
		if (typeof sessionStorage !== 'undefined') sessionStorage.clear();
		vi.resetModules();
	});

	it('set → get returns the snapshot; clear → null (#5)', async () => {
		const s = await loadStore();
		const snap = makeSnapshot();
		s.setPendingScrape(snap);
		expect(s.getPendingScrape()).toEqual(snap);
		s.clearPendingScrape();
		expect(s.getPendingScrape()).toBeNull();
	});

	it('survives a simulated refresh (serialize on set / hydrate on get from sessionStorage) (#6)', async () => {
		const before = await loadStore();
		before.setPendingScrape(makeSnapshot());
		// Simulate refresh: fresh module instance, in-memory state resets to
		// null, sessionStorage retains the serialized snapshot.
		const after = await loadStore();
		expect(after.getPendingScrape()).toEqual(makeSnapshot());
	});

	it('clear removes the sessionStorage entry so a refresh does not resurrect (#6b)', async () => {
		const before = await loadStore();
		before.setPendingScrape(makeSnapshot());
		before.clearPendingScrape();
		const after = await loadStore();
		expect(after.getPendingScrape()).toBeNull();
	});

	it('round-trips the full D4 type (#7)', async () => {
		const s = await loadStore();
		const snap = makeSnapshot({
			browseMode: 'update',
			update: true,
			effectiveOperationMode: 'in-place-norenamefolder',
			isInPlaceImplied: true,
			destination: '/library'
		});
		s.setPendingScrape(snap);
		expect(s.getPendingScrape()).toEqual(snap);
	});

	it('omits selectedScrapers cleanly when showScraperSelector is false', async () => {
		const s = await loadStore();
		const snap = makeSnapshot({ showScraperSelector: false, selectedScrapers: [] });
		s.setPendingScrape(snap);
		expect(s.getPendingScrape()).toEqual(snap);
	});
});
