<script lang="ts">
	import { flip } from 'svelte/animate';
	import { quintOut } from 'svelte/easing';
	import type { Movie } from '$lib/api/types';
	import { apiClient } from '$lib/api/client';
	import Button from './ui/Button.svelte';
	import Card from './ui/Card.svelte';
	import ImageViewer from './ImageViewer.svelte';
	import VideoModal from './VideoModal.svelte';
	import { Plus, Trash2, Image as ImageIcon, Play, RotateCcw } from 'lucide-svelte';

	interface Props {
		movie: Movie;
		displayPosterUrl?: string;
		onUpdate: (movie: Movie) => void;
	}

	let { movie, displayPosterUrl, onUpdate }: Props = $props();

	let screenshots = $state<string[]>([]);
	let posterUrl = $state('');
	let coverUrl = $state('');
	let trailerUrl = $state('');
	let newScreenshotUrl = $state('');

	// Screenshot viewer modal state
	let showViewer = $state(false);
	let viewerIndex = $state(0);

	// Cover viewer modal state
	let showCoverViewer = $state(false);

	// Trailer modal state
	let showTrailerModal = $state(false);

	// Sync state when movie prop changes
	$effect(() => {
		screenshots = movie.screenshot_urls || [];
		posterUrl = movie.poster_url || '';
		coverUrl = movie.cover_url || '';
		trailerUrl = movie.trailer_url || '';
	});

	// Update parent when data changes
	$effect(() => {
		onUpdate({
			...movie,
			screenshot_urls: screenshots,
			poster_url: posterUrl,
			cover_url: coverUrl,
			trailer_url: trailerUrl
		});
	});

	function addScreenshot() {
		if (newScreenshotUrl.trim()) {
			screenshots = [...screenshots, newScreenshotUrl.trim()];
			newScreenshotUrl = '';
		}
	}

	function removeScreenshot(index: number) {
		screenshots = screenshots.filter((_, i) => i !== index);
	}

	function handleKeyPress(e: KeyboardEvent) {
		if (e.key === 'Enter') {
			addScreenshot();
		}
	}

	// Screenshot viewer functions
	function openViewer(index: number) {
		viewerIndex = index;
		showViewer = true;
	}

	function closeViewer() {
		showViewer = false;
	}

	function previewImageURL(url: string): string {
		if (!url) return '';
		if (url.startsWith('/api/v1/')) return url;
		if (url.startsWith('/')) return url;
		return apiClient.getPreviewImageURL(url);
	}
</script>

<div class="space-y-6">
	<!-- Poster Image -->
	<div>
		<h3 class="text-lg font-semibold mb-3">Poster Image</h3>
		<div class="space-y-3">
			<div>
				<label for="poster-url" class="text-sm font-medium mb-1 block">Poster URL</label>
				<input
					id="poster-url"
					type="url"
					bind:value={posterUrl}
					placeholder="https://..."
					class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono text-sm"
				/>
			</div>
			<div>
				<div class="text-sm font-medium mb-1 block">
					Preview{movie.should_crop_poster ? ' (Cropped)' : ''}
				</div>
				{#if displayPosterUrl || posterUrl}
					<div class="w-full max-w-xs aspect-2/3 overflow-hidden rounded border relative">
						{#if movie.should_crop_poster && !displayPosterUrl}
							<!-- Crop to show only right 47.2% of image (removes promotional text on left) -->
							<!-- Only apply cropping if displayPosterUrl is not available (displayPosterUrl is already cropped if temp_poster_url) -->
							<img
								src={posterUrl}
								alt="Poster"
								class="absolute h-full"
								style="right: 0; width: auto; min-width: 211.8%; object-fit: cover; object-position: right center;"
								onerror={(e) => {
									const target = e.currentTarget as HTMLImageElement; target.style.display = 'none';
								}}
							/>
						{:else}
							<!-- Use displayPosterUrl (temp_poster_url if available) or posterUrl directly without cropping -->
							<img
								src={displayPosterUrl || posterUrl}
								alt="Poster"
								class="w-full h-full object-contain"
								onerror={(e) => {
									const target = e.currentTarget as HTMLImageElement; target.style.display = 'none';
								}}
							/>
						{/if}
					</div>
				{:else}
					<div
						class="w-full max-w-xs aspect-2/3 bg-accent rounded border flex items-center justify-center text-muted-foreground"
					>
						<div class="text-center">
							<ImageIcon class="h-12 w-12 mx-auto mb-2 opacity-50" />
							<p class="text-sm">No poster</p>
						</div>
					</div>
				{/if}
			</div>
		</div>
	</div>

	<!-- Cover/Fanart Image -->
	<div>
		<h3 class="text-lg font-semibold mb-3">Cover/Fanart Image</h3>
		<div class="space-y-3">
			<div>
				<label for="cover-url" class="text-sm font-medium mb-1 block">Cover URL</label>
				<input
					id="cover-url"
					type="url"
					bind:value={coverUrl}
					placeholder="https://..."
					class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono text-sm"
				/>
			</div>
			<div>
				<div class="text-sm font-medium mb-1 block">Preview</div>
				{#if coverUrl}
					<button
						onclick={() => (showCoverViewer = true)}
						class="w-full rounded border overflow-hidden hover:opacity-80 transition-opacity cursor-pointer"
					>
							<img
								src={previewImageURL(coverUrl)}
								alt="Cover"
								class="w-full"
							onerror={(e) => {
								const target = e.currentTarget as HTMLImageElement; target.style.display = 'none';
							}}
						/>
					</button>
				{:else}
					<div
						class="w-full h-48 bg-accent rounded border flex items-center justify-center text-muted-foreground"
					>
						<div class="text-center">
							<ImageIcon class="h-12 w-12 mx-auto mb-2 opacity-50" />
							<p class="text-sm">No cover image</p>
						</div>
					</div>
				{/if}
			</div>
		</div>
	</div>

	<!-- Trailer -->
	<div>
		<h3 class="text-lg font-semibold mb-3">Trailer</h3>
		<div class="space-y-3">
			<div>
				<label for="trailer-url" class="text-sm font-medium mb-1 block">Trailer URL</label>
				<input
					id="trailer-url"
					type="url"
					bind:value={trailerUrl}
					placeholder="https://..."
					class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono text-sm"
				/>
			</div>
			<div>
				<div class="text-sm font-medium mb-1 block">Preview</div>
				{#if trailerUrl}
					<button
						onclick={() => (showTrailerModal = true)}
						class="w-full h-48 bg-accent rounded border flex items-center justify-center text-primary hover:bg-accent/80 transition-colors cursor-pointer"
					>
						<div class="text-center">
							<Play class="h-12 w-12 mx-auto mb-2" />
							<p class="text-sm font-medium">Play Trailer</p>
						</div>
					</button>
				{:else}
					<div
						class="w-full h-48 bg-accent rounded border flex items-center justify-center text-muted-foreground"
					>
						<div class="text-center">
							<Play class="h-12 w-12 mx-auto mb-2 opacity-50" />
							<p class="text-sm">No trailer</p>
						</div>
					</div>
				{/if}
			</div>
		</div>
	</div>

	<!-- Screenshots -->
	<div>
		<div class="flex items-center justify-between mb-3">
			<h3 class="text-lg font-semibold">Screenshots ({screenshots.length})</h3>
		</div>

		<!-- Add Screenshot Form -->
		<div class="flex gap-2 mb-4">
			<input
				type="url"
				bind:value={newScreenshotUrl}
				onkeypress={handleKeyPress}
				placeholder="Enter screenshot URL and press Enter or click Add"
				class="flex-1 px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono text-sm"
			/>
			<Button onclick={addScreenshot} disabled={!newScreenshotUrl.trim()}>
				{#snippet children()}
					<Plus class="h-4 w-4 mr-2" />
					Add
				{/snippet}
			</Button>
		</div>

		<!-- Screenshots Grid -->
		{#if screenshots.length === 0}
			<div class="text-center py-8 text-muted-foreground border-2 border-dashed rounded-lg">
				<ImageIcon class="h-12 w-12 mx-auto mb-2 opacity-50" />
				<p>No screenshots added</p>
				<p class="text-xs mt-1">Add screenshot URLs above</p>
			</div>
		{:else}
			<div class="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
				{#each screenshots as url, index (`${url}-${index}`)}
					<div animate:flip={{ duration: 220, easing: quintOut }}>
						<Card class="p-2 group relative">
						<button
							onclick={() => openViewer(index)}
							class="w-full cursor-pointer hover:opacity-80 transition-opacity"
						>
							<img
								src={previewImageURL(url)}
								alt="Screenshot {index + 1}"
								class="w-full aspect-video object-cover rounded"
								onerror={(e) => {
									const target = e.currentTarget as HTMLImageElement; target.style.display = 'none';
								}}
							/>
						</button>
						<div class="mt-2 flex items-center justify-between gap-2">
							<p class="text-xs text-muted-foreground truncate flex-1" title={url}>
								Screenshot {index + 1}
							</p>
							<Button
								variant="ghost"
								size="sm"
								onclick={() => removeScreenshot(index)}
								class="text-destructive hover:bg-destructive/10 p-1 h-auto"
							>
								{#snippet children()}
									<Trash2 class="h-3 w-3" />
								{/snippet}
							</Button>
						</div>
						</Card>
					</div>
				{/each}
			</div>
		{/if}
	</div>
</div>

<!-- Screenshot Viewer Modal -->
<ImageViewer
	bind:show={showViewer}
	images={screenshots.map((url) => previewImageURL(url))}
	initialIndex={viewerIndex}
	onClose={closeViewer}
/>

<!-- Cover Viewer Modal -->
<ImageViewer
	bind:show={showCoverViewer}
	images={[previewImageURL(coverUrl)]}
	initialIndex={0}
	title="Cover/Fanart"
	onClose={() => (showCoverViewer = false)}
/>

<!-- Trailer Modal -->
<VideoModal
	bind:show={showTrailerModal}
	videoUrl={trailerUrl}
	title="Trailer"
	onClose={() => (showTrailerModal = false)}
/>
