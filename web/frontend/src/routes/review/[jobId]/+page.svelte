<script lang="ts">
	import { onDestroy, onMount, untrack } from 'svelte';
	import { fade } from 'svelte/transition';
	import { browser } from '$app/environment';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { apiClient } from '$lib/api/client';
	import { createConfigQuery } from '$lib/query/queries';
	import type { BatchJobResponse, FileResult, Movie, PosterCropResponse, PosterFromURLResponse, Scraper, UpdateRequest } from '$lib/api/types';
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
	import ReviewGridCard from './components/ReviewGridCard.svelte';
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
		normalizeCropBox,
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

	const posterFromUrlMutation = createMutation(() => ({
		mutationFn: async ({ movieId, url }: { movieId: string; url: string }) => {
			return apiClient.updateBatchMoviePosterFromURL(jobId, movieId, { url });
		},
		onSuccess: (data: PosterFromURLResponse, { movieId }) => {
			const currentJob = job;
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
				skipJobSync = true;
				job = updatedJob;

				const updatedEdited = new Map(editedMovies);
				for (const [filePath, movie] of updatedEdited) {
					if (movie.id === movieId) {
						updatedEdited.set(filePath, {
							...movie,
							poster_url: data.poster_url,
							cropped_poster_url: data.cropped_poster_url,
							should_crop_poster: false
						});
					}
				}
				editedMovies = updatedEdited;
			}

			if (currentResult) {
				posterPreviewOverrides = new Map(posterPreviewOverrides).set(currentResult.file_path, {
					url: data.cropped_poster_url,
					version: Date.now()
				});
			}

			void queryClient.invalidateQueries({ queryKey: ['batch-job', jobId] });
			void queryClient.invalidateQueries({ queryKey: ['batch-job-slim', jobId] });
		},
		onError: (err: Error) => {
			toastStore.error(`Failed to set poster from screenshot: ${err.message}`);
		}
	}));

	function applyPosterFromUrl(movieId: string, url: string) {
		if (!job || posterFromUrlMutation.isPending) return;
		posterFromUrlMutation.mutate({ movieId, url });
	}

	const excludeMovieMutation = createMutation(() => ({
		mutationFn: async ({ jobId: mutationJobId, movieId }: { jobId: string; movieId: string }) => {
			return apiClient.excludeBatchMovie(mutationJobId, movieId);
		},
		onSuccess: async (_data, { movieId }) => {
			toastStore.success(`Movie ${movieId} excluded from organization`);
			await queryClient.invalidateQueries({ queryKey: ['batch-job', jobId] });
			await queryClient.invalidateQueries({ queryKey: ['batch-job-slim', jobId] });

			const movieResultsLength = movieResults.length;
			if (movieResultsLength === 0) {
				await goto('/jobs');
				return;
			}

			if (currentMovieIndex >= movieResultsLength) {
				currentMovieIndex = movieResultsLength - 1;
			}
		},
		onError: (err: Error) => {
			toastStore.error(`Failed to exclude movie: ${err.message}`);
		}
	}));

	const saveEditsMutation = createMutation(() => ({
		mutationFn: async () => {
			const savePromises = Array.from(editedMovies.entries()).map(([filePath, movie]) => {
				const movieToSave = { ...movie };
				if (movieToSave.display_title) {
					movieToSave.title = movieToSave.display_title;
				}
				return apiClient.updateBatchMovie(jobId, movieToSave.id, movieToSave);
			});

			if (savePromises.length > 0) {
				await Promise.all(savePromises);
			}
		},
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: ['batch-job', jobId] });
			void queryClient.invalidateQueries({ queryKey: ['batch-job-slim', jobId] });
		},
		onError: (err: Error) => {
			toastStore.error(`Failed to save edits: ${err.message}`);
		}
	}));

	const posterCropMutation = createMutation(() => ({
		mutationFn: async ({ jobId: mutationJobId, movieId, crop }: { jobId: string; movieId: string; crop: PosterCropBox }) => {
			return apiClient.updateBatchMoviePosterCrop(mutationJobId, movieId, crop);
		},
		onSuccess: (response: PosterCropResponse, { movieId }) => {
			const currentResultVal = currentResult;
			if (currentResultVal) {
				const nextOverrides = new Map(posterPreviewOverrides);
				nextOverrides.set(currentResultVal.file_path, {
					url: response.cropped_poster_url,
					version: Date.now()
				});
				posterPreviewOverrides = nextOverrides;

				const cropMetricsVal = cropMetrics;
				const cropBoxVal = cropBox;
				if (cropMetricsVal && cropBoxVal) {
					const nextStates = new Map(posterCropStates);
					nextStates.set(currentResultVal.file_path, normalizeCropBox(cropBoxVal, cropMetricsVal));
					posterCropStates = nextStates;
				}
			}

			toastStore.success('Poster crop updated');
			showPosterCropModal = false;

			void queryClient.invalidateQueries({ queryKey: ['batch-job', jobId] });
			void queryClient.invalidateQueries({ queryKey: ['batch-job-slim', jobId] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message || 'Failed to update poster crop');
		}
	}));

	let currentMovieIndex = $state(0);
	let editedMovies = $state<Map<string, Movie>>(new Map());
	let originalPosterState = $state<Map<string, { poster_url: string; cropped_poster_url: string; should_crop_poster: boolean }>>(new Map());
	let organizing = $state(false);
	let destinationPath = $state('');
	let organizeOperation = $state<OrganizeOperation>('move');
	let showDestinationBrowser = $state(false);
	let tempDestinationPath = $state('');
	let showTrailerModal = $state(false);

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
	let isUpdateMode = $derived($page.url.searchParams.get('update') === 'true');
	let showFieldScraperSources = $state(false);
	const SHOW_FIELD_SCRAPER_SOURCES_KEY = 'javinizer.review.showFieldScraperSources';
	const VIEW_MODE_KEY = 'javinizer.review.viewMode';
	let viewMode = $state<'detail' | 'grid'>('detail');
	let posterCropStatesStorageKey = $derived(`javinizer.review.posterCropStates.${jobId}`);

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

	function getCanOrganize(): boolean {
		if (isUpdateMode) return false;
		if (!config) return false;
		const mode = getEffectiveOperationMode();
		return mode === 'organize' || mode === 'in-place' || mode === 'in-place-norenamefolder';
	}

	const canOrganize = $derived(getCanOrganize());

	// Image viewer state (unified for screenshots and cover)
	let showImageViewer = $state(false);
	let imageViewerImages = $state<string[]>([]);
	let imageViewerIndex = $state(0);
	let imageViewerTitle = $state<string | undefined>(undefined);

	// Sidebar screenshot expansion state
	let showAllSidebarScreenshots = $state(false);

	// Source file path expansion state
	let showFullSourcePath = $state(false);

	let forceOverwrite = $state(false);
	let preserveNfo = $state(false);
	let skipNfo = $state(false);
	let skipDownload = $state(false);

	// Image panel collapse state
	let showImagePanelContent = $state(true);

	// Preview screenshot expansion state
	let showAllPreviewScreenshots = $state(false);

	// Manual poster crop state
	let showPosterCropModal = $state(false);
	let posterCropLoadError = $state<string | null>(null);
	let cropSourceURL = $state('');
	let cropImageElement = $state<HTMLImageElement | null>(null);
	let cropMetrics = $state<PosterCropMetrics | null>(null);
	let cropBox = $state<PosterCropBox | null>(null);
	let cropDragState = $state<PosterCropDragState | null>(null);
	let posterPreviewOverrides = $state<Map<string, PosterPreviewOverride>>(new Map());
	let posterCropStates = $state<Map<string, PosterCropState>>(new Map());

	$effect(() => {
		const jobData = jobQuery.data;
		if (jobData) {
			untrack(() => {
				if (jobData.destination && !destinationPath) {
					destinationPath = jobData.destination;
				}
				if (originalPosterState.size === 0) {
					const posterMap = new Map<string, { poster_url: string; cropped_poster_url: string; should_crop_poster: boolean }>();
					for (const result of Object.values(jobData.results) as FileResult[]) {
						if (result.data) {
							posterMap.set(result.file_path, {
								poster_url: result.data.original_poster_url || result.data.poster_url || '',
								cropped_poster_url: result.data.original_cropped_poster_url || result.data.cropped_poster_url || '',
								should_crop_poster: result.data.original_should_crop_poster ?? result.data.should_crop_poster ?? false
							});
						}
					}
					originalPosterState = posterMap;
				}
			});
		}
	});

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

			if (baseURL.includes('v=')) return baseURL;

			const separator = baseURL.includes('?') ? '&' : '?';
			return `${baseURL}${separator}v=${override.version}`;
		})()
	);

	let editedMovieKey = $derived.by(() => {
		const fp = currentResult?.file_path;
		if (!fp || !editedMovies.has(fp)) return '';
		return JSON.stringify(editedMovies.get(fp));
	});

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

	// Subscribe to WebSocket messages during organize operation
	$effect(() => {
		const unsubscribe = websocketStore.subscribe((ws) => {
			organizeController.handleWebSocketMessage(ws.messages.at(-1));
		});

		return unsubscribe;
	});

	function updateCurrentMovie(movie: Movie) {
		if (!currentResult?.data) return;

		const isActuallyModified = !equal(movie, currentResult.data);

		if (isActuallyModified) {
			editedMovies = new Map(editedMovies).set(currentResult.file_path, movie);
		} else {
			const next = new Map(editedMovies);
			next.delete(currentResult.file_path);
			editedMovies = next;
		}
	}

	function resetCurrentMovie() {
		if (!currentResult?.data) return;
		const next = new Map(editedMovies);
		next.delete(currentResult.file_path);
		editedMovies = next;
	}

	function clearPosterPreviewOverride() {
		if (!currentResult) return;
		const next = new Map(posterPreviewOverrides);
		next.delete(currentResult.file_path);
		posterPreviewOverrides = next;
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
			applyPosterFromUrl(currentMovie.id, original.poster_url);
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
		applyPosterFromUrl(currentMovie.id, url);
	}

	async function saveAllEdits() {
		return saveEditsMutation.mutateAsync();
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
			getBatchJob: (nextJobId) => apiClient.getBatchJob(nextJobId, true),
			organizeBatchJob: (nextJobId, request) => apiClient.organizeBatchJob(nextJobId, request),
			updateBatchJob: (nextJobId, request) => apiClient.updateBatchJob(nextJobId, request)
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
		getPosterCropStates: () => posterCropStates,
		mutatePosterCrop: (mutationJobId, movieId, crop) => {
			posterCropMutation.mutate({ jobId: mutationJobId, movieId, crop });
		}
	});

	const reviewPageController = createReviewPageController({
		getJob: () => job,
		getCurrentMovie: () => currentMovie,
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
		excludeMovie: (mutationJobId, movieId) => {
			excludeMovieMutation.mutate({ jobId: mutationJobId, movieId });
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

	onMount(() => {
		if (browser) {
			showFieldScraperSources =
				localStorage.getItem(SHOW_FIELD_SCRAPER_SOURCES_KEY) === 'true';
			viewMode = localStorage.getItem(VIEW_MODE_KEY) === 'grid' ? 'grid' : 'detail';
			const savedCrops = localStorage.getItem(posterCropStatesStorageKey);
			if (savedCrops) {
				try {
					const parsed = JSON.parse(savedCrops) as Record<string, PosterCropState>;
					const restored = new Map<string, PosterCropState>();
					for (const [k, v] of Object.entries(parsed)) {
						restored.set(k, v);
					}
					posterCropStates = restored;
				} catch {
					localStorage.removeItem(posterCropStatesStorageKey);
				}
			}
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
				canOrganize={canOrganize}
				organizing={organizing}
				movieResultsLength={movieResults.length}
				destinationPath={destinationPath}
				bind:viewMode={viewMode}
				bind:forceOverwrite={forceOverwrite}
				bind:preserveNfo={preserveNfo}
				bind:skipNfo={skipNfo}
				bind:skipDownload={skipDownload}
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

			{#if viewMode === 'grid'}
				<div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-4">
					{#each movieGroups as group, index}
						<ReviewGridCard
							movieGroup={group}
							isSelected={index === currentMovieIndex}
							isEdited={editedMovies.has(group.primaryResult.file_path)}
							displayPosterUrl={(() => {
								const movie = group.primaryResult.data;
								if (!movie) return undefined;
								const override = posterPreviewOverrides.get(group.primaryResult.file_path);
								const baseURL = override?.url || movie.cropped_poster_url || movie.poster_url;
								if (!baseURL) return undefined;
								if (!override) return baseURL;
								if (baseURL.includes('v=')) return baseURL;
								const separator = baseURL.includes('?') ? '&' : '?';
								return `${baseURL}${separator}v=${override.version}`;
							})()}
							previewImageURL={reviewPageController.previewImageURL}
							onclick={() => {
								currentMovieIndex = index;
								viewMode = 'detail';
							}}
						/>
					{/each}
				</div>
			{:else}
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
						onUseScreenshotAsPoster={useScreenshotAsPoster}
						onResetPoster={resetPoster}
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

					<!-- Destination Path (hidden in update mode or metadata-only mode) -->
					{#if canOrganize}
						<DestinationSettingsCard
							bind:destinationPath={destinationPath}
							bind:organizeOperation={organizeOperation}
							preview={preview}
							{previewNeedsDestination}
							effectiveOperationMode={getEffectiveOperationMode()}
							bind:showAllPreviewScreenshots={showAllPreviewScreenshots}
							bind:skipNfo={skipNfo}
							bind:skipDownload={skipDownload}
							onOpenDestinationBrowser={reviewPageController.openDestinationBrowser}
						/>
					{/if}

					<MovieMetadataCard
						currentMovie={currentMovie}
						currentResult={currentResult}
						bind:showFieldScraperSources={showFieldScraperSources}
						isRescraping={rescrapingStates.get(currentResult?.movie_id || '') || false}
						onOpenRescrape={() => currentResult && openRescrapeModal(currentResult.movie_id)}
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
						onUseScreenshotAsPoster={useScreenshotAsPoster}
					/>

					{#if canOrganize}
						<ReviewActionBar
							isUpdateMode={isUpdateMode}
							organizing={organizing}
							destinationPath={destinationPath}
							movieResultsLength={movieResults.length}
							onCancel={() => goto('/browse')}
							onOrganizeAll={organizeAll}
						/>
					{/if}
				</div>
				</div>
			{/key}
			{/if}
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
	posterCropSaving={posterCropMutation.isPending}
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
