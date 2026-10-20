import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { QueryClient } from '@tanstack/query-core';
import type {
	BatchJobResponse,
	FileResult,
	Movie,
	PosterCropResponse,
	PosterFromURLResponse,
} from '$lib/api/types';
import type { PosterCropState, PosterPreviewOverride } from '../review-utils';

type MutationOptions = {
	mutationFn: (variables: unknown) => Promise<unknown>;
	onSuccess?: (data: unknown, variables: unknown) => Promise<void> | void;
};
vi.mock('@tanstack/svelte-query', () => ({
	createMutation: (getOptions: () => MutationOptions) => {
		const options = getOptions();
		return {
			mutate: vi.fn(),
			mutateAsync: async (variables: unknown) => {
				const data = await options.mutationFn(variables);
				await options.onSuccess?.(data, variables);
				return data;
			},
			reset: vi.fn(),
			isPending: false,
		};
	},
}));
import { createReviewMutations } from './review-mutations.svelte';

const targetPath = '/media/movie.part1.mp4';
const siblingPath = '/media/movie.part2.mp4';
const noMoviePath = '/media/movie.part3.mp4';
const unrelatedPath = '/media/unrelated.mp4';
const orphanPath = '/media/overlay-without-result.mp4';
const originalBounds = { x: 0.1, y: 0.2, width: 0.4, height: 0.6 };
const echoedBounds = { x: 0.2, y: 0.1, width: 0.5, height: 0.7 };
const crop = { x: 10, y: 20, width: 100, height: 150 };
const identity = { revision: 4, fingerprint: 'poster-before' };
const urlResponse: PosterFromURLResponse = {
	poster_url: 'https://new.example/poster.jpg',
	cropped_poster_url: '/temp/new-url-poster.jpg',
};
const cropResponse: PosterCropResponse = {
	cropped_poster_url: '/temp/new-crop-poster.jpg',
	should_crop_poster: false,
	poster_crop_bounds: echoedBounds,
	poster_crop_source_full: true,
	original_poster_url: '',
	original_cropped_poster_url: 'server-original-crop.jpg',
	original_should_crop_poster: null,
};
type MutationKind = 'URL' | 'crop';
type RevisionEcho = Pick<PosterCropResponse, 'revision' | 'revisions'>;

function makeMovie(id: string, title = 'Server title'): Movie {
	return {
		id,
		title,
		maker: 'Server maker',
		poster_url: 'https://old.example/poster.jpg',
		cropped_poster_url: '/temp/old-poster.jpg',
		should_crop_poster: true,
		poster_crop_bounds: originalBounds,
		poster_crop_source_full: true,
		original_poster_url: 'client-original.jpg',
		original_cropped_poster_url: 'client-original-crop.jpg',
		original_should_crop_poster: true,
	} as Movie;
}
function makeResult(
	filePath: string,
	resultId: string,
	movieId: string,
	revision: number,
): FileResult {
	return {
		file_path: filePath,
		result_id: resultId,
		movie_id: movieId,
		revision,
		movie: makeMovie(movieId),
		status: 'success',
		started_at: '',
		is_multi_part: true,
		part_number: revision,
		part_suffix: String(revision),
	};
}
function setup(kind: MutationKind, revisions: RevisionEcho = {}) {
	const initialJob = {
		id: 'job-a',
		status: 'completed',
		results: {
			[targetPath]: makeResult(targetPath, 'target', 'MOVIE-001', 1),
			[siblingPath]: makeResult(siblingPath, 'sibling', 'movie-001', 2),
			[noMoviePath]: {
				...makeResult(noMoviePath, 'without-movie', 'Movie-001', 3),
				movie: undefined,
			},
			[unrelatedPath]: makeResult(unrelatedPath, 'unrelated', 'OTHER-001', 4),
		},
	} as BatchJobResponse;
	let job: BatchJobResponse | null = initialJob;
	let currentResult: FileResult | undefined = initialJob.results[targetPath];
	let currentJobId = 'job-a';
	let generation = 3;
	const editedMovies = new Map([
		[targetPath, makeMovie('MOVIE-001', 'Unsaved target title')],
		[siblingPath, makeMovie('movie-001', 'Unsaved sibling title')],
		[noMoviePath, makeMovie('Movie-001', 'Overlay without a server movie')],
		[unrelatedPath, makeMovie('OTHER-001', 'Unsaved unrelated title')],
		[orphanPath, makeMovie('MOVIE-001', 'Orphan overlay')],
	]);
	const initialOverlays = new Map(editedMovies);
	const previews = new Map<string, PosterPreviewOverride>();
	const cropStates = new Map<string, PosterCropState>();
	const queryClient = new QueryClient();
	const invalidateQueries = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue(undefined);
	const response =
		kind === 'URL' ? { ...urlResponse, ...revisions } : { ...cropResponse, ...revisions };
	const deps = {
		getJobId: () => currentJobId,
		getJob: () => job,
		setJob: vi.fn((updatedJob: BatchJobResponse) => {
			job = updatedJob;
		}),
		isCurrentOperation: vi.fn(
			(jobId: string, expectedGeneration: number) =>
				jobId === currentJobId && expectedGeneration === generation,
		),
		getRouteGeneration: () => generation,
		skipJobSync: vi.fn(),
		clearEditStorage: vi.fn(),
		clearEditedMovies: vi.fn(),
		clearPosterPreviewOverrides: vi.fn(),
		getEditedMovies: () => editedMovies,
		getCurrentResult: () => currentResult,
		getPosterPreviewOverrides: () => previews,
		getPosterCropStates: () => cropStates,
		getCropMetrics: vi.fn(() => ({
			sourceWidth: 200,
			sourceHeight: 300,
			displayWidth: 200,
			displayHeight: 300,
			imageOffsetX: 0,
			imageOffsetY: 0,
		})),
		getCropBox: vi.fn(() => crop),
		getQueryClient: () => queryClient,
		getCurrentMovieIndex: () => 0,
		setCurrentMovieIndex: vi.fn(),
		getMovieResultsLength: () => 4,
		gotoJobs: vi.fn(),
		setShowPosterCropModal: vi.fn(),
		updateBatchMoviePosterFromURL: vi.fn().mockResolvedValue(response),
		getBatchMovieSources: vi.fn(),
		overrideBatchMovieField: vi.fn(),
		excludeBatchMovie: vi.fn(),
		updateBatchMovie: vi.fn(),
		updateBatchMoviePosterCrop: vi.fn().mockResolvedValue(response),
		batchExcludeMovies: vi.fn(),
		bulkRescrapeMovies: vi.fn(),
		getSelectedMovieIds: () => new Set<string>(),
		clearSelectedMovieIds: vi.fn(),
		deleteSelectedMovieId: vi.fn(),
		toastSuccess: vi.fn(),
		toastError: vi.fn(),
	};
	const mutations = createReviewMutations(deps);
	return {
		deps,
		initialJob,
		initialOverlays,
		editedMovies,
		previews,
		cropStates,
		invalidateQueries,
		getJob: () => job!,
		setJob: (value: BatchJobResponse | null) => {
			job = value;
		},
		setCurrentResult: (value: FileResult | undefined) => {
			currentResult = value;
		},
		navigate: (jobId: string, nextGeneration: number) => {
			currentJobId = jobId;
			generation = nextGeneration;
		},
		run: (resultId = 'target') =>
			kind === 'URL'
				? mutations.applyPosterFromUrlAsync(resultId, 'https://input.example/poster.jpg')
				: mutations.applyPosterCropAsync('job-a', resultId, crop, 1000, identity),
	};
}

describe.each<MutationKind>(['URL', 'crop'])('poster %s success reconciliation', (kind) => {
	beforeEach(() => {
		vi.spyOn(Date, 'now').mockReturnValue(1234);
	});
	afterEach(() => {
		vi.restoreAllMocks();
	});
	it('reconciles case-variant multipart rows and only existing family overlays immutably', async () => {
		const h = setup(kind);
		for (const result of Object.values(h.initialJob.results)) {
			if (result.movie) Object.freeze(result.movie);
			Object.freeze(result);
		}
		Object.freeze(h.initialJob.results);
		Object.freeze(h.initialJob);
		for (const movie of h.initialOverlays.values()) Object.freeze(movie);
		await h.run();
		expect(h.getJob()).not.toBe(h.initialJob);
		expect(h.getJob().results).not.toBe(h.initialJob.results);
		for (const path of [targetPath, siblingPath]) {
			expect(h.getJob().results[path]).not.toBe(h.initialJob.results[path]);
			expect(h.getJob().results[path].movie).toMatchObject({
				title: 'Server title',
				maker: 'Server maker',
				cropped_poster_url:
					kind === 'URL' ? urlResponse.cropped_poster_url : cropResponse.cropped_poster_url,
				should_crop_poster: false,
			});
			expect(h.editedMovies.get(path)).not.toBe(h.initialOverlays.get(path));
			expect(h.editedMovies.get(path)?.title).toBe(h.initialOverlays.get(path)?.title);
			expect(h.initialJob.results[path].movie?.poster_crop_bounds).toEqual(originalBounds);
			expect(h.initialOverlays.get(path)?.cropped_poster_url).toBe('/temp/old-poster.jpg');
		}
		expect(h.getJob().results[noMoviePath]).toBe(h.initialJob.results[noMoviePath]);
		expect(h.editedMovies.get(noMoviePath)?.cropped_poster_url).toBe(
			kind === 'URL' ? urlResponse.cropped_poster_url : cropResponse.cropped_poster_url,
		);
		for (const path of [unrelatedPath, orphanPath])
			expect(h.editedMovies.get(path)).toBe(h.initialOverlays.get(path));
		expect(h.getJob().results[unrelatedPath]).toBe(h.initialJob.results[unrelatedPath]);
		expect(h.editedMovies.size).toBe(5);
		expect(h.deps.skipJobSync).toHaveBeenCalledOnce();
		expect(h.deps.setJob).toHaveBeenCalledWith(h.getJob(), 'job-a', 3);
	});
	it.each<{ name: string; echo: RevisionEcho; expected: number[] }>([
		{
			name: 'nonempty map wins over conflicting scalar',
			echo: { revision: 999, revisions: { target: 11, sibling: 12, 'without-movie': 13 } },
			expected: [11, 12, 13, 4],
		},
		{
			name: 'nonempty map with absent target never falls back to scalar',
			echo: { revision: 999, revisions: { sibling: 12 } },
			expected: [1, 12, 3, 4],
		},
		{
			name: 'nonempty unknown-only map never falls back to scalar',
			echo: { revision: 999, revisions: { missing: 12 } },
			expected: [1, 2, 3, 4],
		},
		{
			name: 'map updates any known result and ignores unknown IDs',
			echo: { revisions: { unrelated: 14, missing: 99 } },
			expected: [1, 2, 3, 14],
		},
		{
			name: 'empty map falls back to target-only scalar',
			echo: { revision: 11, revisions: {} },
			expected: [11, 2, 3, 4],
		},
		{
			name: 'missing map falls back to target-only scalar',
			echo: { revision: 11 },
			expected: [11, 2, 3, 4],
		},
		{
			name: 'zero scalar remains a present revision',
			echo: { revision: 0 },
			expected: [0, 2, 3, 4],
		},
		{
			name: 'empty map without scalar preserves revisions',
			echo: { revisions: {} },
			expected: [1, 2, 3, 4],
		},
		{ name: 'missing revisions preserve baselines', echo: {}, expected: [1, 2, 3, 4] },
	])('$name', async ({ echo, expected }) => {
		const h = setup(kind, echo);
		await h.run();
		expect(Object.values(h.getJob().results).map((result) => result.revision)).toEqual(expected);
		expect(Object.values(h.initialJob.results).map((result) => result.revision)).toEqual([
			1, 2, 3, 4,
		]);
		expect(Object.keys(h.getJob().results)).toHaveLength(4);
	});
	it('does not create overlays for unedited siblings', async () => {
		const h = setup(kind);
		h.editedMovies.delete(siblingPath);
		await h.run();
		expect(h.editedMovies.has(siblingPath)).toBe(false);
		expect(h.getJob().results[siblingPath].movie?.should_crop_poster).toBe(false);
	});
	it.each(['missing', 'without-family'])('keeps poster state for a %s target', async (resultId) => {
		const h = setup(kind, { revision: 99 });
		if (resultId === 'without-family') {
			h.initialJob.results[targetPath].result_id = resultId;
			h.initialJob.results[targetPath].movie_id = '';
		}
		await h.run(resultId);
		for (const [path, result] of Object.entries(h.initialJob.results))
			expect(h.getJob().results[path].movie).toBe(result.movie);
		expect(h.editedMovies).toEqual(h.initialOverlays);
		expect(h.getJob().results[targetPath].revision).toBe(resultId === 'missing' ? 1 : 99);
	});
	it('keeps request, preview, invalidation and modal behavior in the callback', async () => {
		const h = setup(kind);
		// Existing callbacks intentionally use the currently visible result for preview.
		h.setCurrentResult(h.initialJob.results[siblingPath]);
		await h.run();
		expect(h.previews).toEqual(
			new Map([
				[
					siblingPath,
					{
						url: kind === 'URL' ? urlResponse.cropped_poster_url : cropResponse.cropped_poster_url,
						version: 1234,
					},
				],
			]),
		);
		expect(h.invalidateQueries.mock.calls).toEqual([
			[{ queryKey: ['batch-job', 'job-a'] }],
			[{ queryKey: ['batch-job-slim', 'job-a'] }],
			[{ queryKey: ['actresses'] }],
		]);
		if (kind === 'URL') {
			expect(h.deps.updateBatchMoviePosterFromURL).toHaveBeenCalledWith('job-a', 'target', {
				url: 'https://input.example/poster.jpg',
			});
			expect(h.cropStates.size).toBe(0);
			expect(h.deps.setShowPosterCropModal).not.toHaveBeenCalled();
			expect(h.deps.toastSuccess).not.toHaveBeenCalled();
		} else {
			expect(h.deps.updateBatchMoviePosterCrop).toHaveBeenCalledWith(
				'job-a',
				'target',
				crop,
				1000,
				identity,
			);
			expect(h.cropStates.get(siblingPath)).toEqual({
				xRatio: 0.05,
				yRatio: 20 / 300,
				widthRatio: 0.5,
				heightRatio: 0.5,
			});
			expect(h.deps.setShowPosterCropModal).toHaveBeenCalledWith(false);
			expect(h.deps.toastSuccess).toHaveBeenCalledOnce();
		}
		expect(h.deps.toastError).not.toHaveBeenCalled();
	});
	it('reconciles without a visible result and skips preview state', async () => {
		const h = setup(kind);
		h.setCurrentResult(undefined);
		await h.run();
		expect(h.deps.setJob).toHaveBeenCalledOnce();
		expect(h.previews.size).toBe(0);
		expect(h.cropStates.size).toBe(0);
	});
	it('does not reconcile a job removed while the request is pending', async () => {
		const h = setup(kind);
		const pending = h.run();
		h.setJob(null);
		await pending;
		expect(h.deps.setJob).not.toHaveBeenCalled();
		expect(h.deps.skipJobSync).not.toHaveBeenCalled();
		expect(h.editedMovies).toEqual(h.initialOverlays);
		expect(h.invalidateQueries).toHaveBeenCalledTimes(3);
	});
	it.each([
		{ name: 'another job', jobId: 'job-b', generation: 4 },
		{ name: 'a new generation of the same job', jobId: 'job-a', generation: 4 },
	])('ignores a late success after navigating to $name', async ({ jobId, generation }) => {
		const h = setup(kind);
		const pending = h.run();
		h.navigate(jobId, generation);
		await pending;
		expect(h.deps.isCurrentOperation).toHaveBeenCalledWith('job-a', 3);
		expect(h.deps.setJob).not.toHaveBeenCalled();
		expect(h.deps.skipJobSync).not.toHaveBeenCalled();
		expect(h.editedMovies).toEqual(h.initialOverlays);
		expect(h.previews.size).toBe(0);
		expect(h.cropStates.size).toBe(0);
		expect(h.deps.setShowPosterCropModal).not.toHaveBeenCalled();
		expect(h.deps.toastSuccess).not.toHaveBeenCalled();
		expect(h.invalidateQueries).not.toHaveBeenCalled();
	});
});

describe('poster-specific movie transformations', () => {
	it('URL replacement clears geometry with explicit null and preserves existing baseline fields', async () => {
		const h = setup('URL');
		await h.run();
		for (const movie of [h.getJob().results[targetPath].movie!, h.editedMovies.get(siblingPath)!]) {
			expect(movie).toMatchObject({
				poster_url: urlResponse.poster_url,
				cropped_poster_url: urlResponse.cropped_poster_url,
				should_crop_poster: false,
				poster_crop_bounds: null,
				poster_crop_source_full: true,
				original_poster_url: 'client-original.jpg',
			});
			expect(JSON.parse(JSON.stringify(movie))).toHaveProperty('poster_crop_bounds', null);
		}
	});
	it('crop applies authoritative geometry and server baseline, including an empty original URL', async () => {
		const h = setup('crop');
		await h.run();
		for (const movie of [h.getJob().results[targetPath].movie!, h.editedMovies.get(siblingPath)!]) {
			expect(movie).toMatchObject({
				poster_url: 'https://old.example/poster.jpg',
				poster_crop_bounds: echoedBounds,
				poster_crop_source_full: true,
				original_poster_url: '',
				original_cropped_poster_url: 'server-original-crop.jpg',
				original_should_crop_poster: null,
			});
		}
	});
	it.each([null, undefined])(
		'legacy crop echo with %s bounds removes geometry rather than emitting null',
		async (bounds) => {
			const h = setup('crop');
			h.deps.updateBatchMoviePosterCrop.mockResolvedValue({
				cropped_poster_url: '/legacy-crop.jpg',
				should_crop_poster: false,
				poster_crop_bounds: bounds,
			});
			await h.run();
			for (const movie of [
				h.getJob().results[targetPath].movie!,
				h.editedMovies.get(siblingPath)!,
			]) {
				expect(movie).not.toHaveProperty('poster_crop_bounds');
				expect(movie).not.toHaveProperty('poster_crop_source_full');
				expect(movie.original_poster_url).toBe('client-original.jpg');
				expect(movie.cropped_poster_url).toBe('/legacy-crop.jpg');
			}
		},
	);
});
