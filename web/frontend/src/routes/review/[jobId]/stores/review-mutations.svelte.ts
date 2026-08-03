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
import { overlayFieldOverride, overlayPosterEdit, posterCropOverlayFromResponse, posterEditTargetFilePaths, applyFieldOverrideToEditedMovies, applyFieldOverrideToResults, sameMovieIdentity, type PosterEditOverlay } from './overlay-field-override';
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
	// clearPosterCropStates drops stored crop geometry for the given file
	// paths. posterCropStates is keyed by file path and persisted to
	// localStorage, so any mutation whose poster edit changes the source
	// image server-side (poster-from-URL, source field override, rescrape)
	// must invalidate the geometry measured against the OLD image —
	// otherwise reopening the crop modal restores it
	// (handlePosterCropImageLoad) and applying submits stale bounds.
	clearPosterCropStates: (filePaths: string[]) => void;
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

// Fields whose override can invalidate the server-side crop — mirrored from
// field_override.go (poster_url / cover_url clear CropBounds on an
// effective-source change and re-derive the intent; should_crop_poster
// clears on a flip; id re-keys the cached assets). Combined with the
// response's poster_crop_bounds as the server's actual invalidation signal.
const CROP_AFFECTING_OVERRIDE_FIELDS = new Set([
	'id',
	'poster_url',
	'cover_url',
	'should_crop_poster',
]);

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

	// Resolve the file path a poster mutation targeted from the REQUEST's
	// resultId. onSuccess must NOT read the CURRENT result: the user may have
	// navigated to another movie while the request was in flight, and keying
	// the preview override / crop state off the current result would land that
	// state under the wrong movie's file_path (applyPosterEditToState already
	// keys off the request's resultId for the same reason).
	function filePathForResultId(resultId: string): string | undefined {
		for (const [filePath, r] of Object.entries(deps.getJob()?.results ?? {})) {
			if ((r as FileResult).result_id === resultId) return filePath;
		}
		return undefined;
	}

	const posterFromUrlMutation = createMutation(() => ({
		mutationFn: async ({ resultId, url }: { resultId: string; url: string }) => {
			return deps.updateBatchMoviePosterFromURL(deps.getJobId(), resultId, { url });
		},
		onSuccess: (data: PosterFromURLResponse, { resultId }) => {
			// should_crop_poster comes from the SERVER (derived in
			// PosterEditor.cropIntentAfterPosterFromURL from the prior effective
			// source / provenance), NOT hard-coded: the temp preview is always
			// auto-cropped, so overlaying false for a cover-backed prior would
			// desync Organize's default crop from the preview — and a later
			// whole-movie Save would resubmit the false as a deliberate edit.
			// The reset-poster flow routes through this mutation too, so it
			// inherits the same server-derived intent for the restored URL.
			applyPosterEditToState(resultId, {
				poster_url: data.poster_url,
				cropped_poster_url: data.cropped_poster_url,
				should_crop_poster: data.should_crop_poster,
				poster_crop_bounds: null,
				// Server-echoed revert baseline (backupPosterOriginals may have
				// stamped it just now on a legacy result): carry it, or a Save
				// issued before the refetch applies resubmits empty originals
				// through UpdateMovie and destroys the fresh reset target.
				original_poster_url: data.original_poster_url,
				original_cropped_poster_url: data.original_cropped_poster_url,
				original_should_crop_poster: data.original_should_crop_poster,
			});

			const targetFilePath = filePathForResultId(resultId);
			if (targetFilePath) {
				deps.getPosterPreviewOverrides().set(targetFilePath, {
					url: data.cropped_poster_url,
					version: Date.now(),
				});
			}

			// The replace/reset just installed a NEW source image server-side
			// (bounds cleared — the overlay above mirrors that), so the stored
			// crop geometry was measured against the OLD image: drop it for
			// every part of the movie, or reopening the crop modal would
			// restore it and a blind Apply would submit stale bounds against
			// the new image. Fan out matches applyPosterEditToState (siblings
			// share the {movieID}-full.jpg the source replaced).
			deps.clearPosterCropStates(
				posterEditTargetFilePaths(deps.getJob()?.results ?? {}, resultId),
			);

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
			const targets: { filePath: string; promise: Promise<unknown> }[] = [];
			for (const [filePath, movie] of deps.getEditedMovies().entries()) {
				const movieToSave = buildMovieToSave(movie);
				const resultId = job?.results?.[filePath]?.result_id;
				if (!resultId) continue;
				targets.push({
					filePath,
					promise: deps.updateBatchMovie(deps.getJobId(), resultId, movieToSave),
				});
			}

			// Promise.allSettled, NOT Promise.all: with N pending edits a single
			// failing PATCH leaves the EARLIER ones already committed
			// server-side. Promise.all rejected without distinguishing them, so
			// onError could neither drop the confirmed saves from the pending
			// map nor refetch them — the UI diverged until an unrelated
			// invalidation. Settle every request and report per-file outcomes.
			const settled = await Promise.allSettled(targets.map((t) => t.promise));
			const succeededPaths: string[] = [];
			const failures: { filePath: string; message: string }[] = [];
			settled.forEach((outcome, i) => {
				if (outcome.status === 'fulfilled') {
					succeededPaths.push(targets[i].filePath);
				} else {
					failures.push({
						filePath: targets[i].filePath,
						message:
							outcome.reason instanceof Error ? outcome.reason.message : String(outcome.reason),
					});
				}
			});
			if (failures.length > 0) {
				const err = new Error(
					failures.map((f) => `${f.filePath}: ${f.message}`).join('; '),
				) as Error & { succeededPaths: string[] };
				err.succeededPaths = succeededPaths;
				throw err;
			}
			return targets.length;
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
			// Partial failure: drop ONLY the edits the server confirmed from the
			// pending map (the failed ones stay for retry), and refetch the job
			// — the confirmed PATCHes are committed server-side, so the client
			// must adopt their state instead of showing the pre-save snapshot.
			const succeededPaths = (err as { succeededPaths?: string[] }).succeededPaths;
			if (Array.isArray(succeededPaths) && succeededPaths.length > 0) {
				const editedMovies = deps.getEditedMovies();
				for (const fp of succeededPaths) {
					editedMovies.delete(fp);
				}
			}
			void invalidateJobQueries();
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
			// Capture the metrics WITH the request: the submitted crop box is
			// in THIS source's pixel space, and drag events stay live while the
			// save is in flight — reading getCropMetrics()/getCropBox() at
			// onSuccess time would persist whatever the box has drifted to
			// (possibly another movie's box) under THIS request's file path.
			const metrics = deps.getCropMetrics();
			const response = await deps.updateBatchMoviePosterCrop(mutationJobId, resultId, crop, maxPosterHeight);
			return { response, metrics };
		},
		onSuccess: ({ response, metrics }, { resultId, crop }) => {
			applyPosterEditToState(resultId, posterCropOverlayFromResponse(response));

			const targetFilePath = filePathForResultId(resultId);
			if (targetFilePath) {
				deps.getPosterPreviewOverrides().set(targetFilePath, {
					url: response.cropped_poster_url,
					version: Date.now(),
				});

				if (metrics) {
					const normalized = normalizeCropBox(crop, metrics);
					// The server applies the crop to EVERY file of the movie
					// (applyPosterEditToState above fans out the same way), so
					// persist the geometry under all parts — otherwise reopening
					// the crop editor on a multi-part sibling found no local
					// state and fell back to a blind default box.
					for (const fp of posterEditTargetFilePaths(deps.getJob()?.results ?? {}, resultId)) {
						deps.getPosterCropStates().set(fp, normalized);
					}
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
		onSuccess: (data, { movieIds }) => {
			// Capture the rescraped movies' file paths BEFORE setJob: results
			// are keyed by file path, which survives a rescrape, while a rekey
			// changes movie_id — resolving targets off the replaced job would
			// miss every rekeyed result.
			const rescrapedPaths = new Set<string>();
			// Case-insensitive match (sameMovieIdentity): the backend resolves
			// each requested ID through its folded movie-ID indexKey, so a
			// case-variant sibling's results ARE rescraped server-side and must
			// be captured here or they stay stale until the async refetch.
			for (const [fp, r] of Object.entries(deps.getJob()?.results ?? {})) {
				if (movieIds.some((mid) => sameMovieIdentity(mid, (r as FileResult).movie_id))) {
					rescrapedPaths.add(fp);
				}
			}

			if (data.job) {
				deps.skipJobSync();
				deps.setJob(data.job);
			}

			// A rescrape can replace the effective poster source; the server
			// clears CropBounds exactly then (merge keeps them for an unchanged
			// source — see mergeRescrapeMovie). Mirror that signal locally so
			// reopening the crop modal never restores geometry measured
			// against the old image.
			deps.clearPosterCropStates(
				Array.from(rescrapedPaths).filter((fp) => {
					const newMovie = (data.job?.results?.[fp] as FileResult | undefined)?.movie;
					return newMovie != null && (newMovie.poster_crop_bounds ?? null) === null;
				}),
			);

			if (data.failed > 0) {
				deps.toastError(m.review_rescrape_failed_count({ count: data.failed }));
			} else {
				deps.toastSuccess(m.review_rescraped_count({ count: data.succeeded }));
			}

			void invalidateJobQueries();
		},
		onError: (err: Error) => {
			deps.toastError(m.review_rescrape_movies_failed({ error: err.message }));
			// A persist-failure 500 carries the authoritative per-item results
			// and the post-rollback job in its body (batchRescrapeMovies answers
			// the BulkRescrapeResponse with status 500), but ApiError discards
			// everything except the message — so adopt the server state the
			// only way available here: refetch. Without this the client keeps
			// the pre-rescrape UI for movies that SUCCEEDED, and shows
			// pre-rollback edits as current for the ones that reverted.
			void invalidateJobQueries();
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

				// Crop-affecting overrides (poster_url / cover_url source picks,
				// an explicit should_crop_poster re-pick, an id rekey) can
				// invalidate the crop SERVER-side; the server's own signal is the
				// response's poster_crop_bounds (omitted/nil = cleared — see
				// overlayFieldOverride's absent-key convention). Drop the local
				// crop geometry measured against the old image exactly then, and
				// keep it when the server kept the bounds (same effective source,
				// or the id-rekey asset MOVE — same bytes at the new key).
				// Targets resolve from the PRE-override job: an id override
				// re-keys movie_id in the overlaid results.
				if (
					CROP_AFFECTING_OVERRIDE_FIELDS.has(field) &&
					(data.movie.poster_crop_bounds ?? null) === null
				) {
					deps.clearPosterCropStates(
						posterEditTargetFilePaths(currentJob.results ?? {}, resultId),
					);
				}
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
