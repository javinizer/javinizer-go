import { createMutation } from '@tanstack/svelte-query';
import type { QueryClient } from '@tanstack/svelte-query';
import type { BatchJobResponse, BatchExcludeRequest, BatchExcludeResponse, BulkRescrapeRequest, BulkRescrapeResponse, FileResult, Movie, PosterCropResponse, PosterFromURLResponse } from '$lib/api/types';
import { normalizeCropBox, type PosterCropBox, type PosterCropState, type PosterPreviewOverride, type PosterCropMetrics } from '../review-utils';

	interface ReviewMutationsDeps {
	getJobId: () => string;
	getJob: () => BatchJobResponse | null;
	setJob: (job: BatchJobResponse) => void;
	skipJobSync: () => void;
	getEditedMovies: () => Map<string, Movie>;
	getCurrentResult: () => FileResult | undefined;
	getPosterPreviewOverrides: () => Map<string, PosterPreviewOverride>;
	getPosterCropStates: () => Map<string, PosterCropState>;
	getCropMetrics: () => PosterCropMetrics | null;
	getCropBox: () => PosterCropBox | null;
	getQueryClient: () => QueryClient;
	getCurrentMovieIndex: () => number;
	setCurrentMovieIndex: (index: number) => void;
	getMovieResultsLength: () => number;
	gotoJobs: () => void;
	setShowPosterCropModal: (show: boolean) => void;
	updateBatchMoviePosterFromURL: (jobId: string, movieId: string, body: { url: string }) => Promise<PosterFromURLResponse>;
	excludeBatchMovie: (jobId: string, movieId: string) => Promise<unknown>;
	updateBatchMovie: (jobId: string, movieId: string, movie: Movie) => Promise<unknown>;
	updateBatchMoviePosterCrop: (jobId: string, movieId: string, crop: PosterCropBox) => Promise<PosterCropResponse>;
	batchExcludeMovies: (jobId: string, request: BatchExcludeRequest) => Promise<BatchExcludeResponse>;
	bulkRescrapeMovies: (jobId: string, request: BulkRescrapeRequest) => Promise<BulkRescrapeResponse>;
	getSelectedMovieIds: () => Set<string>;
	clearSelectedMovieIds: () => void;
	deleteSelectedMovieId: (movieId: string) => void;
	toastSuccess: (message: string, duration?: number) => void;
	toastError: (message: string, duration?: number) => void;
}

export function createReviewMutations(deps: ReviewMutationsDeps) {
	const queryClient = deps.getQueryClient();

	function invalidateJobQueries() {
		void queryClient.invalidateQueries({ queryKey: ['batch-job', deps.getJobId()] });
		void queryClient.invalidateQueries({ queryKey: ['batch-job-slim', deps.getJobId()] });
	}

	const posterFromUrlMutation = createMutation(() => ({
		mutationFn: async ({ movieId, url }: { movieId: string; url: string }) => {
			return deps.updateBatchMoviePosterFromURL(deps.getJobId(), movieId, { url });
		},
		onSuccess: (data: PosterFromURLResponse, { movieId }) => {
			const currentJob = deps.getJob();
			if (currentJob) {
				const updatedJob: BatchJobResponse = {
					...currentJob,
					results: { ...currentJob.results }
				};
				for (const [filePath, result] of Object.entries(updatedJob.results)) {
					const r = result as FileResult;
					if (r.movie_id === movieId && r.data) {
						updatedJob.results[filePath] = {
							...r,
							data: {
								...r.data,
								poster_url: data.poster_url,
								cropped_poster_url: data.cropped_poster_url,
								should_crop_poster: false
							}
						};
					}
				}
				deps.skipJobSync();
				deps.setJob(updatedJob);

				const editedMovies = deps.getEditedMovies();
				for (const [filePath, movie] of editedMovies) {
					if (movie.id === movieId) {
						editedMovies.set(filePath, {
							...movie,
							poster_url: data.poster_url,
							cropped_poster_url: data.cropped_poster_url,
							should_crop_poster: false
						});
					}
				}
			}

			const currentResult = deps.getCurrentResult();
			if (currentResult) {
				deps.getPosterPreviewOverrides().set(currentResult.file_path, {
					url: data.cropped_poster_url,
					version: Date.now()
				});
			}

			invalidateJobQueries();
		},
		onError: (err: Error) => {
			deps.toastError(`Failed to set poster from screenshot: ${err.message}`);
		}
	}));

	function applyPosterFromUrl(movieId: string, url: string) {
		if (!deps.getJob() || posterFromUrlMutation.isPending) return;
		posterFromUrlMutation.mutate({ movieId, url });
	}

	const excludeMovieMutation = createMutation(() => ({
		mutationFn: async ({ jobId: mutationJobId, movieId }: { jobId: string; movieId: string }) => {
			return deps.excludeBatchMovie(mutationJobId, movieId);
		},
		onSuccess: async (_data, { movieId }) => {
			deps.deleteSelectedMovieId(movieId);
			deps.toastSuccess(`Movie ${movieId} excluded from organization`);
			invalidateJobQueries();

			const movieResultsLength = deps.getMovieResultsLength();
			const postExcludeLength = movieResultsLength - 1;
			if (postExcludeLength <= 0) {
				await deps.gotoJobs();
				return;
			}

			const currentIndex = deps.getCurrentMovieIndex();
			if (currentIndex >= postExcludeLength) {
				deps.setCurrentMovieIndex(postExcludeLength - 1);
			}
		},
		onError: (err: Error) => {
			deps.toastError(`Failed to exclude movie: ${err.message}`);
		}
	}));

	const saveEditsMutation = createMutation(() => ({
		mutationFn: async () => {
			const savePromises = Array.from(deps.getEditedMovies().entries()).map(([filePath, movie]) => {
				const movieToSave = { ...movie };
				if (movieToSave.display_title) {
					movieToSave.title = movieToSave.display_title;
				}
				return deps.updateBatchMovie(deps.getJobId(), movieToSave.id, movieToSave);
			});

			if (savePromises.length > 0) {
				await Promise.all(savePromises);
			}
		},
		onSuccess: () => {
			invalidateJobQueries();
		},
		onError: (err: Error) => {
			deps.toastError(`Failed to save edits: ${err.message}`);
		}
	}));

	const posterCropMutation = createMutation(() => ({
		mutationFn: async ({ jobId: mutationJobId, movieId, crop }: { jobId: string; movieId: string; crop: PosterCropBox }) => {
			return deps.updateBatchMoviePosterCrop(mutationJobId, movieId, crop);
		},
		onSuccess: (response: PosterCropResponse, { movieId }) => {
			const currentResultVal = deps.getCurrentResult();
			if (currentResultVal) {
				deps.getPosterPreviewOverrides().set(currentResultVal.file_path, {
					url: response.cropped_poster_url,
					version: Date.now()
				});

				const cropMetricsVal = deps.getCropMetrics();
				const cropBoxVal = deps.getCropBox();
				if (cropMetricsVal && cropBoxVal) {
					deps.getPosterCropStates().set(currentResultVal.file_path, normalizeCropBox(cropBoxVal, cropMetricsVal));
				}
			}

			deps.toastSuccess('Poster crop updated');
			deps.setShowPosterCropModal(false);

			invalidateJobQueries();
		},
		onError: (err: Error) => {
			deps.toastError(err.message || 'Failed to update poster crop');
		}
	}));

	const bulkExcludeMutation = createMutation(() => ({
		mutationFn: async ({ movieIds }: { movieIds: string[] }) => {
			return deps.batchExcludeMovies(deps.getJobId(), { movie_ids: movieIds });
		},
		onSuccess: (data) => {
			if (data.job) {
				deps.skipJobSync();
				deps.setJob(data.job);
			}

			deps.clearSelectedMovieIds();

			if (data.failed.length > 0) {
				deps.toastError(`Failed to exclude ${data.failed.length} movie${data.failed.length !== 1 ? 's' : ''}`);
			} else {
				deps.toastSuccess(`Excluded ${data.excluded.length} movie${data.excluded.length !== 1 ? 's' : ''}`);
			}

			invalidateJobQueries();
		},
		onError: (err: Error) => {
			deps.toastError(`Failed to exclude movies: ${err.message}`);
		}
	}));

	const bulkRescrapeMutation = createMutation(() => ({
		mutationFn: async ({ movieIds, selectedScrapers, preset, scalarStrategy, arrayStrategy }: {
			movieIds: string[];
			selectedScrapers: string[];
			preset?: string;
			scalarStrategy?: string;
			arrayStrategy?: string;
		}) => {
			return deps.bulkRescrapeMovies(deps.getJobId(), {
				movie_ids: movieIds,
				selected_scrapers: selectedScrapers,
				preset: preset as 'conservative' | 'gap-fill' | 'aggressive' | undefined,
				scalar_strategy: scalarStrategy as 'prefer-nfo' | 'prefer-scraper' | 'preserve-existing' | 'fill-missing-only' | undefined,
				array_strategy: arrayStrategy as 'merge' | 'replace' | undefined,
			});
		},
		onSuccess: (data) => {
			if (data.job) {
				deps.skipJobSync();
				deps.setJob(data.job);
			}

			if (data.failed > 0) {
				deps.toastError(`Failed to rescrape ${data.failed} movie${data.failed !== 1 ? 's' : ''}`);
			} else {
				deps.toastSuccess(`Rescraped ${data.succeeded} movie${data.succeeded !== 1 ? 's' : ''}`);
			}

			invalidateJobQueries();
		},
		onError: (err: Error) => {
			deps.toastError(`Failed to rescrape movies: ${err.message}`);
		}
	}));

	return {
		posterFromUrlMutation,
		applyPosterFromUrl,
		excludeMovieMutation,
		bulkExcludeMutation,
		bulkRescrapeMutation,
		saveEditsMutation,
		posterCropMutation
	};
}
