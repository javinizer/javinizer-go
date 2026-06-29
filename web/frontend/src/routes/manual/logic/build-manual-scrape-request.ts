import type { BatchScrapeRequest, OperationMode } from '$lib/api/types';

export type InputClass = 'auto' | 'manual-id' | 'manual-url';

export interface ManualRow {
	filePath: string;
	input: string;
}

export interface ManualScrapeOptions {
	destination?: string;
	operation_mode?: OperationMode;
	selected_scrapers?: string[];
	force?: boolean;
	preset?: 'conservative' | 'gap-fill' | 'aggressive';
	scalar_strategy?: BatchScrapeRequest['scalar_strategy'];
	array_strategy?: BatchScrapeRequest['array_strategy'];
	update?: boolean;
}

// classifyInput is a cosmetic hint for the tri-state badge (D5). It is NOT
// authoritative — backend matcher.ParseInput reclassifies on disagreement
// (badge says URL but no enabled scraper CanHandleURLs it → 400 via Phase 2 #2
// or ParseInput reclassifies). Empty/whitespace = Auto (matcher on basename);
// an http(s) URL = Manual-URL; anything else = Manual-ID.
export function classifyInput(input: string): InputClass {
	const trimmed = input.trim();
	if (trimmed === '') return 'auto';
	if (/^https?:\/\//i.test(trimmed)) return 'manual-url';
	return 'manual-id';
}

// buildManualScrapeRequest builds a BatchScrapeRequest for the /manual route.
// files[] and manual_inputs are built from the SAME rows[] in one pass, so
// manual_inputs keys are ⊆ files by construction (F7). Empty/whitespace inputs
// are dropped from manual_inputs; the map is omitted entirely when no row has
// a non-empty input (existing callers unaffected). Inherited opts pass through
// unchanged (#4). strict is always false (matches /browse's startBatchScrape).
// mergeManualInputs keeps manual inputs for files still in the batch and drops
// the rest (D4b: never blind-overwrite). New files (absent from stored) get no
// entry (= Auto); removed files' entries are dropped; empty/whitespace values
// are dropped so the map only carries real overrides. Callers display
// `stored[f] ?? ''` per row.
export function mergeManualInputs(
	stored: Record<string, string>,
	files: string[]
): Record<string, string> {
	const merged: Record<string, string> = {};
	for (const f of files) {
		const v = stored[f];
		if (v !== undefined && v.trim() !== '') {
			merged[f] = v;
		}
	}
	return merged;
}

export function buildManualScrapeRequest(
	rows: ManualRow[],
	opts: ManualScrapeOptions
): BatchScrapeRequest {
	const files: string[] = [];
	const manual_inputs: Record<string, string> = {};
	for (const row of rows) {
		files.push(row.filePath);
		const trimmed = row.input.trim();
		if (trimmed !== '') {
			manual_inputs[row.filePath] = trimmed;
		}
	}
	return {
		files,
		strict: false,
		force: opts.force ?? false,
		destination: opts.destination,
		update: opts.update,
		selected_scrapers: opts.selected_scrapers,
		preset: opts.preset,
		scalar_strategy: opts.scalar_strategy,
		array_strategy: opts.array_strategy,
		operation_mode: opts.operation_mode,
		manual_inputs: Object.keys(manual_inputs).length > 0 ? manual_inputs : undefined
	};
}
