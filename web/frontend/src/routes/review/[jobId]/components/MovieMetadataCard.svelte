<script lang="ts">
	import type { FileResult, Movie } from '$lib/api/types';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import MovieEditor from '$lib/components/MovieEditor.svelte';
	import { AlertTriangle, LoaderCircle, RotateCcw } from 'lucide-svelte';

	interface Props {
		currentMovie: Movie;
		currentResult: FileResult;
		showFieldScraperSources: boolean;
		isRescraping: boolean;
		onOpenRescrape: () => void;
		onResetCurrentMovie: () => void;
		onUpdateCurrentMovie: (movie: Movie) => void;
	}

	let {
		currentMovie,
		currentResult,
		showFieldScraperSources = $bindable(false),
		isRescraping,
		onOpenRescrape,
		onResetCurrentMovie,
		onUpdateCurrentMovie
	}: Props = $props();
</script>

<Card class="p-6">
	<div class="space-y-4">
		<div class="flex items-center justify-between">
			<h2 class="text-xl font-semibold">Movie Metadata</h2>
			<div class="flex items-center gap-3">
				<label class="inline-flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
					<input
						type="checkbox"
						bind:checked={showFieldScraperSources}
						class="w-3.5 h-3.5 text-primary bg-muted border-input rounded focus:ring-primary focus:ring-2"
					/>
					Show scraper per field
				</label>
				<div class="flex gap-2">
					<Button variant="outline" size="sm" onclick={onOpenRescrape} disabled={isRescraping}>
						{#snippet children()}
							{#if isRescraping}
								<LoaderCircle class="h-4 w-4 mr-2 animate-spin" />
								Rescraping...
							{:else}
								<RotateCcw class="h-4 w-4 mr-2" />
								Rescrape
							{/if}
						{/snippet}
					</Button>
					<Button variant="outline" size="sm" onclick={onResetCurrentMovie}>
						{#snippet children()}
							<RotateCcw class="h-4 w-4 mr-2" />
							Reset to Original
						{/snippet}
					</Button>
				</div>
			</div>
		</div>

		{#if currentResult.translation_warning}
			<div class="flex items-start gap-2 rounded-md border border-yellow-500/30 bg-yellow-500/10 px-3 py-2 text-sm text-yellow-600 dark:text-yellow-400">
				<AlertTriangle class="h-4 w-4 mt-0.5 shrink-0" />
				<span>{currentResult.translation_warning}</span>
			</div>
		{/if}

		<MovieEditor
			movie={currentMovie}
			originalMovie={currentResult.data!}
			onUpdate={onUpdateCurrentMovie}
			fieldSources={currentResult.field_sources}
			showFieldSources={showFieldScraperSources}
		/>
	</div>
</Card>
