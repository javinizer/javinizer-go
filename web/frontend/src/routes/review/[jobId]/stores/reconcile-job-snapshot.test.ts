import { expect, it } from 'vitest';
import type { BatchJobResponse, FileResult } from '$lib/api/types';
import { acceptsResultSnapshot, reconcileJobSnapshot } from './reconcile-job-snapshot';
const report = { outcome: 'evidence_available', admitted_at: 'stored', snapshot_digest: 'A' };
const result = (revision?: number, evidence = false): FileResult =>
	({
		result_id: 'stable',
		file_path: '/movie',
		movie_id: 'movie',
		revision,
		movie: { id: 'movie', title: `Revision ${revision}` },
		...(evidence ? { offline_evidence: report } : {}),
	}) as FileResult;
const job = (value: FileResult): BatchJobResponse =>
	({ id: 'job', results: { '/movie': value } }) as unknown as BatchJobResponse;
it.each([false, true])('older snapshot cannot replace current evidence presence=%s', (present) => {
	const current = job(result(3, present));
	const incoming = job(result(2, !present));
	const before = structuredClone({ current, incoming });
	expect(reconcileJobSnapshot(current, incoming)?.results['/movie']).toBe(
		current.results['/movie'],
	);
	expect({ current, incoming }).toEqual(before);
});
it.each([false, true])('newer report replaces evidence presence=%s atomically', (present) => {
	const incoming = job(result(4, present));
	expect(reconcileJobSnapshot(job(result(3, !present)), incoming)).toEqual(incoming);
});
it.each([undefined, -1, NaN, 2.5])(
	'unknown/invalid revision %s cannot downgrade a known one',
	(revision) => {
		expect(acceptsResultSnapshot(result(3, true), result(revision))).toBe(false);
	},
);
it('different result identity cannot overwrite a same-path current record', () => {
	expect(acceptsResultSnapshot(result(3), { ...result(4, true), result_id: 'other' })).toBe(false);
});
it('full snapshots retain pre-existing result removal semantics', () => {
	const current = job(result(3, true));
	expect(reconcileJobSnapshot(current, { ...current, results: {} })?.results).toEqual({});
});
it('new results may be added while another stale result is retained', () => {
	const current = job(result(3));
	const added = { ...result(1, true), result_id: 'second' };
	const incoming = {
		...job(result(2, true)),
		results: { '/movie': result(2, true), '/second': added },
	};
	expect(reconcileJobSnapshot(current, incoming)?.results).toEqual({
		'/movie': current.results['/movie'],
		'/second': added,
	});
});
it('equal revisions and wholly historical revisionless payloads remain compatible', () => {
	expect(acceptsResultSnapshot(result(3), result(3))).toBe(true);
	expect(acceptsResultSnapshot(result(), result())).toBe(true);
	expect(acceptsResultSnapshot(result(), result(1))).toBe(true);
});
it('route clearing and new job identity are never reconciled across jobs', () => {
	const current = job(result(3, true));
	const incoming = { ...job(result(1)), id: 'other' };
	expect(reconcileJobSnapshot(current, incoming)).toBe(incoming);
	expect(reconcileJobSnapshot(null, incoming)).toBe(incoming);
	expect(reconcileJobSnapshot(current, null)).toBeNull();
});
