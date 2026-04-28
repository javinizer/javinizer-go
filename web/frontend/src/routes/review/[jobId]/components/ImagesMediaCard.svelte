<script lang="ts">
	import { quintOut } from 'svelte/easing';
	import { slide } from 'svelte/transition';
	import type { FileResult, Movie } from '$lib/api/types';
	import Card from '$lib/components/ui/Card.svelte';
	import ScreenshotManager from '$lib/components/ScreenshotManager.svelte';
	import { ChevronDown, ChevronUp } from 'lucide-svelte';

	interface Props {
		showScreenshotsPanel: boolean;
		showImagePanelContent: boolean;
		currentMovie: Movie;
		currentResult: FileResult;
		displayPosterUrl?: string;
		showFieldScraperSources: boolean;
		onUpdateCurrentMovie: (movie: Movie) => void;
		onUseScreenshotAsPoster?: (url: string) => void;
	}

	let {
		showScreenshotsPanel,
		showImagePanelContent = $bindable(true),
		currentMovie,
		currentResult,
		displayPosterUrl,
		showFieldScraperSources,
		onUpdateCurrentMovie,
		onUseScreenshotAsPoster
	}: Props = $props();
</script>

{#if showScreenshotsPanel}
	<Card class="p-6">
		<div class="space-y-4">
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

			{#if showImagePanelContent}
				<div transition:slide|local={{ duration: 200, easing: quintOut }}>
					<ScreenshotManager
						movie={currentMovie}
						displayPosterUrl={displayPosterUrl}
						onUpdate={onUpdateCurrentMovie}
						onUseScreenshotAsPoster={onUseScreenshotAsPoster}
						fieldSources={currentResult.field_sources}
						showFieldSources={showFieldScraperSources}
					/>
				</div>
			{/if}
		</div>
	</Card>
{/if}
