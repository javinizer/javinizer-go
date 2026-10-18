import { describe, expect, it } from 'vitest';
import type { FileResult } from '$lib/api/types';
import { isCreditBlockedResult } from './review-state.svelte';

function legacyResult(): FileResult {
	return {
		result_id: 'legacy-credit-result',
		file_path: '/synthetic/EBWH-385.mkv',
		movie_id: 'EBWH-385',
		status: 'failed',
		error_code: 'blocked',
		error: 'apply blocked by an open credit collision',
		movie: { id: 'EBWH-385', code: 'ebwh00385' },
		started_at: '2026-10-18T12:00:00Z',
		ended_at: '2026-10-18T12:01:00Z',
		is_multi_part: false,
		part_number: 0,
		part_suffix: ''
	} as FileResult;
}

describe('legacy Apply credit diagnostics', () => {
	it.each([
		'apply blocked by an open credit collision',
		'apply blocked because credit collision status is unavailable'
	])('retains terminal movie review for %s without rewriting the result', (error) => {
		const result = { ...legacyResult(), error };
		const before = structuredClone(result);
		expect(isCreditBlockedResult(result)).toBe(true);
		expect(result).toEqual(before);
	});

	it.each([
		['unrelated blocker', (r: FileResult) => { r.error = 'provider request blocked'; }],
		['prefixed message', (r: FileResult) => { r.error = `provider: ${r.error}`; }],
		['suffixed message', (r: FileResult) => { r.error += ': provider detail'; }],
		['missing metadata', (r: FileResult) => { r.movie = undefined; }],
		['missing movie identity', (r: FileResult) => { r.movie!.code = ''; }],
		['missing file provenance', (r: FileResult) => { r.file_path = ''; }],
		['missing terminal time', (r: FileResult) => { r.ended_at = undefined; }],
		['nonfailed result', (r: FileResult) => { r.status = 'completed'; }],
		['different code', (r: FileResult) => { r.error_code = 'not_found'; }]
	] as const)('keeps %s outside credit recovery', (_name, change) => {
		const result = legacyResult();
		change(result);
		expect(isCreditBlockedResult(result)).toBe(false);
	});

	it.each(['credit_conflict', 'credit_status_unavailable'] as const)(
		'preserves existing %s handling', (error_code) => {
			const result = legacyResult();
			result.error_code = error_code;
			result.ended_at = undefined;
			expect(isCreditBlockedResult(result)).toBe(true);
		}
	);
});
