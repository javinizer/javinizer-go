<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { apiClient } from '$lib/api/client';
	import type { BatchJobResponse, FileResult, Movie, OrganizePreviewResponse, Scraper } from '$lib/api/types';
	import { toastStore } from '$lib/stores/toast';
	import { websocketStore } from '$lib/stores/websocket';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import MovieEditor from '$lib/components/MovieEditor.svelte';
	import ActressEditor from '$lib/components/ActressEditor.svelte';
	import ScreenshotManager from '$lib/components/ScreenshotManager.svelte';
	import ImageViewer from '$lib/components/ImageViewer.svelte';
	import VideoModal from '$lib/components/VideoModal.svelte';
	import ScraperSelector from '$lib/components/ScraperSelector.svelte';
	import {
		ChevronLeft,
		ChevronRight,
		ChevronDown,
		ChevronUp,
		Play,
		RotateCcw,
		AlertCircle,
		FolderOpen,
		Image as ImageIcon,
		Loader2,
		X,
		Check
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
	let copyOnly = $state(false);
	let showDestinationBrowser = $state(false);
	let showTrailerModal = $state(false);
	let preview: OrganizePreviewResponse | null = $state(null);

	// Organize operation state
	let organizeProgress = $state(0);
	let organizeStatus = $state<'idle' | 'organizing' | 'completed' | 'failed'>('idle');
	let fileStatuses = $state<Map<string, {status: string, error?: string}>>(new Map());

	// Determine which panels to show based on download settings
	const showCoverPanel = $derived(config?.Output?.DownloadCover ?? true);
	const showPosterPanel = $derived(config?.Output?.DownloadPoster ?? true);
	const showTrailerPanel = $derived(config?.Output?.DownloadTrailer ?? false);
	const showScreenshotsPanel = $derived(config?.Output?.DownloadExtrafanart ?? false);

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

	// Rescrape modal state
	let availableScrapers: Scraper[] = $state([]);
	let showRescrapeModal = $state(false);
	let rescrapeMovieId = $state('');
	let rescrapeSelectedScrapers: string[] = $state([]);
	let rescrapingStates = $state<Map<string, boolean>>(new Map());

	// Get all successful movie results
	const movieResults = $derived<FileResult[]>(
		job ? (Object.values((job as BatchJobResponse).results) as FileResult[]).filter((r) => r.status === 'completed' && r.data) : []
	);
	const currentResult = $derived<FileResult | undefined>(movieResults[currentMovieIndex]);
	const currentMovie = $derived<Movie | null>(
		currentResult && currentResult.data
			? editedMovies.get(currentResult.file_path) || currentResult.data
			: null
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
		try {
			preview = await apiClient.previewOrganize(jobId, currentMovie.id, {
				destination: destinationPath,
				copy_only: copyOnly
			});
		} catch (e) {
			console.error('Failed to fetch preview:', e);
			preview = null;
		}
	}

	// Fetch preview when destination, copy mode, or current movie changes
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

	// Subscribe to WebSocket messages during organize operation
	$effect(() => {
		if (organizeStatus !== 'organizing') return;

		const unsubscribe = websocketStore.subscribe((ws) => {
			// Get the latest message
			const msg = ws.messages.at(-1);
			if (!msg || msg.job_id !== jobId) return;

			if (msg.status === 'organizing') {
				organizeProgress = msg.progress || 0;
			}

			if (msg.status === 'failed' && msg.file_path) {
				fileStatuses.set(msg.file_path, {
					status: 'failed',
					error: msg.error
				});
				fileStatuses = new Map(fileStatuses); // trigger reactivity

				// Show toast notification for immediate feedback
				const fileName = msg.file_path.split(/[\\/]/).pop();
				toastStore.error(`Failed to organize ${fileName}: ${msg.error}`, 7000);
			}

			if (msg.status === 'organized' && msg.file_path) {
				fileStatuses.set(msg.file_path, {status: 'success'});
				fileStatuses = new Map(fileStatuses);
			}

			if (msg.status === 'organization_completed') {
				organizeStatus = 'completed';
				organizeProgress = 100;
				organizing = false;

				// Count failures
				const failures = Array.from(fileStatuses.values())
					.filter(s => s.status === 'failed').length;

				if (failures === 0) {
					toastStore.success(msg.message || 'All files organized successfully', 5000);
					setTimeout(() => goto('/browse'), 1000);
				}
				// If there are failures, stay on page to show them
			}
		});

		return unsubscribe;
	});

	function updateCurrentMovie(movie: Movie) {
		if (!currentResult) return;
		editedMovies.set(currentResult.file_path, movie);
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
		showRescrapeModal = true;
	}

	async function executeRescrape() {
		if (rescrapeSelectedScrapers.length === 0) {
			toastStore.error('Please select at least one scraper');
			return;
		}

		if (!currentResult) {
			toastStore.error('No current movie to rescrape');
			return;
		}

		// Set rescraping state
		rescrapingStates.set(rescrapeMovieId, true);
		rescrapingStates = new Map(rescrapingStates);

		try {
			const updatedMovie = await apiClient.rescrapeMovie(rescrapeMovieId, {
				selected_scrapers: rescrapeSelectedScrapers,
				force: true
			});

			console.log('Rescrape successful, updating job results for:', rescrapeMovieId);

			// Update the movie in the job results using the current file path
			if (job && currentResult.file_path) {
				const filePath = currentResult.file_path;
				console.log('Updating result for file:', filePath);

				// Create new results object with updated movie
				const newResults = { ...job.results };

				// Create new result object to trigger reactivity
				newResults[filePath] = {
					...newResults[filePath],
					data: updatedMovie
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

			toastStore.success(`Successfully rescraped ${rescrapeMovieId}`);
			showRescrapeModal = false;
		} catch (error) {
			console.error('Rescrape failed:', error);
			const errorMessage = error instanceof Error ? error.message : JSON.stringify(error);
			toastStore.error('Rescrape failed: ' + errorMessage);
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

		// Clear old WebSocket messages to prevent stale completion messages
		websocketStore.clearMessages();

		organizeStatus = 'organizing';
		organizing = true;
		organizeProgress = 0;
		fileStatuses = new Map();

		try {
			// Save all edited movies to backend first
			if (editedMovies.size > 0) {
				await saveAllEdits();
			}

			// Start organize (returns immediately)
			await apiClient.organizeBatchJob(jobId, {
				destination: destinationPath,
				copy_only: copyOnly
			});

			// DON'T show success or navigate - wait for WebSocket messages
		} catch (e) {
			organizeStatus = 'failed';
			organizing = false;
			const errorMessage = e instanceof Error ? e.message : 'Failed to start organize';
			toastStore.error(errorMessage, 7000);
		}
	}

	function openDestinationBrowser() {
		// TODO: Implement browse dialog
		showDestinationBrowser = true;
	}

	function hasChanges(filePath: string): boolean {
		return editedMovies.has(filePath);
	}

	// Image viewer functions
	function openScreenshotViewer(index: number) {
		if (!currentMovie?.screenshot_urls) return;
		imageViewerImages = currentMovie.screenshot_urls;
		imageViewerIndex = index;
		imageViewerTitle = undefined;
		showImageViewer = true;
	}

	function openCoverViewer() {
		if (!currentMovie?.cover_url) return;
		imageViewerImages = [currentMovie.cover_url];
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
		// Get destination from URL params if provided
		const urlDestination = $page.url.searchParams.get('destination');
		if (urlDestination) {
			destinationPath = urlDestination;
		}
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
					<AlertCircle class="h-12 w-12 mx-auto mb-4" />
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
						Review and edit scraped metadata before organizing files
					</p>
				</div>
				<div class="flex items-center gap-3">
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
							/>
						</div>
					</div>

					<!-- File statuses -->
					{#if fileStatuses.size > 0}
						<div class="space-y-2 max-h-64 overflow-y-auto">
							{#each Array.from(fileStatuses.entries()) as [filePath, status]}
								<div class="flex items-start gap-2 text-sm p-2 rounded {status.status === 'failed' ? 'bg-red-50' : 'bg-green-50'}">
									{#if status.status === 'failed'}
										<AlertCircle class="h-4 w-4 text-red-600 flex-shrink-0 mt-0.5" />
									{:else}
										<Check class="h-4 w-4 text-green-600 flex-shrink-0 mt-0.5" />
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

			<!-- Organize Completed with Errors -->
			{#if organizeStatus === 'completed'}
				{@const failures = Array.from(fileStatuses.values()).filter(s => s.status === 'failed')}
				{@const successes = Array.from(fileStatuses.values()).filter(s => s.status === 'success')}

				{#if failures.length > 0}
					<Card class="p-6 border-orange-500">
						<div class="flex items-start gap-3">
							<AlertCircle class="h-6 w-6 text-orange-600 flex-shrink-0" />
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
									<Button onclick={() => organizeStatus = 'idle'}>
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

			<div class="grid grid-cols-1 lg:grid-cols-[300px_1fr] gap-6">
				<!-- Left Sidebar: Media Preview -->
				<div class="space-y-4">
					<!-- Poster Image -->
					{#if showPosterPanel}
						<Card class="p-4">
							<h3 class="font-semibold mb-3 text-sm">
								Poster{currentMovie.should_crop_poster ? ' (Cropped)' : ''}
							</h3>
							{#if currentMovie.poster_url}
								<div class="w-full aspect-2/3 overflow-hidden rounded border relative">
									{#if currentMovie.should_crop_poster}
										<!-- Crop to show only right 47.2% of image (removes promotional text on left) -->
										<img
											src={currentMovie.poster_url}
											alt="Poster"
											class="absolute h-full"
											style="right: 0; width: auto; min-width: 211.8%; object-fit: cover; object-position: right center;"
											onerror={(e) => {
												(e.currentTarget as HTMLImageElement).src = 'https://via.placeholder.com/300x450?text=No+Poster';
											}}
										/>
									{:else}
										<!-- Use poster directly without cropping -->
										<img
											src={currentMovie.poster_url}
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
										src={currentMovie.cover_url}
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
											src={url}
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

							<div class="text-center">
								<p class="font-semibold">
									Movie {currentMovieIndex + 1} of {movieResults.length}
								</p>
								<p class="text-sm text-muted-foreground">{currentMovie.id}</p>
								{#if hasChanges(currentResult.file_path)}
									<span class="text-xs text-orange-600 flex items-center gap-1 justify-center mt-1">
										<AlertCircle class="h-3 w-3" />
										Modified
									</span>
								{/if}
							</div>

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
					</Card>

					<!-- File Path Info -->
					<Card class="p-4">
						<div class="min-w-0">
							<div class="flex items-center justify-between mb-2">
								<p class="text-sm font-medium">Source File</p>
								{#if currentResult.file_path.length > 80}
									<button
										onclick={() => showFullSourcePath = !showFullSourcePath}
										class="text-xs text-primary hover:text-primary/80 transition-colors cursor-pointer"
									>
										{showFullSourcePath ? 'Hide' : 'Show full path'}
									</button>
								{/if}
							</div>
							<div class="bg-accent rounded px-3 py-2 {showFullSourcePath ? 'overflow-x-auto' : ''}">
								<code class="text-xs block {showFullSourcePath ? 'whitespace-nowrap' : ''}" title={currentResult.file_path}>
									{showFullSourcePath ? currentResult.file_path : truncatePath(currentResult.file_path)}
								</code>
							</div>
						</div>
					</Card>

					<!-- Destination Path -->
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

							<div class="flex items-center gap-2">
								<input
									type="checkbox"
									id="copyOnly"
									bind:checked={copyOnly}
									class="w-4 h-4 text-primary bg-gray-100 border-gray-300 rounded focus:ring-primary focus:ring-2"
								/>
								<label for="copyOnly" class="text-sm text-muted-foreground cursor-pointer">
									Copy files only (don't move)
								</label>
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
										<div class="break-all" style="margin-left: {fileIndent + 4}px">🎬 {preview.file_name}.mp4</div>
										<div class="break-all" style="margin-left: {fileIndent + 4}px">📄 {preview.file_name}.nfo</div>
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

					<!-- Metadata Editor -->
					<Card class="p-6">
						<div class="space-y-4">
							<div class="flex items-center justify-between">
								<h2 class="text-xl font-semibold">Movie Metadata</h2>
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

							<MovieEditor
								movie={currentMovie!}
								originalMovie={currentResult.data!}
								onUpdate={updateCurrentMovie}
							/>
						</div>
					</Card>

					<!-- Actresses -->
					<Card class="p-6">
						<ActressEditor movie={currentMovie!} onUpdate={updateCurrentMovie} />
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
									<ScreenshotManager movie={currentMovie!} onUpdate={updateCurrentMovie} />
								{/if}
							</div>
						</Card>
					{/if}

					<!-- Action Buttons -->
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
				</div>
			</div>
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

<!-- Rescrape Modal -->
{#if showRescrapeModal}
	<div class="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4">
		<Card class="w-full max-w-lg flex flex-col max-h-[90vh]">
			<!-- Header -->
			<div class="p-6 border-b flex items-center justify-between">
				<h2 class="text-xl font-bold">Rescrape {rescrapeMovieId}</h2>
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
							<p class="text-sm font-medium">Rescraping metadata...</p>
							<p class="text-xs text-muted-foreground">
								Fetching data from {rescrapeSelectedScrapers.join(', ')}
							</p>
						</div>
					</div>
				{:else}
					<!-- Selection State -->
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
			</div>

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
							Rescraping...
						{:else}
							<RotateCcw class="h-4 w-4 mr-2" />
							Rescrape
						{/if}
					{/snippet}
				</Button>
			</div>
		</Card>
	</div>
{/if}

