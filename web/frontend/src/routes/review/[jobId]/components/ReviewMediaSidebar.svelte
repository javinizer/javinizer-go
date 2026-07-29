<script lang="ts">
	import type { Movie } from '$lib/api/types';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import { Image as ImageIcon, Play, Settings2 } from 'lucide-svelte';
	import * as m from '$lib/paraglide/messages';

	interface Props {
		currentMovie: Movie;
		displayPosterUrl?: string;
		showPosterPanel: boolean;
		showCoverPanel: boolean;
		showTrailerPanel: boolean;
		showScreenshotsPanel: boolean;
		onOpenImageManager: () => void;
		onOpenTrailerModal: () => void;
		onOpenPosterViewer: () => void;
		onOpenCoverViewer: () => void;
		onOpenScreenshotViewer: (index: number) => void;
		previewImageURL: (url: string | undefined) => string;
	}

	let {
		currentMovie,
		displayPosterUrl,
		showPosterPanel,
		showCoverPanel,
		showTrailerPanel,
		showScreenshotsPanel,
		onOpenImageManager,
		onOpenTrailerModal,
		onOpenPosterViewer,
		onOpenCoverViewer,
		onOpenScreenshotViewer,
		previewImageURL
	}: Props = $props();

	let showAllScreenshots = $state(false);

	const PLACEHOLDER_SVG = "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='300' height='450' fill='%23374151'%3E%3Crect width='300' height='450'/%3E%3Ctext x='50%25' y='50%25' dominant-baseline='middle' text-anchor='middle' fill='%239CA3AF' font-family='system-ui' font-size='14'%3ENo Poster%3C/text%3E%3C/svg%3E";
	const COVER_PLACEHOLDER_SVG = "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='400' height='225' fill='%23374151'%3E%3Crect width='400' height='225'/%3E%3Ctext x='50%25' y='50%25' dominant-baseline='middle' text-anchor='middle' fill='%239CA3AF' font-family='system-ui' font-size='14'%3ENo Cover%3C/text%3E%3C/svg%3E";
</script>

<div class="space-y-4 lg:sticky lg:top-6 lg:self-start lg:max-h-[calc(100vh-8rem)] lg:overflow-y-auto">
	<Card class="p-4">
		<div class="flex items-center justify-between gap-2 mb-3">
			<h3 class="font-semibold text-sm">{currentMovie.id}</h3>
			<Button size="sm" variant="ghost" onclick={onOpenImageManager} class="h-7 text-xs gap-1 shrink-0">
				{#snippet children()}<Settings2 class="h-3.5 w-3.5" />{m.review_manage_images()}{/snippet}
			</Button>
		</div>

		{#if showPosterPanel || showCoverPanel || showTrailerPanel || showScreenshotsPanel}
			{#if showPosterPanel}
				<div class="mb-4">
					<p class="text-xs font-medium text-muted-foreground mb-1.5">{currentMovie.should_crop_poster ? m.review_poster_cropped() : m.review_poster()}</p>
					{#if displayPosterUrl}
						<button onclick={onOpenPosterViewer} class="cursor-zoom-in hover:opacity-80 transition-opacity w-full block" title={m.review_view_poster_image()} aria-label={m.review_view_poster_image()}>
						<div class="w-full max-w-60 mx-auto aspect-2/3 overflow-hidden rounded border relative">
							{#if currentMovie.should_crop_poster && !currentMovie.cropped_poster_url}
								<img
									src={displayPosterUrl}
									alt={m.review_poster_alt()}
									class="absolute h-full"
									style="right: 0; width: auto; min-width: 211.8%; object-fit: cover; object-position: right center;"
									onerror={(e) => { (e.currentTarget as HTMLImageElement).src = PLACEHOLDER_SVG; }}
								/>
							{:else}
								<img
									src={displayPosterUrl}
									alt={m.review_poster_alt()}
									class="w-full h-full object-contain"
									onerror={(e) => { (e.currentTarget as HTMLImageElement).src = PLACEHOLDER_SVG; }}
								/>
							{/if}
						</div>
						</button>
					{:else}
						<div class="w-full max-w-60 mx-auto aspect-2/3 bg-accent rounded border flex items-center justify-center text-muted-foreground">
							<div class="text-center text-xs"><ImageIcon class="h-8 w-8 mx-auto mb-1 opacity-50" />{m.review_no_poster()}</div>
						</div>
					{/if}
				</div>
			{/if}

			{#if showCoverPanel}
				<div class="mb-4">
					<p class="text-xs font-medium text-muted-foreground mb-1.5">{m.review_cover_fanart()}</p>
					{#if currentMovie.cover_url}
						<button onclick={onOpenCoverViewer} class="cursor-zoom-in hover:opacity-80 transition-opacity w-full block" title={m.review_view_cover_image()} aria-label={m.review_view_cover_image()}>
							<img
								src={previewImageURL(currentMovie.cover_url)}
								alt={m.review_cover_alt()}
								class="w-full rounded border"
								onerror={(e) => { (e.currentTarget as HTMLImageElement).src = COVER_PLACEHOLDER_SVG; }}
							/>
						</button>
					{:else}
						<div class="w-full aspect-video bg-accent rounded border flex items-center justify-center text-muted-foreground">
							<div class="text-center text-xs"><ImageIcon class="h-8 w-8 mx-auto mb-1 opacity-50" />{m.review_no_cover_image()}</div>
						</div>
					{/if}
				</div>
			{/if}

			{#if showTrailerPanel && currentMovie.trailer_url}
				<div class="mb-4">
					<Button class="w-full" size="sm" onclick={onOpenTrailerModal}>
						{#snippet children()}<Play class="h-4 w-4 mr-2" />{m.review_play_trailer()}{/snippet}
					</Button>
				</div>
			{/if}

			{#if showScreenshotsPanel && currentMovie.screenshot_urls && currentMovie.screenshot_urls.length > 0}
				<div>
					<p class="text-xs font-medium text-muted-foreground mb-2">{m.review_screenshots_count({ count: currentMovie.screenshot_urls.length })}</p>
					<div class="grid grid-cols-2 gap-2">
						{#each (showAllScreenshots ? currentMovie.screenshot_urls : currentMovie.screenshot_urls.slice(0, 4)) as url, index}
							<button onclick={() => onOpenScreenshotViewer(index)} class="cursor-zoom-in hover:opacity-80 transition-opacity" title={m.review_screenshot_alt()}>
								<img
									src={previewImageURL(url)}
									alt={m.review_screenshot_alt()}
									class="w-full aspect-video object-cover rounded border"
									onerror={(e) => { (e.currentTarget as HTMLImageElement).src = COVER_PLACEHOLDER_SVG; }}
								/>
							</button>
						{/each}
					</div>
					{#if currentMovie.screenshot_urls.length > 4 && !showAllScreenshots}
						<button
							onclick={() => (showAllScreenshots = true)}
							class="w-full text-xs text-primary hover:text-primary/80 hover:bg-accent mt-2 py-1.5 rounded transition-all cursor-pointer"
						>
							{m.review_more_screenshots({ count: currentMovie.screenshot_urls.length - 4 })}
						</button>
					{/if}
					{#if showAllScreenshots && currentMovie.screenshot_urls.length > 4}
						<button
							onclick={() => (showAllScreenshots = false)}
							class="w-full text-xs text-muted-foreground hover:text-primary hover:bg-accent mt-2 py-1.5 rounded transition-all cursor-pointer"
						>
							{m.review_show_less()}
						</button>
					{/if}
				</div>
			{/if}
		{/if}
	</Card>
</div>