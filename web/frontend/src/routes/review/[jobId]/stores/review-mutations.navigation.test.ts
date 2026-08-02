import { describe, it, expect, vi, beforeEach } from 'vitest';
import type {
	BatchJobResponse,
	FileResult,
	Movie,
	PosterCropResponse,
	PosterFromURLResponse,
} from '$lib/api/types';
import type { PosterCropBox, PosterCropMetrics, PosterCropState, PosterPreviewOverride } from '../review-utils';

interface MutationOptions {
	mutationFn: (vars: unknown) => Promise<unknown>;
	onSuccess?: (data: unknown, vars: unknown, context: unknown) => unknown;
	onError?: (err: unknown, vars: unknown, context: unknown) => unknown;
}

const createdMutations: MutationOptions[] = [];

// createReviewMutations is a .svelte.ts module meant to run under Svelte
// component context; @tanstack/svelte-query's createMutation is mocked here
// with a minimal harness that executes mutationFn and dispatches onSuccess /
// onError exactly once per call, so onSuccess wiring can be regression-tested
// with plain deps (mirroring the newest store tests' style: pure deps, no UI).
vi.mock('@tanstack/svelte-query', () => ({
	createMutation: (optionsFn: () => MutationOptions) => {
		const options = optionsFn();
		createdMutations.push(options);
		return {
			isPending: false,
			mutate: (vars: unknown) => {
				void Promise.resolve()
					.then(() => options.mutationFn(vars))
					.then((data) => options.onSuccess?.(data, vars, undefined))
					.catch((err) => options.onError?.(err, vars, undefined));
			},
			mutateAsync: async (vars: unknown) => {
				const data = await options.mutationFn(vars);
				await options.onSuccess?.(data, vars, undefined);
				return data;
			},
		};
	},
}));

import { createReviewMutations } from './review-mutations.svelte';

function deferred<T>() {
	let resolve!: (value: T) => void;
	let reject!: (err: unknown) => void;
	const promise = new Promise<T>((res, rej) => {
		resolve = res;
		reject = rej;
	});
	return { promise, resolve, reject };
}

const FILE_A = '/a/AAA-001.mp4';
const FILE_B = '/b/BBB-002.mp4';

function makeMovie(id: string): Movie {
	return {
		id,
		code: id,
		title: `${id} Title`,
		display_title: `${id} Title`,
	};
}

function makeResult(resultId: string, filePath: string, movieId: string): FileResult {
	return {
		result_id: resultId,
		file_path: filePath,
		movie_id: movieId,
		status: 'completed',
		started_at: '2024-01-01T00:00:00Z',
		is_multi_part: false,
		part_number: 0,
		part_suffix: '',
		movie: makeMovie(movieId),
	};
}

interface Harness {
	mutations: ReturnType<typeof createReviewMutations>;
	posterPreviewOverrides: Map<string, PosterPreviewOverride>;
	posterCropStates: Map<string, PosterCropState>;
	editedMovies: Map<string, Movie>;
	skipJobSync: ReturnType<typeof vi.fn>;
	job: { current: BatchJobResponse };
	// Flipping this simulates the user navigating to another movie mid-flight.
	currentResult: { current: FileResult };
	toastSuccess: ReturnType<typeof vi.fn>;
	toastError: ReturnType<typeof vi.fn>;
	setShowPosterCropModal: ReturnType<typeof vi.fn>;
	api: {
		updateBatchMoviePosterFromURL: ReturnType<typeof vi.fn>;
		updateBatchMoviePosterCrop: ReturnType<typeof vi.fn>;
	};
}

function makeHarness(): Harness {
	const resultA = makeResult('res-A', FILE_A, 'AAA-001');
	const resultB = makeResult('res-B', FILE_B, 'BBB-002');
	const job: { current: BatchJobResponse } = {
		current: {
			job_id: 'job-1',
			results: { [FILE_A]: resultA, [FILE_B]: resultB },
		} as unknown as BatchJobResponse,
	};
	const currentResult = { current: resultA };

	const posterPreviewOverrides = new Map<string, PosterPreviewOverride>();
	const posterCropStates = new Map<string, PosterCropState>();
	const editedMovies = new Map<string, Movie>();
	const skipJobSync = vi.fn();
	const toastSuccess = vi.fn();
	const toastError = vi.fn();
	const setShowPosterCropModal = vi.fn();
	const updateBatchMoviePosterFromURL = vi.fn(
		async (
			_jobId: string,
			_resultId: string,
			_body: { url: string },
		): Promise<PosterFromURLResponse> => {
			throw new Error('updateBatchMoviePosterFromURL not stubbed for this test');
		},
	);
	const updateBatchMoviePosterCrop = vi.fn(
		async (
			_jobId: string,
			_resultId: string,
			_crop: PosterCropBox,
			_maxPosterHeight?: number,
		): Promise<PosterCropResponse> => {
			throw new Error('updateBatchMoviePosterCrop not stubbed for this test');
		},
	);
	const api = { updateBatchMoviePosterFromURL, updateBatchMoviePosterCrop };

	const cropMetrics: PosterCropMetrics = {
		sourceWidth: 400,
		sourceHeight: 600,
		displayWidth: 200,
		displayHeight: 300,
		imageOffsetX: 0,
		imageOffsetY: 0,
	};
	const cropBox: PosterCropBox = { x: 10, y: 20, width: 300, height: 450 };

	const noop = () => {};
	const mutations = createReviewMutations({
		getJobId: () => 'job-1',
		getJob: () => job.current,
		setJob: (next) => {
			job.current = next;
		},
		skipJobSync,
		clearEditStorage: noop,
		clearEditedMovies: noop,
		clearPosterPreviewOverrides: noop,
		getEditedMovies: () => editedMovies,
		getCurrentResult: () => currentResult.current,
		getPosterPreviewOverrides: () => posterPreviewOverrides,
		getPosterCropStates: () => posterCropStates,
		getCropMetrics: () => cropMetrics,
		getCropBox: () => cropBox,
		getQueryClient: () => ({ invalidateQueries: vi.fn(async () => {}) }) as never,
		getCurrentMovieIndex: () => 0,
		setCurrentMovieIndex: noop,
		getMovieResultsLength: () => 2,
		gotoJobs: async () => {},
		setShowPosterCropModal,
		updateBatchMoviePosterFromURL: api.updateBatchMoviePosterFromURL,
		getBatchMovieSources: vi.fn(),
		overrideBatchMovieField: vi.fn(),
		excludeBatchMovie: vi.fn(),
		updateBatchMovie: vi.fn(),
		updateBatchMoviePosterCrop: api.updateBatchMoviePosterCrop,
		batchExcludeMovies: vi.fn(),
		bulkRescrapeMovies: vi.fn(),
		getSelectedMovieIds: () => new Set<string>(),
		clearSelectedMovieIds: noop,
		deleteSelectedMovieId: noop,
		toastSuccess,
		toastError,
	});

	return {
		mutations,
		posterPreviewOverrides,
		posterCropStates,
		editedMovies,
		skipJobSync,
		job,
		currentResult,
		toastSuccess,
		toastError,
		setShowPosterCropModal,
		api,
	};
}

describe('review-mutations — mid-flight navigation keys poster state by the REQUEST resultId', () => {
	beforeEach(() => {
		createdMutations.length = 0;
	});

	it('posterFromUrlMutation: posterPreviewOverride lands under the request target even after navigating away', async () => {
		const h = makeHarness();
		const pending = deferred<PosterFromURLResponse>();
		h.api.updateBatchMoviePosterFromURL.mockImplementation(() => pending.promise);

		// Trigger the mutation against result A while A is current...
		const done = h.mutations.applyPosterFromUrlAsync('res-A', 'https://example.com/new-poster.jpg');
		expect(h.api.updateBatchMoviePosterFromURL).toHaveBeenCalledWith('job-1', 'res-A', {
			url: 'https://example.com/new-poster.jpg',
		});

		// ...the user navigates to movie B while the request is in flight...
		h.currentResult.current = h.job.current.results![FILE_B] as FileResult;

		// ...then the response arrives.
		pending.resolve({
			poster_url: 'https://example.com/new-poster.jpg',
			cropped_poster_url: '/api/v1/temp/posters/job-1/AAA-001.jpg',
			should_crop_poster: false,
		});
		await done;

		// The preview override is keyed to the REQUEST target (A), not the
		// movie the user navigated to (B).
		expect(h.posterPreviewOverrides.get(FILE_A)).toMatchObject({
			url: '/api/v1/temp/posters/job-1/AAA-001.jpg',
		});
		expect(h.posterPreviewOverrides.has(FILE_B)).toBe(false);

		// The job-state overlay likewise landed on A (it always keyed off
		// resultId — pinned here for parity).
		const movieA = (h.job.current.results![FILE_A] as FileResult).movie!;
		expect(movieA.poster_url).toBe('https://example.com/new-poster.jpg');
		expect(movieA.cropped_poster_url).toBe('/api/v1/temp/posters/job-1/AAA-001.jpg');
	});

	it('posterCropMutation: preview override AND crop state land under the request target even after navigating away', async () => {
		const h = makeHarness();
		const pending = deferred<PosterCropResponse>();
		h.api.updateBatchMoviePosterCrop.mockImplementation(() => pending.promise);

		const crop: PosterCropBox = { x: 10, y: 20, width: 300, height: 450 };
		const done = h.mutations.applyPosterCropAsync('job-1', 'res-A', crop, undefined);
		expect(h.api.updateBatchMoviePosterCrop).toHaveBeenCalledWith('job-1', 'res-A', crop, undefined);

		// Navigate to movie B mid-flight.
		h.currentResult.current = h.job.current.results![FILE_B] as FileResult;

		pending.resolve({
			cropped_poster_url: '/api/v1/temp/posters/job-1/AAA-001.jpg',
			poster_crop_bounds: { x: 10, y: 20, width: 300, height: 450 },
		});
		await done;

		// Both maps key off the request target (A), not the current movie (B).
		expect(h.posterPreviewOverrides.get(FILE_A)).toMatchObject({
			url: '/api/v1/temp/posters/job-1/AAA-001.jpg',
		});
		expect(h.posterPreviewOverrides.has(FILE_B)).toBe(false);

		expect(h.posterCropStates.get(FILE_A)).toMatchObject({
			xRatio: 10 / 400,
			yRatio: 20 / 600,
			widthRatio: 300 / 400,
			heightRatio: 450 / 600,
		});
		expect(h.posterCropStates.has(FILE_B)).toBe(false);

		// Success-side UI effects still fire.
		expect(h.toastSuccess).toHaveBeenCalledTimes(1);
		expect(h.setShowPosterCropModal).toHaveBeenCalledWith(false);

		const movieA = (h.job.current.results![FILE_A] as FileResult).movie!;
		expect(movieA.cropped_poster_url).toBe('/api/v1/temp/posters/job-1/AAA-001.jpg');
		expect(movieA.poster_crop_bounds).toEqual({ x: 10, y: 20, width: 300, height: 450 });
	});
});
describe('review-mutations — poster-from-URL adopts the SERVER-DERIVED crop intent', () => {
	beforeEach(() => {
		createdMutations.length = 0;
	});

	it('pending-edit overlay adopts should_crop_poster=true from the server (the audit-7 regression)', async () => {
		// Regression: the poster-from-URL temp preview is ALWAYS auto-cropped
		// server-side, and a cover-backed prior source derives
		// ShouldCropPoster=true, so Organize will default-crop the downloaded
		// image. Hard-coding should_crop_poster:false here would land a false
		// on the pending edit; a later whole-movie Save resends it with an
		// unchanged poster_source, which updateBatchMovie treats as deliberate
		// — Organize downloads the image WHOLE while the preview showed it
		// cropped. The overlay must adopt data.should_crop_poster verbatim.
		const h = makeHarness();
		// A pending edit exists (e.g. user edited the title) with the
		// movie's pre-URL poster state.
		h.editedMovies.set(FILE_A, {
			...makeMovie('AAA-001'),
			title: 'User Edited Title',
			poster_url: 'https://example.com/old-cover.jpg',
			cropped_poster_url: '/api/v1/temp/posters/job-1/AAA-001.jpg?v=1',
			should_crop_poster: false,
		});

		h.api.updateBatchMoviePosterFromURL.mockResolvedValue({
			poster_url: 'https://example.com/new-cover.jpg',
			cropped_poster_url: '/api/v1/temp/posters/job-1/AAA-001.jpg?v=2',
			should_crop_poster: true,
		});
		await h.mutations.applyPosterFromUrlAsync('res-A', 'https://example.com/new-cover.jpg');

		// Pending edit adopts the server intent; unrelated edits survive.
		const pending = h.editedMovies.get(FILE_A)!;
		expect(pending.should_crop_poster).toBe(true);
		expect(pending.poster_url).toBe('https://example.com/new-cover.jpg');
		expect(pending.cropped_poster_url).toBe('/api/v1/temp/posters/job-1/AAA-001.jpg?v=2');
		expect(pending.poster_crop_bounds).toBeNull();
		expect(pending.title).toBe('User Edited Title');

		// Non-pending job state reflects the same server value.
		const movieA = (h.job.current.results![FILE_A] as FileResult).movie!;
		expect(movieA.should_crop_poster).toBe(true);
		expect(movieA.poster_url).toBe('https://example.com/new-cover.jpg');

		// skipJobSync must fire BEFORE setJob so the job-sync watcher cannot
		// clobber the just-overlaid state with a stale server snapshot.
		expect(h.skipJobSync).toHaveBeenCalled();
	});

	it('non-pending job state adopts should_crop_poster=false for a poster-grade prior', async () => {
		const h = makeHarness();
		h.api.updateBatchMoviePosterFromURL.mockResolvedValue({
			poster_url: 'https://example.com/explicit-poster.jpg',
			cropped_poster_url: '/api/v1/temp/posters/job-1/AAA-001.jpg?v=2',
			should_crop_poster: false,
		});
		await h.mutations.applyPosterFromUrlAsync('res-A', 'https://example.com/explicit-poster.jpg');

		const movieA = (h.job.current.results![FILE_A] as FileResult).movie!;
		expect(movieA.should_crop_poster).toBe(false);
		expect(movieA.poster_crop_bounds).toBeNull();
	});

	it('resetPoster route (applyPosterFromUrl with the baseline URL) carries the restored server intent', async () => {
		// The restore-to-baseline half of the reset flow routes through the
		// same mutation: review-state.resetPoster calls
		// mutations.applyPosterFromUrl(resultId, baseline.poster_url). The
		// server re-derives the intent for the restored image
		// (cropIntentAfterPosterFromURL — Reset stays a fixed point), and the
		// overlay must carry THAT value, not a stale pre-reset one.
		const h = makeHarness();
		h.editedMovies.set(FILE_A, {
			...makeMovie('AAA-001'),
			poster_url: 'https://example.com/drifted-url.jpg',
			should_crop_poster: false,
		});

		h.api.updateBatchMoviePosterFromURL.mockResolvedValue({
			// Server restored the scraped baseline and re-derived its intent.
			poster_url: 'https://example.com/baseline-cover.jpg',
			cropped_poster_url: '/api/v1/temp/posters/job-1/AAA-001.jpg?v=3',
			should_crop_poster: true,
		});
		// Same entry point resetPoster uses on the URL-changed branch.
		await h.mutations.applyPosterFromUrlAsync('res-A', 'https://example.com/baseline-cover.jpg');

		const pending = h.editedMovies.get(FILE_A)!;
		expect(pending.poster_url).toBe('https://example.com/baseline-cover.jpg');
		expect(pending.should_crop_poster).toBe(true);

		const movieA = (h.job.current.results![FILE_A] as FileResult).movie!;
		expect(movieA.should_crop_poster).toBe(true);
		expect(movieA.poster_url).toBe('https://example.com/baseline-cover.jpg');
	});
});
