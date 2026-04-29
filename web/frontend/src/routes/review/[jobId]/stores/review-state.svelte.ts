import { onDestroy, onMount, untrack } from 'svelte';
import { SvelteMap } from 'svelte/reactivity';
import { browser } from '$app/environment';
import { goto } from '$app/navigation';
import type { Page } from '@sveltejs/kit';
import { createQuery, useQueryClient } from '@tanstack/svelte-query';
import { apiClient } from '$lib/api/client';
import { createConfigQuery } from '$lib/query/queries';
import type { BatchJobResponse, FileResult, Movie, Scraper, UpdateRequest } from '$lib/api/types';
import { toastStore } from '$lib/stores/toast';
import { websocketStore } from '$lib/stores/websocket';
import { createOrganizeController, type FileStatus, type OrganizeOperation } from '../logic/organize-controller';
import { createRescrapeController, type ArrayStrategy, type ScalarStrategy } from '../logic/rescrape-controller';
import { createPosterCropController, type PosterCropDragState } from '../logic/poster-crop-controller';
import { createReviewPageController } from '../logic/review-page-controller';
import {
	normalizeCropBox,
	type PosterCropBox,
	type PosterCropMetrics,
	type PosterCropState,
	type PosterPreviewOverride
} from '../review-utils';
import equal from 'fast-deep-equal';
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

	let currentMovieIndex = $state(0);
	let editedMovies = new SvelteMap<string, Movie>();
	let originalPosterState = new SvelteMap<string, { poster_url: string; cropped_poster_url: string; should_crop_poster: boolean }>();
	let organizing = $state(false);
	let destinationPath = $state('');
	let organizeOperation = $state<OrganizeOperation>('move');
	let showDestinationBrowser = $state(false);
	let tempDestinationPath = $state('');
	let showTrailerModal = $state(false);

	let isUpdateMode = $derived(pageStore.url.searchParams.get('update') === 'true');
	let showFieldScraperSources = $state(false);
	const SHOW_FIELD_SCRAPER_SOURCES_KEY = 'javinizer.review.showFieldScraperSources';
	const VIEW_MODE_KEY = 'javinizer.review.viewMode';
	let viewMode = $state<'detail' | 'grid'>('detail');
	let posterCropStatesStorageKey = $derived(`javinizer.review.posterCropStates.${jobId}`);

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
	let cropDragState = $state<PosterCropDragState | null>(null);
	let posterPreviewOverrides = new SvelteMap<string, PosterPreviewOverride>();
	let posterCropStates = new SvelteMap<string, PosterCropState>();

	$effect(() => {
		const jobData = jobQuery.data;
		if (jobData) {
			untrack(() => {
				if (jobData.destination && !destinationPath) {
					destinationPath = jobData.destination;
				}
				if (originalPosterState.size === 0) {
					for (const result of Object.values(jobData.results) as FileResult[]) {
						if (result.data) {
							originalPosterState.set(result.file_path, {
								poster_url: result.data.original_poster_url || result.data.poster_url || '',
								cropped_poster_url: result.data.original_cropped_poster_url || result.data.cropped_poster_url || '',
								should_crop_poster: (result.data.original_should_crop_poster ?? result.data.should_crop_poster) ?? false
							});
						}
					}
				}
			});
		}
	});

	let availableScrapers: Scraper[] = $state([]);
	let showRescrapeModal = $state(false);
	let rescrapeMovieId = $state('');
	let rescrapeSelectedScrapers: string[] = $state([]);
	let rescrapingStates = new SvelteMap<string, boolean>();
	let manualSearchMode = $state(false);
	let manualSearchInput = $state('');

	let rescrapePreset: string | undefined = $state(undefined);
	let rescrapeScalarStrategy: ScalarStrategy = $state('prefer-nfo');
	let rescrapeArrayStrategy: ArrayStrategy = $state('merge');

	const movieGroups = $derived<MovieGroup[]>(
		job ? (() => {
			const excluded = (job as BatchJobResponse).excluded || {};
			const allResults = (Object.values((job as BatchJobResponse).results) as FileResult[])
				.filter((r) => {
					if (r.status !== 'completed' || !r.data) {
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
				primaryResult: results[0]
			}));
		})() : []
	);

	const movieResults = $derived<FileResult[]>(movieGroups.map(g => g.primaryResult));

	const currentMovieGroup = $derived<MovieGroup | undefined>(movieGroups[currentMovieIndex]);
	const currentResult = $derived<FileResult | undefined>(currentMovieGroup?.primaryResult);
	const currentMovie = $derived<Movie | null>(
		currentResult && currentResult.data
			? editedMovies.get(currentResult.file_path) || currentResult.data
			: null
	);

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
		})()
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
				? currentResult.file_path.substring(0, currentResult.file_path.replace(/\\/g, '/').lastIndexOf('/'))
				: '';
			const destMatchesSrc = destinationPath.trim() !== '' && destinationPath.trim() === srcDir.trim();
			const noFolderFormat = !config?.output?.folder_format;
			const noSubfolderFormat = !config?.output?.subfolder_format || config.output.subfolder_format.length === 0;
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
		return mode === 'organize' || mode === 'in-place' || mode === 'in-place-norenamefolder';
	}

	const canOrganize = $derived(getCanOrganize());

	let previewEnabled = $derived.by(() => {
		if (!currentMovie) return false;
		if (organizeStatus === 'organizing') return false;
		const operationMode = getEffectiveOperationMode();
		const needsDestination = operationMode === 'organize';
		return needsDestination ? destinationPath.trim() !== '' : true;
	});

	const previewQuery = createQuery(() => ({
		queryKey: ['organize-preview', jobId, currentMovie?.id, destinationPath, organizeOperation, skipNfo, skipDownload, editedMovieKey],
		queryFn: () => {
			const operationMode = getEffectiveOperationMode();
			const copyOnly = organizeOperation !== 'move';
			const linkMode = organizeOperation === 'hardlink'
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

			return apiClient.previewOrganize(jobId, currentMovie!.id, {
				destination: destinationPath,
				copy_only: copyOnly,
				link_mode: linkMode,
				operation_mode: operationMode as 'organize' | 'in-place' | 'in-place-norenamefolder' | 'metadata-only' | 'preview',
				skip_nfo: skipNfo,
				skip_download: skipDownload,
				movie: movieOverride,
			});
		},
		enabled: previewEnabled,
		staleTime: 300,
	}));

	let preview = $derived(previewQuery.data ?? null);
	let previewNeedsDestination = $derived(
		!!currentMovie && getEffectiveOperationMode() === 'organize' && !destinationPath.trim()
	);

	const mutations = createReviewMutations({
		getJobId: () => jobId,
		getJob: () => job,
		setJob: (nextJob) => { job = nextJob; },
		skipJobSync: () => { skipJobSync = true; },
		getEditedMovies: () => editedMovies,
		getCurrentResult: () => currentResult,
		getPosterPreviewOverrides: () => posterPreviewOverrides,
		getPosterCropStates: () => posterCropStates,
		getCropMetrics: () => cropMetrics,
		getCropBox: () => cropBox,
		getQueryClient: () => queryClient,
		getCurrentMovieIndex: () => currentMovieIndex,
		setCurrentMovieIndex: (index) => { currentMovieIndex = index; },
		getMovieResultsLength: () => movieResults.length,
		gotoJobs: () => { void goto('/jobs'); },
		setShowPosterCropModal: (show) => { showPosterCropModal = show; },
		updateBatchMoviePosterFromURL: (mutationJobId, movieId, body) => apiClient.updateBatchMoviePosterFromURL(mutationJobId, movieId, body),
		excludeBatchMovie: (mutationJobId, movieId) => apiClient.excludeBatchMovie(mutationJobId, movieId),
		updateBatchMovie: (mutationJobId, movieId, movie) => apiClient.updateBatchMovie(mutationJobId, movieId, movie),
		updateBatchMoviePosterCrop: (mutationJobId, movieId, crop) => apiClient.updateBatchMoviePosterCrop(mutationJobId, movieId, crop),
		toastSuccess: (message, duration) => toastStore.success(message, duration),
		toastError: (message, duration) => toastStore.error(message, duration),
	});

	function updateCurrentMovie(movie: Movie) {
		if (!currentResult?.data) return;

		const isActuallyModified = !equal(movie, currentResult.data);

		if (isActuallyModified) {
			editedMovies.set(currentResult.file_path, movie);
		} else {
			editedMovies.delete(currentResult.file_path);
		}
	}

	function resetCurrentMovie() {
		if (!currentResult?.data) return;
		editedMovies.delete(currentResult.file_path);
	}

	function clearPosterPreviewOverride() {
		if (!currentResult) return;
		posterPreviewOverrides.delete(currentResult.file_path);
	}

	function resetPoster() {
		if (!currentResult || !currentMovie) return;

		const original = originalPosterState.get(currentResult.file_path);
		if (!original || !original.poster_url) return;

		const posterChanged = currentMovie.poster_url !== original.poster_url
			|| currentMovie.cropped_poster_url !== original.cropped_poster_url
			|| currentMovie.should_crop_poster !== original.should_crop_poster;
		if (!posterChanged) return;

		if (original.poster_url !== currentMovie.poster_url) {
			mutations.applyPosterFromUrl(currentMovie.id, original.poster_url);
		} else {
			updateCurrentMovie({
				...currentMovie,
				cropped_poster_url: original.cropped_poster_url,
				should_crop_poster: original.should_crop_poster
			});
			clearPosterPreviewOverride();
		}
	}

	function useScreenshotAsPoster(url: string) {
		if (!currentMovie || !currentResult) return;

		const confirmed = typeof window === 'undefined'
			? true
			: window.confirm('Use this screenshot as the poster? This will replace the current poster image.');

		if (!confirmed) return;

		clearPosterPreviewOverride();
		mutations.applyPosterFromUrl(currentMovie.id, url);
	}

	async function saveAllEdits() {
		return mutations.saveEditsMutation.mutateAsync();
	}

	const organizeController = createOrganizeController({
		getJobId: () => jobId,
		getIsUpdateMode: () => isUpdateMode,
		getJob: () => job,
		setJob: (nextJob) => { job = nextJob; },
		getDestinationPath: () => destinationPath,
		getOrganizeOperation: () => organizeOperation,
		getOperationMode: () => getEffectiveOperationMode(),
		getEditedMovies: () => editedMovies,
		saveAllEdits,
		getOrganizeStatus: () => organizeStatus,
		setOrganizeStatus: (nextStatus) => { organizeStatus = nextStatus; },
		setOrganizing: (nextOrganizing) => { organizing = nextOrganizing; },
		setOrganizeProgress: (nextProgress) => { organizeProgress = nextProgress; },
		getFileStatuses: () => fileStatuses,
		getExpectedOrganizeFilePaths: () => expectedOrganizeFilePaths,
		setExpectedOrganizeFilePaths: (nextPaths) => { expectedOrganizeFilePaths = nextPaths; },
		clearWebSocketMessages: websocketStore.clearMessages,
		toastSuccess: (message, duration) => toastStore.success(message, duration),
		toastError: (message, duration) => toastStore.error(message, duration),
		toastInfo: (message, duration) => toastStore.info(message, duration),
		navigateBrowse: () => { void goto('/browse'); },
		api: {
			getBatchJob: (nextJobId) => apiClient.getBatchJob(nextJobId, true),
			organizeBatchJob: (nextJobId, request) => apiClient.organizeBatchJob(nextJobId, request),
			updateBatchJob: (nextJobId, request) => apiClient.updateBatchJob(nextJobId, request)
		}
	});

	const rescrapeController = createRescrapeController({
		getJobId: () => jobId,
		getCurrentResult: () => currentResult,
		getJob: () => job,
		setJob: (nextJob) => { job = nextJob; },
		getEditedMovies: () => editedMovies,
		getAvailableScrapers: () => availableScrapers,
		setAvailableScrapers: (scrapers) => { availableScrapers = scrapers; },
		getRescrapeMovieId: () => rescrapeMovieId,
		setRescrapeMovieId: (movieId) => { rescrapeMovieId = movieId; },
		getSelectedScrapers: () => rescrapeSelectedScrapers,
		setSelectedScrapers: (scrapers) => { rescrapeSelectedScrapers = scrapers; },
		getManualSearchMode: () => manualSearchMode,
		setManualSearchMode: (manual) => { manualSearchMode = manual; },
		getManualSearchInput: () => manualSearchInput,
		setManualSearchInput: (input) => { manualSearchInput = input; },
		setShowRescrapeModal: (show) => { showRescrapeModal = show; },
		getRescrapePreset: () => rescrapePreset,
		setRescrapePreset: (preset) => { rescrapePreset = preset; },
		getRescrapeScalarStrategy: () => rescrapeScalarStrategy,
		setRescrapeScalarStrategy: (strategy) => { rescrapeScalarStrategy = strategy; },
		getRescrapeArrayStrategy: () => rescrapeArrayStrategy,
		setRescrapeArrayStrategy: (strategy) => { rescrapeArrayStrategy = strategy; },
		getRescrapingStates: () => rescrapingStates,
		toastSuccess: (message, duration) => toastStore.success(message, duration),
		toastError: (message, duration) => toastStore.error(message, duration),
		api: {
			getScrapers: () => apiClient.getScrapers(),
			rescrapeBatchMovie: (nextJobId, movieId, req) =>
				apiClient.rescrapeBatchMovie(nextJobId, movieId, req)
		}
	});

	const posterCropController = createPosterCropController({
		getBrowser: () => browser,
		getJobId: () => jobId,
		getCurrentMovie: () => currentMovie,
		getCurrentResult: () => currentResult,
		getShowPosterCropModal: () => showPosterCropModal,
		setShowPosterCropModal: (show) => { showPosterCropModal = show; },
		setPosterCropLoadError: (errorMessage) => { posterCropLoadError = errorMessage; },
		getCropSourceURL: () => cropSourceURL,
		setCropSourceURL: (url) => { cropSourceURL = url; },
		getCropImageElement: () => cropImageElement,
		setCropImageElement: (imageElement) => { cropImageElement = imageElement; },
		getCropMetrics: () => cropMetrics,
		setCropMetrics: (metrics) => { cropMetrics = metrics; },
		getCropBox: () => cropBox,
		setCropBox: (nextBox) => { cropBox = nextBox; },
		getCropDragState: () => cropDragState,
		setCropDragState: (state) => { cropDragState = state; },
		getPosterCropStates: () => posterCropStates,
		mutatePosterCrop: (mutationJobId, movieId, crop) => {
			mutations.posterCropMutation.mutate({ jobId: mutationJobId, movieId, crop });
		}
	});

	const reviewPageController = createReviewPageController({
		getJob: () => job,
		getCurrentMovie: () => currentMovie,
		getEditedMovies: () => editedMovies,
		getDestinationPath: () => destinationPath,
		setDestinationPath: (path) => { destinationPath = path; },
		getTempDestinationPath: () => tempDestinationPath,
		setTempDestinationPath: (path) => { tempDestinationPath = path; },
		setShowDestinationBrowser: (show) => { showDestinationBrowser = show; },
		setShowImageViewer: (show) => { showImageViewer = show; },
		setImageViewerImages: (images) => { imageViewerImages = images; },
		setImageViewerIndex: (index) => { imageViewerIndex = index; },
		setImageViewerTitle: (title) => { imageViewerTitle = title; },
		excludeMovie: (mutationJobId, movieId) => {
			mutations.excludeMovieMutation.mutate({ jobId: mutationJobId, movieId });
		},
		api: {
			getPreviewImageURL: (url) => apiClient.getPreviewImageURL(url)
		}
	});

	function applyRescrapePreset(preset: 'conservative' | 'gap-fill' | 'aggressive') {
		rescrapeController.applyRescrapePreset(preset);
	}

	async function openRescrapeModal(movieId: string) {
		await rescrapeController.openRescrapeModal(movieId);
	}

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
			showFieldScraperSources ? 'true' : 'false'
		);
	});

	$effect(() => {
		if (!browser) return;
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

	$effect(() => {
		const unsubscribe = websocketStore.subscribe((ws) => {
			organizeController.handleWebSocketMessage(ws.messages.at(-1));
		});

		return unsubscribe;
	});

	onMount(() => {
		if (browser) {
			showFieldScraperSources =
				localStorage.getItem(SHOW_FIELD_SCRAPER_SOURCES_KEY) === 'true';
			viewMode = localStorage.getItem(VIEW_MODE_KEY) === 'grid' ? 'grid' : 'detail';
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
		}
		const urlDestination = pageStore.url.searchParams.get('destination');
		if (urlDestination) {
			destinationPath = urlDestination;
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
		get job() { return job; },
		get loading() { return loading; },
		get error() { return error; },
		get config() { return config; },
		get currentMovieIndex() { return currentMovieIndex; },
		set currentMovieIndex(v) { currentMovieIndex = v; },
		get editedMovies() { return editedMovies; },
		get organizing() { return organizing; },
		set organizing(v) { organizing = v; },
		get destinationPath() { return destinationPath; },
		set destinationPath(v) { destinationPath = v; },
		get organizeOperation() { return organizeOperation; },
		set organizeOperation(v) { organizeOperation = v; },
		get showDestinationBrowser() { return showDestinationBrowser; },
		set showDestinationBrowser(v) { showDestinationBrowser = v; },
		get tempDestinationPath() { return tempDestinationPath; },
		set tempDestinationPath(v) { tempDestinationPath = v; },
		get showTrailerModal() { return showTrailerModal; },
		set showTrailerModal(v) { showTrailerModal = v; },
		get isUpdateMode() { return isUpdateMode; },
		get showFieldScraperSources() { return showFieldScraperSources; },
		set showFieldScraperSources(v) { showFieldScraperSources = v; },
		get viewMode() { return viewMode; },
		set viewMode(v) { viewMode = v; },
		get organizeProgress() { return organizeProgress; },
		set organizeProgress(v) { organizeProgress = v; },
		get organizeStatus() { return organizeStatus; },
		set organizeStatus(v) { organizeStatus = v; },
		get fileStatuses() { return fileStatuses; },
		get expectedOrganizeFilePaths() { return expectedOrganizeFilePaths; },
		set expectedOrganizeFilePaths(v) { expectedOrganizeFilePaths = v; },
		get showCoverPanel() { return showCoverPanel; },
		get showPosterPanel() { return showPosterPanel; },
		get showTrailerPanel() { return showTrailerPanel; },
		get showScreenshotsPanel() { return showScreenshotsPanel; },
		get showImageViewer() { return showImageViewer; },
		set showImageViewer(v) { showImageViewer = v; },
		get imageViewerImages() { return imageViewerImages; },
		set imageViewerImages(v) { imageViewerImages = v; },
		get imageViewerIndex() { return imageViewerIndex; },
		set imageViewerIndex(v) { imageViewerIndex = v; },
		get imageViewerTitle() { return imageViewerTitle; },
		set imageViewerTitle(v) { imageViewerTitle = v; },
		get showAllSidebarScreenshots() { return showAllSidebarScreenshots; },
		set showAllSidebarScreenshots(v) { showAllSidebarScreenshots = v; },
		get showFullSourcePath() { return showFullSourcePath; },
		set showFullSourcePath(v) { showFullSourcePath = v; },
		get forceOverwrite() { return forceOverwrite; },
		set forceOverwrite(v) { forceOverwrite = v; },
		get preserveNfo() { return preserveNfo; },
		set preserveNfo(v) { preserveNfo = v; },
		get skipNfo() { return skipNfo; },
		set skipNfo(v) { skipNfo = v; },
		get skipDownload() { return skipDownload; },
		set skipDownload(v) { skipDownload = v; },
		get showImagePanelContent() { return showImagePanelContent; },
		set showImagePanelContent(v) { showImagePanelContent = v; },
		get showAllPreviewScreenshots() { return showAllPreviewScreenshots; },
		set showAllPreviewScreenshots(v) { showAllPreviewScreenshots = v; },
		get showPosterCropModal() { return showPosterCropModal; },
		set showPosterCropModal(v) { showPosterCropModal = v; },
		get posterCropLoadError() { return posterCropLoadError; },
		set posterCropLoadError(v) { posterCropLoadError = v; },
		get cropSourceURL() { return cropSourceURL; },
		set cropSourceURL(v) { cropSourceURL = v; },
		get cropImageElement() { return cropImageElement; },
		set cropImageElement(v) { cropImageElement = v; },
		get cropMetrics() { return cropMetrics; },
		set cropMetrics(v) { cropMetrics = v; },
		get cropBox() { return cropBox; },
		set cropBox(v) { cropBox = v; },
		get cropDragState() { return cropDragState; },
		set cropDragState(v) { cropDragState = v; },
		get posterPreviewOverrides() { return posterPreviewOverrides; },
		get posterCropStates() { return posterCropStates; },
		get availableScrapers() { return availableScrapers; },
		set availableScrapers(v) { availableScrapers = v; },
		get showRescrapeModal() { return showRescrapeModal; },
		set showRescrapeModal(v) { showRescrapeModal = v; },
		get rescrapeMovieId() { return rescrapeMovieId; },
		set rescrapeMovieId(v) { rescrapeMovieId = v; },
		get rescrapeSelectedScrapers() { return rescrapeSelectedScrapers; },
		set rescrapeSelectedScrapers(v) { rescrapeSelectedScrapers = v; },
		get rescrapingStates() { return rescrapingStates; },
		get manualSearchMode() { return manualSearchMode; },
		set manualSearchMode(v) { manualSearchMode = v; },
		get manualSearchInput() { return manualSearchInput; },
		set manualSearchInput(v) { manualSearchInput = v; },
		get rescrapePreset() { return rescrapePreset; },
		set rescrapePreset(v) { rescrapePreset = v; },
		get rescrapeScalarStrategy() { return rescrapeScalarStrategy; },
		set rescrapeScalarStrategy(v) { rescrapeScalarStrategy = v; },
		get rescrapeArrayStrategy() { return rescrapeArrayStrategy; },
		set rescrapeArrayStrategy(v) { rescrapeArrayStrategy = v; },
		get movieGroups() { return movieGroups; },
		get movieResults() { return movieResults; },
		get currentMovieGroup() { return currentMovieGroup; },
		get currentResult() { return currentResult; },
		get currentMovie() { return currentMovie; },
		get displayPosterUrl() { return displayPosterUrl; },
		get preview() { return preview; },
		get previewNeedsDestination() { return previewNeedsDestination; },
		get canOrganize() { return canOrganize; },
		posterFromUrlMutation: mutations.posterFromUrlMutation,
		posterCropMutation: mutations.posterCropMutation,
		resolvePosterUrl,
		getEffectiveOperationMode,
		updateCurrentMovie,
		resetCurrentMovie,
		resetPoster,
		useScreenshotAsPoster,
		saveAllEdits,
		organizeController,
		rescrapeController,
		posterCropController,
		reviewPageController,
		applyRescrapePreset,
		openRescrapeModal,
		executeRescrape,
		organizeAll,
		updateAll,
		retryFailed,
	};
}
