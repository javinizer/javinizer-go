import { describe, expect, it } from 'vitest';
import type { BatchJobResponse, FileResult } from '$lib/api/types';
import { getCreditFailureKind, getCreditGuidance } from './credit-guidance';

const path = '/synthetic/movie-part2.mp4';
function result(overrides: Partial<FileResult> = {}): FileResult {
	return {
		file_path: path,
		movie_id: 'movie',
		result_id: 'part2',
		status: 'failed',
		error_code: 'credit_conflict',
		error: 'Credit collision',
		ended_at: '2026-10-21T12:00:00Z',
		movie: { id: 'movie', code: 'MOVIE-001', title: 'Movie' },
		...overrides,
	} as FileResult;
}
function context(row: FileResult) {
	return {
		job: { results: { [path]: row }, excluded: {} } as unknown as BatchJobResponse,
		groups: [{ primaryResult: row, results: [row] }],
	};
}

describe('credit guidance classification', () => {
	it('does not mistake generic or incomplete legacy failures for credit conflicts', () => {
		expect(getCreditFailureKind(result({ error_code: undefined }))).toBeNull();
		expect(getCreditFailureKind(result({ status: 'completed' }))).toBeNull();
		expect(getCreditFailureKind(result({ movie: undefined }))).toBeNull();
		expect(
			getCreditFailureKind(result({ error_code: 'blocked', error: 'A credit collision happened' })),
		).toBeNull();
		expect(
			getCreditFailureKind(
				result({
					error_code: 'blocked',
					error: 'apply blocked by an open credit collision',
					ended_at: undefined,
				}),
			),
		).toBeNull();
	});

	it.each([
		['apply blocked by an open credit collision', 'conflict'],
		['apply blocked because credit collision status is unavailable', 'unavailable'],
	] as const)('recognizes the exact historical diagnostic %s', (error, kind) => {
		expect(getCreditFailureKind(result({ error_code: 'blocked', error }))).toBe(kind);
	});

	it('keeps the failed attempt classification instead of borrowing newer state', () => {
		const { job, groups } = context(
			result({ error_code: 'credit_status_unavailable', error: 'New error' }),
		);
		expect(
			getCreditGuidance(path, { status: 'failed', error: 'Old error' }, job, groups),
		).toBeNull();
		expect(
			getCreditGuidance(
				path,
				{ status: 'failed', error: 'Old error', error_code: 'credit_conflict' },
				job,
				groups,
			),
		).toEqual({ kind: 'conflict', movieIndex: 0 });
		expect(
			getCreditGuidance(path, { status: 'success', error_code: 'credit_conflict' }, job, groups),
		).toBeNull();
	});

	it('retains the typed historical failure after the current result succeeds', () => {
		const { job, groups } = context(
			result({ status: 'completed', error: undefined, error_code: undefined }),
		);
		expect(
			getCreditGuidance(path, { status: 'failed', error_code: 'credit_conflict' }, job, groups),
		).toEqual({ kind: 'conflict', movieIndex: 0 });
	});

	it('retains useful explanation without an unsafe navigation target', () => {
		const { job, groups } = context(result());
		const status = { status: 'failed', error_code: 'credit_conflict' as const };
		expect(getCreditGuidance(path, status, null, [])).toEqual({
			kind: 'conflict',
			movieIndex: null,
		});
		expect(getCreditGuidance(path, status, job, [])).toEqual({
			kind: 'conflict',
			movieIndex: null,
		});
		job.excluded = { [path]: true };
		expect(getCreditGuidance(path, status, job, groups)).toEqual({
			kind: 'conflict',
			movieIndex: null,
		});
	});
});

it.each([{ movie: { ...result().movie!, code: '' } }, { file_path: '' }, { ended_at: undefined }])(
	'requires all legacy terminal evidence (%j)',
	(missing) => {
		expect(
			getCreditFailureKind(
				result({
					error_code: 'blocked',
					error: 'apply blocked by an open credit collision',
					...missing,
				}),
			),
		).toBeNull();
	},
);

it('classifies typed unavailability and matching untyped terminal snapshots', () => {
	const row = result({ error_code: 'credit_status_unavailable' });
	const { job, groups } = context(row);
	expect(getCreditFailureKind(row)).toBe('unavailable');
	expect(
		getCreditGuidance(
			path,
			{ status: 'failed', error_code: 'credit_status_unavailable' },
			job,
			groups,
		),
	).toEqual({ kind: 'unavailable', movieIndex: 0 });
	expect(getCreditGuidance(path, { status: 'failed', error: row.error }, job, groups)).toEqual({
		kind: 'unavailable',
		movieIndex: 0,
	});
});

it('never navigates stale groups without a current job or file', () => {
	const { job, groups } = context(result());
	const status = { status: 'failed', error_code: 'credit_conflict' as const };
	expect(getCreditGuidance(path, status, null, groups)).toEqual({
		kind: 'conflict',
		movieIndex: null,
	});
	delete job.results[path];
	expect(getCreditGuidance(path, status, job, groups)).toEqual({
		kind: 'conflict',
		movieIndex: null,
	});
});

it.each([undefined, { ...result().movie!, code: '' }])(
	'does not navigate a group without a usable movie code (%j)',
	(movie) => {
		const { job, groups } = context(result({ movie }));
		expect(
			getCreditGuidance(path, { status: 'failed', error_code: 'credit_conflict' }, job, groups),
		).toEqual({ kind: 'conflict', movieIndex: null });
	},
);
