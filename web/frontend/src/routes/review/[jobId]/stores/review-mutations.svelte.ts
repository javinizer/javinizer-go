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
import { overlayFieldOverride } from './overlay-field-override';
import {
	applyCropEcho,
	clearCropGeometry,
	rescrapeClearedMovieKeys,
	siblingResultFilePaths,
} from './poster-crop-sync';
import { buildMovieToSave, rebaseOverlayOntoMovie } from './save-helpers';
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
	updateBatchMovie: (jobId: string, resultId: string, movie: Movie, expectedResultRevision?: number | Record<string, number>) => Promise<unknown>;
	updateBatchMoviePosterCrop: (
		jobId: string,
		resultId: string,
		crop: PosterCropBox,
		maxPosterHeight?: number,
		identity?: { revision: number; fingerprint: string },
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

	const posterFromUrlMutation = createMutation(() => ({
		mutationFn: async ({ resultId, url }: { resultId: string; url: string }) => {
			return deps.updateBatchMoviePosterFromURL(deps.getJobId(), resultId, { url });
		},
		onSuccess: (data: PosterFromURLResponse, { resultId }) => {
			const currentJob = deps.getJob();
			if (currentJob) {
				// The backend applies the new poster to every part of the movie —
				// sync siblings too, or a stale sibling overlay would re-upload the
				// old poster (and crop state) on the next save.
				const partPaths = siblingResultFilePaths(
					currentJob.results as Record<string, FileResult>,
					resultId,
				);
				const fromUrlEcho = (movie: Movie): Movie =>
					clearCropGeometry({
						...movie,
						poster_url: data.poster_url,
						cropped_poster_url: data.cropped_poster_url,
						should_crop_poster: false,
					});
				const updatedJob: BatchJobResponse = {
					...currentJob,
					results: { ...currentJob.results },
				};
				for (const filePath of partPaths) {
					const r = updatedJob.results[filePath] as FileResult;
					if (r?.movie) {
						updatedJob.results[filePath] = { ...r, movie: fromUrlEcho(r.movie) };
					}
				}
				// D12: advance the CAS baseline ONLY for the target part —
				// siblings' fresh revisions arrive via the invalidated query
				// (copying the target's revision onto them would lie, codex
				// r24).
				// Advance EVERY family part's CAS baseline from the committed
				// revision map (codex r26); fall back to the single target
				// field for older servers.
				const advanceRevision = (filePath: string, rev: number) => {
					const r = updatedJob.results[filePath] as FileResult | undefined;
					if (r) updatedJob.results[filePath] = { ...r, revision: rev };
				};
				if (data.revisions && Object.keys(data.revisions).length > 0) {
					const resultToPath = new Map(
						Object.entries(updatedJob.results).map(([fp, r]) => [
							(r as FileResult).result_id,
							fp,
						] as const),
					);
					for (const [rid, rev] of Object.entries(data.revisions)) {
						const fp = resultToPath.get(rid);
						if (fp) advanceRevision(fp, rev);
					}
				} else if (data.revision !== undefined) {
					for (const [filePath, r0] of Object.entries(updatedJob.results)) {
						const r = r0 as FileResult;
						if (r?.result_id === resultId) {
							updatedJob.results[filePath] = { ...r, revision: data.revision };
						}
					}
				}
				deps.skipJobSync();
				deps.setJob(updatedJob);

				const editedMovies = deps.getEditedMovies();
				for (const filePath of partPaths) {
					const movie = editedMovies.get(filePath);
					if (movie) {
						editedMovies.set(filePath, fromUrlEcho(movie));
					}
				}
			}

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
			// POSTER-WRITE-HARDENING D1/D14: the backend commits a whole-movie
			// save transactionally for EVERY part of the family — fan out ONE
			// request per movie, not one per part (parts sharing a movie_id are
			// deduped to the first part's result).
			// POSTER-WRITE-HARDENING D1/D14: the backend commits a whole-movie
			// save transactionally for EVERY part of the family — fan out ONE
			// request per movie, not one per part. Keep the LATEST edit entry
			// per family (codex r9 P2): Map iteration is insertion-ordered, so
			// if the user edited sibling parts differently, only the newest
			// overlay is the intent worth persisting.
			const latestByFamily = new Map<string, [string, Movie]>();
			const familyPathsByKey = new Map<string, string[]>();
			for (const [filePath, movie] of deps.getEditedMovies()) {
				const resultEntry = job?.results?.[filePath];
				const resultId = resultEntry?.result_id;
				if (!resultId) continue;
				const movieId = resultEntry?.movie_id ?? '';
				// codex r43 FE P2: family dedup must fold case like sameFamily
				// (and the backend resultstore index) — raw keys race two saves on
				// case-variant multipart siblings (one 409s or overlays persist).
				const key = movieId !== '' ? movieId.toLowerCase() : '__unpathed__' + filePath;
				// Map.set on an existing key keeps the ORIGINAL insertion
				// position; delete+set moves the entry to the tail so sibling
				// edit order A,B,A resolves to A's newest overlay (codex r9-II).
				latestByFamily.delete(key);
				latestByFamily.set(key, [filePath, movie]);
				// codex P2-E: family membership must be captured pre-save — the
				// post-save refetch rekeys results, so movie_id lookups miss the
				// clear below. FS paths stay stable through renames.
				familyPathsByKey.set(key, [...(familyPathsByKey.get(key) ?? []), filePath]);
			}
			// codex P1 (rebase): capture each edited path's PRE-SAVE server baseline.
			// When a save is rejected and the refetch installs newer revisions, keeping
			// the stale whole-movie overlay against the fresh baseline would let the
			// next save pass CAS and clobber the concurrent edit — onSuccess rebases
			// the user's field deltas onto the refreshed movie instead.
			const preSaveServer = new Map<string, Movie | undefined>();
			for (const fp of deps.getEditedMovies().keys()) {
				preSaveServer.set(fp, (job?.results?.[fp] as FileResult | undefined)?.movie);
			}
			const savePromises = Array.from(latestByFamily.entries()).map(([key, [filePath, movie]]) => {
				const resultEntry = job?.results?.[filePath];
				if (!resultEntry?.result_id) return null;
				const movieToSave = buildMovieToSave(movie);
				// Codex r20+r39: multipart families need the whole family revision
				// map (one sibling's stale baseline would otherwise silently pass
				// target-only CAS and overwrite a newer sibling edit).
				const partsRevisions: Record<string, number> = {};
				for (const [fp, r0] of Object.entries(job?.results ?? {})) {
					const r = r0 as FileResult;
					// codex P2-G: the backend folds movie IDs to lower case — a
					// case-variant sibling MUST bundle into the family CAS here.
					const sameFamily =
						(r.movie_id ?? '').toLowerCase() === (resultEntry?.movie_id ?? '').toLowerCase();
					if (sameFamily && r.result_id && typeof r.revision === 'number') {
						partsRevisions[r.result_id] = r.revision;
					}
				}
				const isMultipart = Object.keys(partsRevisions).length > 1;
				return deps.updateBatchMovie(
					deps.getJobId(),
					resultEntry.result_id,
					movieToSave,
					isMultipart ? partsRevisions : resultEntry.revision,
				);
			});

const ops = Array.from(latestByFamily.entries());
			const settled = await Promise.allSettled(savePromises.filter((p): p is Promise<unknown> => p !== null));
			// codex P2-F: paths come by family-key lookup — latestByFamily reorders
			// (delete+set) on interleaved sibling edits, so positional indexing of
			// familyPathsByKey would misalign them.
			return {
				rows: settled.map((s, i) => ({
					key: ops[i]?.[0] ?? '',
					status: s.status,
					paths: familyPathsByKey.get(ops[i]?.[0] ?? '') ?? [],
				})),
				preSaveServer,
			};
		},
		onSuccess: async (payload: {
			rows: Array<{ key: string; status: string; paths: string[] }>;
			preSaveServer: Map<string, Movie | undefined>;
		}) => {
			const ops0 = payload.rows ?? [];
			const preSaveServer = payload.preSaveServer;
			const failed = ops0.filter((r) => r.status === 'rejected');
			const succeeded = ops0.filter((r) => r.status === 'fulfilled');
			// Always refetch after this batch: even the all-rejected case must
			// advance the CAS baseline or a same-session retry loops on the
			// same stale revision (codex r38).
			// codex P3-B: if the refetch itself failed, the pane still carries
			// PRE-save baselines; deleting overlays now would produce a 409 storm
			// the next save. Keep them when the refresh failed (retry-prone but correct).
			// codex P3-D: paused refetches keep dataUpdatedAt while reporting a
			// prior success status. Gate on an ADVANCED dataUpdatedAt — only a
			// completed fetch counts as refreshed.
			const qk = ['batch-job', deps.getJobId()];
			const before = queryClient.getQueryState(qk)?.dataUpdatedAt ?? 0;
			await invalidateJobQueries().catch(() => {});
			const post = queryClient.getQueryState(qk);
			const refreshed = (() => {
				if (post?.status !== 'success') return false;
				return (post.dataUpdatedAt ?? 0) > before;
			})();
			// codex cloud P2 (@338): fulfilled families clear on PATCH SUCCESS alone — a
			// failed refresh never strands them (a stale whole-movie overlay can
			// resend the old snapshot and overwrite intervening changes).
			if (succeeded.length > 0) {
				const editedMovies = deps.getEditedMovies();
				for (const ok of succeeded) {
					// codex P2-E: delete by PRE-SAVE family membership (immunity to the
					// post-commit rekey the refetch has already installed).
					for (const fp of ok.paths) editedMovies.delete(fp);
				}
				deps.toastSuccess(m.review_changes_saved());
			}
			if (failed.length > 0) {
				// Partial failure: clean families were cleared above; rejected
				// overlays stay + baseline advance via refetch, so retry works.
				deps.toastError(m.review_save_edits_failed({ error: `${failed.length} movie(s) rejected` }));
			}
			if (failed.length > 0 && refreshed) {
				// codex P1: rebase rejected overlays onto the refreshed server movie —
				// otherwise the next save pairs the stale overlay with a FRESH CAS
				// revision and silently overwrites whatever the concurrent op changed.
				const freshJob = queryClient.getQueryData<BatchJobResponse>(qk);
				const editedMovies = deps.getEditedMovies();
				for (const bad of failed) {
					for (const fp of bad.paths) {
						const overlay = editedMovies.get(fp);
						const baseline = preSaveServer.get(fp);
						const freshMovie = (freshJob?.results?.[fp] as FileResult | undefined)?.movie;
						if (overlay && baseline && freshMovie) {
							editedMovies.set(fp, rebaseOverlayOntoMovie(baseline, overlay, freshMovie));
						}
					}
				}
			}
			deps.clearPosterPreviewOverrides();
			// codex r39: keep REJECTED edits reload-safe — write only the
			// surviving rejected set back to sessionStorage instead of
			// clearEditStorage()'s blanket wipe.
			if (failed.length > 0) {
				const remaining: Record<string, Movie> = {};
				for (const [fp, mv] of deps.getEditedMovies()) remaining[fp] = mv;
				try {
					if (Object.keys(remaining).length > 0) {
						sessionStorage.setItem(`javinizer.review.editedMovies.${deps.getJobId()}`, JSON.stringify(remaining));
					} else {
						sessionStorage.removeItem(`javinizer.review.editedMovies.${deps.getJobId()}`);
					}
				} catch {
					// storage full → fall back to clearEditStorage behavior
					deps.clearEditStorage();
				}
			} else if (refreshed) {
				// codex P3-C: untouched overlays need their session copy intact —
				// otherwise a pre-refetch page reload loses the pane's retained
				// edits while the on-screen state still shows them.
				deps.clearEditStorage();
			}
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
			identity,
		}: {
			jobId: string;
			resultId: string;
			crop: PosterCropBox;
			maxPosterHeight?: number;
			identity?: { revision: number; fingerprint: string };
		}) => {
			return deps.updateBatchMoviePosterCrop(mutationJobId, resultId, crop, maxPosterHeight, identity);
		},
		onSuccess: (response: PosterCropResponse, { resultId }) => {
			// Sync the server-echoed crop state into the visible job-result state
			// and the pending editedMovies overlay, so a pre-organize saveAllEdits()
			// cannot re-upload stale pre-crop intent (should_crop_poster=true) and
			// clobber the crop. A null echo (legacy already-cropped source) drops
			// the key — the server holds no applyable geometry either.
			const job = deps.getJob();
			if (job) {
				// Same movie ID = same poster: the server applied the crop to every
				// part, so sync every part here (multipart; see siblingResultFilePaths).
				const partPaths = siblingResultFilePaths(
					job.results as Record<string, FileResult>,
					resultId,
				);
				const updatedJob: BatchJobResponse = { ...job, results: { ...job.results } };
				for (const filePath of partPaths) {
					const r = updatedJob.results[filePath] as FileResult;
					if (r?.movie) {
						updatedJob.results[filePath] = { ...r, movie: applyCropEcho(r.movie, response) };
					}
				}
				const advanceRevision = (filePath: string, rev: number) => {
					const r = updatedJob.results[filePath] as FileResult | undefined;
					if (r) updatedJob.results[filePath] = { ...r, revision: rev };
				};
				if (response.revisions && Object.keys(response.revisions).length > 0) {
					const resultToPath = new Map(
						Object.entries(updatedJob.results).map(([fp, r]) => [
							(r as FileResult).result_id,
							fp,
						] as const),
					);
					for (const [rid, rev] of Object.entries(response.revisions)) {
						const fp = resultToPath.get(rid);
						if (fp) advanceRevision(fp, rev);
					}
				} else if (response.revision !== undefined) {
					for (const [filePath, r0] of Object.entries(updatedJob.results)) {
						const r = r0 as FileResult;
						if (r?.result_id === resultId) {
							updatedJob.results[filePath] = { ...r, revision: response.revision };
						}
					}
				}
				deps.skipJobSync();
				deps.setJob(updatedJob);

				const editedMovies = deps.getEditedMovies();
				for (const filePath of partPaths) {
					const movie = editedMovies.get(filePath);
					if (movie) {
						editedMovies.set(filePath, applyCropEcho(movie, response));
					}
				}
			}

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

	async function applyPosterCropAsync(
		jobId: string,
		resultId: string,
		crop: PosterCropBox,
		maxPosterHeight?: number,
		identity?: { revision: number; fingerprint: string },
	) {
		await posterCropMutation.mutateAsync({ jobId, resultId, crop, maxPosterHeight, identity });
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
			if (data.job) {
				deps.skipJobSync();
				deps.setJob(data.job);
			}

			// Rescrape clears stored crop geometry server-side; mirror that into
			// pending overlays so a later save cannot re-upload pre-rescrape
			// bounds. Only movies that actually succeeded — a failed rescrape kept
			// its server-side geometry, and an explicit null here would clear it.
			const succeededIds = rescrapeClearedMovieKeys(data.results ?? []);
			const jobSnapshot = data.job ?? deps.getJob();
			if (jobSnapshot && succeededIds.size > 0) {
				const editedMovies = deps.getEditedMovies();
				for (const [filePath, result] of Object.entries(jobSnapshot.results ?? {})) {
					const fr = result as FileResult;
					// codex cloud P1: fold to match poster-crop-sync keys — an
					// overlay keyed by the OLD ID spelling must clear too.
					if (!succeededIds.has((fr.movie_id ?? '').toLowerCase())) continue;
					const pending = editedMovies.get(filePath);
					if (pending && (pending.poster_crop_bounds != null || pending.poster_crop_source_full)) {
						editedMovies.set(filePath, clearCropGeometry(pending));
					}
				}
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
					results: { ...currentJob.results },
				};
				for (const [filePath, result] of Object.entries(updatedJob.results)) {
					const r = result as FileResult;
					if (r.result_id === resultId) {
						updatedJob.results[filePath] = {
							...r,
							movie: data.movie,
							field_sources: data.field_sources ?? r.field_sources,
							actress_sources: data.actress_sources ?? r.actress_sources,
							// codex r24: advance the CAS baseline without a refetch.
							...(data.revision !== undefined ? { revision: data.revision } : {}),
						};
					}
				}
				if (data.revisions && Object.keys(data.revisions).length > 0) {
					const resultToPath = new Map(
						Object.entries(updatedJob.results).map(([fp, r]) => [(r as FileResult).result_id, fp] as const),
					);
					for (const [rid, revision] of Object.entries(data.revisions)) {
						const filePath = resultToPath.get(rid);
						if (filePath) {
							const r = updatedJob.results[filePath] as FileResult;
							updatedJob.results[filePath] = { ...r, revision };
						}
					}
				}
				deps.skipJobSync();
				deps.setJob(updatedJob);

				// Overlay the overridden field onto any in-flight edit so a subsequent
				// Save doesn't clobber the override (and unsaved edits to other fields survive).
				const editedMovies = deps.getEditedMovies();
				for (const [filePath, movie] of editedMovies) {
					const editedResultId = currentJob.results?.[filePath]?.result_id;
					if (editedResultId === resultId && data.movie) {
						const merged: Movie = { ...movie };
						overlayFieldOverride(merged, field, data.movie);
						editedMovies.set(filePath, merged);
					}
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
