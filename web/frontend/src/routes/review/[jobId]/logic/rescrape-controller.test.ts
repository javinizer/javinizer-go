import { describe, it, expect, vi } from 'vitest';
import { createRescrapeController } from './rescrape-controller';
import type { BatchJobResponse, BatchRescrapeResponse, FileResult, Movie } from '$lib/api/types';

const FILE_A = '/a/AAA-001.mp4';
const FILE_B = '/a/AAA-001-cd2.mp4'; // multi-part sibling of A (same movie_id)
const FILE_C = '/c/CCC-003.mp4';

function makeMovie(id: string, overrides: Partial<Movie> = {}): Movie {
	return {
		id,
		code: id,
		title: `${id} Title`,
		display_title: `${id} Title`,
		...overrides,
	};
}

function makeResult(resultId: string, filePath: string, movieId: string): FileResult {
	return {
		result_id: resultId,
		file_path: filePath,
		movie_id: movieId,
		status: 'completed',
		started_at: '2024-01-01T00:00:00Z',
		is_multi_part: filePath === FILE_B,
		part_number: 0,
		part_suffix: '',
		movie: makeMovie(movieId),
	};
}

function makeHarness() {
	const resultA = makeResult('res-A', FILE_A, 'AAA-001');
	const resultB = makeResult('res-A2', FILE_B, 'AAA-001');
	const resultC = makeResult('res-C', FILE_C, 'CCC-003');
	const job: { current: BatchJobResponse } = {
		current: {
			job_id: 'job-1',
			results: { [FILE_A]: resultA, [FILE_B]: resultB, [FILE_C]: resultC },
		} as unknown as BatchJobResponse,
	};
	const editedMovies = new Map<string, Movie>();
	const clearPosterCropStates = vi.fn((filePaths: string[]) => void filePaths);
	const invalidateJobQueries = vi.fn();
	const toastSuccess = vi.fn();
	const toastError = vi.fn();
	const setShowRescrapeModal = vi.fn();
	const rescrapeBatchMovie = vi.fn(async (): Promise<BatchRescrapeResponse> => {
		throw new Error('rescrapeBatchMovie not stubbed for this test');
	});

	const controller = createRescrapeController({
		getJobId: () => 'job-1',
		getCurrentResult: () => resultA,
		getJob: () => job.current,
		setJob: (next) => {
			job.current = next;
		},
		getEditedMovies: () => editedMovies,
		getAvailableScrapers: () => [],
		setAvailableScrapers: () => {},
		getRescrapeResultId: () => 'res-A',
		setRescrapeResultId: () => {},
		getSelectedScrapers: () => ['dmm'],
		setSelectedScrapers: () => {},
		getManualSearchMode: () => false,
		setManualSearchMode: () => {},
		getManualSearchInput: () => '',
		setManualSearchInput: () => {},
		setShowRescrapeModal,
		getRescrapePreset: () => undefined,
		setRescrapePreset: () => {},
		getRescrapeScalarStrategy: () => '',
		setRescrapeScalarStrategy: () => {},
		getRescrapeArrayStrategy: () => '',
		setRescrapeArrayStrategy: () => {},
		getRescrapingStates: () => new Map<string, boolean>(),
		clearPosterCropStates,
		invalidateJobQueries,
		toastSuccess,
		toastError,
		api: {
			getScrapers: async () => [],
			rescrapeBatchMovie,
		},
	});

	return {
		controller,
		job,
		editedMovies,
		clearPosterCropStates,
		invalidateJobQueries,
		toastSuccess,
		toastError,
		setShowRescrapeModal,
		rescrapeBatchMovie,
	};
}

describe('executeRescrape — bulk-contract parity (crop invalidation, rekey, refetch)', () => {
	it('null bounds in the response drop the movie’s local crop geometry (ALL parts), propagate the rekeyed movie_id, and refetch the job', async () => {
		// Regression: the single-rescrape path mirrored none of
		// bulkRescrapeMutation's contract — a rescrape that switched the
		// effective poster source left stale crop geometry (measured against
		// the old image) on every part of the movie, left the result hanging
		// on the OLD movie_id after a rekey, and never re-fetched the job.
		const h = makeHarness();
		h.rescrapeBatchMovie.mockResolvedValue({
			movie: makeMovie('AAA-009'), // rekeyed; poster_crop_bounds absent = cleared
		});

		await h.controller.executeRescrape();

		expect(h.clearPosterCropStates).toHaveBeenCalledTimes(1);
		const cleared = [...(h.clearPosterCropStates.mock.calls[0][0] as string[])].sort();
		expect(cleared).toEqual([FILE_A, FILE_B].sort());

		const updated = h.job.current.results![FILE_A] as FileResult;
		expect(updated.movie_id).toBe('AAA-009');
		expect(updated.movie?.id).toBe('AAA-009');
		// Sibling untouched by the synthetic result update (server fanout
		// lands via the refetch below).
		expect((h.job.current.results![FILE_B] as FileResult).movie_id).toBe('AAA-001');

		expect(h.invalidateJobQueries).toHaveBeenCalledTimes(1);
		expect(h.toastSuccess).toHaveBeenCalled();
		expect(h.setShowRescrapeModal).toHaveBeenCalledWith(false);
	});

	it('bounds the server KEPT keep the local crop geometry', async () => {
		const h = makeHarness();
		h.rescrapeBatchMovie.mockResolvedValue({
			movie: makeMovie('AAA-001', {
				poster_crop_bounds: { x: 1, y: 2, width: 3, height: 4 },
			}),
		});

		await h.controller.executeRescrape();

		expect(h.clearPosterCropStates).not.toHaveBeenCalled();
		expect(h.invalidateJobQueries).toHaveBeenCalledTimes(1);
	});

	it('invalidates the job queries on failure (mirrors bulk: a failed rescrape may still have applied partial state)', async () => {
		const h = makeHarness();
		h.rescrapeBatchMovie.mockRejectedValue(new Error('boom'));

		await h.controller.executeRescrape();

		expect(h.toastError).toHaveBeenCalled();
		expect(h.invalidateJobQueries).toHaveBeenCalledTimes(1);
	});
});
describe('executeRescrape — sibling pending-edit reconciliation (P1)', () => {
	it('overlays the server poster state onto same-family sibling pending edits while preserving their unrelated edits', async () => {
		// The backend I7 fan-out mirrors the rescraped movie's poster state
		// onto every same-ID sibling (rescrape_phase.go), but the response
		// only carries the rescraped part. A pending edit on sibling B would
		// resubmit its stale poster group (and Original* baseline) through the
		// next whole-movie Save, overwriting the server fan-out.
		const h = makeHarness();
		h.editedMovies.set(FILE_A, makeMovie('AAA-001', { title: 'A pending (dropped wholesale)' }));
		h.editedMovies.set(
			FILE_B,
			makeMovie('AAA-001', {
				title: 'B pending title',
				poster_url: 'https://old/stale-poster.jpg',
				cover_url: 'https://old/stale-cover.jpg',
				cropped_poster_url: '/api/v1/temp/posters/job-1/AAA-001.jpg?v=old',
				should_crop_poster: true,
				poster_crop_bounds: { x: 1, y: 2, width: 3, height: 4 },
				original_poster_url: 'https://older/orig-p.jpg',
			}),
		);
		// Unrelated movie: must be untouched.
		h.editedMovies.set(FILE_C, makeMovie('CCC-003', { title: 'C pending' }));

		h.rescrapeBatchMovie.mockResolvedValue({
			movie: makeMovie('AAA-001', {
				poster_url: 'https://new/poster.jpg',
				cover_url: 'https://new/cover.jpg',
				cropped_poster_url: '/api/v1/temp/posters/job-1/AAA-001.jpg?v=9',
				should_crop_poster: false,
				// poster_crop_bounds absent = the server cleared the crop
				original_poster_url: 'https://orig/p.jpg',
				original_cropped_poster_url: '/api/v1/temp/posters/job-1/AAA-001.jpg?v=1',
				original_should_crop_poster: false,
				original_cover_url: 'https://orig/c.jpg',
			}),
		});

		await h.controller.executeRescrape();

		// Existing behavior: the rescraped part's own pending edit is dropped.
		expect(h.editedMovies.has(FILE_A)).toBe(false);

		const sibling = h.editedMovies.get(FILE_B);
		expect(sibling, 'sibling pending edit must survive').toBeDefined();
		// Unrelated edited field preserved.
		expect(sibling?.title).toBe('B pending title');
		// Poster group reconciled to the server state (incl. Original* baseline).
		expect(sibling?.poster_url).toBe('https://new/poster.jpg');
		expect(sibling?.cover_url).toBe('https://new/cover.jpg');
		expect(sibling?.cropped_poster_url).toBe('/api/v1/temp/posters/job-1/AAA-001.jpg?v=9');
		expect(sibling?.should_crop_poster).toBe(false);
		expect(sibling?.poster_crop_bounds).toBeNull();
		expect(sibling?.original_poster_url).toBe('https://orig/p.jpg');
		expect(sibling?.original_cropped_poster_url).toBe('/api/v1/temp/posters/job-1/AAA-001.jpg?v=1');
		expect(sibling?.original_should_crop_poster).toBe(false);
		expect(sibling?.original_cover_url).toBe('https://orig/c.jpg');

		// Unrelated movie untouched.
		expect(h.editedMovies.get(FILE_C)?.title).toBe('C pending');
	});
});
