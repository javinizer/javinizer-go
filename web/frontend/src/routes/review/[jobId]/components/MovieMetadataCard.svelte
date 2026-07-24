<script lang="ts">
	import type { FileResult, Movie, FieldDifference } from '$lib/api/types';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import MovieEditor from '$lib/components/MovieEditor.svelte';
	import NfoDiffSummary from './NfoDiffSummary.svelte';
	import { createFavoriteGenresQuery } from '$lib/query/queries';
	import { LoaderCircle, RotateCcw, TableProperties } from 'lucide-svelte';
	import * as m from '$lib/paraglide/messages';

	interface Props {
		currentMovie: Movie;
		currentResult: FileResult;
		showFieldScraperSources: boolean;
		isRescraping: boolean;
		jobId?: string;
		onOpenRescrape: () => void;
		onOpenSourceViewer: () => void;
		onResetCurrentMovie: () => void;
		onUpdateCurrentMovie: (movie: Movie) => void;
		nfoDifferences?: FieldDifference[];
	}

	let {
		currentMovie,
		currentResult,
		showFieldScraperSources = $bindable(false),
		isRescraping,
		jobId,
		onOpenRescrape,
		onOpenSourceViewer,
		onResetCurrentMovie,
		onUpdateCurrentMovie,
		nfoDifferences
	}: Props = $props();

	const favoritesQuery = createFavoriteGenresQuery();
	let favoriteGenres = $derived<string[]>(
		favoritesQuery.isError ? [] : (favoritesQuery.data?.favorites ?? [])
	);
</script>

<Card class="p-6">
	<div class="space-y-4">
		<div class="flex items-center justify-between">
			<h2 class="text-xl font-semibold">{m.review_movie_metadata()}</h2>
			<div class="flex items-center gap-3">
				<label class="inline-flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
					<input
						type="checkbox"
						bind:checked={showFieldScraperSources}
						class="w-3.5 h-3.5 text-primary bg-muted border-input rounded focus:ring-primary focus:ring-2"
					/>
					{m.review_show_scraper_per_field()}
				</label>
				<div class="flex gap-2">
					<Button variant="outline" size="sm" onclick={onOpenSourceViewer}>
						{#snippet children()}
							<TableProperties class="h-4 w-4 mr-2" />
							{m.review_sources()}
						{/snippet}
					</Button>
					<Button variant="outline" size="sm" onclick={onOpenRescrape} disabled={isRescraping}>
						{#snippet children()}
							{#if isRescraping}
								<LoaderCircle class="h-4 w-4 mr-2 animate-spin" />
								{m.review_rescraping()}
							{:else}
								<RotateCcw class="h-4 w-4 mr-2" />
								{m.review_rescrape()}
							{/if}
						{/snippet}
					</Button>
					<Button variant="outline" size="sm" onclick={onResetCurrentMovie}>
						{#snippet children()}
							<RotateCcw class="h-4 w-4 mr-2" />
							{m.review_reset_to_original()}
						{/snippet}
					</Button>
				</div>
			</div>
		</div>

		<MovieEditor
			movie={currentMovie}
			originalMovie={currentResult.movie!}
			onUpdate={onUpdateCurrentMovie}
			fieldSources={currentResult.field_sources}
			showFieldSources={showFieldScraperSources}
			jobId={jobId}
			resultId={currentResult.result_id}
			nfoDifferences={nfoDifferences}
			favoriteGenres={favoriteGenres}
		/>

		{#if nfoDifferences && nfoDifferences.length > 0}
			<NfoDiffSummary nfoDifferences={nfoDifferences} />
		{/if}
	</div>
</Card>
