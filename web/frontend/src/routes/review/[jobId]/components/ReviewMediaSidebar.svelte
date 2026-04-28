<script lang="ts">
	import type { Movie } from '$lib/api/types';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import { Image as ImageIcon, ImagePlus, Play } from 'lucide-svelte';

	interface Props {
		currentMovie: Movie;
		displayPosterUrl?: string;
		showPosterPanel: boolean;
		showCoverPanel: boolean;
		showTrailerPanel: boolean;
		showScreenshotsPanel: boolean;
		showAllSidebarScreenshots: boolean;
		showTrailerModal: boolean;
		onOpenPosterCropModal: () => void;
		onOpenCoverViewer: () => void;
		onOpenScreenshotViewer: (index: number) => void;
		onUseScreenshotAsPoster: (url: string) => void;
		previewImageURL: (url: string | undefined) => string;
	}

	let {
		currentMovie,
		displayPosterUrl,
		showPosterPanel,
		showCoverPanel,
		showTrailerPanel,
		showScreenshotsPanel,
		showAllSidebarScreenshots = $bindable(false),
		showTrailerModal = $bindable(false),
		onOpenPosterCropModal,
		onOpenCoverViewer,
		onOpenScreenshotViewer,
		onUseScreenshotAsPoster,
		previewImageURL
	}: Props = $props();
</script>

<div class="space-y-4 lg:sticky lg:top-6 lg:self-start lg:max-h-[calc(100vh-8rem)] lg:overflow-y-auto">
	{#if showPosterPanel}
		<Card class="p-4">
			<div class="flex items-center justify-between gap-2 mb-3">
				<h3 class="font-semibold text-sm">Poster{currentMovie.should_crop_poster ? ' (Cropped)' : ''}</h3>
				<Button
					size="sm"
					variant="outline"
					onclick={onOpenPosterCropModal}
					disabled={!currentMovie.id}
					class="text-xs"
				>
					{#snippet children()}Adjust Crop{/snippet}
				</Button>
			</div>
			{#if displayPosterUrl}
				<div class="w-full aspect-2/3 overflow-hidden rounded border relative">
					{#if currentMovie.should_crop_poster && !currentMovie.cropped_poster_url}
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

	{#if showCoverPanel}
		<Card class="p-4">
			<h3 class="font-semibold mb-3 text-sm">Cover/Fanart</h3>
			{#if currentMovie.cover_url}
				<button onclick={onOpenCoverViewer} class="cursor-pointer hover:opacity-80 transition-opacity w-full">
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

	{#if showScreenshotsPanel && currentMovie.screenshot_urls && currentMovie.screenshot_urls.length > 0}
		<Card class="p-4">
			<h3 class="font-semibold mb-3 text-sm">Screenshots ({currentMovie.screenshot_urls.length})</h3>
			<div class="grid grid-cols-2 gap-2">
				{#each (showAllSidebarScreenshots ? currentMovie.screenshot_urls : currentMovie.screenshot_urls.slice(0, 4)) as url, index}
					<div class="relative group">
						<button onclick={() => onOpenScreenshotViewer(index)} class="cursor-pointer hover:opacity-80 transition-opacity">
							<img
								src={previewImageURL(url)}
								alt="Screenshot"
								class="w-full aspect-video object-cover rounded border"
								onerror={(e) => {
									(e.currentTarget as HTMLImageElement).src = 'https://via.placeholder.com/400x225?text=Error';
								}}
							/>
						</button>
						<button
							onclick={(e: MouseEvent) => { e.stopPropagation(); onUseScreenshotAsPoster(url); }}
							title="Use as Poster"
							class="absolute bottom-1 right-1 p-1 rounded-full bg-black/60 hover:bg-black/80 text-white opacity-0 group-hover:opacity-100 transition-opacity cursor-pointer"
						>
							<ImagePlus class="h-3 w-3" />
						</button>
					</div>
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
