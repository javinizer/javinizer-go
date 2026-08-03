import { describe, it, expect, vi, beforeEach } from 'vitest';
import type {
	BatchJobResponse,
	BulkRescrapeResponse,
	FieldOverrideResponse,
	FileResult,
	Movie,
	PosterCropResponse,
	PosterFromURLResponse,
} from '$lib/api/types';
import {
	normalizeCropBox,
	type PosterCropBox,
	type PosterCropMetrics,
	type PosterCropState,
	type PosterPreviewOverride,
} from '../review-utils';

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
	refetchJob: ReturnType<typeof vi.fn>;
	// Flipping this simulates the user navigating to another movie mid-flight.
	currentResult: { current: FileResult };
	toastSuccess: ReturnType<typeof vi.fn>;
	toastError: ReturnType<typeof vi.fn>;
	setShowPosterCropModal: ReturnType<typeof vi.fn>;
	// Mutate .box mid-flight to simulate drag events landing during the save.
	cropLive: { metrics: PosterCropMetrics | null; box: PosterCropBox | null };
	invalidateQueries: ReturnType<typeof vi.fn>;
	api: {
		updateBatchMoviePosterFromURL: ReturnType<typeof vi.fn>;
		updateBatchMoviePosterCrop: ReturnType<typeof vi.fn>;
		overrideBatchMovieField: ReturnType<typeof vi.fn>;
		bulkRescrapeMovies: ReturnType<typeof vi.fn>;
		updateBatchMovie: ReturnType<typeof vi.fn>;
	};
}

function makeHarness(opts: { multipart?: boolean } = {}): Harness {
	const resultA = makeResult('res-A', FILE_A, 'AAA-001');
	const resultB = opts.multipart
		? makeResult('res-A2', FILE_B, 'AAA-001')
		: makeResult('res-B', FILE_B, 'BBB-002');
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
			_expectedSourceURL?: string,
		): Promise<PosterCropResponse> => {
			throw new Error('updateBatchMoviePosterCrop not stubbed for this test');
		},
	);
	const overrideBatchMovieField = vi.fn(async () => {
		throw new Error('overrideBatchMovieField not stubbed for this test');
	});
	const bulkRescrapeMovies = vi.fn(async () => {
		throw new Error('bulkRescrapeMovies not stubbed for this test');
	});
	const updateBatchMovie = vi.fn(async () => ({}));
	const api = {
		updateBatchMoviePosterFromURL,
		updateBatchMoviePosterCrop,
		overrideBatchMovieField,
		bulkRescrapeMovies,
		updateBatchMovie,
	};

	// Held in a mutable box so a test can drift the LIVE crop geometry
	// mid-flight the way drag events do while a crop save is in flight.
	const cropLive: { metrics: PosterCropMetrics | null; box: PosterCropBox | null } = {
		metrics: {
			sourceWidth: 400,
			sourceHeight: 600,
			displayWidth: 200,
			displayHeight: 300,
			imageOffsetX: 0,
			imageOffsetY: 0,
		},
		box: { x: 10, y: 20, width: 300, height: 450 },
	};

	const noop = () => {};
	const invalidateQueries = vi.fn(async () => {});
	// Default: the refetch returns the same job the handler already holds
	// (no rekey in flight) — tests exercising the rekey guard override this.
	const refetchJob = vi.fn(async () => job.current);
	const mutations = createReviewMutations({
		getJobId: () => 'job-1',
		getJob: () => job.current,
		setJob: (next) => {
			job.current = next;
		},
		skipJobSync,
		clearEditStorage: noop,
		clearEditedMovies: () => {
			editedMovies.clear();
		},
		clearPosterPreviewOverrides: () => {
			posterPreviewOverrides.clear();
		},
		getEditedMovies: () => editedMovies,
		getCurrentResult: () => currentResult.current,
		getPosterPreviewOverrides: () => posterPreviewOverrides,
		getPosterCropStates: () => posterCropStates,
		clearPosterCropStates: (filePaths: string[]) => {
			for (const fp of filePaths) posterCropStates.delete(fp);
		},
		getCropMetrics: () => cropLive.metrics,
		getCropBox: () => cropLive.box,
		getQueryClient: () => ({ invalidateQueries }) as never,
		refetchJob,
		getCurrentMovieIndex: () => 0,
		setCurrentMovieIndex: noop,
		getMovieResultsLength: () => 2,
		gotoJobs: async () => {},
		setShowPosterCropModal,
		updateBatchMoviePosterFromURL: api.updateBatchMoviePosterFromURL,
		getBatchMovieSources: vi.fn(),
		overrideBatchMovieField: api.overrideBatchMovieField,
		excludeBatchMovie: vi.fn(),
		updateBatchMovie: api.updateBatchMovie,
		updateBatchMoviePosterCrop: api.updateBatchMoviePosterCrop,
		batchExcludeMovies: vi.fn(),
		bulkRescrapeMovies: api.bulkRescrapeMovies,
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
		refetchJob,
		currentResult,
		toastSuccess,
		toastError,
		setShowPosterCropModal,
		api,
		cropLive,
		invalidateQueries,
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
		expect(h.api.updateBatchMoviePosterCrop).toHaveBeenCalledWith('job-1', 'res-A', crop, undefined, undefined);

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

	it('posterCropMutation: a DRAG during the save cannot bleed into the stored crop state', async () => {
		// Regression: the success handler used to read the LIVE global crop
		// box/metrics, but the modal's drag listeners stay active while the
		// request is in flight — a box dragged after submission was persisted
		// under the completed request's file path, desyncing client crop state
		// from the bounds the server stored. The stored state must be
		// normalized from the SUBMITTED box and the request-time metrics.
		const h = makeHarness();
		const pending = deferred<PosterCropResponse>();
		h.api.updateBatchMoviePosterCrop.mockImplementation(() => pending.promise);

		const submitted: PosterCropBox = { x: 10, y: 20, width: 300, height: 450 };
		const done = h.mutations.applyPosterCropAsync('job-1', 'res-A', submitted, undefined);
		expect(h.api.updateBatchMoviePosterCrop).toHaveBeenCalledWith('job-1', 'res-A', submitted, undefined, undefined);

		// Drag events move the live box while the request is in flight.
		h.cropLive.box = { x: 200, y: 100, width: 150, height: 225 };

		pending.resolve({
			cropped_poster_url: '/api/v1/temp/posters/job-1/AAA-001.jpg',
			poster_crop_bounds: { x: 10, y: 20, width: 300, height: 450 },
		});
		await done;

		// The stored state mirrors the SUBMITTED geometry, not the drifted box.
		expect(h.posterCropStates.get(FILE_A)).toMatchObject({
			xRatio: 10 / 400,
			yRatio: 20 / 600,
			widthRatio: 300 / 400,
			heightRatio: 450 / 600,
		});
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

describe('review-mutations — source-changing edits invalidate local crop geometry', () => {
	beforeEach(() => {
		createdMutations.length = 0;
	});

	const staleCrop = (): PosterCropState => ({
		xRatio: 0.1,
		yRatio: 0.1,
		widthRatio: 0.5,
		heightRatio: 0.5,
	});

	it('posterFromUrlMutation: clears stored crop geometry for the replaced source (all parts of the movie)', async () => {
		// Regression (Codex P1): the poster-from-URL/reset flow installs a NEW
		// source image server-side (bounds cleared) but posterCropStates is
		// keyed by file_path and survives — reopening the crop modal restored
		// geometry measured against the OLD image and Apply submitted the
		// stale bounds. Multi-part siblings share the same {movieID}-full.jpg,
		// so both parts' entries must go.
		const h = makeHarness();
		h.posterCropStates.set(FILE_A, staleCrop());
		h.posterCropStates.set(FILE_B, staleCrop());

		h.api.updateBatchMoviePosterFromURL.mockResolvedValue({
			poster_url: 'https://example.com/new-poster.jpg',
			cropped_poster_url: '/api/v1/temp/posters/job-1/AAA-001.jpg',
			should_crop_poster: false,
		} satisfies PosterFromURLResponse);
		await h.mutations.applyPosterFromUrlAsync('res-A', 'https://example.com/new-poster.jpg');

		expect(h.posterCropStates.has(FILE_A)).toBe(false);
		expect(
			h.posterCropStates.get(FILE_B),
			'unrelated movies keep their crop state',
		).toEqual(staleCrop());

		const movieA = (h.job.current.results![FILE_A] as FileResult).movie!;
		expect(movieA.poster_crop_bounds).toBeNull();
	});

	it('fieldOverrideMutation: a source-changing override clear (bounds nil in the response) dropped the local crop geometry', async () => {
		// Server-side (field_override.go): a poster_url/cover_url override
		// that changes the effective source clears CropBounds; the response
		// omits poster_crop_bounds (absent key = cleared). The local crop
		// state must follow or the crop modal resurrects stale geometry.
		const h = makeHarness();
		h.posterCropStates.set(FILE_A, staleCrop());

		const newMovie: Movie = {
			...makeMovie('AAA-001'),
			poster_url: 'https://example.com/from-other-source.jpg',
			cropped_poster_url: '/api/v1/temp/posters/job-1/AAA-001.jpg',
			should_crop_poster: false,
			// poster_crop_bounds intentionally ABSENT — the server's clear signal.
		};
		h.api.overrideBatchMovieField.mockResolvedValue({
			movie: newMovie,
			field_sources: { poster_url: 'other-scraper' },
		} satisfies FieldOverrideResponse);

		await h.mutations.applyFieldOverrideAsync('res-A', 'poster_url', 'other-scraper');

		expect(h.posterCropStates.has(FILE_A)).toBe(false);
	});

	it('fieldOverrideMutation: bounds the server KEPT (unchanged effective source) keep the local crop geometry', async () => {
		// The same override with an unchanged effective source preserves the
		// crop server-side (bounds shipped in the response) — the local state
		// was measured against that very image and must survive.
		const h = makeHarness();
		h.posterCropStates.set(FILE_A, staleCrop());
		h.posterCropStates.set(FILE_B, staleCrop());

		const bounds = { x: 40, y: 60, width: 200, height: 300 };
		const newMovie: Movie = {
			...makeMovie('AAA-001'),
			poster_url: 'https://example.com/same-source.jpg',
			cropped_poster_url: '/api/v1/temp/posters/job-1/AAA-001.jpg',
			should_crop_poster: false,
			poster_crop_bounds: bounds,
		};
		h.api.overrideBatchMovieField.mockResolvedValue({
			movie: newMovie,
			field_sources: { poster_url: 'other-scraper' },
		} satisfies FieldOverrideResponse);

		await h.mutations.applyFieldOverrideAsync('res-A', 'poster_url', 'other-scraper');

		expect(h.posterCropStates.get(FILE_A)).toEqual(staleCrop());
		expect(h.posterCropStates.get(FILE_B)).toEqual(staleCrop());
	});

	it('fieldOverrideMutation: non-poster fields never touch local crop geometry', async () => {
		// A title override ships no poster_crop_bounds either (the field is
		// untouched server-side), but nothing about the poster changed — the
		// stored geometry must survive.
		const h = makeHarness();
		h.posterCropStates.set(FILE_A, staleCrop());

		h.api.overrideBatchMovieField.mockResolvedValue({
			movie: { ...makeMovie('AAA-001'), title: 'New Title', display_title: 'New Title' },
			field_sources: { title: 'other-scraper' },
		} satisfies FieldOverrideResponse);

		await h.mutations.applyFieldOverrideAsync('res-A', 'title', 'other-scraper');

		expect(h.posterCropStates.get(FILE_A)).toEqual(staleCrop());
	});

	it('bulkRescrapeMutation: clears crop geometry for rescraped movies whose bounds the server cleared', async () => {
		// Regression (Codex P1, rescrape dual-site): a rescrape that switches
		// the effective poster source clears CropBounds server-side
		// (mergeRescrapeMovie); the local crop state measured against the old
		// image must go. Targets capture BEFORE setJob so a rescrape rekey
		// (AAA-001 → AAA-009, movie_id changes; the file_path key survives)
		// still resolves.
		const h = makeHarness();
		h.posterCropStates.set(FILE_A, staleCrop());
		h.posterCropStates.set(FILE_B, staleCrop());

		const rekeyedResult: FileResult = {
			...makeResult('res-A2', FILE_A, 'AAA-009'),
			movie: {
				...makeMovie('AAA-009'),
				poster_url: 'https://example.com/fresh.jpg',
				cropped_poster_url: '/api/v1/temp/posters/job-1/AAA-009.jpg',
				should_crop_poster: false,
				// poster_crop_bounds absent — source changed, bounds cleared.
			},
		};
		const newJob = {
			...h.job.current,
			results: { [FILE_A]: rekeyedResult, [FILE_B]: h.job.current.results![FILE_B] },
		} as unknown as BatchJobResponse;
		h.api.bulkRescrapeMovies.mockResolvedValue({
			results: [],
			succeeded: 1,
			failed: 0,
			job: newJob,
		} satisfies BulkRescrapeResponse);

		await h.mutations.bulkRescrapeMutation.mutateAsync({
			movieIds: ['AAA-001'],
			selectedScrapers: ['dmm'],
		});

		expect(
			h.posterCropStates.has(FILE_A),
			'the rekeyed rescrape cleared the bounds server-side; stale geometry must go',
		).toBe(false);
		expect(
			h.posterCropStates.get(FILE_B),
			'movies outside the rescrape keep their crop state',
		).toEqual(staleCrop());
	});

	it('bulkRescrapeMutation: bounds the server KEPT (same effective source) keep the local crop geometry', async () => {
		const h = makeHarness();
		h.posterCropStates.set(FILE_A, staleCrop());

		const bounds = { x: 40, y: 60, width: 200, height: 300 };
		const refreshedResult: FileResult = {
			...makeResult('res-A', FILE_A, 'AAA-001'),
			movie: {
				...makeMovie('AAA-001'),
				poster_url: 'https://example.com/same.jpg',
				poster_crop_bounds: bounds,
			},
		};
		const newJob = {
			...h.job.current,
			results: { [FILE_A]: refreshedResult, [FILE_B]: h.job.current.results![FILE_B] },
		} as unknown as BatchJobResponse;
		h.api.bulkRescrapeMovies.mockResolvedValue({
			results: [],
			succeeded: 1,
			failed: 0,
			job: newJob,
		} satisfies BulkRescrapeResponse);

		await h.mutations.bulkRescrapeMutation.mutateAsync({
			movieIds: ['AAA-001'],
			selectedScrapers: ['dmm'],
		});

		expect(h.posterCropStates.get(FILE_A)).toEqual(staleCrop());
	});

	it('bulkRescrapeMutation: case-variant sibling of a requested movie is treated as rescraped (P1-6)', async () => {
		// The backend folds movie IDs (resultstore indexKey lowercases every
		// index/lookup), so requesting 'AAA-001' rescrapes the results filed
		// under 'aaa-001' too. The client target capture must match the same
		// way or the variant sibling keeps stale crop geometry measured
		// against the source the server just replaced.
		const h = makeHarness();
		const lowerResult: FileResult = {
			...makeResult('res-B', FILE_B, 'aaa-001'),
			movie: makeMovie('aaa-001'),
		};
		h.job.current = {
			...h.job.current,
			results: {
				[FILE_A]: h.job.current.results![FILE_A],
				[FILE_B]: lowerResult,
			},
		} as BatchJobResponse;
		h.posterCropStates.set(FILE_B, staleCrop());

		const clearedLower: FileResult = {
			...lowerResult,
			movie: {
				...makeMovie('AAA-009'),
				poster_url: 'https://example.com/fresh.jpg',
				should_crop_poster: false,
				// poster_crop_bounds absent — source changed, bounds cleared.
			},
		};
		const newJob = {
			...h.job.current,
			results: {
				[FILE_A]: h.job.current.results![FILE_A],
				[FILE_B]: clearedLower,
			},
		} as unknown as BatchJobResponse;
		h.api.bulkRescrapeMovies.mockResolvedValue({
			results: [],
			succeeded: 1,
			failed: 0,
			job: newJob,
		} satisfies BulkRescrapeResponse);

		await h.mutations.bulkRescrapeMutation.mutateAsync({
			movieIds: ['AAA-001'],
			selectedScrapers: ['dmm'],
		});

		expect(
			h.posterCropStates.has(FILE_B),
			"the case-variant sibling WAS rescraped server-side (folded index); its stale crop geometry must go",
		).toBe(false);
	});

	it('bulkRescrapeMutation: a persist-failure 500 refetches the authoritative job state', async () => {
		// The bulk rescrape handler answers a persist-failure 500 WITH the
		// structured per-item results + updated job in the body, but ApiError
		// keeps only the message — so onError must refetch to adopt the
		// server state (succeeded movies AND rolled-back failures) instead of
		// leaving the client on stale pre-rescrape state.
		const h = makeHarness();
		h.api.bulkRescrapeMovies.mockRejectedValue(new Error('API request failed'));

		h.mutations.bulkRescrapeMutation.mutate({
			movieIds: ['AAA-001'],
			selectedScrapers: ['dmm'],
		});
		await new Promise((resolve) => setTimeout(resolve, 0));

		expect(h.toastError).toHaveBeenCalledOnce();
		expect(h.invalidateQueries).toHaveBeenCalledWith({ queryKey: ['batch-job', 'job-1'] });
	});
});

describe('saveEditsMutation — partial failure keeps failed edits pending and refetches', () => {
	it('partial failure: drops only CONFIRMED saves, keeps the failed entry pending, refetches, and reports per-file detail', async () => {
		// Regression: Promise.all rejected on the first failure while earlier
		// PATCHes had already COMMITTED server-side; onError only toasted, so
		// the committed saves stayed as pending local edits and the UI
		// diverged from the server until an unrelated invalidation.
		const h = makeHarness();
		h.editedMovies.set(FILE_A, makeMovie('AAA-001'));
		h.editedMovies.set(FILE_B, makeMovie('BBB-002'));
		h.api.updateBatchMovie.mockImplementation(async (_jobId: string, resultId: string) => {
			if (resultId === 'res-B') throw new Error('server 500');
			return {};
		});

		h.mutations.saveEditsMutation.mutate();
		await vi.waitFor(() => expect(h.toastError).toHaveBeenCalled());

		expect(h.toastError.mock.calls[0][0]).toContain(FILE_B);
		expect(h.editedMovies.has(FILE_A), 'confirmed save drops its pending edit').toBe(false);
		expect(h.editedMovies.has(FILE_B), 'failed save keeps its pending edit for retry').toBe(true);
		expect(h.invalidateQueries).toHaveBeenCalledWith({ queryKey: ['batch-job', 'job-1'] });
		expect(h.toastSuccess).not.toHaveBeenCalled();
	});

	it('full success: saves all edits and clears pending state (unchanged contract)', async () => {
		const h = makeHarness();
		h.editedMovies.set(FILE_A, makeMovie('AAA-001'));
		h.editedMovies.set(FILE_B, makeMovie('BBB-002'));
		h.api.updateBatchMovie.mockResolvedValue({});

		await h.mutations.saveEditsMutation.mutateAsync();

		expect(h.toastSuccess).toHaveBeenCalled();
		expect(h.editedMovies.size).toBe(0);
		expect(h.invalidateQueries).toHaveBeenCalledWith({ queryKey: ['batch-job', 'job-1'] });
	});
});

describe('posterCropMutation — crop state fans out to every part of the movie', () => {
	it('persists the post-crop normalized state for ALL posterEditTargetFilePaths', async () => {
		// Regression: the server applies a crop to every file of the movie,
		// but only the REQUEST target's file_path got the persisted crop
		// state — reopening the crop editor on a multi-part sibling fell
		// through to a blind default box.
		const h = makeHarness({ multipart: true });
		h.api.updateBatchMoviePosterCrop.mockResolvedValue({
			cropped_poster_url: '/api/v1/temp/posters/job-1/AAA-001.jpg',
			poster_crop_bounds: { x: 10, y: 20, width: 300, height: 450 },
		} satisfies PosterCropResponse);

		const crop: PosterCropBox = { x: 10, y: 20, width: 300, height: 450 };
		await h.mutations.applyPosterCropAsync('job-1', 'res-A', crop, undefined);

		const metrics = h.cropLive.metrics;
		if (!metrics) throw new Error('harness metrics missing');
		const expected = normalizeCropBox(crop, metrics);
		expect(h.posterCropStates.get(FILE_A)).toEqual(expected);
		expect(h.posterCropStates.get(FILE_B)).toEqual(expected);
	});
});
