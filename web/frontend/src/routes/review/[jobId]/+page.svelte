<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { apiClient } from '$lib/api/client';
	import type { BatchJobResponse, FileResult, Movie, OrganizePreviewResponse } from '$lib/api/types';
	import { toastStore } from '$lib/stores/toast';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import MovieEditor from '$lib/components/MovieEditor.svelte';
	import ActressEditor from '$lib/components/ActressEditor.svelte';
	import ScreenshotManager from '$lib/components/ScreenshotManager.svelte';
	import ImageViewer from '$lib/components/ImageViewer.svelte';
	import VideoModal from '$lib/components/VideoModal.svelte';
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
		X
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

	// Image panel collapse state
	let showImagePanelContent = $state(true);

	// Preview screenshot expansion state
	let showAllPreviewScreenshots = $state(false);

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
			const response = await fetch('http://localhost:8080/api/v1/config');
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

		organizing = true;
		try {
			// Save all edited movies to backend first
			if (editedMovies.size > 0) {
				await saveAllEdits();
			}

			// Then call organize with destination
			await apiClient.organizeBatchJob(jobId, {
				destination: destinationPath,
				copy_only: copyOnly
			});

			const fileCount = movieResults.length;
			const action = copyOnly ? 'copied' : 'organized';
			toastStore.success(
				`Successfully ${action} ${fileCount} file${fileCount !== 1 ? 's' : ''} to ${destinationPath}`,
				7000
			);

			// Short delay to allow toast to be seen before navigation
			setTimeout(() => {
				goto('/browse');
			}, 500);
		} catch (e) {
			const errorMessage = e instanceof Error ? e.message : 'Failed to organize files';
			error = errorMessage;
			toastStore.error(errorMessage, 7000);
			organizing = false;
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

			<div class="grid grid-cols-1 lg:grid-cols-[300px_1fr] gap-6">
				<!-- Left Sidebar: Media Preview -->
				<div class="space-y-4">
					<!-- Poster Image -->
					{#if showPosterPanel}
						<Card class="p-4">
							<h3 class="font-semibold mb-3 text-sm">Poster (Cropped)</h3>
							{#if currentMovie.poster_url}
								<!-- Crop to show only right 47.2% of image (removes promotional text on left) -->
								<div class="w-full aspect-2/3 overflow-hidden rounded border relative">
									<img
										src={currentMovie.poster_url}
										alt="Poster"
										class="absolute h-full"
										style="right: 0; width: auto; min-width: 211.8%; object-fit: cover; object-position: right center;"
										onerror={(e) => {
											(e.currentTarget as HTMLImageElement).src = 'https://via.placeholder.com/300x450?text=No+Poster';
										}}
									/>
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
										class="w-full rounded border aspect-video object-cover"
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
				<div class="space-y-6">
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
						<div>
							<p class="text-sm font-medium mb-2">Source File</p>
							<code class="text-xs bg-accent px-3 py-2 rounded block truncate">
								{currentResult.file_path}
							</code>
						</div>
					</Card>

					<!-- Destination Path -->
					<Card class="p-4">
						<div class="space-y-3">
							<div class="flex items-center gap-2">
								<FolderOpen class="h-5 w-5 text-primary" />
								<h3 class="font-semibold">Output Destination</h3>
							</div>
							<div class="flex gap-2">
								<input
									type="text"
									bind:value={destinationPath}
									placeholder="Enter destination path (e.g., /path/to/output)"
									class="flex-1 px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono text-sm"
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
								<div class="mt-3 p-3 bg-accent/50 rounded border border-dashed">
									<p class="text-xs font-medium mb-2 text-muted-foreground">Preview:</p>
									<div class="font-mono text-xs space-y-1">
										<div class="text-muted-foreground">📁 {destinationPath}/</div>
										<div class="ml-4 text-muted-foreground">
											📁 {preview.folder_name}/
										</div>
										<div class="ml-8">🎬 {preview.file_name}.mp4</div>
										<div class="ml-8">📄 {preview.file_name}.nfo</div>
										<div class="ml-8">🖼️ {preview.file_name}-poster.jpg</div>
										<div class="ml-8">🖼️ {preview.file_name}-fanart.jpg</div>
										{#if preview.screenshots && preview.screenshots.length > 0}
											<div class="ml-8 text-muted-foreground">📁 extrafanart/</div>
											{#each (showAllPreviewScreenshots ? preview.screenshots : preview.screenshots.slice(0, 3)) as screenshot}
												<div class="ml-12">🖼️ {screenshot}</div>
											{/each}
											{#if preview.screenshots.length > 3 && !showAllPreviewScreenshots}
												<button
													onclick={() => (showAllPreviewScreenshots = true)}
													class="ml-12 text-muted-foreground hover:text-primary transition-colors cursor-pointer text-left"
												>
													... and {preview.screenshots.length - 3} more
												</button>
											{/if}
											{#if showAllPreviewScreenshots && preview.screenshots.length > 3}
												<button
													onclick={() => (showAllPreviewScreenshots = false)}
													class="ml-12 text-muted-foreground hover:text-primary transition-colors cursor-pointer text-left"
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
								<Button variant="outline" size="sm" onclick={resetCurrentMovie}>
									{#snippet children()}
										<RotateCcw class="h-4 w-4 mr-2" />
										Reset to Original
									{/snippet}
								</Button>
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

