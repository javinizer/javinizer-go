import type { OperationMode } from '$lib/api/types';

export type BrowseMode = 'scrape' | 'update';

export type ScalarStrategy = 'prefer-nfo' | 'prefer-scraper' | 'preserve-existing' | 'fill-missing-only';
export type ArrayStrategy = 'merge' | 'replace';

// PendingScrape is the D4 snapshot carried from /browse to /manual: the
// inherited globals needed to rebuild the BatchScrapeRequest. It carries the
// DERIVED effectiveOperationMode + isInPlaceImplied (not the raw override) and
// both browseMode + update (SageEagle must-fix #4: one ambiguous operationMode
// can't reconstruct update:true, which would drop preset/strategies on /manual).
// selectedScrapers is carried but only honored when showScraperSelector is true.
export interface PendingScrape {
	files: string[];
	browseMode: BrowseMode;
	update: boolean;
	effectiveOperationMode: OperationMode;
	isInPlaceImplied: boolean;
	showScraperSelector: boolean;
	destination: string;
	selectedScrapers: string[];
	force: boolean;
	preset?: 'conservative' | 'gap-fill' | 'aggressive';
	scalarStrategy?: ScalarStrategy;
	arrayStrategy?: ArrayStrategy;
}

const STORAGE_KEY = 'javinizer_pending_scrape';

// Module singleton (mirrors background-job.svelte.ts). On a page refresh the
// module re-evaluates and state resets to null; getPendingScrape then hydrates
// from sessionStorage so the /manual tracer survives the refresh.
let state: PendingScrape | null = $state(null);

function hydrate(): PendingScrape | null {
	if (typeof sessionStorage === 'undefined') return null;
	const raw = sessionStorage.getItem(STORAGE_KEY);
	if (!raw) return null;
	try {
		return JSON.parse(raw) as PendingScrape;
	} catch {
		sessionStorage.removeItem(STORAGE_KEY);
		return null;
	}
}

// buildPendingScrapeSnapshot derives the D4 snapshot from /browse state for
// setPendingScrape before goto('/manual'). update is ALWAYS derived from
// browseMode (SageEagle must-fix #4: one ambiguous operationMode can't
// reconstruct update:true, which would drop preset/strategies on /manual),
// so the caller never passes it.
export function buildPendingScrapeSnapshot(
	input: Omit<PendingScrape, 'update'>
): PendingScrape {
	return { ...input, update: input.browseMode === 'update' };
}

export function getPendingScrape(): PendingScrape | null {
	if (state === null) {
		state = hydrate();
	}
	return state;
}

export function setPendingScrape(snapshot: PendingScrape): void {
	state = snapshot;
	if (typeof sessionStorage !== 'undefined') {
		sessionStorage.setItem(STORAGE_KEY, JSON.stringify(snapshot));
	}
}

export function clearPendingScrape(): void {
	state = null;
	if (typeof sessionStorage !== 'undefined') {
		sessionStorage.removeItem(STORAGE_KEY);
	}
}
