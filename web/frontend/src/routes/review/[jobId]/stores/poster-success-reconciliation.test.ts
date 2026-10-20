import { describe, expect, it, vi } from 'vitest';
import type { BatchJobResponse, Movie } from '$lib/api/types';
import { reconcilePosterSuccess } from './poster-success-reconciliation';

describe('reconcilePosterSuccess', () => {
	it('returns immutable job and existing-overlay updates without changing either input', () => {
		const serverMovie = Object.freeze({ id: 'MOVIE-1', title: 'Server title' } as Movie);
		const editedMovie = Object.freeze({ ...serverMovie, title: 'Unsaved title' });
		const unrelated = Object.freeze({ id: 'OTHER-1', title: 'Unrelated' } as Movie);
		const target = Object.freeze({
			result_id: 'target',
			movie_id: 'MOVIE-1',
			revision: 1,
			movie: serverMovie,
		});
		const sibling = Object.freeze({
			result_id: 'sibling',
			movie_id: 'movie-1',
			revision: 2,
			movie: serverMovie,
		});
		const other = Object.freeze({
			result_id: 'other',
			movie_id: 'OTHER-1',
			revision: 3,
			movie: unrelated,
		});
		const job = Object.freeze({
			id: 'job-a',
			results: Object.freeze({ target, sibling, other }),
		}) as unknown as BatchJobResponse;
		const edits = new Map([
			['target', editedMovie],
			['other', unrelated],
		]);
		const set = vi.spyOn(edits, 'set');
		const transform = vi.fn((movie: Movie): Movie => ({ ...movie, poster_url: 'new-poster.jpg' }));

		const reconciled = reconcilePosterSuccess(
			job,
			edits,
			'target',
			{
				revision: 999,
				revisions: { target: 11, sibling: 12 },
			},
			transform,
		);

		expect(set).not.toHaveBeenCalled();
		expect(edits).toEqual(
			new Map([
				['target', editedMovie],
				['other', unrelated],
			]),
		);
		expect(reconciled.editedMovieUpdates).toEqual(
			new Map([['target', { ...editedMovie, poster_url: 'new-poster.jpg' }]]),
		);
		expect(reconciled.editedMovieUpdates.get('target')).not.toBe(editedMovie);
		expect(reconciled.job).not.toBe(job);
		expect(reconciled.job.results).not.toBe(job.results);
		expect(reconciled.job.results.target).toEqual({
			...target,
			revision: 11,
			movie: { ...serverMovie, poster_url: 'new-poster.jpg' },
		});
		expect(reconciled.job.results.sibling).toEqual({
			...sibling,
			revision: 12,
			movie: { ...serverMovie, poster_url: 'new-poster.jpg' },
		});
		expect(reconciled.job.results.other).toBe(other);
		expect(transform.mock.calls).toEqual([[serverMovie], [serverMovie], [editedMovie]]);
	});
});
