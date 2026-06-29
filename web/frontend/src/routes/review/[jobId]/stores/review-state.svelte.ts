import { onDestroy, onMount, untrack } from 'svelte';
import { SvelteMap, SvelteSet } from 'svelte/reactivity';
import { browser } from '$app/environment';
import { goto } from '$app/navigation';
import type { Page } from '@sveltejs/kit';
import { createQuery, useQueryClient } from '@tanstack/svelte-query';
import { apiClient } from '$lib/api/client';
import { createConfigQuery } from '$lib/query/queries';
import type {
	BatchJobResponse,
	FileResult,
	Movie,
	Scraper,
	UpdateRequest,
	CompletenessConfig,
} from '$lib/api/types';
import { toastStore } from '$lib/stores/toast';
import { confirmDialog } from '$lib/stores/dialog.svelte';
import { websocketStore } from '$lib/stores/websocket';
import {
	createOrganizeController,
	type FileStatus,
	type OrganizeOperation,
} from '../logic/organize-controller';
import {
	createRescrapeController,
	type ArrayStrategy,
	type ScalarStrategy,
} from '../logic/rescrape-controller';
import {
	createPosterCropController,
	type PosterCropDragState,
} from '../logic/poster-crop-controller';
import { createReviewPageController } from '../logic/review-page-controller';
import {
	normalizeCropBox,
	type PosterCropBox,
	type PosterCropMetrics,
	type PosterCropState,
	type PosterPreviewOverride,
} from '../review-utils';
import equal from 'fast-deep-equal';
import { calculateCompleteness, type CompletenessTier } from '$lib/utils/completeness';
import { nextOrganizeProgress } from '$lib/utils/job-progress';
import { createReviewMutations } from './review-mutations.svelte';

interface MovieGroup {
	movieId: string;
	results: FileResult[];
	primaryResult: FileResult;
}

export function createReviewState(pageStore: Page) {
	let jobId = $derived(pageStore.params.jobId as string);

	const queryClient = useQueryClient();

	const jobQuery = createQuery(() => ({
		queryKey: ['batch-job', jobId],
		queryFn: () => apiClient.getBatchJob(jobId, true),
		placeholderData: (prev) => prev,
	}));

	let job = $state<BatchJobResponse | null>(null);
	let skipJobSync = false;

	$effect(() => {
		const data = jobQuery.data;
		const isPending = jobQuery.isPending;
		const isPlaceholder = jobQuery.isPlaceholderData;
		untrack(() => {
			if (skipJobSync) {
				skipJobSync = false;
				return;
			}
			if (data) {
				job = JSON.parse(JSON.stringify(data));
			} else if (isPending && !isPlaceholder) {
				job = null;
			}
		});
	});

	let loading = $derived(jobQuery.isPending);
	let error = $derived(jobQuery.error?.message ?? null);

	const configQuery = createConfigQuery();
	let config = $derived(configQuery.data ?? null);

	let completenessConfig = $derived<CompletenessConfig | undefined>(
		config?.metadata?.completeness?.enabled ? config.metadata.completeness : undefined,
	);

	let currentMovieIndex = $state(0);
	let editedMovies = new SvelteMap<string, Movie>();
	let selectedMovieIds = new SvelteSet<string>();
	let lastSelectedMovieId = $state<string | null>(null);
	let completenessFilter = new SvelteSet<CompletenessTier>(['incomplete', 'partial', 'complete']);
	let selectionMode = $state(false);
	let organizing = $state(false);
	let destinationPath = $state('');
	let organizeOperation = $state<OrganizeOperation>('move');
	let showDestinationBrowser = $state(false);
	let tempDestinationPath = $state('');
	let showTrailerModal = $state(false);

	let isUpdateMode = $derived(job?.update ?? false);
	let showFieldScraperSources = $state(false);
	const SHOW_FIELD_SCRAPER_SOURCES_KEY = 'javinizer.review.showFieldScraperSources';
	const VIEW_MODE_KEY = 'javinizer.review.viewMode';
	let viewMode = $state<'detail' | 'grid-poster' | 'grid-cover'>('detail');
	let viewModeInitialized = $state(false);
	let posterCropStatesStorageKey = $derived(`javinizer.review.posterCropStates.${jobId}`);
	let editedMoviesStorageKey = $derived(`javinizer.review.editedMovies.${jobId}`);
	let posterPreviewOverridesStorageKey = $derived(
		`javinizer.review.posterPreviewOverrides.${jobId}`,
	);

	let organizeProgress = $state(0);
	let organizeStatus = $state<'idle' | 'organizing' | 'completed' | 'failed'>('idle');
	let fileStatuses = new SvelteMap<string, FileStatus>();
	let expectedOrganizeFilePaths = $state<string[]>([]);

	const showCoverPanel = $derived(config?.output?.download_cover ?? true);
	const showPosterPanel = $derived(config?.output?.download_poster ?? true);
	const showTrailerPanel = $derived(config?.output?.download_trailer ?? true);
	const showScreenshotsPanel = $derived(config?.output?.download_extrafanart ?? true);

	let showImageViewer = $state(false);
	let imageViewerImages = $state<string[]>([]);
	let imageViewerIndex = $state(0);
	let imageViewerTitle = $state<string | undefined>(undefined);

	let showAllSidebarScreenshots = $state(false);
	let showFullSourcePath = $state(false);

	let forceOverwrite = $state(false);
	let preserveNfo = $state(false);
	let skipNfo = $state(false);
	let skipDownload = $state(false);

	let showImagePanelContent = $state(true);
	let showAllPreviewScreenshots = $state(false);

	let showPosterCropModal = $state(false);
	let posterCropLoadError = $state<string | null>(null);
	let cropSourceURL = $state('');
	let cropImageElement = $state<HTMLImageElement | null>(null);
	let cropMetrics = $state<PosterCropMetrics | null>(null);
	let cropBox = $state<PosterCropBox | null>(null);
	let maxPosterHeight = $state<number | null>(null);
	let cropDragState = $state<PosterCropDragState | null>(null);
	let cropApplying = $state(false);
	let posterPreviewOverrides = new SvelteMap<string, PosterPreviewOverride>();
	let posterCropStates = new SvelteMap<string, PosterCropState>();

	$effect(() => {
		const jobData = jobQuery.data;
		if (jobData) {
			untrack(() => {
				if (jobData.destination && !destinationPath) {
					destinationPath = jobData.destination;
				}
			});
		}
	});

	let availableScrapers: Scraper[] = $state([]);
	let showRescrapeModal = $state(false);
	let rescrapeMovieId = $state('');
	let rescrapeResultId = $state('');
	let rescrapeSelectedScrapers: string[] = $state([]);
	let rescrapingStates = new SvelteMap<string, boolean>();
	let manualSearchMode = $state(false);
	let manualSearchInput = $state('');
	let rescrapeTargetResult = $state<FileResult | null>(null);

	let rescrapePreset: string | undefined = $state(undefined);
	let rescrapeScalarStrategy: ScalarStrategy = $state('prefer-nfo');
	let rescrapeArrayStrategy: ArrayStrategy = $state('merge');

	let bulkRescraping = $state(false);
	let bulkRescrapeProgress: { movie_id: string; status: string; error?: string }[] = $state([]);
	let bulkRescrapeMovieIds: string[] = $state([]);

	const movieGroups = $derived<MovieGroup[]>(
		job
			? (() => {
					const excluded = (job as BatchJobResponse).excluded || {};
					const allResults = (
						Object.values((job as BatchJobResponse).results) as FileResult[]
					).filter((r) => {
						if (r.status !== 'completed' || !r.movie) {
							return false;
						}
						if (excluded[r.file_path]) {
							return false;
						}
						return true;
					});

					const grouped = new Map<string, FileResult[]>();
					for (const result of allResults) {
						const movieId = result.movie_id;
						if (!grouped.has(movieId)) {
							grouped.set(movieId, []);
						}
						grouped.get(movieId)!.push(result);
					}

					return Array.from(grouped.entries()).map(([movieId, results]) => ({
						movieId,
						results,
						primaryResult: results[0],
					}));
				})()
			: [],
	);

	const failedResults = $derived<FileResult[]>(
		job
			? (Object.values((job as BatchJobResponse).results) as FileResult[]).filter(
					(r) => r.status === 'failed' && !((job as BatchJobResponse).excluded ?? {})[r.file_path],
				)
			: [],
	);

	const tierCounts = $derived.by<Record<CompletenessTier, number>>(() => {
		const counts: Record<CompletenessTier, number> = { incomplete: 0, partial: 0, complete: 0 };
		for (const group of movieGroups) {
			const movie = getEffectiveMovie(group.primaryResult.file_path, group.primaryResult.movie);
			if (movie) {
				const { tier } = calculateCompleteness(movie, completenessConfig);
				counts[tier]++;
			}
		}
		return counts;
	});

	const filteredMovieGroups = $derived<MovieGroup[]>(
		completenessFilter.size === 3
			? movieGroups
			: movieGroups.filter((group) => {
					const movie = getEffectiveMovie(group.primaryResult.file_path, group.primaryResult.movie);
					if (!movie) return false;
					const { tier } = calculateCompleteness(movie, completenessConfig);
					return completenessFilter.has(tier);
				}),
	);

	const movieResults = $derived<FileResult[]>(movieGroups.map((g) => g.primaryResult));

	const currentMovieGroup = $derived<MovieGroup | undefined>(movieGroups[currentMovieIndex]);
	const currentResult = $derived<FileResult | undefined>(currentMovieGroup?.primaryResult);
	const currentMovie = $derived<Movie | null>(
		currentResult && currentResult.movie
			? editedMovies.get(currentResult.file_path) || currentResult.movie
			: null,
	);

	function getEffectiveMovie(filePath: string, original: Movie | null | undefined): Movie | null {
		if (!original) return null;
		return editedMovies.get(filePath) || original;
	}

	function resolvePosterUrl(movie: Movie, filePath: string): string | undefined {
		const override = posterPreviewOverrides.get(filePath);
		const baseURL = override?.url || movie.cropped_poster_url || movie.poster_url;
		if (!baseURL) return undefined;
		if (!override) return baseURL;
		if (baseURL.includes('v=')) return baseURL;
		const separator = baseURL.includes('?') ? '&' : '?';
		return `${baseURL}${separator}v=${override.version}`;
	}

	const displayPosterUrl = $derived<string | undefined>(
		(() => {
			if (!currentMovie || !currentResult) return undefined;
			return resolvePosterUrl(currentMovie, currentResult.file_path);
		})(),
	);

	let editedMovieKey = $derived.by(() => {
		const fp = currentResult?.file_path;
		if (!fp || !editedMovies.has(fp)) return '';
		return JSON.stringify(editedMovies.get(fp));
	});

	function getEffectiveOperationMode(): string {
		const configured = job?.operation_mode_override || config?.output?.operation_mode || 'organize';
		if (configured === 'organize') {
			const srcDir = currentResult?.file_path
				? currentResult.file_path.substring(
						0,
						currentResult.file_path.replace(/\\/g, '/').lastIndexOf('/'),
					)
				: '';
			const destMatchesSrc =
				destinationPath.trim() !== '' && destinationPath.trim() === srcDir.trim();
			const noFolderFormat = !config?.output?.folder_format;
			const noSubfolderFormat =
				!config?.output?.subfolder_format || config.output.subfolder_format.length === 0;
			if (destMatchesSrc && noFolderFormat && noSubfolderFormat) {
				return 'in-place-norenamefolder';
			}
		}
		return configured;
	}

	function getCanOrganize(): boolean {
		if (isUpdateMode) return false;
		if (!config) return false;
		const mode = getEffectiveOperationMode();
		return (
			mode === 'organize' ||
			mode === 'in-place' ||
			mode === 'in-place-norenamefolder' ||
			mode === 'metadata-artwork'
		);
	}

	const canOrganize = $derived(getCanOrganize());

	let previewEnabled = $derived.by(() => {
		if (!currentMovie) return false;
		if (organizeStatus === 'organizing') return false;
		const operationMode = getEffectiveOperationMode();
		const needsDestination = operationMode === 'organize';
		return needsDestination ? destinationPath.trim() !== '' : true;
	});

	const previewQuery = createQuery(() => {
		// Resolve the effective operation mode ONCE in this reactive callback so it
		// participates in the queryKey. getEffectiveOperationMode reads reactive
		// state not otherwise covered by the key (job.operation_mode_override,
		// config.output.operation_mode, folder/subfolder format); without it here a
		// config/override mode change with the other key parts unchanged would
		// reuse a stale preview. queryFn captures the same value as the key.
		const operationMode = getEffectiveOperationMode();
		return {
		queryKey: [
			'organize-preview',
			jobId,
			currentResult?.result_id,
			currentMovie?.id,
			operationMode,
			destinationPath,
			organizeOperation,
			skipNfo,
			skipDownload,
			editedMovieKey,
		],
		queryFn: () => {
			const copyOnly = organizeOperation !== 'move';
			const linkMode =
				organizeOperation === 'hardlink'
					? 'hard'
					: organizeOperation === 'softlink'
						? 'soft'
						: undefined;

			const fp = currentResult?.file_path ?? '';
			const isEdited = editedMovies.has(fp);
			let movieOverride: Movie | undefined;
			if (isEdited) {
				const edited = editedMovies.get(fp);
				movieOverride = edited ? { ...edited } : undefined;
				if (movieOverride && movieOverride.display_title) {
					movieOverride.title = movieOverride.display_title;
				}
			}

			return apiClient.previewOrganize(jobId, currentResult!.result_id, {
				destination: destinationPath,
				copy_only: copyOnly,
				link_mode: linkMode,
				operation_mode: operationMode as
					| 'organize'
					| 'in-place'
					| 'in-place-norenamefolder'
					| 'metadata-artwork'
					| 'preview',
				skip_nfo: skipNfo,
				skip_download: skipDownload,
				movie: movieOverride,
			});
		},
		enabled: previewEnabled,
		staleTime: 300,
	};
	});

	let preview = $derived(previewQuery.data ?? null);
	let previewNeedsDestination = $derived(
		!!currentMovie && getEffectiveOperationMode() === 'organize' && !destinationPath.trim(),
	);

	const mutations = createReviewMutations({
		getJobId: () => jobId,
		getJob: () => job,
		setJob: (nextJob) => {
			job = nextJob;
		},
		skipJobSync: () => {
			skipJobSync = true;
		},
		getEditedMovies: () => editedMovies,
		getCurrentResult: () => currentResult,
		getPosterPreviewOverrides: () => posterPreviewOverrides,
		getPosterCropStates: () => posterCropStates,
		getCropMetrics: () => cropMetrics,
		getCropBox: () => cropBox,
		getQueryClient: () => queryClient,
		getCurrentMovieIndex: () => currentMovieIndex,
		setCurrentMovieIndex: (index) => {
			currentMovieIndex = index;
		},
		getMovieResultsLength: () => movieResults.length,
		gotoJobs: () => {
			void goto('/jobs');
		},
		setShowPosterCropModal: (show) => {
			showPosterCropModal = show;
		},
		updateBatchMoviePosterFromURL: (mutationJobId, resultId, body) =>
			apiClient.updateBatchMoviePosterFromURL(mutationJobId, resultId, body),
		excludeBatchMovie: (mutationJobId, resultId) =>
			apiClient.excludeBatchMovie(mutationJobId, resultId),
		updateBatchMovie: (mutationJobId, resultId, movie) =>
			apiClient.updateBatchMovie(mutationJobId, resultId, movie),
		updateBatchMoviePosterCrop: (mutationJobId, resultId, crop, maxPosterHeight) =>
			apiClient.updateBatchMoviePosterCrop(mutationJobId, resultId, {
				...crop,
				// Omit max_poster_height when null OR undefined so a nullable crop
				// height is never serialized as `max_poster_height: null`.
				...(maxPosterHeight != null ? { max_poster_height: maxPosterHeight } : {}),
			}),
		batchExcludeMovies: (mutationJobId, request) =>
			apiClient.batchExcludeMovies(mutationJobId, request),
		bulkRescrapeMovies: (mutationJobId, request) =>
			apiClient.bulkRescrapeMovies(mutationJobId, request),
		getSelectedMovieIds: () => selectedMovieIds,
		clearSelectedMovieIds: () => {
			selectedMovieIds.clear();
			lastSelectedMovieId = null;
		},
		deleteSelectedMovieId: (movieId: string) => {
			selectedMovieIds.delete(movieId);
			if (lastSelectedMovieId === movieId) {
				lastSelectedMovieId = null;
			}
		},
		toastSuccess: (message, duration) => toastStore.success(message, duration),
		toastError: (message, duration) => toastStore.error(message, duration),
		clearEditStorage,
		clearEditedMovies,
		clearPosterPreviewOverrides,
	});

	function updateCurrentMovie(movie: Movie) {
		if (!currentResult?.movie) return;

		const isActuallyModified = !equal(movie, currentResult.movie);

		if (isActuallyModified) {
			editedMovies.set(currentResult.file_path, movie);

			if (
				movie.poster_url !== currentResult.movie?.poster_url ||
				movie.cropped_poster_url !== currentResult.movie?.cropped_poster_url
			) {
				posterPreviewOverrides.delete(currentResult.file_path);
			}
		} else {
			editedMovies.delete(currentResult.file_path);
		}
	}

	function resetCurrentMovie() {
		if (!currentResult?.movie) return;
		editedMovies.delete(currentResult.file_path);
	}

	function toggleMovieSelection(movieId: string, shiftKey: boolean) {
		if (!selectionMode) return;
		if (shiftKey && lastSelectedMovieId !== null) {
			const fromIndex = filteredMovieGroups.findIndex((g) => g.movieId === lastSelectedMovieId);
			const toIndex = filteredMovieGroups.findIndex((g) => g.movieId === movieId);
			if (fromIndex !== -1 && toIndex !== -1) {
				selectMovieRange(fromIndex, toIndex);
			}
		} else {
			if (selectedMovieIds.has(movieId)) {
				selectedMovieIds.delete(movieId);
			} else {
				selectedMovieIds.add(movieId);
				lastSelectedMovieId = movieId;
			}
		}
	}

	function selectMovieRange(fromIndex: number, toIndex: number) {
		const start = Math.min(fromIndex, toIndex);
		const end = Math.max(fromIndex, toIndex);
		for (let i = start; i <= end; i++) {
			const group = filteredMovieGroups[i];
			if (group) {
				selectedMovieIds.add(group.movieId);
			}
		}
	}

	function selectAllMovies() {
		for (const group of filteredMovieGroups) {
			selectedMovieIds.add(group.movieId);
		}
	}

	function deselectAllMovies() {
		selectedMovieIds.clear();
		lastSelectedMovieId = null;
	}

	function toggleCompletenessTier(tier: CompletenessTier) {
		if (completenessFilter.has(tier)) {
			completenessFilter.delete(tier);
		} else {
			completenessFilter.add(tier);
		}
	}

	function toggleSelectionMode() {
		selectionMode = !selectionMode;
		if (!selectionMode) {
			selectedMovieIds.clear();
			lastSelectedMovieId = null;
		}
	}

	const selectedCount = $derived(selectedMovieIds.size);
	const allSelected = $derived(
		filteredMovieGroups.length > 0 &&
			filteredMovieGroups.every((g) => selectedMovieIds.has(g.movieId)),
	);

	async function bulkExcludeMovies() {
		if (selectedMovieIds.size === 0) return;
		const count = selectedMovieIds.size;
		const confirmed = await confirmDialog(
			'Exclude Movies',
			`Exclude ${count} movie${count !== 1 ? 's' : ''} from this job?`,
			{ confirmLabel: 'Exclude', variant: 'danger' },
		);
		if (!confirmed) return;
		mutations.bulkExcludeMutation.mutate({
			resultIds: filteredMovieGroups
				.filter((g) => selectedMovieIds.has(g.movieId))
				.flatMap((g) => g.results.map((r) => r.result_id)),
		});
	}

	async function openBulkRescrapeModal() {
		if (selectedMovieIds.size === 0) return;
		if (availableScrapers.length === 0) {
			try {
				availableScrapers = await apiClient.getScrapers();
			} catch {
				toastStore.error('Failed to load scrapers');
				return;
			}
		}
		rescrapeMovieId = '';
		bulkRescrapeMovieIds = Array.from(selectedMovieIds);
		rescrapeSelectedScrapers = availableScrapers.filter((s) => s.enabled).map((s) => s.name);
		manualSearchMode = false;
		manualSearchInput = '';
		rescrapePreset = undefined;
		rescrapeScalarStrategy = 'prefer-nfo';
		rescrapeArrayStrategy = 'merge';
		showRescrapeModal = true;
	}

	async function executeBulkRescrape() {
		if (bulkRescrapeMovieIds.length === 0) return;
		const selectedScrapers = rescrapeSelectedScrapers;
		if (selectedScrapers.length === 0) {
			toastStore.error('Please select at least one scraper');
			return;
		}

		bulkRescraping = true;
		bulkRescrapeProgress = bulkRescrapeMovieIds.map((id) => ({ movie_id: id, status: 'pending' }));
		showRescrapeModal = false;

		try {
			const result = await mutations.bulkRescrapeMutation.mutateAsync({
				movieIds: bulkRescrapeMovieIds,
				selectedScrapers,
				preset: rescrapePreset,
				scalarStrategy: rescrapeScalarStrategy || undefined,
				arrayStrategy: rescrapeArrayStrategy || undefined,
			});

			bulkRescrapeProgress = result.results.map((r) => ({
				movie_id: r.movie_id,
				status: r.status,
				error: r.error,
			}));

			if (result.job) {
				skipJobSync = true;
				job = JSON.parse(JSON.stringify(result.job));
			}

			void queryClient.invalidateQueries({ queryKey: ['batch-job', jobId] });
		} catch (error) {
			const errorMessage = error instanceof Error ? error.message : String(error);
			toastStore.error(`Bulk rescrape failed: ${errorMessage}`);
		} finally {
			bulkRescraping = false;
		}
	}

	function clearPosterPreviewOverride() {
		if (!currentResult) return;
		posterPreviewOverrides.delete(currentResult.file_path);
	}

	// The scraped-image revert baseline. The backend populates the
	// poster-original group (OriginalPosterURL/OriginalCroppedPosterURL/
	// OriginalShouldCropPoster/OriginalCoverURL) at scrape + rescrape time, so
	// the authoritative baseline lives on the movie itself — reading it from
	// the movie (not a one-shot snapshot map) means it stays correct across an
	// in-review rescrape, where the snapshot maps would go stale and Reset
	// would restore the *prior* content's image. The `|| current` fallback
	// covers older movies persisted before the baseline was eagerly set.
	//
	// The fallback reads from the unedited loaded movie (`currentResult.movie`)
	// — NOT `currentMovie`, which may already be an `editedMovies` override.
	// Anchoring the fallback to the edited movie would make the baseline drift
	// with the edit, so Reset (which compares the edited `currentMovie` against
	// this baseline) would no-op incorrectly for legacy rows.
	const posterBaseline = $derived.by(() => {
		if (!currentResult || !currentResult.movie) return undefined;
		const loaded = currentResult.movie;
		return {
			poster_url: loaded.original_poster_url || loaded.poster_url || '',
			cropped_poster_url: loaded.original_cropped_poster_url || loaded.cropped_poster_url || '',
			should_crop_poster: loaded.original_should_crop_poster ?? loaded.should_crop_poster ?? false,
		};
	});

	const coverBaseline = $derived.by(() => {
		if (!currentResult || !currentResult.movie) return undefined;
		const loaded = currentResult.movie;
		return loaded.original_cover_url || loaded.cover_url || '';
	});

	// Whether the current movie has drifted from its scraped baseline — mirrors
	// resetPoster/resetCover's no-op guards so the Reset button is disabled
	// exactly when Reset would do nothing (UX: no "reset" when already at baseline).
	const canResetPoster = $derived.by(() => {
		if (!currentResult || !currentMovie) return false;
		const original = posterBaseline;
		if (!original || !original.poster_url) return false;
		return (
			currentMovie.poster_url !== original.poster_url ||
			currentMovie.cropped_poster_url !== original.cropped_poster_url ||
			currentMovie.should_crop_poster !== original.should_crop_poster
		);
	});

	const canResetCover = $derived.by(() => {
		if (!currentResult || !currentMovie) return false;
		const original = coverBaseline;
		if (original === undefined || original === '') return false;
		return currentMovie.cover_url !== original;
	});

	function resetPoster() {
		if (!currentResult || !currentMovie) return;

		const original = posterBaseline;
		if (!original || !original.poster_url) return;

		const posterChanged =
			currentMovie.poster_url !== original.poster_url ||
			currentMovie.cropped_poster_url !== original.cropped_poster_url ||
			currentMovie.should_crop_poster !== original.should_crop_poster;
		if (!posterChanged) return;

		if (original.poster_url !== currentMovie.poster_url) {
			mutations.applyPosterFromUrl(currentResult!.result_id, original.poster_url);
		} else {
			updateCurrentMovie({
				...currentMovie,
				cropped_poster_url: original.cropped_poster_url,
				should_crop_poster: original.should_crop_poster,
			});
			clearPosterPreviewOverride();
		}
	}

	function resetCover() {
		if (!currentResult || !currentMovie) return;

		const original = coverBaseline;
		if (original === undefined || original === '') return;

		if (currentMovie.cover_url === original) return;

		updateCurrentMovie({ ...currentMovie, cover_url: original });
	}

	async function useScreenshotAsPoster(url: string) {
		if (!currentMovie || !currentResult) return;

		const confirmed = await confirmDialog(
			'Set as Poster',
			'Use this screenshot as the poster? This will replace the current poster image.',
		);

		if (!confirmed) return;

		clearPosterPreviewOverride();
		mutations.applyPosterFromUrl(currentResult!.result_id, url);
	}

	async function useScreenshotAsCover(url: string) {
		if (!currentMovie || !currentResult) return;

		const confirmed = await confirmDialog(
			'Set as Cover/Fanart',
			'Use this screenshot as the cover/fanart? This will replace the current cover image.',
		);

		if (!confirmed) return;

		updateCurrentMovie({ ...currentMovie, cover_url: url });
	}

	function clearEditStorage() {
		if (!browser) return;
		sessionStorage.removeItem(editedMoviesStorageKey);
		sessionStorage.removeItem(posterPreviewOverridesStorageKey);
	}

	function clearEditedMovies() {
		editedMovies.clear();
	}

	function clearPosterPreviewOverrides() {
		posterPreviewOverrides.clear();
	}

	async function saveAllEdits() {
		return mutations.saveEditsMutation.mutateAsync();
	}

	const organizeController = createOrganizeController({
		getJobId: () => jobId,
		getIsUpdateMode: () => isUpdateMode,
		getJob: () => job,
		setJob: (nextJob) => {
			job = nextJob;
		},
		getDestinationPath: () => destinationPath,
		getOrganizeOperation: () => organizeOperation,
		getOperationMode: () => getEffectiveOperationMode(),
		getEditedMovies: () => editedMovies,
		saveAllEdits,
		getOrganizeStatus: () => organizeStatus,
		setOrganizeStatus: (nextStatus) => {
			organizeStatus = nextStatus;
		},
		setOrganizing: (nextOrganizing) => {
			organizing = nextOrganizing;
		},
		setOrganizeProgress: (nextProgress) => {
			// Monotonic high-water guard: the organize bar must never move backward
			// during a run. Aggregate progress-stream messages are already monotonic
			// (backend high-water mutex); this guards against any per-file message
			// that slips through the controller's bar-drive filter and out-of-order
			// delivery. An explicit 0 resets the bar for a fresh run
			// (prepareOrganizeRun -> setOrganizeProgress(0)); allow it through.
			const next = nextOrganizeProgress(organizeProgress, nextProgress);
			if (next !== null) {
				organizeProgress = next;
			}
		},
		getFileStatuses: () => fileStatuses,
		getExpectedOrganizeFilePaths: () => expectedOrganizeFilePaths,
		setExpectedOrganizeFilePaths: (nextPaths) => {
			expectedOrganizeFilePaths = nextPaths;
		},
		clearWebSocketMessages: websocketStore.clearMessages,
		toastSuccess: (message, duration) => toastStore.success(message, duration),
		toastError: (message, duration) => toastStore.error(message, duration),
		toastInfo: (message, duration) => toastStore.info(message, duration),
		navigateBrowse: () => {
			void goto('/browse');
		},
		api: {
			getBatchJob: (nextJobId) => apiClient.getBatchJob(nextJobId, true),
			organizeBatchJob: (nextJobId, request) => apiClient.organizeBatchJob(nextJobId, request),
			updateBatchJob: (nextJobId, request) => apiClient.updateBatchJob(nextJobId, request),
		},
	});

	const rescrapeController = createRescrapeController({
		getJobId: () => jobId,
		getCurrentResult: () => rescrapeTargetResult ?? currentResult,
		getJob: () => job,
		setJob: (nextJob) => {
			job = nextJob;
		},
		getEditedMovies: () => editedMovies,
		getAvailableScrapers: () => availableScrapers,
		setAvailableScrapers: (scrapers) => {
			availableScrapers = scrapers;
		},
		getRescrapeResultId: () => rescrapeResultId,
		setRescrapeResultId: (resultId) => {
			rescrapeResultId = resultId;
		},
		getSelectedScrapers: () => rescrapeSelectedScrapers,
		setSelectedScrapers: (scrapers) => {
			rescrapeSelectedScrapers = scrapers;
		},
		getManualSearchMode: () => manualSearchMode,
		setManualSearchMode: (manual) => {
			manualSearchMode = manual;
		},
		getManualSearchInput: () => manualSearchInput,
		setManualSearchInput: (input) => {
			manualSearchInput = input;
		},
		setShowRescrapeModal: (show) => {
			showRescrapeModal = show;
		},
		getRescrapePreset: () => rescrapePreset,
		setRescrapePreset: (preset) => {
			rescrapePreset = preset;
		},
		getRescrapeScalarStrategy: () => rescrapeScalarStrategy,
		setRescrapeScalarStrategy: (strategy) => {
			rescrapeScalarStrategy = strategy;
		},
		getRescrapeArrayStrategy: () => rescrapeArrayStrategy,
		setRescrapeArrayStrategy: (strategy) => {
			rescrapeArrayStrategy = strategy;
		},
		getRescrapingStates: () => rescrapingStates,
		toastSuccess: (message, duration) => toastStore.success(message, duration),
		toastError: (message, duration) => toastStore.error(message, duration),
		api: {
			getScrapers: () => apiClient.getScrapers(),
			rescrapeBatchMovie: (nextJobId, resultId, req) =>
				apiClient.rescrapeBatchMovie(nextJobId, resultId, req),
		},
	});

	const posterCropController = createPosterCropController({
		getBrowser: () => browser,
		getJobId: () => jobId,
		getCurrentMovie: () => currentMovie,
		getCurrentResult: () => currentResult,
		getShowPosterCropModal: () => showPosterCropModal,
		setShowPosterCropModal: (show) => {
			showPosterCropModal = show;
		},
		setPosterCropLoadError: (errorMessage) => {
			posterCropLoadError = errorMessage;
		},
		getCropSourceURL: () => cropSourceURL,
		setCropSourceURL: (url) => {
			cropSourceURL = url;
		},
		getCropImageElement: () => cropImageElement,
		setCropImageElement: (imageElement) => {
			cropImageElement = imageElement;
		},
		getCropMetrics: () => cropMetrics,
		setCropMetrics: (metrics) => {
			cropMetrics = metrics;
		},
		getCropBox: () => cropBox,
		setCropBox: (nextBox) => {
			cropBox = nextBox;
		},
		getMaxPosterHeight: () => maxPosterHeight,
		setMaxPosterHeight: (h) => {
			maxPosterHeight = h;
		},
		getCropDragState: () => cropDragState,
		setCropDragState: (state) => {
			cropDragState = state;
		},
		getPosterCropStates: () => posterCropStates,
		applyPosterFromUrlAsync: (resultId, url) => mutations.applyPosterFromUrlAsync(resultId, url),
		mutatePosterCropAsync: (mutationJobId, resultId, crop, maxPosterHeightArg) => {
			return mutations.applyPosterCropAsync(mutationJobId, resultId, crop, maxPosterHeightArg);
		},
		setCropApplying: (applying) => { cropApplying = applying; }
	});

	const reviewPageController = createReviewPageController({
		getJob: () => job,
		getCurrentMovie: () => currentMovie,
		getCurrentResult: () => currentResult,
		getEditedMovies: () => editedMovies,
		getDestinationPath: () => destinationPath,
		setDestinationPath: (path) => {
			destinationPath = path;
		},
		getTempDestinationPath: () => tempDestinationPath,
		setTempDestinationPath: (path) => {
			tempDestinationPath = path;
		},
		setShowDestinationBrowser: (show) => {
			showDestinationBrowser = show;
		},
		setShowImageViewer: (show) => {
			showImageViewer = show;
		},
		setImageViewerImages: (images) => {
			imageViewerImages = images;
		},
		setImageViewerIndex: (index) => {
			imageViewerIndex = index;
		},
		setImageViewerTitle: (title) => {
			imageViewerTitle = title;
		},
		excludeMovie: (mutationJobId, resultId) => {
			mutations.excludeMovieMutation.mutate({ jobId: mutationJobId, resultId });
		},
		api: {
			getPreviewImageURL: (url) => apiClient.getPreviewImageURL(url),
		},
	});

	function applyRescrapePreset(preset: 'conservative' | 'gap-fill' | 'aggressive') {
		rescrapeController.applyRescrapePreset(preset);
	}

	async function openRescrapeModal(movieId: string) {
		bulkRescrapeMovieIds = [];
		rescrapeTargetResult = null;
		const group = movieGroups.find((g) => g.movieId === movieId);
		if (!group) {
			toastStore.error('Unable to open rescrape: movie not found');
			return;
		}
		await rescrapeController.openRescrapeModal(group.primaryResult.result_id);
	}

	async function openRescrapeModalForFailed(result: FileResult) {
		bulkRescrapeMovieIds = [];
		if (availableScrapers.length === 0) {
			try {
				availableScrapers = await apiClient.getScrapers();
			} catch {
				toastStore.error('Failed to load scrapers');
				return;
			}
		}
		rescrapeTargetResult = result;
		rescrapeMovieId = result.movie_id;
		rescrapeResultId = result.result_id;
		rescrapeSelectedScrapers = availableScrapers.filter((s) => s.enabled).map((s) => s.name);
		manualSearchMode = true;
		manualSearchInput = '';
		rescrapePreset = undefined;
		rescrapeScalarStrategy = 'prefer-nfo';
		rescrapeArrayStrategy = 'merge';
		showRescrapeModal = true;
	}

	$effect(() => {
		if (!showRescrapeModal) {
			rescrapeTargetResult = null;
		}
	});

	async function executeRescrape(mode?: { manualSearchMode: boolean; manualSearchInput: string }) {
		await rescrapeController.executeRescrape(mode);
	}

	async function organizeAll() {
		await organizeController.organizeAll(skipNfo, skipDownload);
	}

	async function updateAll() {
		const options: UpdateRequest = {};
		if (forceOverwrite) options.force_overwrite = true;
		if (preserveNfo) options.preserve_nfo = true;
		if (skipNfo) options.skip_nfo = true;
		if (skipDownload) options.skip_download = true;
		await organizeController.updateAll(options);
	}

	async function retryFailed() {
		await organizeController.retryFailed();
	}

	$effect(() => {
		currentMovieIndex;
		showFullSourcePath = false;
	});

	$effect(() => {
		if (!browser) return;
		localStorage.setItem(
			SHOW_FIELD_SCRAPER_SOURCES_KEY,
			showFieldScraperSources ? 'true' : 'false',
		);
	});

	$effect(() => {
		if (!browser) return;
		if (!viewModeInitialized) return;
		localStorage.setItem(VIEW_MODE_KEY, viewMode);
	});

	$effect(() => {
		if (!browser) return;
		if (posterCropStates.size === 0) return;
		const entries: Record<string, PosterCropState> = {};
		posterCropStates.forEach((v, k) => {
			entries[k] = v;
		});
		localStorage.setItem(posterCropStatesStorageKey, JSON.stringify(entries));
	});

	/**
	 * Restore editedMovies + posterPreviewOverrides from sessionStorage.
	 *
	 * MUST be declared BEFORE the persistence $effects (below) so it runs
	 * first on mount — otherwise the persistence effect's removeItem-
	 * when-empty branch (which fires its initial-mount run before this
	 * restore completes) destroys the sessionStorage entry before this
	 * restore reads it (the original 683b4a1e bug: every reload wiped
	 * in-progress edits because persistence's initial-mount run raced
	 * ahead of restore). $effect effects run in declaration order on
	 * the initial flush, so declaring this first guarantees the restore
	 * populates editedMovies before persistence's removeItem branch
	 * observes (the now-stale) size===0.
	 */
	$effect(() => {
		if (!browser) return;
		// Clear job-scoped state before restoring so edits/overrides from a
		// PREVIOUS jobId cannot leak into the new job. This effect's only reactive
		// dependencies are the jobId-derived storage keys, so it re-runs exactly
		// on mount (maps already empty — clear is a no-op) and on jobId change
		// (clears the prior job's in-memory entries before merging the new job's
		// saved ones). Untracked so the clear does not trip the persistence
		// effects' removeItem-when-empty branch mid-restore. Prior-job edits are
		// already safe in sessionStorage under the prior job's key (the
		// persistence effects wrote them on every edit).
		untrack(() => {
			editedMovies.clear();
			posterPreviewOverrides.clear();
		});
		const savedEditedMovies = sessionStorage.getItem(editedMoviesStorageKey);
		if (savedEditedMovies) {
			try {
				const parsed = JSON.parse(savedEditedMovies) as Record<string, Movie>;
				untrack(() => {
					for (const [k, v] of Object.entries(parsed)) {
						editedMovies.set(k, v);
					}
				});
			} catch {
				sessionStorage.removeItem(editedMoviesStorageKey);
			}
		}
		const savedOverrides = sessionStorage.getItem(posterPreviewOverridesStorageKey);
		if (savedOverrides) {
			try {
				const parsed = JSON.parse(savedOverrides) as Record<string, PosterPreviewOverride>;
				untrack(() => {
					for (const [k, v] of Object.entries(parsed)) {
						posterPreviewOverrides.set(k, v);
					}
				});
			} catch {
				sessionStorage.removeItem(posterPreviewOverridesStorageKey);
			}
		}
	});

	$effect(() => {
		if (!browser) return;
		if (editedMovies.size === 0) {
			sessionStorage.removeItem(editedMoviesStorageKey);
			return;
		}
		const entries: Record<string, Movie> = {};
		editedMovies.forEach((v, k) => {
			entries[k] = v;
		});
		sessionStorage.setItem(editedMoviesStorageKey, JSON.stringify(entries));
	});

	$effect(() => {
		if (!browser) return;
		if (posterPreviewOverrides.size === 0) {
			sessionStorage.removeItem(posterPreviewOverridesStorageKey);
			return;
		}
		const entries: Record<string, PosterPreviewOverride> = {};
		posterPreviewOverrides.forEach((v, k) => {
			entries[k] = v;
		});
		sessionStorage.setItem(posterPreviewOverridesStorageKey, JSON.stringify(entries));
	});

	$effect(() => {
		const unsubscribe = websocketStore.subscribe((ws) => {
			organizeController.handleWebSocketMessage(ws.messages.at(-1));
		});

		return unsubscribe;
	});

	onMount(() => {
		if (browser) {
			showFieldScraperSources = localStorage.getItem(SHOW_FIELD_SCRAPER_SOURCES_KEY) === 'true';
			const savedViewMode = localStorage.getItem(VIEW_MODE_KEY);
			if (
				savedViewMode === 'grid-cover' ||
				savedViewMode === 'grid-poster' ||
				savedViewMode === 'grid'
			) {
				viewMode = savedViewMode === 'grid' ? 'grid-poster' : savedViewMode;
			} else {
				// 'detail' is intentionally NOT restored from storage here, even
				// though it is a valid view mode. 'detail' is a transient drill-in
				// state reached by clicking a grid card (+page.svelte), not a list-
				// view preference. Restoring it would make the review list open in
				// detail view after a user drilled into one movie and navigated
				// away — the configured default (cfgDefault below) should govern
				// the initial list view instead. Only the grid variants persist as
				// genuine view-mode preferences.
				const cfgDefault = config?.webui?.default_review_view ?? 'grid-poster';
				if (
					cfgDefault === 'grid-cover' ||
					cfgDefault === 'grid-poster' ||
					cfgDefault === 'detail'
				) {
					viewMode = cfgDefault;
				} else {
					console.warn(
						`Invalid webui.default_review_view value "${cfgDefault}", falling back to "grid-poster"`,
					);
					viewMode = 'grid-poster';
				}
			}
			viewModeInitialized = true;
			const savedCrops = localStorage.getItem(posterCropStatesStorageKey);
			if (savedCrops) {
				try {
					const parsed = JSON.parse(savedCrops) as Record<string, PosterCropState>;
					untrack(() => {
						for (const [k, v] of Object.entries(parsed)) {
							posterCropStates.set(k, v);
						}
					});
				} catch {
					localStorage.removeItem(posterCropStatesStorageKey);
				}
			}

			// editedMovies + posterPreviewOverrides restore moved to a $effect
			// declared before the persistence $effects above — restore-on-mount
			// now runs as an $effect (in declaration order, before persistence)
			// so the persistence effect's initial-mount removeItem-when-empty
			// branch no longer destroys the entries before restore reads them.
		}

		window.addEventListener('resize', posterCropController.handleWindowResize);

		return () => {
			window.removeEventListener('resize', posterCropController.handleWindowResize);
		};
	});

	onDestroy(() => {
		organizeController.cleanup();
		posterCropController.cleanup();
	});

	return {
		get job() {
			return job;
		},
		get loading() {
			return loading;
		},
		get error() {
			return error;
		},
		get config() {
			return config;
		},
		get completenessConfig() {
			return completenessConfig;
		},
		get currentMovieIndex() {
			return currentMovieIndex;
		},
		set currentMovieIndex(v) {
			currentMovieIndex = v;
		},
		get editedMovies() {
			return editedMovies;
		},
		get organizing() {
			return organizing;
		},
		set organizing(v) {
			organizing = v;
		},
		get destinationPath() {
			return destinationPath;
		},
		set destinationPath(v) {
			destinationPath = v;
		},
		get organizeOperation() {
			return organizeOperation;
		},
		set organizeOperation(v) {
			organizeOperation = v;
		},
		get showDestinationBrowser() {
			return showDestinationBrowser;
		},
		set showDestinationBrowser(v) {
			showDestinationBrowser = v;
		},
		get tempDestinationPath() {
			return tempDestinationPath;
		},
		set tempDestinationPath(v) {
			tempDestinationPath = v;
		},
		get showTrailerModal() {
			return showTrailerModal;
		},
		set showTrailerModal(v) {
			showTrailerModal = v;
		},
		get isUpdateMode() {
			return isUpdateMode;
		},
		get showFieldScraperSources() {
			return showFieldScraperSources;
		},
		set showFieldScraperSources(v) {
			showFieldScraperSources = v;
		},
		get viewMode() {
			return viewMode;
		},
		set viewMode(v) {
			viewMode = v;
		},
		get organizeProgress() {
			return organizeProgress;
		},
		get organizeStatus() {
			return organizeStatus;
		},
		set organizeStatus(v) {
			organizeStatus = v;
		},
		get fileStatuses() {
			return fileStatuses;
		},
		get expectedOrganizeFilePaths() {
			return expectedOrganizeFilePaths;
		},
		set expectedOrganizeFilePaths(v) {
			expectedOrganizeFilePaths = v;
		},
		get showCoverPanel() {
			return showCoverPanel;
		},
		get showPosterPanel() {
			return showPosterPanel;
		},
		get showTrailerPanel() {
			return showTrailerPanel;
		},
		get showScreenshotsPanel() {
			return showScreenshotsPanel;
		},
		get showImageViewer() {
			return showImageViewer;
		},
		set showImageViewer(v) {
			showImageViewer = v;
		},
		get imageViewerImages() {
			return imageViewerImages;
		},
		set imageViewerImages(v) {
			imageViewerImages = v;
		},
		get imageViewerIndex() {
			return imageViewerIndex;
		},
		set imageViewerIndex(v) {
			imageViewerIndex = v;
		},
		get imageViewerTitle() {
			return imageViewerTitle;
		},
		set imageViewerTitle(v) {
			imageViewerTitle = v;
		},
		get showAllSidebarScreenshots() {
			return showAllSidebarScreenshots;
		},
		set showAllSidebarScreenshots(v) {
			showAllSidebarScreenshots = v;
		},
		get showFullSourcePath() {
			return showFullSourcePath;
		},
		set showFullSourcePath(v) {
			showFullSourcePath = v;
		},
		get forceOverwrite() {
			return forceOverwrite;
		},
		set forceOverwrite(v) {
			forceOverwrite = v;
		},
		get preserveNfo() {
			return preserveNfo;
		},
		set preserveNfo(v) {
			preserveNfo = v;
		},
		get skipNfo() {
			return skipNfo;
		},
		set skipNfo(v) {
			skipNfo = v;
		},
		get skipDownload() {
			return skipDownload;
		},
		set skipDownload(v) {
			skipDownload = v;
		},
		get showImagePanelContent() {
			return showImagePanelContent;
		},
		set showImagePanelContent(v) {
			showImagePanelContent = v;
		},
		get showAllPreviewScreenshots() {
			return showAllPreviewScreenshots;
		},
		set showAllPreviewScreenshots(v) {
			showAllPreviewScreenshots = v;
		},
		get showPosterCropModal() {
			return showPosterCropModal;
		},
		set showPosterCropModal(v) {
			showPosterCropModal = v;
		},
		get posterCropLoadError() {
			return posterCropLoadError;
		},
		set posterCropLoadError(v) {
			posterCropLoadError = v;
		},
		get cropSourceURL() {
			return cropSourceURL;
		},
		set cropSourceURL(v) {
			cropSourceURL = v;
		},
		get cropImageElement() {
			return cropImageElement;
		},
		set cropImageElement(v) {
			cropImageElement = v;
		},
		get cropMetrics() {
			return cropMetrics;
		},
		set cropMetrics(v) {
			cropMetrics = v;
		},
		get cropBox() {
			return cropBox;
		},
		set cropBox(v) {
			cropBox = v;
		},
		get maxPosterHeight() {
			return maxPosterHeight;
		},
		set maxPosterHeight(v) {
			maxPosterHeight = v;
		},
		get cropDragState() {
			return cropDragState;
		},
		set cropDragState(v) {
			cropDragState = v;
		},
		get posterPreviewOverrides() {
			return posterPreviewOverrides;
		},
		get posterCropStates() {
			return posterCropStates;
		},
		get availableScrapers() {
			return availableScrapers;
		},
		set availableScrapers(v) {
			availableScrapers = v;
		},
		get showRescrapeModal() {
			return showRescrapeModal;
		},
		set showRescrapeModal(v) {
			showRescrapeModal = v;
		},
		get rescrapeMovieId() {
			return rescrapeMovieId;
		},
		set rescrapeMovieId(v) {
			rescrapeMovieId = v;
		},
		get rescrapeResultId() {
			return rescrapeResultId;
		},
		set rescrapeResultId(v) {
			rescrapeResultId = v;
		},
		get rescrapeSelectedScrapers() {
			return rescrapeSelectedScrapers;
		},
		set rescrapeSelectedScrapers(v) {
			rescrapeSelectedScrapers = v;
		},
		get rescrapingStates() {
			return rescrapingStates;
		},
		get manualSearchMode() {
			return manualSearchMode;
		},
		set manualSearchMode(v) {
			manualSearchMode = v;
		},
		get manualSearchInput() {
			return manualSearchInput;
		},
		set manualSearchInput(v) {
			manualSearchInput = v;
		},
		get rescrapePreset() {
			return rescrapePreset;
		},
		set rescrapePreset(v) {
			rescrapePreset = v;
		},
		get rescrapeScalarStrategy() {
			return rescrapeScalarStrategy;
		},
		set rescrapeScalarStrategy(v) {
			rescrapeScalarStrategy = v;
		},
		get rescrapeArrayStrategy() {
			return rescrapeArrayStrategy;
		},
		set rescrapeArrayStrategy(v) {
			rescrapeArrayStrategy = v;
		},
		get movieGroups() {
			return movieGroups;
		},
		get failedResults() {
			return failedResults;
		},
		get movieResults() {
			return movieResults;
		},
		get currentMovieGroup() {
			return currentMovieGroup;
		},
		get currentResult() {
			return currentResult;
		},
		get canResetPoster() {
			return canResetPoster;
		},
		get canResetCover() {
			return canResetCover;
		},
		get currentMovie() {
			return currentMovie;
		},
		get displayPosterUrl() {
			return displayPosterUrl;
		},
		get preview() {
			return preview;
		},
		get previewNeedsDestination() {
			return previewNeedsDestination;
		},
		get canOrganize() {
			return canOrganize;
		},
		posterFromUrlMutation: mutations.posterFromUrlMutation,
		posterCropMutation: mutations.posterCropMutation,
		get posterCropSaving() { return mutations.posterCropMutation.isPending || cropApplying; },
		bulkExcludeMutation: mutations.bulkExcludeMutation,
		bulkRescrapeMutation: mutations.bulkRescrapeMutation,
		resolvePosterUrl,
		getEffectiveMovie,
		getEffectiveOperationMode,
		updateCurrentMovie,
		resetCurrentMovie,
		resetPoster,
		resetCover,
		useScreenshotAsPoster,
		useScreenshotAsCover,
		saveAllEdits,
		get selectedMovieIds() {
			return selectedMovieIds;
		},
		get selectedCount() {
			return selectedCount;
		},
		get allSelected() {
			return allSelected;
		},
		toggleMovieSelection,
		selectMovieRange,
		selectAllMovies,
		deselectAllMovies,
		get completenessFilter() {
			return completenessFilter;
		},
		get selectionMode() {
			return selectionMode;
		},
		get filteredMovieGroups() {
			return filteredMovieGroups;
		},
		get tierCounts() {
			return tierCounts;
		},
		toggleCompletenessTier,
		toggleSelectionMode,
		bulkExcludeMovies,
		get bulkRescraping() {
			return bulkRescraping;
		},
		get bulkRescrapeProgress() {
			return bulkRescrapeProgress;
		},
		get bulkRescrapeMovieIds() {
			return bulkRescrapeMovieIds;
		},
		dismissBulkRescrapeProgress() {
			bulkRescrapeProgress = [];
		},
		openBulkRescrapeModal,
		executeBulkRescrape,
		organizeController,
		rescrapeController,
		posterCropController,
		reviewPageController,
		applyRescrapePreset,
		openRescrapeModal,
		openRescrapeModalForFailed,
		executeRescrape,
		organizeAll,
		updateAll,
		retryFailed,
	};
}
