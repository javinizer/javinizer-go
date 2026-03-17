<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { flip } from 'svelte/animate';
	import { quintOut } from 'svelte/easing';
	import { fade, scale, slide } from 'svelte/transition';
	import { browser } from '$app/environment';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { apiClient } from '$lib/api/client';
	import type { BatchJobResponse, FileResult, Movie, OrganizePreviewResponse, Scraper } from '$lib/api/types';
	import { toastStore } from '$lib/stores/toast';
	import { websocketStore } from '$lib/stores/websocket';
	import { portalToBody } from '$lib/actions/portal';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import FileBrowser from '$lib/components/FileBrowser.svelte';
	import MovieEditor from '$lib/components/MovieEditor.svelte';
	import ActressEditor from '$lib/components/ActressEditor.svelte';
	import ScreenshotManager from '$lib/components/ScreenshotManager.svelte';
	import ImageViewer from '$lib/components/ImageViewer.svelte';
	import VideoModal from '$lib/components/VideoModal.svelte';
	import ScraperSelector from '$lib/components/ScraperSelector.svelte';
	import equal from 'fast-deep-equal';
	import {
		ChevronLeft,
		ChevronRight,
		ChevronDown,
		ChevronUp,
		Play,
		RotateCcw,
		CircleAlert,
		FolderOpen,
		Image as ImageIcon,
		Loader2,
		X,
		Check,
		RefreshCw,
		Trash2
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
	type OrganizeOperation = 'move' | 'copy' | 'hardlink' | 'softlink';
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
	let fileStatuses = $state<Map<string, {status: string, error?: string}>>(new Map());
	let expectedOrganizeFilePaths = $state<string[]>([]);
	const ORGANIZE_POLL_INTERVAL_MS = 1500;
	const ORGANIZE_POLL_TIMEOUT_MS = 10 * 60 * 1000;
	let organizePollTimer: ReturnType<typeof setTimeout> | null = null;
	let organizeCompletionTimer: ReturnType<typeof setTimeout> | null = null;

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

	// Smart path truncation - show beginning and end with ... in middle
	function truncatePath(path: string, maxLength: number = 80): string {
		if (path.length <= maxLength) return path;

		const ellipsis = '...';
		const charsToShow = maxLength - ellipsis.length;
		const frontChars = Math.ceil(charsToShow * 0.4); // 40% at start
		const backChars = Math.floor(charsToShow * 0.6); // 60% at end (filename is more important)

		return path.slice(0, frontChars) + ellipsis + path.slice(-backChars);
	}

	// Image panel collapse state
	let showImagePanelContent = $state(true);

	// Preview screenshot expansion state
	let showAllPreviewScreenshots = $state(false);

	interface PosterPreviewOverride {
		url: string;
		version: number;
	}

	interface PosterCropBox {
		x: number;
		y: number;
		width: number;
		height: number;
	}

	interface PosterCropMetrics {
		sourceWidth: number;
		sourceHeight: number;
		displayWidth: number;
		displayHeight: number;
		imageOffsetX: number;
		imageOffsetY: number;
	}

	interface PosterCropState {
		xRatio: number;
		yRatio: number;
		widthRatio: number;
		heightRatio: number;
	}

	// Manual poster crop state
	let showPosterCropModal = $state(false);
	let posterCropSaving = $state(false);
	let posterCropLoadError = $state<string | null>(null);
	let cropSourceURL = $state('');
	let cropStageElement = $state<HTMLDivElement | null>(null);
	let cropImageElement = $state<HTMLImageElement | null>(null);
	let cropMetrics = $state<PosterCropMetrics | null>(null);
	let cropBox = $state<PosterCropBox | null>(null);
	let cropDragState = $state<{ startX: number; startY: number; originX: number; originY: number } | null>(null);
	let posterPreviewOverrides = $state<Map<string, PosterPreviewOverride>>(new Map());
	let posterCropStates = $state<Map<string, PosterCropState>>(new Map());

	const LANDSCAPE_CROP_WIDTH_RATIO = 0.472;
	const POSTER_TARGET_ASPECT_RATIO = 2 / 3;

	// Rescrape modal state
	let availableScrapers: Scraper[] = $state([]);
	let showRescrapeModal = $state(false);
	let rescrapeMovieId = $state('');
	let rescrapeSelectedScrapers: string[] = $state([]);
	let rescrapingStates = $state<Map<string, boolean>>(new Map());
	// Manual search mode state
	let manualSearchMode = $state(false);
	let manualSearchInput = $state('');
	// Merge strategies for update mode (two independent options)
	type ScalarStrategy = '' | 'prefer-nfo' | 'prefer-scraper' | 'preserve-existing' | 'fill-missing-only';
	type ArrayStrategy = '' | 'merge' | 'replace';

	let rescrapePreset: string | undefined = $state(undefined);  // Merge strategy preset: conservative, gap-fill, aggressive
	let rescrapeScalarStrategy: ScalarStrategy = $state('prefer-nfo');  // For scalar fields
	let rescrapeArrayStrategy: ArrayStrategy = $state('merge');        // For array fields

	// Apply preset to rescrape scalar and array strategies
	function applyRescrapePreset(preset: string) {
		rescrapePreset = preset;
		switch (preset) {
			case 'conservative':
				rescrapeScalarStrategy = 'preserve-existing';
				rescrapeArrayStrategy = 'merge';
				break;
			case 'gap-fill':
				rescrapeScalarStrategy = 'fill-missing-only';
				rescrapeArrayStrategy = 'merge';
				break;
			case 'aggressive':
				rescrapeScalarStrategy = 'prefer-scraper';
				rescrapeArrayStrategy = 'replace';
				break;
		}
	}

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
			loading = false;
		} catch (e) {
			console.error('Failed to fetch batch job:', e);
			error = e instanceof Error ? e.message : 'Failed to fetch job';
			loading = false;
		}
	}

	async function fetchConfig() {
		try {
			const response = await fetch('/api/v1/config');
			config = await response.json();
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

	function clearOrganizePollTimer() {
		if (organizePollTimer !== null) {
			clearTimeout(organizePollTimer);
			organizePollTimer = null;
		}
	}

	function clearOrganizeCompletionTimer() {
		if (organizeCompletionTimer !== null) {
			clearTimeout(organizeCompletionTimer);
			organizeCompletionTimer = null;
		}
	}

	function getOrganizeEligibleFilePaths(batchJob: BatchJobResponse | null): string[] {
		if (!batchJob) return [];
		const excluded = batchJob.excluded || {};
		return Object.entries(batchJob.results || {})
			.filter(([filePath, result]) => !excluded[filePath] && result.status === 'completed' && !!result.data)
			.map(([filePath]) => filePath);
	}

	function finalizeOrganizeSuccess(message?: string) {
		if (organizeStatus !== 'organizing' || organizeCompletionTimer !== null) {
			return;
		}

		clearOrganizePollTimer();
		organizeProgress = 100;

		// Show 100% progress bar for 800ms before showing completion UI
		organizeCompletionTimer = setTimeout(() => {
			organizeCompletionTimer = null;
			if (organizeStatus !== 'organizing') return;

			organizeStatus = 'completed';
			organizing = false;

			// WebSocket messages can be dropped/reconnected; if no per-file statuses
			// arrived, synthesize successful entries from known eligible files.
			if (fileStatuses.size === 0 && expectedOrganizeFilePaths.length > 0) {
				const synthesized = new Map<string, {status: string, error?: string}>();
				for (const filePath of expectedOrganizeFilePaths) {
					synthesized.set(filePath, {status: 'success'});
				}
				fileStatuses = synthesized;
			}

			const failures = Array.from(fileStatuses.values()).filter((s) => s.status === 'failed').length;
			if (failures === 0) {
				const action = isUpdateMode ? 'updated' : 'organized';
				toastStore.success(message || `All files ${action} successfully! Redirecting in 5 seconds...`, 8000);
				setTimeout(() => goto('/browse'), 5000);
			}
		}, 800);
	}

	function finalizeOrganizeFailure(message: string) {
		if (organizeStatus !== 'organizing') return;

		clearOrganizePollTimer();
		clearOrganizeCompletionTimer();
		organizeStatus = 'failed';
		organizing = false;
		toastStore.error(message, 7000);
	}

	function startOrganizeCompletionPolling() {
		clearOrganizePollTimer();
		const startedAt = Date.now();

		const pollOnce = async () => {
			if (organizeStatus !== 'organizing') {
				clearOrganizePollTimer();
				return;
			}

			try {
				const latestJob = await apiClient.getBatchJob(jobId);
				job = latestJob;

				if (latestJob.status === 'completed') {
					const action = isUpdateMode ? 'Update' : 'Organization';
					finalizeOrganizeSuccess(`${action} completed successfully! Redirecting in 5 seconds...`);
					return;
				}

				if (latestJob.status === 'failed') {
					const action = isUpdateMode ? 'update' : 'organization';
					finalizeOrganizeFailure(`The ${action} job failed.`);
					return;
				}

				if (latestJob.status === 'cancelled') {
					const action = isUpdateMode ? 'Update' : 'Organization';
					finalizeOrganizeFailure(`${action} was cancelled.`);
					return;
				}
			} catch (e) {
				console.warn('Failed to poll batch job status:', e);
			}

			if (Date.now() - startedAt >= ORGANIZE_POLL_TIMEOUT_MS) {
				const action = isUpdateMode ? 'Update' : 'Organization';
				finalizeOrganizeFailure(`${action} timed out while waiting for completion.`);
				return;
			}

			organizePollTimer = setTimeout(() => {
				void pollOnce();
			}, ORGANIZE_POLL_INTERVAL_MS);
		};

		void pollOnce();
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
		if (organizeStatus !== 'organizing') return;

		const unsubscribe = websocketStore.subscribe((ws) => {
			// Get the latest message
			const msg = ws.messages.at(-1);
			if (!msg || msg.job_id !== jobId) return;

			// Handle progress messages from any status that includes progress data
			if (msg.progress !== undefined && msg.progress !== null) {
				organizeProgress = msg.progress;
			}

			if (msg.status === 'failed' && msg.file_path) {
				fileStatuses.set(msg.file_path, {
					status: 'failed',
					error: msg.error
				});
				fileStatuses = new Map(fileStatuses); // trigger reactivity

				// Show toast notification for immediate feedback
				const fileName = msg.file_path.split(/[\\/]/).pop();
				const action = isUpdateMode ? 'update' : 'organize';
				toastStore.error(`Failed to ${action} ${fileName}: ${msg.error}`, 7000);
			}

			// Fatal operation-level error from backend
			if (msg.status === 'error' && !msg.file_path) {
				finalizeOrganizeFailure(msg.message || 'Operation failed');
				return;
			}

			if (msg.status === 'cancelled' && !msg.file_path) {
				const action = isUpdateMode ? 'Update' : 'Organization';
				finalizeOrganizeFailure(`${action} was cancelled.`);
				return;
			}

			// Handle both organized and updated success messages
			if ((msg.status === 'organized' || msg.status === 'updated') && msg.file_path) {
				fileStatuses.set(msg.file_path, {status: 'success'});
				fileStatuses = new Map(fileStatuses);
			}

			// Handle completion for both operations
			if (msg.status === 'organization_completed' || msg.status === 'update_completed') {
				finalizeOrganizeSuccess(msg.message);
			}
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

	async function openRescrapeModal(movieId: string) {
		if (availableScrapers.length === 0) {
			try {
				availableScrapers = await apiClient.getScrapers();
			} catch (error) {
				console.error('Failed to fetch scrapers:', error);
				toastStore.error('Failed to load scrapers');
				return;
			}
		}
		rescrapeMovieId = movieId;
		rescrapeSelectedScrapers = availableScrapers
			.filter((s) => s.enabled)
			.map((s) => s.name);
		manualSearchMode = false;
		manualSearchInput = '';
		showRescrapeModal = true;
	}

	async function executeRescrape() {
		// Validate common requirements
		if (rescrapeSelectedScrapers.length === 0) {
			toastStore.error('Please select at least one scraper');
			return;
		}

		if (!currentResult) {
			toastStore.error('No current movie to update');
			return;
		}

		// Manual search mode - validate input
		if (manualSearchMode) {
			const input = manualSearchInput.trim();
			if (!input) {
				toastStore.error('Please enter a content ID, DVD ID, or URL');
				return;
			}
		}

		// Set rescraping state
		rescrapingStates.set(rescrapeMovieId, true);
		rescrapingStates = new Map(rescrapingStates);

		try {
			// Call batch-aware rescrape endpoint (handles both modes)
			const response = await apiClient.rescrapeBatchMovie(jobId, rescrapeMovieId, {
				force: true,
				selected_scrapers: rescrapeSelectedScrapers,
				manual_search_input: manualSearchMode ? manualSearchInput.trim() : undefined,
				preset: rescrapePreset as 'conservative' | 'gap-fill' | 'aggressive' | undefined,
				scalar_strategy: rescrapeScalarStrategy !== '' ? rescrapeScalarStrategy : undefined,
				array_strategy: rescrapeArrayStrategy !== '' ? rescrapeArrayStrategy : undefined
			});

			const updatedMovie = response.movie;

			console.log(manualSearchMode ? 'Manual search successful' : 'Rescrape successful', ', updating job results');

			// Update the movie in the job results using the current file path
			if (job && currentResult.file_path) {
				const filePath = currentResult.file_path;
				console.log('Updating result for file:', filePath);

				// Create new results object with updated movie
				const newResults = { ...job.results };

				// Create new result object to trigger reactivity
				newResults[filePath] = {
					...newResults[filePath],
					data: updatedMovie,
					field_sources: response.field_sources ?? newResults[filePath].field_sources,
					actress_sources: response.actress_sources ?? newResults[filePath].actress_sources
				};

				// Create new job object to trigger Svelte reactivity
				job = {
					...job,
					results: newResults
				};
				console.log('Job results updated successfully');
			}

			// Clear any edited state for this movie
			if (editedMovies.has(currentResult.file_path)) {
				editedMovies.delete(currentResult.file_path);
				editedMovies = editedMovies;
			}

			toastStore.success(manualSearchMode
				? `Successfully scraped metadata for ${manualSearchInput.trim()}`
				: `Successfully rescraped ${rescrapeMovieId}`
			);
			showRescrapeModal = false;
		} catch (error) {
			console.error(manualSearchMode ? 'Manual search failed' : 'Rescrape failed', ':', error);
			const errorMessage = error instanceof Error ? error.message : JSON.stringify(error);
			toastStore.error((manualSearchMode ? 'Manual search failed: ' : 'Rescrape failed: ') + errorMessage);
		} finally {
			rescrapingStates.delete(rescrapeMovieId);
			rescrapingStates = new Map(rescrapingStates);
		}
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

	async function organizeAll() {
		if (!destinationPath.trim()) {
			toastStore.error('Please enter a destination path');
			return;
		}

		const copyOnly = organizeOperation !== 'move';
		const linkMode = organizeOperation === 'hardlink'
			? 'hard'
			: organizeOperation === 'softlink'
				? 'soft'
				: undefined;

		// Clear old WebSocket messages to prevent stale completion messages
		websocketStore.clearMessages();

		organizeStatus = 'organizing';
		organizing = true;
		organizeProgress = 0;
		fileStatuses = new Map();
		expectedOrganizeFilePaths = getOrganizeEligibleFilePaths(job);
		clearOrganizePollTimer();
		clearOrganizeCompletionTimer();

		try {
			// Save all edited movies to backend first
			if (editedMovies.size > 0) {
				await saveAllEdits();
			}

			// Start organize (returns immediately)
			await apiClient.organizeBatchJob(jobId, {
				destination: destinationPath,
				copy_only: copyOnly,
				link_mode: linkMode
			});

			startOrganizeCompletionPolling();
		} catch (e) {
			organizeStatus = 'failed';
			organizing = false;
			clearOrganizePollTimer();
			const errorMessage = e instanceof Error ? e.message : 'Failed to start organize';
			toastStore.error(errorMessage, 7000);
		}
	}

	async function updateAll() {
		// Clear old WebSocket messages to prevent stale completion messages
		websocketStore.clearMessages();

		organizeStatus = 'organizing';
		organizing = true;
		organizeProgress = 0;
		fileStatuses = new Map();
		expectedOrganizeFilePaths = getOrganizeEligibleFilePaths(job);
		clearOrganizePollTimer();
		clearOrganizeCompletionTimer();

		try {
			// Save all edited movies to backend first
			if (editedMovies.size > 0) {
				await saveAllEdits();
			}

			// Start update (returns immediately)
			await apiClient.updateBatchJob(jobId);

			startOrganizeCompletionPolling();
		} catch (e) {
			organizeStatus = 'failed';
			organizing = false;
			clearOrganizePollTimer();
			const errorMessage = e instanceof Error ? e.message : 'Failed to start update';
			toastStore.error(errorMessage, 7000);
		}
	}

	async function retryFailed() {
		// Get count of failed files before clearing
		const failedCount = Array.from(fileStatuses.values()).filter(s => s.status === 'failed').length;

		if (failedCount === 0) {
			return;
		}

		toastStore.info(`Retrying ${failedCount} failed file${failedCount > 1 ? 's' : ''}...`);

		// Clear WebSocket messages and reset state
		websocketStore.clearMessages();
		organizeStatus = 'organizing';
		organizing = true;
		organizeProgress = 0;
		fileStatuses = new Map();

		// Call the appropriate function based on current mode
		if (isUpdateMode) {
			await updateAll();
		} else {
			await organizeAll();
		}
	}

	async function excludeCurrentMovie() {
		if (!currentMovie || !job) return;

		try {
			await apiClient.excludeBatchMovie(job.id, currentMovie.id);
			toastStore.success(`Movie ${currentMovie.id} excluded from organization`);

			// Store the current movie count before refresh
			const previousMovieCount = movieResults.length;

			// Refetch the job to update the state (this will recalculate movieResults)
			await fetchJob();

			// Update current index to navigate to next movie or previous if at end
			if (movieResults.length === 0) {
				// All movies excluded, navigate back to browse
				await goto('/batch');
			} else if (currentMovieIndex >= movieResults.length) {
				// If we were at the last movie, go to the new last movie
				currentMovieIndex = movieResults.length - 1;
			}
			// Otherwise stay at the same index (which now shows the next movie)

		} catch (err) {
			toastStore.error(`Failed to exclude movie: ${err}`);
		}
	}

	function openDestinationBrowser() {
		tempDestinationPath = destinationPath;
		showDestinationBrowser = true;
	}

	function handleDestinationSelect(files: string[]) {
		// This is called when navigating - we'll ignore file selections
		// and just track the current path from the browser
	}

	function handleDestinationPathChange(path: string) {
		tempDestinationPath = path;
	}

	function confirmDestination() {
		destinationPath = tempDestinationPath;
		showDestinationBrowser = false;
	}

	function cancelDestination() {
		showDestinationBrowser = false;
	}

	function hasChanges(filePath: string): boolean {
		return editedMovies.has(filePath);
	}

	function previewImageURL(url: string | undefined): string {
		if (!url) return '';
		if (url.startsWith('/api/v1/')) return url;
		if (url.startsWith('/')) return url;
		return apiClient.getPreviewImageURL(url);
	}

	function clamp(value: number, min: number, max: number): number {
		return Math.min(max, Math.max(min, value));
	}

	function normalizeCropBox(box: PosterCropBox, metrics: PosterCropMetrics): PosterCropState {
		return {
			xRatio: box.x / metrics.sourceWidth,
			yRatio: box.y / metrics.sourceHeight,
			widthRatio: box.width / metrics.sourceWidth,
			heightRatio: box.height / metrics.sourceHeight
		};
	}

	function restoreCropBox(state: PosterCropState, sourceWidth: number, sourceHeight: number): PosterCropBox {
		const width = clamp(Math.round(state.widthRatio * sourceWidth), 1, sourceWidth);
		const height = clamp(Math.round(state.heightRatio * sourceHeight), 1, sourceHeight);
		const maxX = Math.max(0, sourceWidth - width);
		const maxY = Math.max(0, sourceHeight - height);

		return {
			x: clamp(Math.round(state.xRatio * sourceWidth), 0, maxX),
			y: clamp(Math.round(state.yRatio * sourceHeight), 0, maxY),
			width,
			height
		};
	}

	function getDefaultPosterCropBox(sourceWidth: number, sourceHeight: number): PosterCropBox {
		const sourceAspect = sourceWidth / sourceHeight;

		if (sourceAspect > 1.2) {
			const width = Math.max(1, Math.round(sourceWidth * LANDSCAPE_CROP_WIDTH_RATIO));
			return {
				x: sourceWidth - width,
				y: 0,
				width,
				height: sourceHeight
			};
		}

		let width = sourceWidth;
		let height = sourceHeight;
		if (sourceAspect > POSTER_TARGET_ASPECT_RATIO) {
			width = Math.max(1, Math.round(sourceHeight * POSTER_TARGET_ASPECT_RATIO));
		} else {
			height = Math.max(1, Math.round(sourceWidth / POSTER_TARGET_ASPECT_RATIO));
		}

		return {
			x: Math.max(0, Math.floor((sourceWidth - width) / 2)),
			y: Math.max(0, Math.floor((sourceHeight - height) / 2)),
			width,
			height
		};
	}

	function refreshPosterCropMetrics() {
		if (!cropImageElement || !cropMetrics) return;
		const displayWidth = cropImageElement.clientWidth;
		const displayHeight = cropImageElement.clientHeight;
		if (displayWidth <= 0 || displayHeight <= 0) return;

		cropMetrics = {
			...cropMetrics,
			displayWidth,
			displayHeight,
			imageOffsetX: cropImageElement.offsetLeft,
			imageOffsetY: cropImageElement.offsetTop
		};
	}

	function handlePosterCropImageLoad() {
		posterCropLoadError = null;
		if (!cropImageElement) return;

		const sourceWidth = cropImageElement.naturalWidth;
		const sourceHeight = cropImageElement.naturalHeight;
		if (sourceWidth <= 0 || sourceHeight <= 0) {
			posterCropLoadError = 'Failed to read poster dimensions';
			return;
		}

		const displayWidth = cropImageElement.clientWidth;
		const displayHeight = cropImageElement.clientHeight;
		if (displayWidth <= 0 || displayHeight <= 0) {
			posterCropLoadError = 'Failed to measure poster layout';
			return;
		}

		cropMetrics = {
			sourceWidth,
			sourceHeight,
			displayWidth,
			displayHeight,
			imageOffsetX: cropImageElement.offsetLeft,
			imageOffsetY: cropImageElement.offsetTop
		};

		const savedCrop = currentResult ? posterCropStates.get(currentResult.file_path) : undefined;
		cropBox = savedCrop
			? restoreCropBox(savedCrop, sourceWidth, sourceHeight)
			: getDefaultPosterCropBox(sourceWidth, sourceHeight);

		refreshPosterCropMetrics();
	}

	function handlePosterCropImageError() {
		if (currentMovie && cropSourceURL.includes('-full.jpg')) {
			const fallbackURL = `/api/v1/temp/posters/${jobId}/${currentMovie.id}.jpg`;
			cropSourceURL = `${fallbackURL}?v=${Date.now()}`;
			return;
		}

		posterCropLoadError = 'Poster source is not available for manual cropping';
		cropMetrics = null;
		cropBox = null;
	}

	function openPosterCropModal() {
		if (!currentMovie) return;
		const fullPosterURL = `/api/v1/temp/posters/${jobId}/${currentMovie.id}-full.jpg`;
		cropSourceURL = `${fullPosterURL}?v=${Date.now()}`;
		posterCropLoadError = null;
		cropMetrics = null;
		cropBox = null;
		cropDragState = null;
		showPosterCropModal = true;
	}

	function closePosterCropModal() {
		stopPosterCropDrag();
		showPosterCropModal = false;
	}

	function startPosterCropDrag(event: MouseEvent) {
		if (!browser || event.button !== 0 || !cropMetrics || !cropBox) return;
		event.preventDefault();

		cropDragState = {
			startX: event.clientX,
			startY: event.clientY,
			originX: cropBox.x,
			originY: cropBox.y
		};

		window.addEventListener('mousemove', movePosterCropBox);
		window.addEventListener('mouseup', stopPosterCropDrag);
	}

	function movePosterCropBox(event: MouseEvent) {
		if (!cropDragState || !cropMetrics || !cropBox) return;
		event.preventDefault();
		refreshPosterCropMetrics();

		const scaleX = cropMetrics.displayWidth / cropMetrics.sourceWidth;
		const scaleY = cropMetrics.displayHeight / cropMetrics.sourceHeight;
		if (scaleX <= 0 || scaleY <= 0) return;

		const deltaXSource = (event.clientX - cropDragState.startX) / scaleX;
		const deltaYSource = (event.clientY - cropDragState.startY) / scaleY;

		const maxX = Math.max(0, cropMetrics.sourceWidth - cropBox.width);
		const maxY = Math.max(0, cropMetrics.sourceHeight - cropBox.height);

		cropBox = {
			...cropBox,
			x: clamp(Math.round(cropDragState.originX + deltaXSource), 0, maxX),
			y: clamp(Math.round(cropDragState.originY + deltaYSource), 0, maxY)
		};
	}

	function stopPosterCropDrag() {
		cropDragState = null;
		if (!browser) return;
		window.removeEventListener('mousemove', movePosterCropBox);
		window.removeEventListener('mouseup', stopPosterCropDrag);
	}

	function resetPosterCropBox() {
		if (!cropMetrics) return;
		cropBox = getDefaultPosterCropBox(cropMetrics.sourceWidth, cropMetrics.sourceHeight);
	}

	function getPosterCropOverlayStyle(): string {
		if (!cropMetrics || !cropBox) return '';

		const scaleX = cropMetrics.displayWidth / cropMetrics.sourceWidth;
		const scaleY = cropMetrics.displayHeight / cropMetrics.sourceHeight;
		const left = Math.round(cropMetrics.imageOffsetX + cropBox.x * scaleX);
		const top = Math.round(cropMetrics.imageOffsetY + cropBox.y * scaleY);
		const width = Math.round(cropBox.width * scaleX);
		const height = Math.round(cropBox.height * scaleY);

		return `left:${left}px;top:${top}px;width:${width}px;height:${height}px;box-shadow:0 0 0 9999px rgba(0,0,0,0.45);`;
	}

	async function applyPosterCrop() {
		if (!currentMovie || !currentResult || !cropBox || posterCropSaving) return;

		posterCropSaving = true;
		try {
			const response = await apiClient.updateBatchMoviePosterCrop(jobId, currentMovie.id, {
				x: cropBox.x,
				y: cropBox.y,
				width: cropBox.width,
				height: cropBox.height
			});

			const nextOverrides = new Map(posterPreviewOverrides);
			nextOverrides.set(currentResult.file_path, {
				url: response.cropped_poster_url,
				version: Date.now()
			});
			posterPreviewOverrides = nextOverrides;

			if (cropMetrics) {
				const nextCropStates = new Map(posterCropStates);
				nextCropStates.set(currentResult.file_path, normalizeCropBox(cropBox, cropMetrics));
				posterCropStates = nextCropStates;
			}

			toastStore.success('Poster crop updated');
			closePosterCropModal();
		} catch (e) {
			const errorMessage = e instanceof Error ? e.message : 'Failed to update poster crop';
			toastStore.error(errorMessage);
		} finally {
			posterCropSaving = false;
		}
	}

	// Image viewer functions
	function openScreenshotViewer(index: number) {
		if (!currentMovie?.screenshot_urls) return;
		imageViewerImages = currentMovie.screenshot_urls.map((url) => previewImageURL(url));
		imageViewerIndex = index;
		imageViewerTitle = undefined;
		showImageViewer = true;
	}

	function openCoverViewer() {
		if (!currentMovie?.cover_url) return;
		imageViewerImages = [previewImageURL(currentMovie.cover_url)];
		imageViewerIndex = 0;
		imageViewerTitle = 'Cover/Fanart';
		showImageViewer = true;
	}

	function closeImageViewer() {
		showImageViewer = false;
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

		const handleResize = () => {
			if (showPosterCropModal) {
				refreshPosterCropMetrics();
			}
		};
		window.addEventListener('resize', handleResize);

		return () => {
			window.removeEventListener('resize', handleResize);
		};
	});

	onDestroy(() => {
		clearOrganizePollTimer();
		clearOrganizeCompletionTimer();
		stopPosterCropDrag();
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
			<!-- Header -->
			<div class="flex items-center justify-between mb-6">
				<div>
					<h1 class="text-3xl font-bold">Review & Edit Metadata</h1>
					<p class="text-muted-foreground mt-1">
						{#if isUpdateMode}
							Metadata and media files have been updated in place. Review and edit as needed.
						{:else}
							Review and edit scraped metadata before organizing files
						{/if}
					</p>
				</div>
				<div class="flex items-center gap-3">
					<Button variant="outline" onclick={() => goto('/browse')} disabled={organizing}>
						{#snippet children()}
							<X class="h-4 w-4 mr-2" />
							{isUpdateMode ? 'Close' : 'Cancel'}
						{/snippet}
					</Button>
					{#if isUpdateMode}
						<Button onclick={updateAll} disabled={organizing}>
							{#snippet children()}
								{#if organizing}
									<Loader2 class="h-4 w-4 mr-2 animate-spin" />
								{:else}
									<RefreshCw class="h-4 w-4 mr-2" />
								{/if}
								{organizing ? 'Updating...' : `Update ${movieResults.length} File${movieResults.length !== 1 ? 's' : ''}`}
							{/snippet}
						</Button>
					{:else}
						<Button onclick={organizeAll} disabled={organizing || !destinationPath.trim()}>
							{#snippet children()}
								{#if organizing}
									<Loader2 class="h-4 w-4 mr-2" />
								{:else}
									<Play class="h-4 w-4 mr-2" />
								{/if}
								{organizing ? 'Organizing...' : `Organize ${movieResults.length} File${movieResults.length !== 1 ? 's' : ''}`}
							{/snippet}
						</Button>
					{/if}
				</div>
			</div>

			<!-- Organize Progress UI -->
			{#if organizeStatus === 'organizing'}
				<Card class="p-6">
					<h3 class="font-semibold mb-4">Organizing Files...</h3>

					<!-- Progress bar -->
					<div class="mb-4">
						<div class="flex justify-between text-sm mb-1">
							<span>Progress</span>
							<span>{Math.round(organizeProgress)}%</span>
						</div>
						<div class="w-full bg-gray-200 rounded-full h-2">
							<div
								class="bg-blue-600 h-2 rounded-full transition-all duration-300"
								style="width: {organizeProgress}%"
							></div>
						</div>
					</div>

					<!-- File statuses -->
					{#if fileStatuses.size > 0}
						<div class="space-y-2 max-h-64 overflow-y-auto">
							{#each Array.from(fileStatuses.entries()) as [filePath, status] (filePath)}
								<div animate:flip={{ duration: 220, easing: quintOut }} class="flex items-start gap-2 text-sm p-2 rounded {status.status === 'failed' ? 'bg-red-50' : 'bg-green-50'}">
									{#if status.status === 'failed'}
										<CircleAlert class="h-4 w-4 text-red-600 shrink-0 mt-0.5" />
									{:else}
										<Check class="h-4 w-4 text-green-600 shrink-0 mt-0.5" />
									{/if}
									<div class="flex-1 min-w-0">
										<div class="font-medium truncate">{filePath.split(/[\\/]/).pop()}</div>
										{#if status.error}
											<div class="text-red-700 text-xs mt-1">{status.error}</div>
										{/if}
									</div>
								</div>
							{/each}
						</div>
					{/if}
				</Card>
			{/if}

			<!-- Organize Completed Successfully (no errors) -->
			{#if organizeStatus === 'completed'}
				{@const failures = Array.from(fileStatuses.values()).filter(s => s.status === 'failed')}
				{@const successes = Array.from(fileStatuses.values()).filter(s => s.status === 'success')}
				{@const successCount = successes.length > 0 ? successes.length : expectedOrganizeFilePaths.length}

				{#if failures.length === 0}
					<Card class="p-6 border-green-500 bg-green-50">
						<div class="flex items-start gap-3">
							<Check class="h-6 w-6 text-green-600 shrink-0" />
							<div class="flex-1">
								<h3 class="font-semibold mb-2 text-green-900">
									{isUpdateMode ? 'Update Complete!' : 'Organization Complete!'}
								</h3>
								<p class="text-sm text-green-800 mb-3">
									All {successCount} file(s) {isUpdateMode ? 'updated' : 'organized'} successfully
								</p>
								<p class="text-xs text-green-700">
									Redirecting to browse page in a few seconds...
								</p>
								<div class="mt-4">
									<Button onclick={() => goto('/browse')} variant="outline">
										{#snippet children()}
											<ChevronLeft class="h-4 w-4 mr-2" />
											Return to Browse Now
										{/snippet}
									</Button>
								</div>
							</div>
						</div>
					</Card>
				{:else if failures.length > 0}
					<Card class="p-6 border-orange-500">
						<div class="flex items-start gap-3">
							<CircleAlert class="h-6 w-6 text-orange-600 shrink-0" />
							<div class="flex-1">
								<h3 class="font-semibold mb-2">Organization Completed with Errors</h3>
								<p class="text-sm text-muted-foreground mb-4">
									{successes.length} file(s) organized successfully, {failures.length} failed
								</p>

								<!-- Failed files list -->
								<div class="space-y-2 max-h-96 overflow-y-auto">
									<h4 class="font-medium text-sm">Failed Files:</h4>
									{#each Array.from(fileStatuses.entries()).filter(([_, s]) => s.status === 'failed') as [filePath, status]}
										<div class="bg-red-50 p-3 rounded text-sm">
											<div class="font-medium">{filePath.split(/[\\/]/).pop()}</div>
											<div class="text-red-700 text-xs mt-1">{status.error}</div>
										</div>
									{/each}
								</div>

								<div class="mt-4 flex gap-2">
									<Button onclick={retryFailed}>
										{#snippet children()}
											Retry Failed
										{/snippet}
									</Button>
									<Button variant="outline" onclick={() => goto('/browse')}>
										{#snippet children()}
											Continue Anyway
										{/snippet}
									</Button>
								</div>
							</div>
						</div>
					</Card>
				{/if}
			{/if}

			{#key currentResult.file_path}
				<div class="grid grid-cols-1 lg:grid-cols-[300px_1fr] gap-6" in:fade|local={{ duration: 180 }}>
				<!-- Left Sidebar: Media Preview -->
				<div class="space-y-4 lg:sticky lg:top-6 lg:self-start lg:max-h-[calc(100vh-8rem)] lg:overflow-y-auto">
					<!-- Poster Image -->
					{#if showPosterPanel}
						<Card class="p-4">
							<div class="flex items-center justify-between gap-2 mb-3">
								<h3 class="font-semibold text-sm">
									Poster{currentMovie.should_crop_poster ? ' (Cropped)' : ''}
								</h3>
								<Button
									size="sm"
									variant="outline"
									onclick={openPosterCropModal}
									disabled={!currentMovie.id}
									class="text-xs"
								>
									{#snippet children()}
										Adjust Crop
									{/snippet}
								</Button>
							</div>
							{#if displayPosterUrl}
								<div class="w-full aspect-2/3 overflow-hidden rounded border relative">
									{#if currentMovie.should_crop_poster && !currentMovie.cropped_poster_url}
										<!-- Crop to show only right 47.2% of image (removes promotional text on left) -->
										<!-- Only apply cropping if cropped_poster_url is not available (cropped_poster_url is already cropped) -->
										<img
											src={displayPosterUrl}
											alt="Poster"
											class="absolute h-full"
											style="right: 0; width: auto; min-width: 211.8%; object-fit: cover; object-position: right center;"
											onerror={(e) => {
												(e.currentTarget as HTMLImageElement).src = 'https://via.placeholder.com/300x450?text=No+Poster';
											}}
										/>
									{:else}
										<!-- Use poster directly without cropping (either cropped_poster_url or regular poster) -->
										<img
											src={displayPosterUrl}
											alt="Poster"
											class="w-full h-full object-contain"
											onerror={(e) => {
												(e.currentTarget as HTMLImageElement).src = 'https://via.placeholder.com/300x450?text=No+Poster';
											}}
										/>
									{/if}
								</div>
							{:else}
								<div class="w-full aspect-2/3 bg-accent rounded border flex items-center justify-center text-muted-foreground">
									<div class="text-center text-xs">
										<ImageIcon class="h-8 w-8 mx-auto mb-2 opacity-50" />
										<p>No poster</p>
									</div>
								</div>
							{/if}
						</Card>
					{/if}

					<!-- Cover/Fanart Image -->
					{#if showCoverPanel}
						<Card class="p-4">
							<h3 class="font-semibold mb-3 text-sm">Cover/Fanart</h3>
							{#if currentMovie.cover_url}
								<button
									onclick={openCoverViewer}
									class="cursor-pointer hover:opacity-80 transition-opacity w-full"
								>
									<img
										src={previewImageURL(currentMovie.cover_url)}
										alt="Cover"
										class="rounded border object-contain"
										style="max-width: 100%; max-height: 400px; width: auto;"
										onerror={(e) => {
											(e.currentTarget as HTMLImageElement).src = 'https://via.placeholder.com/400x225?text=No+Cover';
										}}
									/>
								</button>
							{:else}
								<div class="w-full aspect-video bg-accent rounded border flex items-center justify-center text-muted-foreground">
									<div class="text-center text-xs">
										<ImageIcon class="h-8 w-8 mx-auto mb-2 opacity-50" />
										<p>No cover image</p>
									</div>
								</div>
							{/if}
						</Card>
					{/if}

					<!-- Trailer -->
					{#if showTrailerPanel && currentMovie.trailer_url}
						<Card class="p-4">
							<h3 class="font-semibold mb-3 text-sm">Trailer</h3>
							<Button class="w-full" onclick={() => (showTrailerModal = true)}>
								{#snippet children()}
									<Play class="h-4 w-4 mr-2" />
									Play Trailer
								{/snippet}
							</Button>
						</Card>
					{/if}

					<!-- Screenshots Preview -->
					{#if showScreenshotsPanel && currentMovie.screenshot_urls && currentMovie.screenshot_urls.length > 0}
						<Card class="p-4">
							<h3 class="font-semibold mb-3 text-sm">
								Screenshots ({currentMovie.screenshot_urls.length})
							</h3>
							<div class="grid grid-cols-2 gap-2">
								{#each (showAllSidebarScreenshots ? currentMovie.screenshot_urls : currentMovie.screenshot_urls.slice(0, 4)) as url, index}
									<button
										onclick={() => openScreenshotViewer(index)}
										class="cursor-pointer hover:opacity-80 transition-opacity"
									>
										<img
											src={previewImageURL(url)}
											alt="Screenshot"
											class="w-full aspect-video object-cover rounded border"
											onerror={(e) => {
												(e.currentTarget as HTMLImageElement).src = 'https://via.placeholder.com/400x225?text=Error';
											}}
										/>
									</button>
								{/each}
							</div>
							{#if currentMovie.screenshot_urls.length > 4 && !showAllSidebarScreenshots}
								<button
									onclick={() => (showAllSidebarScreenshots = true)}
									class="w-full text-xs text-primary hover:text-primary/80 hover:bg-accent mt-2 py-1 rounded transition-all hover:scale-[1.01] active:scale-[0.99] cursor-pointer"
								>
									+{currentMovie.screenshot_urls.length - 4} more
								</button>
							{/if}
							{#if showAllSidebarScreenshots && currentMovie.screenshot_urls.length > 4}
								<button
									onclick={() => (showAllSidebarScreenshots = false)}
									class="w-full text-xs text-muted-foreground hover:text-primary hover:bg-accent mt-2 py-1 rounded transition-colors cursor-pointer"
								>
									Show less
								</button>
							{/if}
						</Card>
					{/if}
				</div>

				<!-- Right: Main Content -->
				<div class="space-y-6 min-w-0">
					<!-- Movie Navigation -->
					<Card class="p-4">
						<div class="flex items-center justify-between">
							<Button
								variant="outline"
								onclick={() => (currentMovieIndex = Math.max(0, currentMovieIndex - 1))}
								disabled={currentMovieIndex === 0}
							>
								{#snippet children()}
									<ChevronLeft class="h-4 w-4 mr-2" />
									Previous
								{/snippet}
							</Button>

							<div class="text-center flex-1 mx-4">
								<p class="font-semibold">
									Movie {currentMovieIndex + 1} of {movieResults.length}
								</p>
								<p class="text-sm text-muted-foreground">{currentMovie.id}</p>
								{#if hasChanges(currentResult.file_path)}
									<span class="text-xs text-orange-600 flex items-center gap-1 justify-center mt-1">
										<CircleAlert class="h-3 w-3" />
										Modified
									</span>
								{/if}
							</div>

							<div class="flex gap-2">
								<Button
									variant="outline"
									onclick={excludeCurrentMovie}
									class="text-destructive hover:bg-destructive hover:text-destructive-foreground"
								>
									{#snippet children()}
										<Trash2 class="h-4 w-4 mr-2" />
										Remove
									{/snippet}
								</Button>

								<Button
									variant="outline"
									onclick={() =>
										(currentMovieIndex = Math.min(movieResults.length - 1, currentMovieIndex + 1))}
									disabled={currentMovieIndex === movieResults.length - 1}
								>
									{#snippet children()}
										Next
										<ChevronRight class="h-4 w-4 ml-2" />
									{/snippet}
								</Button>
							</div>
						</div>
					</Card>

					<!-- File Path Info (supports multi-part files) -->
					<Card class="p-4">
						<div class="min-w-0">
							<div class="flex items-center justify-between mb-2">
								<p class="text-sm font-medium">
									{#if currentMovieGroup && currentMovieGroup.results.length > 1}
										Source Files ({currentMovieGroup.results.length} parts)
									{:else}
										Source File
									{/if}
								</p>
								{#if currentResult.file_path.length > 80}
									<button
										onclick={() => showFullSourcePath = !showFullSourcePath}
										class="text-xs text-primary hover:text-primary/80 transition-colors cursor-pointer"
									>
										{showFullSourcePath ? 'Hide' : 'Show full path'}
									</button>
								{/if}
							</div>
							{#if currentMovieGroup && currentMovieGroup.results.length > 1}
								<!-- Multi-part file list -->
								<div class="space-y-2">
									{#each currentMovieGroup.results as result, index}
										<div class="bg-accent rounded px-3 py-2 {showFullSourcePath ? 'overflow-x-auto' : ''}">
											<code class="text-xs block {showFullSourcePath ? 'whitespace-nowrap' : ''}" title={result.file_path}>
												<span class="text-muted-foreground mr-2">Part {index + 1}:</span>
												{showFullSourcePath ? result.file_path : truncatePath(result.file_path)}
											</code>
										</div>
									{/each}
								</div>
							{:else}
								<!-- Single file -->
								<div class="bg-accent rounded px-3 py-2 {showFullSourcePath ? 'overflow-x-auto' : ''}">
									<code class="text-xs block {showFullSourcePath ? 'whitespace-nowrap' : ''}" title={currentResult.file_path}>
										{showFullSourcePath ? currentResult.file_path : truncatePath(currentResult.file_path)}
									</code>
								</div>
							{/if}
						</div>
					</Card>

					<!-- Destination Path (hidden in update mode) -->
					{#if !isUpdateMode}
						<Card class="p-4">
							<div class="space-y-3 min-w-0">
								<div class="flex items-center gap-2">
									<FolderOpen class="h-5 w-5 text-primary" />
									<h3 class="font-semibold">Output Destination</h3>
								</div>
								<div class="flex gap-2 min-w-0">
									<input
										type="text"
										bind:value={destinationPath}
										placeholder="Enter destination path (e.g., /path/to/output)"
										class="flex-1 min-w-0 px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono text-sm"
										title={destinationPath}
									/>
									<Button onclick={openDestinationBrowser} variant="outline">
										{#snippet children()}
											<FolderOpen class="h-4 w-4 mr-2" />
											Browse
										{/snippet}
									</Button>
								</div>

								<div class="space-y-2">
									<label for="organizeOperation" class="text-sm font-medium">
										File operation
									</label>
									<select
										id="organizeOperation"
										bind:value={organizeOperation}
										class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all text-sm"
									>
										<option value="move">Move files</option>
										<option value="copy">Copy files</option>
										<option value="hardlink">Hard link files</option>
										<option value="softlink">Soft link files</option>
									</select>
									<p class="text-xs text-muted-foreground">
										{#if organizeOperation === 'hardlink'}
											Hard links require source and destination on the same filesystem.
										{:else if organizeOperation === 'softlink'}
											Soft links point to the original file path. Windows may require Developer Mode or elevated privileges.
										{:else if organizeOperation === 'copy'}
											Copy creates independent destination files and keeps originals unchanged.
										{:else}
											Move relocates source files into the organized destination.
										{/if}
									</p>
								</div>

								{#if preview}
									{@const pathParts = preview.full_path
										.replace(destinationPath + '/', '')
										.split('/')
										.filter(p => p && !p.includes('.mp4'))}
									{@const fileIndent = pathParts.length * 4}
									<div class="mt-3 p-3 bg-accent/50 rounded border border-dashed overflow-hidden">
										<p class="text-xs font-medium mb-2 text-muted-foreground">Preview:</p>
										<div class="font-mono text-xs space-y-1 overflow-x-auto">
											<div class="text-muted-foreground break-all">📁 {destinationPath}/</div>
											{#each pathParts as part, index}
												<div class="text-muted-foreground break-all" style="margin-left: {(index + 1) * 4}px">
													📁 {part}/
												</div>
											{/each}
											{#if preview.video_files && preview.video_files.length > 0}
												<!-- Multi-part video files -->
												{#each preview.video_files as videoFile, index}
													{@const fileName = videoFile.split(/[\\/]/).pop()}
													<div class="break-all" style="margin-left: {fileIndent + 4}px">🎬 {fileName}</div>
												{/each}
											{:else}
												<!-- Single video file -->
												<div class="break-all" style="margin-left: {fileIndent + 4}px">🎬 {preview.file_name}.mp4</div>
											{/if}
											{#if preview.nfo_paths && preview.nfo_paths.length > 0}
												<!-- Multi-part NFO files (per_file enabled) -->
												{#each preview.nfo_paths as nfoFile, index}
													{@const fileName = nfoFile.split(/[\\/]/).pop()}
													<div class="break-all" style="margin-left: {fileIndent + 4}px">📄 {fileName}</div>
												{/each}
											{:else}
												<!-- Single NFO file -->
												<div class="break-all" style="margin-left: {fileIndent + 4}px">📄 {preview.file_name}.nfo</div>
											{/if}
											<div class="break-all" style="margin-left: {fileIndent + 4}px">🖼️ {preview.file_name}-poster.jpg</div>
											<div class="break-all" style="margin-left: {fileIndent + 4}px">🖼️ {preview.file_name}-fanart.jpg</div>
											{#if preview.screenshots && preview.screenshots.length > 0}
												<div class="text-muted-foreground break-all" style="margin-left: {fileIndent + 4}px">📁 extrafanart/</div>
												{#each (showAllPreviewScreenshots ? preview.screenshots : preview.screenshots.slice(0, 3)) as screenshot}
													<div class="break-all" style="margin-left: {fileIndent + 8}px">🖼️ {screenshot}</div>
												{/each}
												{#if preview.screenshots.length > 3 && !showAllPreviewScreenshots}
													<button
														onclick={() => (showAllPreviewScreenshots = true)}
														class="text-muted-foreground hover:text-primary transition-colors cursor-pointer text-left"
														style="margin-left: {fileIndent + 8}px"
													>
														... and {preview.screenshots.length - 3} more
													</button>
												{/if}
												{#if showAllPreviewScreenshots && preview.screenshots.length > 3}
													<button
														onclick={() => (showAllPreviewScreenshots = false)}
														class="text-muted-foreground hover:text-primary transition-colors cursor-pointer text-left"
														style="margin-left: {fileIndent + 8}px"
													>
														Show less
													</button>
												{/if}
											{/if}
										</div>
									</div>
								{:else}
									<p class="text-xs text-muted-foreground">
										Files will be organized with metadata, artwork, and NFO files in this directory
									</p>
								{/if}
							</div>
						</Card>
					{/if}

					<!-- Metadata Editor -->
					<Card class="p-6">
						<div class="space-y-4">
							<div class="flex items-center justify-between">
								<h2 class="text-xl font-semibold">Movie Metadata</h2>
								<div class="flex items-center gap-3">
									<label class="inline-flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
										<input
											type="checkbox"
											bind:checked={showFieldScraperSources}
											class="w-3.5 h-3.5 text-primary bg-gray-100 border-gray-300 rounded focus:ring-primary focus:ring-2"
										/>
										Show scraper per field
									</label>
									<div class="flex gap-2">
									<Button
										variant="outline"
										size="sm"
										onclick={() => currentMovie && openRescrapeModal(currentMovie.id)}
										disabled={rescrapingStates.get(currentMovie?.id || '') || false}
									>
										{#snippet children()}
											{#if rescrapingStates.get(currentMovie?.id || '')}
												<Loader2 class="h-4 w-4 mr-2 animate-spin" />
												Rescraping...
											{:else}
												<RotateCcw class="h-4 w-4 mr-2" />
												Rescrape
											{/if}
										{/snippet}
									</Button>
									<Button variant="outline" size="sm" onclick={resetCurrentMovie}>
										{#snippet children()}
											<RotateCcw class="h-4 w-4 mr-2" />
											Reset to Original
										{/snippet}
									</Button>
									</div>
								</div>
							</div>

							<MovieEditor
								movie={currentMovie!}
								originalMovie={currentResult.data!}
								onUpdate={updateCurrentMovie}
								fieldSources={currentResult.field_sources}
								showFieldSources={showFieldScraperSources}
							/>
						</div>
					</Card>

					<!-- Actresses -->
					<Card class="p-6">
						<ActressEditor
							movie={currentMovie!}
							onUpdate={updateCurrentMovie}
							actressSources={currentResult.actress_sources}
							showFieldSources={showFieldScraperSources}
						/>
					</Card>

					<!-- Screenshots & Media -->
					{#if showScreenshotsPanel}
						<Card class="p-6">
							<div class="space-y-4">
								<!-- Header with collapse button -->
								<button
									onclick={() => (showImagePanelContent = !showImagePanelContent)}
									class="w-full flex items-center justify-between hover:bg-accent/50 -mx-6 px-6 py-2 rounded transition-colors cursor-pointer"
								>
									<h2 class="text-xl font-semibold">Images & Media</h2>
									{#if showImagePanelContent}
										<ChevronUp class="h-5 w-5 text-muted-foreground" />
									{:else}
										<ChevronDown class="h-5 w-5 text-muted-foreground" />
									{/if}
								</button>

								<!-- Collapsible content -->
								{#if showImagePanelContent}
									<div transition:slide|local={{ duration: 200, easing: quintOut }}>
										<ScreenshotManager
											movie={currentMovie!}
											displayPosterUrl={displayPosterUrl}
											onUpdate={updateCurrentMovie}
											fieldSources={currentResult.field_sources}
											showFieldSources={showFieldScraperSources}
										/>
									</div>
								{/if}
							</div>
						</Card>
					{/if}

					<!-- Action Buttons (hidden in update mode) -->
					{#if !isUpdateMode}
						<Card class="p-4">
							<div class="flex items-center justify-end gap-3">
								<Button variant="outline" onclick={() => goto('/browse')} disabled={organizing}>
									{#snippet children()}
										<X class="h-4 w-4 mr-2" />
										Cancel
									{/snippet}
								</Button>
								<Button onclick={organizeAll} disabled={organizing || !destinationPath.trim()}>
									{#snippet children()}
										{#if organizing}
											<Loader2 class="h-4 w-4 mr-2 animate-spin" />
										{:else}
											<Play class="h-4 w-4 mr-2" />
										{/if}
										{organizing ? 'Organizing...' : `Organize ${movieResults.length} File${movieResults.length !== 1 ? 's' : ''}`}
									{/snippet}
								</Button>
							</div>
						</Card>
					{/if}
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
	onClose={closeImageViewer}
/>

<!-- Poster Crop Modal -->
{#if showPosterCropModal}
	<div class="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4" use:portalToBody in:fade|local={{ duration: 140 }} out:fade|local={{ duration: 120 }}>
		<div
			class="w-full max-w-5xl"
			in:scale|local={{ start: 0.97, duration: 180, easing: quintOut }}
			out:scale|local={{ start: 1, opacity: 0.7, duration: 130, easing: quintOut }}
		>
			<Card class="w-full flex flex-col max-h-[92vh]">
				<div class="p-4 border-b flex items-center justify-between">
					<div>
						<h2 class="text-lg font-semibold">Adjust Poster Crop</h2>
						<p class="text-xs text-muted-foreground">Drag the fixed crop box to choose the area to keep.</p>
					</div>
					<Button variant="ghost" size="icon" onclick={closePosterCropModal} disabled={posterCropSaving}>
						{#snippet children()}
							<X class="h-4 w-4" />
						{/snippet}
					</Button>
				</div>

				<div class="flex-1 min-h-0 overflow-hidden">
					<div bind:this={cropStageElement} class="relative w-full h-full p-10 bg-black/40 border-y min-h-[280px] flex items-center justify-center overflow-hidden">
						<img
							bind:this={cropImageElement}
							src={cropSourceURL}
							alt="Poster crop source"
							class="block max-w-full max-h-full select-none"
							draggable="false"
							onload={handlePosterCropImageLoad}
							onerror={handlePosterCropImageError}
						/>
						{#if cropMetrics && cropBox}
							<div
								class="absolute border-2 border-white cursor-move touch-none"
								style={getPosterCropOverlayStyle()}
								onmousedown={startPosterCropDrag}
								aria-label="Poster crop selection"
								role="button"
								tabindex="-1"
							>
								<div class="absolute -bottom-7 right-0 bg-black/75 text-white text-[10px] px-1.5 py-0.5 rounded whitespace-nowrap">
									{cropBox.width} x {cropBox.height}
								</div>
							</div>
						{/if}
						{#if posterCropLoadError}
							<div class="absolute top-3 left-3 right-3 rounded border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive z-10">
								{posterCropLoadError}
							</div>
						{/if}
					</div>
				</div>

				<div class="p-4 border-t flex items-center justify-between gap-2">
					<Button variant="outline" onclick={resetPosterCropBox} disabled={!cropMetrics || posterCropSaving}>
						{#snippet children()}
							Reset Position
						{/snippet}
					</Button>
					<div class="flex items-center gap-2">
						<Button variant="outline" onclick={closePosterCropModal} disabled={posterCropSaving}>
							{#snippet children()}Cancel{/snippet}
						</Button>
						<Button onclick={applyPosterCrop} disabled={!cropBox || !!posterCropLoadError || posterCropSaving}>
							{#snippet children()}
								{#if posterCropSaving}
									<Loader2 class="h-4 w-4 mr-2 animate-spin" />
									Applying...
								{:else}
									Apply Crop
								{/if}
							{/snippet}
						</Button>
					</div>
				</div>
			</Card>
		</div>
	</div>
{/if}

<!-- Rescrape Modal -->
{#if showRescrapeModal}
	<div class="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4" use:portalToBody in:fade|local={{ duration: 140 }} out:fade|local={{ duration: 120 }}>
		<div class="w-full max-w-lg" in:scale|local={{ start: 0.97, duration: 180, easing: quintOut }} out:scale|local={{ start: 1, opacity: 0.7, duration: 130, easing: quintOut }}>
		<Card class="w-full flex flex-col max-h-[90vh]">
			<!-- Header -->
			<div class="p-6 border-b flex items-center justify-between">
				<h2 class="text-xl font-bold">{manualSearchMode ? 'Manual Search' : `Rescrape ${rescrapeMovieId}`}</h2>
				<Button
					variant="ghost"
					size="icon"
					onclick={() => (showRescrapeModal = false)}
					disabled={rescrapingStates.get(rescrapeMovieId) || false}
				>
					{#snippet children()}
						<X class="h-4 w-4" />
					{/snippet}
				</Button>
			</div>

			<!-- Body -->
			<div class="flex-1 overflow-auto p-6">
				{#if rescrapingStates.get(rescrapeMovieId)}
					<!-- Loading State -->
					<div class="flex flex-col items-center justify-center py-8 space-y-4">
						<Loader2 class="h-12 w-12 animate-spin text-primary" />
						<div class="text-center space-y-2">
							<p class="text-sm font-medium">{manualSearchMode ? 'Scraping metadata...' : 'Rescraping metadata...'}</p>
							<p class="text-xs text-muted-foreground">
								Fetching data from {rescrapeSelectedScrapers.join(', ')}
							</p>
						</div>
					</div>
{:else}
				<!-- Mode Toggle -->
				<div class="flex gap-2 mb-6 p-1 bg-accent rounded-lg">
					<button
						onclick={() => manualSearchMode = false}
						class="flex-1 px-4 py-2 rounded transition-all {!manualSearchMode ? 'bg-white shadow-sm font-medium' : 'text-muted-foreground hover:text-foreground'}"
					>
						Rescrape from File
					</button>
					<button
						onclick={() => manualSearchMode = true}
						class="flex-1 px-4 py-2 rounded transition-all {manualSearchMode ? 'bg-white shadow-sm font-medium' : 'text-muted-foreground hover:text-foreground'}"
					>
						Manual Search
					</button>
				</div>

				{#if manualSearchMode}
					<!-- Manual Search Input -->
					<div class="space-y-4">
						<div>
							<label for="manual-search-input" class="text-sm font-medium mb-2 block">
								DVD ID, Content ID, or Direct URL
							</label>
							<input
								id="manual-search-input"
								type="text"
								bind:value={manualSearchInput}
								placeholder="e.g., IPX-123 or https://www.dmm.co.jp/..."
								class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono text-sm"
							/>
							<p class="text-xs text-muted-foreground mt-2">
								Enter a DVD ID (e.g., IPX-123), content ID (e.g., ipx00535), or a direct URL from DMM or R18.dev
							</p>
						</div>

						<div>
							<p class="text-sm text-muted-foreground mb-4">
								Select which scrapers to use. The results will be aggregated according to your configured priorities.
							</p>

							<ScraperSelector
								scrapers={availableScrapers}
								bind:selected={rescrapeSelectedScrapers}
								disabled={false}
							/>
						</div>
					</div>
				{:else}
					<!-- Standard Rescrape -->
					<p class="text-sm text-muted-foreground mb-4">
						Select which scrapers to use for fetching fresh metadata. The results will be
						aggregated according to your configured priorities.
					</p>

					<ScraperSelector
						scrapers={availableScrapers}
						bind:selected={rescrapeSelectedScrapers}
						disabled={false}
					/>
				{/if}

				<!-- Merge Strategy Selection -->
				<div class="mt-6 space-y-4">
					<div>
						<h3 class="font-semibold mb-2">NFO Merge Strategy</h3>
						<p class="text-sm text-muted-foreground mb-3">
							Choose how to merge existing NFO data with freshly scraped data. Leave empty to replace all data.
						</p>
					</div>

					<!-- Preset Selection -->
					<div class="space-y-2">
						<div class="flex items-center justify-between">
							<h4 class="text-sm font-medium">Quick Presets</h4>
							{#if rescrapePreset}
								<button
									onclick={() => { rescrapePreset = undefined; }}
									class="text-xs text-primary hover:underline"
								>
									Clear preset
								</button>
							{/if}
						</div>
						<div class="grid grid-cols-3 gap-2">
							<button
								onclick={() => applyRescrapePreset('conservative')}
								class="p-3 rounded-lg border-2 text-sm transition-all {rescrapePreset === 'conservative' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
							>
								<div class="font-medium">🛡️ Conservative</div>
								<div class="text-xs text-muted-foreground mt-1">Never overwrite existing</div>
							</button>
							<button
								onclick={() => applyRescrapePreset('gap-fill')}
								class="p-3 rounded-lg border-2 text-sm transition-all {rescrapePreset === 'gap-fill' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
							>
								<div class="font-medium">📝 Gap Fill</div>
								<div class="text-xs text-muted-foreground mt-1">Fill missing fields only</div>
							</button>
							<button
								onclick={() => applyRescrapePreset('aggressive')}
								class="p-3 rounded-lg border-2 text-sm transition-all {rescrapePreset === 'aggressive' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
							>
								<div class="font-medium">⚡ Aggressive</div>
								<div class="text-xs text-muted-foreground mt-1">Trust scrapers completely</div>
							</button>
						</div>
					</div>

					<!-- Individual Strategy Selection -->
					<div class="space-y-2">
						<h4 class="text-sm font-medium">Or Choose Individual Strategies</h4>
						<div class="grid grid-cols-2 gap-2">
							<button
								onclick={() => { rescrapeScalarStrategy = 'prefer-nfo'; rescrapePreset = undefined; }}
								class="p-3 rounded-lg border-2 text-sm transition-all {rescrapeScalarStrategy === 'prefer-nfo' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
							>
								<div class="font-medium">Prefer NFO</div>
								<div class="text-xs text-muted-foreground mt-1">Keep existing data</div>
							</button>
							<button
								onclick={() => { rescrapeScalarStrategy = 'prefer-scraper'; rescrapePreset = undefined; }}
								class="p-3 rounded-lg border-2 text-sm transition-all {rescrapeScalarStrategy === 'prefer-scraper' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
							>
								<div class="font-medium">Prefer Scraped</div>
								<div class="text-xs text-muted-foreground mt-1">Update with fresh data</div>
							</button>
							<button
								onclick={() => { rescrapeScalarStrategy = 'preserve-existing'; rescrapePreset = undefined; }}
								class="p-3 rounded-lg border-2 text-sm transition-all {rescrapeScalarStrategy === 'preserve-existing' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
							>
								<div class="font-medium">Preserve Existing</div>
								<div class="text-xs text-muted-foreground mt-1">Never overwrite</div>
							</button>
							<button
								onclick={() => { rescrapeScalarStrategy = 'fill-missing-only'; rescrapePreset = undefined; }}
								class="p-3 rounded-lg border-2 text-sm transition-all {rescrapeScalarStrategy === 'fill-missing-only' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
							>
								<div class="font-medium">Fill Missing Only</div>
								<div class="text-xs text-muted-foreground mt-1">Safe gap filling</div>
							</button>
							<button
								onclick={() => { rescrapeScalarStrategy = ''; rescrapePreset = undefined; }}
								class="p-3 rounded-lg border-2 text-sm transition-all col-span-2 {rescrapeScalarStrategy === '' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
							>
								<div class="font-medium">Replace All</div>
								<div class="text-xs text-muted-foreground mt-1">Fresh scrape only (ignore existing NFO)</div>
							</button>
						</div>
					</div>
				</div>
			{/if}			</div>

			<!-- Footer -->
			<div class="p-6 border-t flex items-center justify-end gap-3">
				<Button
					variant="outline"
					onclick={() => (showRescrapeModal = false)}
					disabled={rescrapingStates.get(rescrapeMovieId) || false}
				>
					{#snippet children()}Cancel{/snippet}
				</Button>
				<Button
					onclick={executeRescrape}
					disabled={rescrapingStates.get(rescrapeMovieId) || false}
				>
					{#snippet children()}
						{#if rescrapingStates.get(rescrapeMovieId)}
							<Loader2 class="h-4 w-4 mr-2 animate-spin" />
							{manualSearchMode ? 'Scraping...' : 'Rescraping...'}
						{:else}
							<RotateCcw class="h-4 w-4 mr-2" />
							{manualSearchMode ? 'Search' : 'Rescrape'}
						{/if}
					{/snippet}
				</Button>
			</div>
		</Card>
		</div>
	</div>
{/if}

<!-- Destination Browser Modal -->
{#if showDestinationBrowser}
	<div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4" use:portalToBody in:fade|local={{ duration: 140 }} out:fade|local={{ duration: 120 }}>
		<div class="bg-background rounded-lg shadow-xl max-w-4xl w-full max-h-[80vh] flex flex-col" in:scale|local={{ start: 0.97, duration: 180, easing: quintOut }} out:scale|local={{ start: 1, opacity: 0.7, duration: 130, easing: quintOut }}>
			<!-- Modal Header -->
			<div class="p-6 border-b flex items-center justify-between">
				<div>
					<h2 class="text-xl font-bold">Select Destination Folder</h2>
					<p class="text-sm text-muted-foreground mt-1">
						Navigate to and select the folder where files will be organized
					</p>
				</div>
				<button
					onclick={cancelDestination}
					class="text-muted-foreground hover:text-foreground transition-colors"
				>
					✕
				</button>
			</div>

			<!-- Modal Body -->
			<div class="flex-1 overflow-auto p-6">
				<FileBrowser
					initialPath={destinationPath || '/'}
					onFileSelect={handleDestinationSelect}
					onPathChange={handleDestinationPathChange}
					multiSelect={false}
					folderOnly={true}
				/>
			</div>

			<!-- Modal Footer -->
			<div class="p-6 border-t space-y-3">
				<div class="flex items-center gap-2">
					<span class="text-sm font-medium text-muted-foreground">Selected Path:</span>
					<code
						class="flex-1 px-3 py-1.5 bg-accent rounded text-sm font-mono text-foreground overflow-x-auto"
					>
						{tempDestinationPath || destinationPath || '/'}
					</code>
				</div>
				<div class="flex items-center justify-end gap-2">
					<Button variant="outline" onclick={cancelDestination}>
						{#snippet children()}
							Cancel
						{/snippet}
					</Button>
					<Button onclick={confirmDestination}>
						{#snippet children()}
							Use This Folder
						{/snippet}
					</Button>
				</div>
			</div>
		</div>
	</div>
{/if}
