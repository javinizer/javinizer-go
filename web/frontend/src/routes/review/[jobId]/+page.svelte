<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { fade } from 'svelte/transition';
	import { browser } from '$app/environment';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { apiClient } from '$lib/api/client';
	import type { BatchJobResponse, FileResult, Movie, OrganizePreviewResponse, Scraper } from '$lib/api/types';
	import { toastStore } from '$lib/stores/toast';
	import { websocketStore } from '$lib/stores/websocket';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import ActressEditor from '$lib/components/ActressEditor.svelte';
	import ImageViewer from '$lib/components/ImageViewer.svelte';
	import VideoModal from '$lib/components/VideoModal.svelte';
	import DestinationBrowserModal from './components/DestinationBrowserModal.svelte';
	import DestinationSettingsCard from './components/DestinationSettingsCard.svelte';
	import ImagesMediaCard from './components/ImagesMediaCard.svelte';
	import MovieNavigationCard from './components/MovieNavigationCard.svelte';
	import MovieMetadataCard from './components/MovieMetadataCard.svelte';
	import OrganizeStatusCard from './components/OrganizeStatusCard.svelte';
	import PosterCropModal from './components/PosterCropModal.svelte';
	import ReviewActionBar from './components/ReviewActionBar.svelte';
	import ReviewHeader from './components/ReviewHeader.svelte';
	import ReviewMediaSidebar from './components/ReviewMediaSidebar.svelte';
	import RescrapeModal from './components/RescrapeModal.svelte';
	import SourceFilesCard from './components/SourceFilesCard.svelte';
	import {
		createOrganizeController,
		type FileStatus,
		type OrganizeOperation
	} from './logic/organize-controller';
	import {
		createRescrapeController,
		type ArrayStrategy,
		type ScalarStrategy
	} from './logic/rescrape-controller';
	import {
		createPosterCropController,
		type PosterCropDragState
	} from './logic/poster-crop-controller';
	import { createReviewPageController } from './logic/review-page-controller';
	import {
		type PosterCropBox,
		type PosterCropMetrics,
		type PosterCropState,
		type PosterPreviewOverride
	} from './review-utils';
	import equal from 'fast-deep-equal';
	import {
		ChevronLeft,
		CircleAlert
	} from 'lucide-svelte';

	let jobId = $derived($page.params.jobId as string);
	let job: BatchJobResponse | null = $state(null);
	let config: any = $state(null);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let currentMovieIndex = $state(0);
	let editedMovies = $state<Map<string, Movie>>(new Map());
	let organizing = $state(false);
	let destinationPath = $state('');
	let organizeOperation = $state<OrganizeOperation>('move');
	let showDestinationBrowser = $state(false);
	let tempDestinationPath = $state('');
	let showTrailerModal = $state(false);
	let preview: OrganizePreviewResponse | null = $state(null);
	let isUpdateMode = $derived($page.url.searchParams.get('update') === 'true');
	let showFieldScraperSources = $state(false);
	const SHOW_FIELD_SCRAPER_SOURCES_KEY = 'javinizer.review.showFieldScraperSources';

	// Organize operation state
	let organizeProgress = $state(0);
	let organizeStatus = $state<'idle' | 'organizing' | 'completed' | 'failed'>('idle');
	let fileStatuses = $state<Map<string, FileStatus>>(new Map());
	let expectedOrganizeFilePaths = $state<string[]>([]);

	// Determine which panels to show based on download settings
	// Config uses snake_case JSON property names
	const showCoverPanel = $derived(config?.output?.download_cover ?? true);
	const showPosterPanel = $derived(config?.output?.download_poster ?? true);
	const showTrailerPanel = $derived(config?.output?.download_trailer ?? true);
	const showScreenshotsPanel = $derived(config?.output?.download_extrafanart ?? true);

	// Image viewer state (unified for screenshots and cover)
	let showImageViewer = $state(false);
	let imageViewerImages = $state<string[]>([]);
	let imageViewerIndex = $state(0);
	let imageViewerTitle = $state<string | undefined>(undefined);

	// Sidebar screenshot expansion state
	let showAllSidebarScreenshots = $state(false);

	// Source file path expansion state
	let showFullSourcePath = $state(false);

	// Image panel collapse state
	let showImagePanelContent = $state(true);

	// Preview screenshot expansion state
	let showAllPreviewScreenshots = $state(false);

	// Manual poster crop state
	let showPosterCropModal = $state(false);
	let posterCropSaving = $state(false);
	let posterCropLoadError = $state<string | null>(null);
	let cropSourceURL = $state('');
	let cropImageElement = $state<HTMLImageElement | null>(null);
	let cropMetrics = $state<PosterCropMetrics | null>(null);
	let cropBox = $state<PosterCropBox | null>(null);
	let cropDragState = $state<PosterCropDragState | null>(null);
	let posterPreviewOverrides = $state<Map<string, PosterPreviewOverride>>(new Map());
	let posterCropStates = $state<Map<string, PosterCropState>>(new Map());

	// Rescrape modal state
	let availableScrapers: Scraper[] = $state([]);
	let showRescrapeModal = $state(false);
	let rescrapeMovieId = $state('');
	let rescrapeSelectedScrapers: string[] = $state([]);
	let rescrapingStates = $state<Map<string, boolean>>(new Map());
	// Manual search mode state
	let manualSearchMode = $state(false);
	let manualSearchInput = $state('');

	let rescrapePreset: string | undefined = $state(undefined);  // Merge strategy preset: conservative, gap-fill, aggressive
	let rescrapeScalarStrategy: ScalarStrategy = $state('prefer-nfo');  // For scalar fields
	let rescrapeArrayStrategy: ArrayStrategy = $state('merge');        // For array fields

	// Group file results by movie_id to handle multi-part files
	// Each movie group contains all file results with the same movie_id
	interface MovieGroup {
		movieId: string;
		results: FileResult[];
		primaryResult: FileResult; // The first result in the group (for display)
	}

	const movieGroups = $derived<MovieGroup[]>(
		job ? (() => {
			const excluded = (job as BatchJobResponse).excluded || {};
			const allResults = (Object.values((job as BatchJobResponse).results) as FileResult[])
				.filter((r) => {
					// Filter out files that are not completed or don't have data
					if (r.status !== 'completed' || !r.data) {
						return false;
					}
					// Filter out files that are excluded
					if (excluded[r.file_path]) {
						return false;
					}
					return true;
				});

			// Group by movie_id
			const grouped = new Map<string, FileResult[]>();
			for (const result of allResults) {
				const movieId = result.movie_id;
				if (!grouped.has(movieId)) {
					grouped.set(movieId, []);
				}
				grouped.get(movieId)!.push(result);
			}

			// Convert to MovieGroup array
			return Array.from(grouped.entries()).map(([movieId, results]) => ({
				movieId,
				results,
				primaryResult: results[0] // Use first result as primary
			}));
		})() : []
	);

	// Get all successful movie results (kept for backward compatibility with UI)
	const movieResults = $derived<FileResult[]>(movieGroups.map(g => g.primaryResult));

	const currentMovieGroup = $derived<MovieGroup | undefined>(movieGroups[currentMovieIndex]);
	const currentResult = $derived<FileResult | undefined>(currentMovieGroup?.primaryResult);
	const currentMovie = $derived<Movie | null>(
		currentResult && currentResult.data
			? editedMovies.get(currentResult.file_path) || currentResult.data
			: null
	);

	// Use manual override if available, then cropped_poster_url, then poster_url.
	// Manual overrides add a cache-buster so updated temp posters refresh immediately.
	const displayPosterUrl = $derived<string | undefined>(
		(() => {
			if (!currentMovie || !currentResult) return undefined;

			const override = posterPreviewOverrides.get(currentResult.file_path);
			const baseURL = override?.url || currentMovie.cropped_poster_url || currentMovie.poster_url;
			if (!baseURL) return undefined;

			if (!override) return baseURL;

			const separator = baseURL.includes('?') ? '&' : '?';
			return `${baseURL}${separator}v=${override.version}`;
		})()
	);

	async function fetchJob() {
		try {
			job = await apiClient.getBatchJob(jobId);
			if (job && job.destination && !destinationPath) {
				destinationPath = job.destination;
			}
			loading = false;
		} catch (e) {
			console.error('Failed to fetch batch job:', e);
			error = e instanceof Error ? e.message : 'Failed to fetch job';
			loading = false;
		}
	}

	async function fetchConfig() {
		try {
			config = await apiClient.getConfig();
		} catch (e) {
			console.error('Failed to fetch config:', e);
		}
	}

	async function fetchPreview() {
		if (!destinationPath.trim() || !currentMovie) {
			preview = null;
			return;
		}

		const copyOnly = organizeOperation !== 'move';
		const linkMode = organizeOperation === 'hardlink'
			? 'hard'
			: organizeOperation === 'softlink'
				? 'soft'
				: undefined;

		try {
			preview = await apiClient.previewOrganize(jobId, currentMovie.id, {
				destination: destinationPath,
				copy_only: copyOnly,
				link_mode: linkMode
			});
		} catch (e) {
			console.error('Failed to fetch preview:', e);
			preview = null;
		}
	}

	// Fetch preview when destination, operation mode, or current movie changes
	$effect(() => {
		if (destinationPath && currentMovie) {
			fetchPreview();
		} else {
			preview = null;
		}
	});

	// Reset full path display when navigating between movies
	$effect(() => {
		currentMovieIndex; // track dependency
		showFullSourcePath = false;
	});

	$effect(() => {
		if (!browser) return;
		localStorage.setItem(
			SHOW_FIELD_SCRAPER_SOURCES_KEY,
			showFieldScraperSources ? 'true' : 'false'
		);
	});

	// Subscribe to WebSocket messages during organize operation
	$effect(() => {
		const unsubscribe = websocketStore.subscribe((ws) => {
			organizeController.handleWebSocketMessage(ws.messages.at(-1));
		});

		return unsubscribe;
	});

	function updateCurrentMovie(movie: Movie) {
		if (!currentResult?.data) return;

		// Use fast-deep-equal to compare with original
		const isActuallyModified = !equal(movie, currentResult.data);

		if (isActuallyModified) {
			editedMovies.set(currentResult.file_path, movie);
		} else {
			// Remove from edited movies if no actual changes
			editedMovies.delete(currentResult.file_path);
		}
		editedMovies = editedMovies; // Trigger reactivity
	}

	function resetCurrentMovie() {
		if (!currentResult?.data) return;
		editedMovies.delete(currentResult.file_path);
		editedMovies = editedMovies;
	}

	async function saveAllEdits() {
		// Save all edited movies to backend
		const savePromises = Array.from(editedMovies.entries()).map(([filePath, movie]) => {
			return apiClient.updateBatchMovie(jobId, movie.id, movie);
		});

		if (savePromises.length > 0) {
			await Promise.all(savePromises);
		}
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
			organizeProgress = nextProgress;
		},
		getFileStatuses: () => fileStatuses,
		setFileStatuses: (nextStatuses) => {
			fileStatuses = nextStatuses;
		},
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
			getBatchJob: (nextJobId) => apiClient.getBatchJob(nextJobId),
			organizeBatchJob: (nextJobId, request) => apiClient.organizeBatchJob(nextJobId, request),
			updateBatchJob: (nextJobId) => apiClient.updateBatchJob(nextJobId)
		}
	});

	const rescrapeController = createRescrapeController({
		getJobId: () => jobId,
		getCurrentResult: () => currentResult,
		getJob: () => job,
		setJob: (nextJob) => {
			job = nextJob;
		},
		getEditedMovies: () => editedMovies,
		setEditedMovies: (movies) => {
			editedMovies = movies;
		},
		getAvailableScrapers: () => availableScrapers,
		setAvailableScrapers: (scrapers) => {
			availableScrapers = scrapers;
		},
		getRescrapeMovieId: () => rescrapeMovieId,
		setRescrapeMovieId: (movieId) => {
			rescrapeMovieId = movieId;
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
		setRescrapingStates: (states) => {
			rescrapingStates = states;
		},
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
		setShowPosterCropModal: (show) => {
			showPosterCropModal = show;
		},
		getPosterCropSaving: () => posterCropSaving,
		setPosterCropSaving: (saving) => {
			posterCropSaving = saving;
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
		getCropDragState: () => cropDragState,
		setCropDragState: (state) => {
			cropDragState = state;
		},
		getPosterPreviewOverrides: () => posterPreviewOverrides,
		setPosterPreviewOverrides: (overrides) => {
			posterPreviewOverrides = overrides;
		},
		getPosterCropStates: () => posterCropStates,
		setPosterCropStates: (states) => {
			posterCropStates = states;
		},
		toastSuccess: (message, duration) => toastStore.success(message, duration),
		toastError: (message, duration) => toastStore.error(message, duration),
		api: {
			updateBatchMoviePosterCrop: (nextJobId, movieId, crop) =>
				apiClient.updateBatchMoviePosterCrop(nextJobId, movieId, crop)
		}
	});

	const reviewPageController = createReviewPageController({
		getJob: () => job,
		getCurrentMovie: () => currentMovie,
		getMovieResultsLength: () => movieResults.length,
		getCurrentMovieIndex: () => currentMovieIndex,
		setCurrentMovieIndex: (index) => {
			currentMovieIndex = index;
		},
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
		refetchJob: fetchJob,
		toastSuccess: (message, duration) => toastStore.success(message, duration),
		toastError: (message, duration) => toastStore.error(message, duration),
		navigateBatch: () => goto('/jobs'),
		api: {
			excludeBatchMovie: (nextJobId, movieId) => apiClient.excludeBatchMovie(nextJobId, movieId),
			getPreviewImageURL: (url) => apiClient.getPreviewImageURL(url)
		}
	});

	function applyRescrapePreset(preset: 'conservative' | 'gap-fill' | 'aggressive') {
		rescrapeController.applyRescrapePreset(preset);
	}

	async function openRescrapeModal(movieId: string) {
		await rescrapeController.openRescrapeModal(movieId);
	}

	async function executeRescrape() {
		await rescrapeController.executeRescrape();
	}

	async function organizeAll() {
		await organizeController.organizeAll();
	}

	async function updateAll() {
		await organizeController.updateAll();
	}

	async function retryFailed() {
		await organizeController.retryFailed();
	}

	onMount(() => {
		fetchJob();
		fetchConfig();
		if (browser) {
			showFieldScraperSources =
				localStorage.getItem(SHOW_FIELD_SCRAPER_SOURCES_KEY) === 'true';
		}
		// Get destination from URL params if provided
		const urlDestination = $page.url.searchParams.get('destination');
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
</script>

<div class="container mx-auto px-4 py-8">
	<div class="max-w-7xl mx-auto space-y-6">
		{#if loading}
			<div class="text-center py-12">
				<p class="text-muted-foreground">Loading batch job...</p>
			</div>
		{:else if error}
			<Card class="p-6">
				<div class="text-center text-destructive">
					<CircleAlert class="h-12 w-12 mx-auto mb-4" />
					<p class="font-semibold">Error</p>
					<p class="text-sm">{error}</p>
					<Button onclick={() => goto('/browse')} class="mt-4">
						{#snippet children()}
							<ChevronLeft class="h-4 w-4 mr-2" />
							Back to Browse
						{/snippet}
					</Button>
				</div>
			</Card>
		{:else if job && movieResults.length === 0}
			<Card class="p-6">
				<div class="text-center">
					<p class="text-muted-foreground">No movies to review</p>
					<Button onclick={() => goto('/browse')} class="mt-4">
						{#snippet children()}
							<ChevronLeft class="h-4 w-4 mr-2" />
							Back to Browse
						{/snippet}
					</Button>
				</div>
			</Card>
		{:else if currentMovie && currentResult}
			<ReviewHeader
				isUpdateMode={isUpdateMode}
				organizing={organizing}
				movieResultsLength={movieResults.length}
				destinationPath={destinationPath}
				onClose={() => goto('/browse')}
				onUpdateAll={updateAll}
				onOrganizeAll={organizeAll}
			/>

			<OrganizeStatusCard
				organizeStatus={organizeStatus}
				organizeProgress={organizeProgress}
				fileStatuses={fileStatuses}
				expectedOrganizeFilePaths={expectedOrganizeFilePaths}
				isUpdateMode={isUpdateMode}
				onRetryFailed={retryFailed}
				onContinue={() => goto('/browse')}
			/>

			{#key currentResult.file_path}
				<div class="grid grid-cols-1 lg:grid-cols-[300px_1fr] gap-6" in:fade|local={{ duration: 180 }}>
					<ReviewMediaSidebar
						currentMovie={currentMovie}
						displayPosterUrl={displayPosterUrl}
						showPosterPanel={showPosterPanel}
						showCoverPanel={showCoverPanel}
						showTrailerPanel={showTrailerPanel}
						showScreenshotsPanel={showScreenshotsPanel}
						bind:showAllSidebarScreenshots={showAllSidebarScreenshots}
						bind:showTrailerModal={showTrailerModal}
						onOpenPosterCropModal={posterCropController.openPosterCropModal}
						onOpenCoverViewer={reviewPageController.openCoverViewer}
						onOpenScreenshotViewer={reviewPageController.openScreenshotViewer}
						previewImageURL={reviewPageController.previewImageURL}
					/>

				<!-- Right: Main Content -->
				<div class="space-y-6 min-w-0">
					<MovieNavigationCard
						bind:currentMovieIndex={currentMovieIndex}
						movieResultsLength={movieResults.length}
						currentMovieId={currentMovie.id}
						hasChanges={reviewPageController.hasChanges(currentResult.file_path)}
						onExclude={reviewPageController.excludeCurrentMovie}
					/>

					<SourceFilesCard
						sourceResults={currentMovieGroup?.results || [currentResult]}
						primaryFilePath={currentResult.file_path}
						bind:showFullSourcePath={showFullSourcePath}
					/>

					<!-- Destination Path (hidden in update mode) -->
					{#if !isUpdateMode}
						<DestinationSettingsCard
							bind:destinationPath={destinationPath}
							bind:organizeOperation={organizeOperation}
							preview={preview}
							bind:showAllPreviewScreenshots={showAllPreviewScreenshots}
							onOpenDestinationBrowser={reviewPageController.openDestinationBrowser}
						/>
					{/if}

					<MovieMetadataCard
						currentMovie={currentMovie}
						currentResult={currentResult}
						bind:showFieldScraperSources={showFieldScraperSources}
						isRescraping={rescrapingStates.get(currentMovie?.id || '') || false}
						onOpenRescrape={() => currentMovie && openRescrapeModal(currentMovie.id)}
						onResetCurrentMovie={resetCurrentMovie}
						onUpdateCurrentMovie={updateCurrentMovie}
					/>

					<!-- Actresses -->
					<Card class="p-6">
						<ActressEditor
							movie={currentMovie!}
							onUpdate={updateCurrentMovie}
							actressSources={currentResult.actress_sources}
							showFieldSources={showFieldScraperSources}
						/>
					</Card>

					<ImagesMediaCard
						showScreenshotsPanel={showScreenshotsPanel}
						bind:showImagePanelContent={showImagePanelContent}
						currentMovie={currentMovie}
						currentResult={currentResult}
						displayPosterUrl={displayPosterUrl}
						showFieldScraperSources={showFieldScraperSources}
						onUpdateCurrentMovie={updateCurrentMovie}
					/>

					<ReviewActionBar
						isUpdateMode={isUpdateMode}
						organizing={organizing}
						destinationPath={destinationPath}
						movieResultsLength={movieResults.length}
						onCancel={() => goto('/browse')}
						onOrganizeAll={organizeAll}
					/>
				</div>
				</div>
			{/key}
{/if}
	</div>
</div>

<!-- Trailer Modal -->
<VideoModal
	bind:show={showTrailerModal}
	videoUrl={currentMovie?.trailer_url ?? ''}
	title="Trailer"
	onClose={() => (showTrailerModal = false)}
/>

<!-- Image Viewer (for screenshots and cover) -->
<ImageViewer
	bind:show={showImageViewer}
	images={imageViewerImages}
	initialIndex={imageViewerIndex}
	title={imageViewerTitle}
	onClose={reviewPageController.closeImageViewer}
/>

<PosterCropModal
	bind:show={showPosterCropModal}
	posterCropSaving={posterCropSaving}
	posterCropLoadError={posterCropLoadError}
	cropSourceURL={cropSourceURL}
	cropMetrics={cropMetrics}
	cropBox={cropBox}
	overlayStyle={posterCropController.getPosterCropOverlayStyle()}
	onClose={posterCropController.closePosterCropModal}
	onReset={posterCropController.resetPosterCropBox}
	onApply={posterCropController.applyPosterCrop}
	onImageLoad={posterCropController.handlePosterCropImageLoad}
	onImageError={posterCropController.handlePosterCropImageError}
	onCropMouseDown={posterCropController.startPosterCropDrag}
/>

<RescrapeModal
	bind:show={showRescrapeModal}
	rescraping={rescrapingStates.get(rescrapeMovieId) || false}
	rescrapeMovieId={rescrapeMovieId}
	availableScrapers={availableScrapers}
	bind:selectedScrapers={rescrapeSelectedScrapers}
	bind:manualSearchMode={manualSearchMode}
	bind:manualSearchInput={manualSearchInput}
	bind:rescrapePreset={rescrapePreset}
	bind:rescrapeScalarStrategy={rescrapeScalarStrategy}
	onApplyPreset={(preset) => applyRescrapePreset(preset)}
	onExecute={executeRescrape}
/>

<DestinationBrowserModal
	bind:show={showDestinationBrowser}
	destinationPath={destinationPath}
	bind:tempDestinationPath={tempDestinationPath}
	onCancel={reviewPageController.cancelDestination}
	onConfirm={reviewPageController.confirmDestination}
/>
