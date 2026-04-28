<script lang="ts">
	import type { FileResult, Movie } from '$lib/api/types';
	import { CircleAlert, Image as ImageIcon } from 'lucide-svelte';

	interface MovieGroup {
		movieId: string;
		results: FileResult[];
		primaryResult: FileResult;
	}

	interface Props {
		movieGroup: MovieGroup;
		isSelected: boolean;
		isEdited: boolean;
		displayPosterUrl?: string;
		previewImageURL: (url: string | undefined) => string;
		onclick: () => void;
	}

	let {
		movieGroup,
		isSelected,
		isEdited,
		displayPosterUrl,
		previewImageURL,
		onclick
	}: Props = $props();

	const movie = $derived(movieGroup.primaryResult.data as Movie | undefined);
	const posterSrc = $derived(
		displayPosterUrl ? previewImageURL(displayPosterUrl) : undefined
	);

	const PLACEHOLDER_SVG = "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='300' height='450' fill='%23374151'%3E%3Crect width='300' height='450'/%3E%3Ctext x='50%25' y='50%25' dominant-baseline='middle' text-anchor='middle' fill='%239CA3AF' font-family='system-ui' font-size='14'%3ENo Poster%3C/text%3E%3C/svg%3E";
</script>

<button
	class="group text-left rounded-lg border {isSelected ? 'ring-2 ring-primary' : 'border-border'} bg-card overflow-hidden cursor-pointer transition-all duration-150 hover:scale-[1.02] hover:shadow-md"
	onclick={onclick}
>
	<div class="relative w-full aspect-2/3 overflow-hidden bg-muted">
		{#if posterSrc}
			<img
				src={posterSrc}
				alt={movie?.display_title || movieGroup.movieId}
				class="w-full h-full object-cover"
				onerror={(e) => {
					(e.currentTarget as HTMLImageElement).src = PLACEHOLDER_SVG;
				}}
			/>
		{:else}
			<div class="w-full h-full flex items-center justify-center text-muted-foreground">
				<ImageIcon class="h-8 w-8" />
			</div>
		{/if}

		<span class="absolute top-2 right-2 bg-black/70 text-white text-xs font-medium px-2 py-0.5 rounded-full">
			{movieGroup.movieId}
		</span>

		{#if isEdited}
			<span class="absolute top-2 left-2 text-orange-600 bg-orange-100 dark:bg-orange-900/40 text-xs font-medium px-1.5 py-0.5 rounded-full flex items-center gap-1">
				<CircleAlert class="h-3 w-3" />
				Modified
			</span>
		{/if}
	</div>

	<div class="p-3 space-y-1">
		<p class="font-semibold text-sm truncate">
			{movie?.display_title || movieGroup.movieId}
		</p>
		{#if movie?.maker}
			<p class="text-muted-foreground text-xs truncate">{movie.maker}</p>
		{/if}
	</div>
</button>
