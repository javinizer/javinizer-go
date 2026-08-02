import { createMutation } from '@tanstack/svelte-query';
import type { QueryClient } from '@tanstack/svelte-query';
import type {
	BatchJobResponse,
	BatchExcludeRequest,
	BatchExcludeResponse,
	BulkRescrapeRequest,
	BulkRescrapeResponse,
	FieldOverrideResponse,
	FileResult,
	Movie,
	PosterCropResponse,
	PosterFromURLResponse,
	SourceResultsResponse,
} from '$lib/api/types';
import {
	normalizeCropBox,
	type PosterCropBox,
	type PosterCropState,
	type PosterPreviewOverride,
	type PosterCropMetrics,
} from '../review-utils';
import { overlayFieldOverride, overlayPosterEdit, posterCropOverlayFromResponse, posterEditTargetFilePaths, applyFieldOverrideToEditedMovies, applyFieldOverrideToResults, type PosterEditOverlay } from './overlay-field-override';
import { buildMovieToSave } from './save-helpers';
import * as m from '$lib/paraglide/messages';

interface ReviewMutationsDeps {
	getJobId: () => string;
	getJob: () => BatchJobResponse | null;
	setJob: (job: BatchJobResponse) => void;
	skipJobSync: () => void;
	clearEditStorage: () => void;
	clearEditedMovies: () => void;
	clearPosterPreviewOverrides: () => void;
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
	updateBatchMoviePosterFromURL: (
		jobId: string,
		resultId: string,
		body: { url: string },
	) => Promise<PosterFromURLResponse>;
	getBatchMovieSources: (jobId: string, resultId: string) => Promise<SourceResultsResponse>;
	overrideBatchMovieField: (
		jobId: string,
		resultId: string,
		body: { field: string; source: string },
	) => Promise<FieldOverrideResponse>;
	excludeBatchMovie: (jobId: string, resultId: string) => Promise<unknown>;
	updateBatchMovie: (jobId: string, resultId: string, movie: Movie) => Promise<unknown>;
	updateBatchMoviePosterCrop: (
		jobId: string,
		resultId: string,
		crop: PosterCropBox,
		maxPosterHeight?: number,
	) => Promise<PosterCropResponse>;
	batchExcludeMovies: (
		jobId: string,
		request: BatchExcludeRequest,
	) => Promise<BatchExcludeResponse>;
	bulkRescrapeMovies: (
		jobId: string,
		request: BulkRescrapeRequest,
	) => Promise<BulkRescrapeResponse>;
	getSelectedMovieIds: () => Set<string>;
	clearSelectedMovieIds: () => void;
	deleteSelectedMovieId: (movieId: string) => void;
	toastSuccess: (message: string, duration?: number) => void;
	toastError: (message: string, duration?: number) => void;
}

export function createReviewMutations(deps: ReviewMutationsDeps) {
	const queryClient = deps.getQueryClient();

	function invalidateJobQueries() {
		return Promise.all([
			queryClient.invalidateQueries({ queryKey: ['batch-job', deps.getJobId()] }),
			queryClient.invalidateQueries({ queryKey: ['batch-job-slim', deps.getJobId()] }),
			queryClient.invalidateQueries({ queryKey: ['actresses'] }),
		]);
	}

	// Overlay a poster edit (manual crop / poster-from-URL) onto the job
	// results AND any in-flight editedMovies entries for the same result, so a
	// subsequent whole-movie Save (organizeAll → saveAllEdits → buildMovieToSave
	// sends every field) cannot clobber the server-side crop state with a stale
	// pre-edit snapshot.
	function applyPosterEditToState(resultId: string, edit: PosterEditOverlay) {
		const currentJob = deps.getJob();
		if (!currentJob) return;

		// Server-side poster edits fan out to every file of the movie
		// (PosterEditor iterates FindFilePathsForMovieID) — mirror that here so
		// multi-part siblings never hold stale poster state.
		const targets = new Set(posterEditTargetFilePaths(currentJob.results ?? {}, resultId));

		const updatedJob: BatchJobResponse = {
			...currentJob,
			results: { ...currentJob.results },
		};
		for (const [filePath, result] of Object.entries(updatedJob.results)) {
			const r = result as FileResult;
			if (targets.has(filePath) && r.movie) {
				updatedJob.results[filePath] = { ...r, movie: overlayPosterEdit(r.movie, edit) };
			}
		}
		deps.skipJobSync();
		deps.setJob(updatedJob);

		const editedMovies = deps.getEditedMovies();
		for (const [filePath, movie] of editedMovies) {
			if (targets.has(filePath)) {
				editedMovies.set(filePath, overlayPosterEdit(movie, edit));
			}
		}
	}

	const posterFromUrlMutation = createMutation(() => ({
		mutationFn: async ({ resultId, url }: { resultId: string; url: string }) => {
			return deps.updateBatchMoviePosterFromURL(deps.getJobId(), resultId, { url });
		},
		onSuccess: (data: PosterFromURLResponse, { resultId }) => {
			applyPosterEditToState(resultId, {
				poster_url: data.poster_url,
				cropped_poster_url: data.cropped_poster_url,
				should_crop_poster: false,
				poster_crop_bounds: null,
			});

			const currentResult = deps.getCurrentResult();
			if (currentResult) {
				deps.getPosterPreviewOverrides().set(currentResult.file_path, {
					url: data.cropped_poster_url,
					version: Date.now(),
				});
			}

			void invalidateJobQueries();
		},
		onError: (err: Error) => {
			deps.toastError(m.review_poster_from_screenshot_failed({ error: err.message }));
		},
	}));

	function applyPosterFromUrl(resultId: string, url: string) {
		if (!deps.getJob() || posterFromUrlMutation.isPending) return;
		posterFromUrlMutation.mutate({ resultId, url });
	}

	async function applyPosterFromUrlAsync(resultId: string, url: string) {
		if (!deps.getJob()) return;
		await posterFromUrlMutation.mutateAsync({ resultId, url });
	}

	const excludeMovieMutation = createMutation(() => ({
		mutationFn: async ({ jobId: mutationJobId, resultId }: { jobId: string; resultId: string }) => {
			return deps.excludeBatchMovie(mutationJobId, resultId);
		},
		onSuccess: async (_data, { resultId }) => {
			const job = deps.getJob();
			for (const [, r] of Object.entries(job?.results ?? {})) {
				const fr = r as FileResult;
				if (fr.result_id === resultId) {
					deps.deleteSelectedMovieId(fr.movie_id);
					break;
				}
			}
			deps.toastSuccess(m.review_movie_excluded());
			void invalidateJobQueries();

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
			deps.toastError(m.review_exclude_movie_failed({ error: err.message }));
		},
	}));

	const saveEditsMutation = createMutation(() => ({
		mutationFn: async () => {
			const job = deps.getJob();
			const savePromises = Array.from(deps.getEditedMovies().entries()).map(([filePath, movie]) => {
				const movieToSave = buildMovieToSave(movie);
				const resultId = job?.results?.[filePath]?.result_id;
				if (!resultId) return null;
				return deps.updateBatchMovie(deps.getJobId(), resultId, movieToSave);
			});

			const sent = savePromises.filter((p): p is Promise<unknown> => p !== null);
			if (sent.length > 0) {
				await Promise.all(sent);
			}
			return sent.length;
		},
		onSuccess: async (sent: number) => {
			if (sent > 0) {
				await invalidateJobQueries().catch(() => {});
				deps.toastSuccess(m.review_changes_saved());
			}
			deps.clearEditedMovies();
			deps.clearPosterPreviewOverrides();
			deps.clearEditStorage();
		},
		onError: (err: Error) => {
			deps.toastError(m.review_save_edits_failed({ error: err.message }));
		},
	}));

	const posterCropMutation = createMutation(() => ({
		mutationFn: async ({
			jobId: mutationJobId,
			resultId,
			crop,
			maxPosterHeight,
		}: {
			jobId: string;
			resultId: string;
			crop: PosterCropBox;
			maxPosterHeight?: number;
		}) => {
			return deps.updateBatchMoviePosterCrop(mutationJobId, resultId, crop, maxPosterHeight);
		},
		onSuccess: (response: PosterCropResponse, { resultId }) => {
			applyPosterEditToState(resultId, posterCropOverlayFromResponse(response));

			const currentResultVal = deps.getCurrentResult();
			if (currentResultVal) {
				deps.getPosterPreviewOverrides().set(currentResultVal.file_path, {
					url: response.cropped_poster_url,
					version: Date.now(),
				});

				const cropMetricsVal = deps.getCropMetrics();
				const cropBoxVal = deps.getCropBox();
				if (cropMetricsVal && cropBoxVal) {
					deps
						.getPosterCropStates()
						.set(currentResultVal.file_path, normalizeCropBox(cropBoxVal, cropMetricsVal));
				}
			}

			deps.toastSuccess(m.review_poster_crop_updated());
			deps.setShowPosterCropModal(false);

			void invalidateJobQueries();
		},
		onError: (err: Error) => {
			deps.toastError(err.message || m.review_poster_crop_failed());
		},
	}));

	async function applyPosterCropAsync(jobId: string, resultId: string, crop: PosterCropBox, maxPosterHeight?: number) {
		await posterCropMutation.mutateAsync({ jobId, resultId, crop, maxPosterHeight });
	}

	const bulkExcludeMutation = createMutation(() => ({
		mutationFn: async ({ resultIds }: { resultIds: string[] }) => {
			return deps.batchExcludeMovies(deps.getJobId(), { result_ids: resultIds });
		},
		onSuccess: (data) => {
			if (data.job) {
				deps.skipJobSync();
				deps.setJob(data.job);
			}

			deps.clearSelectedMovieIds();

			if (data.failed.length > 0) {
				deps.toastError(
					m.review_exclude_failed_count({ count: data.failed.length }),
				);
			} else {
				deps.toastSuccess(
					m.review_excluded_count({ count: data.excluded.length }),
				);
			}

			void invalidateJobQueries();
		},
		onError: (err: Error) => {
			deps.toastError(m.review_exclude_movies_failed({ error: err.message }));
		},
	}));

	const bulkRescrapeMutation = createMutation(() => ({
		mutationFn: async ({
			movieIds,
			selectedScrapers,
			preset,
			scalarStrategy,
			arrayStrategy,
		}: {
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
				scalar_strategy: scalarStrategy as
					| 'prefer-nfo'
					| 'prefer-scraper'
					| 'preserve-existing'
					| 'fill-missing-only'
					| 'merge-arrays'
					| undefined,
				array_strategy: arrayStrategy as 'merge' | 'replace' | undefined,
			});
		},
		onSuccess: (data) => {
			if (data.job) {
				deps.skipJobSync();
				deps.setJob(data.job);
			}

			if (data.failed > 0) {
				deps.toastError(m.review_rescrape_failed_count({ count: data.failed }));
			} else {
				deps.toastSuccess(m.review_rescraped_count({ count: data.succeeded }));
			}

			void invalidateJobQueries();
		},
		onError: (err: Error) => {
			deps.toastError(m.review_rescrape_movies_failed({ error: err.message }));
		},
	}));

	// overlayFieldOverride is imported from ./overlay-field-override so it can be
	// unit-tested independently (the .svelte.ts module can't export locals).
	// applyFieldOverrideToResults / applyFieldOverrideToEditedMovies fan the
	// override out to every part of the movie (server-side fanout reference:
	// ApplyFieldOverride → FindFilePathsForMovieID).

	const fieldOverrideMutation = createMutation(() => ({
		mutationFn: async ({
			resultId,
			field,
			source,
		}: {
			resultId: string;
			field: string;
			source: string;
		}) => {
			return deps.overrideBatchMovieField(deps.getJobId(), resultId, { field, source });
		},
		onSuccess: (data: FieldOverrideResponse, { resultId, field, source }) => {
			const currentJob = deps.getJob();
			if (currentJob && data.movie) {
				const updatedJob: BatchJobResponse = {
					...currentJob,
					results: applyFieldOverrideToResults(currentJob.results ?? {}, resultId, field, data),
				};
				deps.skipJobSync();
				deps.setJob(updatedJob);

				// Overlay the overridden field onto any in-flight edits for every
				// part of the movie so a subsequent Save doesn't clobber the override
				// (and unsaved edits to other fields survive per part).
				applyFieldOverrideToEditedMovies(
					deps.getEditedMovies(),
					currentJob.results ?? {},
					resultId,
					field,
					data.movie,
				);
			}
			deps.toastSuccess(m.review_field_replaced({ field, source }));
			void invalidateJobQueries();
		},
		onError: (err: Error) => {
			deps.toastError(m.review_field_override_failed({ error: err.message }));
			},
	}));

	async function applyFieldOverrideAsync(resultId: string, field: string, source: string) {
		if (!deps.getJob()) return;
		await fieldOverrideMutation.mutateAsync({ resultId, field, source });
	}

	return {
		posterFromUrlMutation,
		applyPosterFromUrl,
		applyPosterFromUrlAsync,
		excludeMovieMutation,
		bulkExcludeMutation,
		bulkRescrapeMutation,
		saveEditsMutation,
		posterCropMutation,
		applyPosterCropAsync,
		fieldOverrideMutation,
		applyFieldOverrideAsync,
	};
}
