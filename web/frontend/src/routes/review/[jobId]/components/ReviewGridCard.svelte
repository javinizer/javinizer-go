<script lang="ts">
	import type { FileResult, Movie, CompletenessConfig } from '$lib/api/types';
	import { CircleAlert, Image as ImageIcon } from 'lucide-svelte';
	import { calculateCompleteness } from '$lib/utils/completeness';
	import CompletenessDial from '$lib/components/CompletenessDial.svelte';
	import CompletenessBreakdownTooltip from './CompletenessBreakdownTooltip.svelte';

	interface MovieGroup {
		movieId: string;
		results: FileResult[];
		primaryResult: FileResult;
	}

	interface Props {
		movieGroup: MovieGroup;
		isSelected: boolean;
		isEdited: boolean;
		isBulkSelected: boolean;
		selectionMode: boolean;
		displayPosterUrl?: string;
		displayCoverUrl?: string;
		displayImageType?: 'poster' | 'cover';
		previewImageURL: (url: string | undefined) => string;
		onclick: (e: MouseEvent) => void;
		completenessConfig?: CompletenessConfig;
	}

	let {
		movieGroup,
		isSelected,
		isEdited,
		isBulkSelected,
		selectionMode,
		displayPosterUrl,
		displayCoverUrl,
		displayImageType = 'poster',
		previewImageURL,
		onclick,
		completenessConfig
	}: Props = $props();

	const movie = $derived(movieGroup.primaryResult.data as Movie | undefined);
	const imageSrc = $derived(
		displayImageType === 'cover'
			? (displayCoverUrl ? previewImageURL(displayCoverUrl) : undefined)
			: (displayPosterUrl ? previewImageURL(displayPosterUrl) : undefined)
	);

	const completeness = $derived(
		movie ? calculateCompleteness(movie, completenessConfig) : null
	);

	let dialHovered = $state(false);
	let showTooltip = $state(false);
	let hoverTimeout: ReturnType<typeof setTimeout> | null = null;

	let tooltipId = $derived(`completeness-tooltip-${movieGroup.movieId}`);

	function onDialMouseEnter() {
		hoverTimeout = setTimeout(() => { showTooltip = true; }, 175);
		dialHovered = true;
	}

	function onDialMouseLeave() {
		if (hoverTimeout) clearTimeout(hoverTimeout);
		showTooltip = false;
		dialHovered = false;
	}

	$effect(() => {
		return () => {
			if (hoverTimeout) clearTimeout(hoverTimeout);
		};
	});

	const PLACEHOLDER_SVG = "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='300' height='450' fill='%23374151'%3E%3Crect width='300' height='450'/%3E%3Ctext x='50%25' y='50%25' dominant-baseline='middle' text-anchor='middle' fill='%239CA3AF' font-family='system-ui' font-size='14'%3ENo Poster%3C/text%3E%3C/svg%3E";
	const COVER_PLACEHOLDER_SVG = "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='400' height='225' fill='%23374151'%3E%3Crect width='400' height='225'/%3E%3Ctext x='50%25' y='50%25' dominant-baseline='middle' text-anchor='middle' fill='%239CA3AF' font-family='system-ui' font-size='14'%3ENo Cover%3C/text%3E%3C/svg%3E";
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div
	class="group text-left rounded-lg border {selectionMode ? (isBulkSelected ? 'ring-2 ring-blue-500 border-blue-500/50' : 'border-border') : (isSelected ? 'ring-2 ring-primary' : 'border-border')} bg-card overflow-visible cursor-pointer transition-all duration-150 hover:scale-[1.02] hover:shadow-md focus-visible:outline-none focus-visible:ring-2 {selectionMode ? 'focus-visible:ring-blue-500' : 'focus-visible:ring-primary'}"
	role={selectionMode ? 'checkbox' : 'button'}
	tabindex="0"
	aria-label="{selectionMode ? 'Select' : 'View details for'} {movieGroup.movieId}"
	aria-checked={selectionMode ? isBulkSelected : undefined}
	onclick={(e: MouseEvent) => onclick(e)}
	onkeydown={(e: KeyboardEvent) => {
		if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			onclick(new MouseEvent('click', { shiftKey: e.shiftKey }));
		}
	}}
>
	<div class="relative w-full {displayImageType === 'cover' ? 'aspect-video' : 'aspect-2/3'} bg-muted">
		{#if imageSrc}
			<img
				src={imageSrc}
				alt={movie?.display_title || movieGroup.movieId}
				class="w-full h-full object-cover"
				onerror={(e) => {
					(e.currentTarget as HTMLImageElement).src = displayImageType === 'cover' ? COVER_PLACEHOLDER_SVG : PLACEHOLDER_SVG;
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
			<span class="absolute top-9 left-2 text-orange-600 dark:text-orange-400 bg-orange-100 dark:bg-orange-900/40 text-xs font-medium px-1.5 py-0.5 rounded-full flex items-center gap-1">
				<CircleAlert class="h-3 w-3" />
				Modified
			</span>
		{/if}

		{#if completeness}
			<!-- svelte-ignore a11y_no_static_element_interactions -->
			<div
				class="absolute bottom-2 right-2 z-10"
				onmouseenter={onDialMouseEnter}
				onmouseleave={onDialMouseLeave}
			>
				<CompletenessBreakdownTooltip
					breakdown={completeness.breakdown}
					visible={showTooltip}
					id={tooltipId}
				/>
				<CompletenessDial score={completeness.score} tier={completeness.tier} aria-describedby={tooltipId} />
			</div>
		{/if}
	</div>

	<div class="{displayImageType === 'cover' ? 'px-2 py-1.5' : 'p-3'} space-y-1">
		<p class="font-semibold text-sm truncate">
			{movie?.display_title || movieGroup.movieId}
		</p>
		{#if movie?.maker}
			<p class="text-muted-foreground text-xs truncate">{movie.maker}</p>
		{/if}
	</div>
</div>
