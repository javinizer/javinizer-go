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

// Same minimal createMutation harness as review-mutations.navigation.test.ts:
// executes mutationFn and awaits onSuccess once per call.
vi.mock('@tanstack/svelte-query', () => ({
	createMutation: (optionsFn: () => MutationOptions) => {
		const options = optionsFn();
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

const FILE_A = '/a/AAA-001-cd1.mp4';
const FILE_B = '/a/AAA-001-cd2.mp4';

function makeResult(resultId: string, filePath: string, movieId: string): FileResult {
	return {
		result_id: resultId,
		file_path: filePath,
		movie_id: movieId,
		status: 'completed',
		started_at: '2024-01-01T00:00:00Z',
		is_multi_part: true,
		part_number: filePath.includes('cd2') ? 2 : 1,
		part_suffix: filePath.includes('cd2') ? '-cd2' : '-cd1',
		movie: {
			id: movieId,
			code: movieId,
			title: `${movieId} Title`,
			display_title: `${movieId} Title`,
			poster_url: 'https://example.com/stable-source.jpg',
			should_crop_poster: true,
		},
	};
}

function makeHarness(opts: {
	refetchJob?: () => Promise<BatchJobResponse | null>;
	cropResponse?: PosterCropResponse;
	fromUrlResponse?: PosterFromURLResponse;
}) {
	// The multipart A family: two siblings sharing movie AAA-001.
	const job = {
		current: {
			job_id: 'job-1',
			results: {
				[FILE_A]: makeResult('res-A', FILE_A, 'AAA-001'),
				[FILE_B]: makeResult('res-A2', FILE_B, 'AAA-001'),
			},
		} as unknown as BatchJobResponse,
	};
	const editedMovies = new Map<string, Movie>();
	const posterCropStates = new Map<string, PosterCropState>();
	const refetchJob = vi.fn(opts.refetchJob ?? (async () => job.current));
	const toastSuccess = vi.fn();
	const noop = () => {};
	const metrics: PosterCropMetrics = {
		sourceWidth: 400,
		sourceHeight: 600,
		displayWidth: 200,
		displayHeight: 300,
		imageOffsetX: 0,
		imageOffsetY: 0,
	};
	const mutations = createReviewMutations({
		getJobId: () => 'job-1',
		getJob: () => job.current,
		setJob: (next) => {
			job.current = next;
		},
		skipJobSync: noop,
		clearEditStorage: noop,
		clearEditedMovies: noop,
		clearPosterPreviewOverrides: noop,
		getEditedMovies: () => editedMovies,
		getCurrentResult: () => job.current.results![FILE_A] as FileResult,
		getPosterPreviewOverrides: () => new Map<string, PosterPreviewOverride>(),
		getPosterCropStates: () => posterCropStates,
		clearPosterCropStates: (filePaths: string[]) => {
			for (const fp of filePaths) posterCropStates.delete(fp);
		},
		getCropMetrics: () => metrics,
		getCropBox: () => null,
		getQueryClient: () => ({ invalidateQueries: vi.fn(async () => {}) }) as never,
		refetchJob,
		getCurrentMovieIndex: () => 0,
		setCurrentMovieIndex: noop,
		getMovieResultsLength: () => 2,
		gotoJobs: async () => {},
		setShowPosterCropModal: noop,
		updateBatchMoviePosterFromURL: vi.fn(async () => {
			if (!opts.fromUrlResponse) throw new Error('poster-from-URL not stubbed');
			return opts.fromUrlResponse;
		}),
		getBatchMovieSources: vi.fn(),
		overrideBatchMovieField: vi.fn(),
		excludeBatchMovie: vi.fn(),
		updateBatchMovie: vi.fn(),
		updateBatchMoviePosterCrop: vi.fn(async () => {
			if (!opts.cropResponse) throw new Error('crop not stubbed');
			return opts.cropResponse;
		}),
		batchExcludeMovies: vi.fn(),
		bulkRescrapeMovies: vi.fn(),
		getSelectedMovieIds: () => new Set<string>(),
		clearSelectedMovieIds: noop,
		deleteSelectedMovieId: noop,
		toastSuccess,
		toastError: vi.fn(),
	});
	return { mutations, job, editedMovies, posterCropStates, refetchJob, toastSuccess };
}

function resultAt(h: { job: { current: BatchJobResponse } }, fp: string): FileResult {
	return h.job.current.results![fp] as FileResult;
}

describe('poster mutations — rekey-during-flight re-resolves the fan-out family (Codex P2)', () => {
	beforeEach(() => vi.clearAllMocks());

	it('posterCropMutation: a cross-tab A→B rekey mid-flight fans the crop out over B’s family, not the stale A snapshot', async () => {
		// Another tab re-keyed AAA-001 → AAA-009 while the crop request was in
		// flight. The server converged on AAA-009 (resultId→path resolution is
		// rekey-safe); the refetched job shows BOTH siblings at the new key...
		const rekeyed: BatchJobResponse = {
			job_id: 'job-1',
			results: {
				[FILE_A]: makeResult('res-A', FILE_A, 'AAA-009'),
				[FILE_B]: makeResult('res-A2', FILE_B, 'AAA-009'),
			},
		} as unknown as BatchJobResponse;
		const bounds = { x: 10, y: 20, width: 300, height: 450 };
		const h = makeHarness({
			refetchJob: async () => rekeyed,
			cropResponse: {
				cropped_poster_url: '/api/v1/temp/posters/job-1/AAA-009.jpg?v=2',
				poster_crop_bounds: bounds,
			},
		});
		// A pending whole-movie edit on the sibling: the exact Save-clobber
		// vector the P2 calls out (saving an "A" sibling must not be able to
		// resubmit pre-edit poster state over B's converged family).
		h.editedMovies.set(FILE_B, {
			...(resultAt(h, FILE_B).movie as Movie),
			title: 'User Edited Title',
		});

		const crop: PosterCropBox = { x: 10, y: 20, width: 300, height: 450 };
		await h.mutations.applyPosterCropAsync('job-1', 'res-A', crop, undefined);

		expect(h.refetchJob).toHaveBeenCalledOnce();
		// The authoritative job replaced the stale snapshot wholesale...
		expect(resultAt(h, FILE_A).movie_id).toBe('AAA-009');
		expect(resultAt(h, FILE_B).movie_id).toBe('AAA-009');
		// ...and the crop fanned out over the CONVERGED family: both siblings
		// carry the server-stored bounds and the B-keyed preview URL.
		for (const fp of [FILE_A, FILE_B]) {
			const movie = resultAt(h, fp).movie!;
			expect(movie.poster_crop_bounds).toEqual(bounds);
			expect(movie.cropped_poster_url).toBe('/api/v1/temp/posters/job-1/AAA-009.jpg?v=2');
		}
		// Pending edits fan out the same way; the user's unrelated edit survives.
		const pendingSibling = h.editedMovies.get(FILE_B)!;
		expect(pendingSibling.poster_crop_bounds).toEqual(bounds);
		expect(pendingSibling.cropped_poster_url).toBe('/api/v1/temp/posters/job-1/AAA-009.jpg?v=2');
		expect(pendingSibling.title).toBe('User Edited Title');
		expect(h.toastSuccess).toHaveBeenCalledOnce();
	});

	it('posterFromUrlMutation: the rekeyed family, not the stale one, receives the URL/reset overlay and the crop-geometry drop', async () => {
		const rekeyed: BatchJobResponse = {
			job_id: 'job-1',
			results: {
				[FILE_A]: makeResult('res-A', FILE_A, 'AAA-009'),
				[FILE_B]: makeResult('res-A2', FILE_B, 'AAA-009'),
			},
		} as unknown as BatchJobResponse;
		const h = makeHarness({
			refetchJob: async () => rekeyed,
			fromUrlResponse: {
				poster_url: 'https://example.com/baseline.jpg',
				cropped_poster_url: '/api/v1/temp/posters/job-1/AAA-009.jpg?v=3',
				should_crop_poster: true,
			},
		});
		// Stale geometry measured against the OLD image on both parts — the
		// clear must resolve through the REFETCHED family either way (paths
		// coincide here, but movie_id must not stay on A).
		h.posterCropStates.set(FILE_A, { xRatio: 0.1, yRatio: 0.1, widthRatio: 0.5, heightRatio: 0.5 });

		await h.mutations.applyPosterFromUrlAsync('res-A', 'https://example.com/baseline.jpg');

		expect(h.refetchJob).toHaveBeenCalledOnce();
		expect(resultAt(h, FILE_A).movie_id).toBe('AAA-009');
		for (const fp of [FILE_A, FILE_B]) {
			expect(resultAt(h, fp).movie!.poster_url).toBe('https://example.com/baseline.jpg');
		}
		expect(h.posterCropStates.has(FILE_A)).toBe(false);
	});

	it('a FAILED refetch falls back to the pre-fix best-effort overlay on the current job (never worse than today)', async () => {
		const bounds = { x: 1, y: 2, width: 100, height: 150 };
		const h = makeHarness({
			refetchJob: async () => {
				throw new Error('network down');
			},
			cropResponse: {
				cropped_poster_url: '/api/v1/temp/posters/job-1/AAA-001.jpg?v=2',
				poster_crop_bounds: bounds,
			},
		});

		await h.mutations.applyPosterCropAsync('job-1', 'res-A', { x: 1, y: 2, width: 100, height: 150 }, undefined);

		expect(h.refetchJob).toHaveBeenCalledOnce();
		// Overlay still applied to the current (possibly stale) job...
		expect(resultAt(h, FILE_A).movie!.poster_crop_bounds).toEqual(bounds);
		expect(resultAt(h, FILE_B).movie!.poster_crop_bounds).toEqual(bounds);
		// ...and the mutation still succeeds (trailing invalidation retries the refetch).
		expect(h.toastSuccess).toHaveBeenCalledOnce();
	});

	it('a null refetch result (204-class delete race) also falls back to the current job', async () => {
		const h = makeHarness({
			refetchJob: async () => null,
			cropResponse: {
				cropped_poster_url: '/api/v1/temp/posters/job-1/AAA-001.jpg?v=2',
				poster_crop_bounds: { x: 0, y: 0, width: 50, height: 75 },
			},
		});

		await h.mutations.applyPosterCropAsync('job-1', 'res-A', { x: 0, y: 0, width: 50, height: 75 }, undefined);

		expect(resultAt(h, FILE_A).movie!.poster_crop_bounds).toEqual({ x: 0, y: 0, width: 50, height: 75 });
		expect(h.toastSuccess).toHaveBeenCalledOnce();
	});

	it('no rekey in flight: the refetch is adopted and the overlay is unchanged from the pre-fix behavior', async () => {
		// Default refetchJob returns the SAME job the handler holds — the
		// extra round-trip is the "minimal targeted invalidation+refetch"
		// cost, and the fan-out must be identical for the steady state.
		const bounds = { x: 5, y: 5, width: 200, height: 300 };
		const h = makeHarness({
			cropResponse: {
				cropped_poster_url: '/api/v1/temp/posters/job-1/AAA-001.jpg?v=2',
				poster_crop_bounds: bounds,
			},
		});
		const jobBefore = h.job.current;

		await h.mutations.applyPosterCropAsync('job-1', 'res-A', { x: 5, y: 5, width: 200, height: 300 }, undefined);

		expect(h.refetchJob).toHaveBeenCalledOnce();
		expect(h.job.current).not.toBe(jobBefore); // adopted the refetched (cloned) job
		expect(resultAt(h, FILE_A).movie_id).toBe('AAA-001');
		expect(resultAt(h, FILE_A).movie!.poster_crop_bounds).toEqual(bounds);
		expect(resultAt(h, FILE_B).movie!.poster_crop_bounds).toEqual(bounds);
	});
});