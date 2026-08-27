import { beforeEach, describe, expect, it, vi } from 'vitest';
import { QueryClient } from '@tanstack/query-core';
import type { BatchJobResponse, FileResult, Movie } from '$lib/api/types';

type TestMutationOptions = {
	mutationFn: (variables: unknown) => Promise<unknown>;
	onSuccess?: (data: unknown, variables: unknown, context: unknown) => Promise<void> | void;
	onError?: (error: unknown, variables: unknown, context: unknown) => Promise<void> | void;
};

const mutationOptions: TestMutationOptions[] = [];

vi.mock('@tanstack/svelte-query', () => ({
	createMutation: (getOptions: () => TestMutationOptions) => {
		const options = getOptions();
		mutationOptions.push(options);
		return {
			mutate: vi.fn(),
			mutateAsync: async (variables: unknown) => {
				try {
					const data = await options.mutationFn(variables);
					await options.onSuccess?.(data, variables, undefined);
					return data;
				} catch (error) {
					await options.onError?.(error, variables, undefined);
					throw error;
				}
			},
			reset: vi.fn(),
			isPending: false,
		};
	},
}));

import { createReviewMutations } from './review-mutations.svelte';

function makeMovie(id: string): Movie {
	return { id, title: id } as Movie;
}

function makeJob(filePath: string, movie: Movie): BatchJobResponse {
	const result = {
		result_id: 'result-a',
		movie_id: movie.id,
		revision: 1,
		movie,
	} as unknown as FileResult;
	return {
		id: 'job-a',
		results: { [filePath]: result },
	} as unknown as BatchJobResponse;
}

describe('review mutations route-change reconciliation', () => {
	beforeEach(() => {
		mutationOptions.length = 0;
		sessionStorage.clear();
	});

	it('invalidates and removes fulfilled job A overlays after navigating to job B', async () => {
		const queryClient = new QueryClient();
		const invalidateQueries = vi
			.spyOn(queryClient, 'invalidateQueries')
			.mockResolvedValue(undefined);
		const savedPath = '/media/a.mp4';
		const untouchedPath = '/media/a-untouched.mp4';
		const savedMovie = makeMovie('saved');
		const untouchedMovie = makeMovie('untouched');
		const job = makeJob(savedPath, savedMovie);
		const jobAEditedMovies = new Map<string, Movie>([[savedPath, savedMovie]]);
		const jobBEditedMovies = new Map<string, Movie>([['/media/b.mp4', makeMovie('job-b')]]);
		let activeEditedMovies = jobAEditedMovies;
		let routeIsCurrent = true;
		let releasePatch!: () => void;
		const patch = new Promise<void>((resolve) => {
			releasePatch = resolve;
		});
		const updateBatchMovie = vi.fn(() => patch);
		const clearEditStorage = vi.fn();
		const clearPosterPreviewOverrides = vi.fn();
		const toastSuccess = vi.fn();
		const toastError = vi.fn();

		sessionStorage.setItem(
			'javinizer.review.editedMovies.job-a',
			JSON.stringify({ [savedPath]: savedMovie, [untouchedPath]: untouchedMovie }),
		);
		sessionStorage.setItem(
			'javinizer.review.posterPreviewOverrides.job-a',
			JSON.stringify({
				[savedPath]: { url: 'saved-preview' },
				[untouchedPath]: { url: 'keep-preview' },
			}),
		);

		const deps = {
			getJobId: () => 'job-a',
			getJob: () => job,
			setJob: vi.fn(),
			isCurrentOperation: () => routeIsCurrent,
			getRouteGeneration: () => 1,
			skipJobSync: vi.fn(),
			clearEditStorage,
			clearEditedMovies: vi.fn(),
			clearPosterPreviewOverrides,
			getEditedMovies: () => activeEditedMovies,
			getCurrentResult: () => undefined,
			getPosterPreviewOverrides: () => new Map(),
			getPosterCropStates: () => new Map(),
			getCropMetrics: () => null,
			getCropBox: () => null,
			getQueryClient: () => queryClient,
			getCurrentMovieIndex: () => 0,
			setCurrentMovieIndex: vi.fn(),
			getMovieResultsLength: () => 1,
			gotoJobs: vi.fn(),
			setShowPosterCropModal: vi.fn(),
			updateBatchMoviePosterFromURL: vi.fn(),
			getBatchMovieSources: vi.fn(),
			overrideBatchMovieField: vi.fn(),
			excludeBatchMovie: vi.fn(),
			updateBatchMovie,
			updateBatchMoviePosterCrop: vi.fn(),
			batchExcludeMovies: vi.fn(),
			bulkRescrapeMovies: vi.fn(),
			getSelectedMovieIds: () => new Set<string>(),
			clearSelectedMovieIds: vi.fn(),
			deleteSelectedMovieId: vi.fn(),
			toastSuccess,
			toastError,
		} as Parameters<typeof createReviewMutations>[0];
		const mutations = createReviewMutations(deps);

		const save = mutations.saveEditsMutation.mutateAsync({ jobId: 'job-a', generation: 1 });
		expect(updateBatchMovie).toHaveBeenCalledOnce();
		activeEditedMovies = jobBEditedMovies;
		routeIsCurrent = false;
		releasePatch();
		await save;

		expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: ['batch-job', 'job-a'] });
		expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: ['batch-job-slim', 'job-a'] });
		expect(
			JSON.parse(sessionStorage.getItem('javinizer.review.editedMovies.job-a') ?? '{}'),
		).toEqual({
			[untouchedPath]: untouchedMovie,
		});
		expect(
			JSON.parse(sessionStorage.getItem('javinizer.review.posterPreviewOverrides.job-a') ?? '{}'),
		).toEqual({
			[untouchedPath]: { url: 'keep-preview' },
		});
		expect(Array.from(activeEditedMovies.keys())).toEqual(['/media/b.mp4']);
		expect(clearEditStorage).not.toHaveBeenCalled();
		expect(clearPosterPreviewOverrides).not.toHaveBeenCalled();
		expect(toastSuccess).not.toHaveBeenCalled();
		expect(toastError).not.toHaveBeenCalled();
	});
});
