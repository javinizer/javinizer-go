<script lang="ts">
	import type { FileResult, Movie, FieldDifference } from '$lib/api/types';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import MovieEditor from '$lib/components/MovieEditor.svelte';
	import NfoDiffSummary from './NfoDiffSummary.svelte';
	import TranslationWarningBadge from '$lib/components/TranslationWarningBadge.svelte';
	import { createFavoriteGenresQuery } from '$lib/query/queries';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { apiClient } from '$lib/api/client';
	import { toastStore } from '$lib/stores/toast';
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
	const favoritedGenreNames = $derived.by(() => {
		const set = new Set<string>();
		for (const g of favoriteGenres) set.add(g.trim().toLowerCase());
		return set;
	});

	const queryClient = useQueryClient();
	const addFavoriteMutation = createMutation(() => ({
		mutationFn: (genre: string) => apiClient.addFavoriteGenre({ genre }),
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: ['genre-favorites'] });
			void queryClient.invalidateQueries({ queryKey: ['config'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message || m.genres_add_favorite_failed(), 4000);
		},
	}));
	const removeFavoriteMutation = createMutation(() => ({
		mutationFn: (genre: string) => apiClient.deleteFavoriteGenre(genre),
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: ['genre-favorites'] });
			void queryClient.invalidateQueries({ queryKey: ['config'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message || m.genres_remove_favorite_failed(), 4000);
		},
	}));

	function handleAddFavorite(genre: string) {
		addFavoriteMutation.mutate(genre);
	}
	function handleRemoveFavorite(genre: string) {
		removeFavoriteMutation.mutate(genre);
	}
	let favoriteMutationPending = $derived(addFavoriteMutation.isPending || removeFavoriteMutation.isPending);
</script>

<Card class="p-4 lg:p-5" data-testid="movie-metadata-card">
	<div class="space-y-3">
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

		<TranslationWarningBadge
			code={currentResult.translation_warning_code}
			warning={currentResult.translation_warning}
		/>

		<MovieEditor
			movie={currentMovie}
			originalMovie={currentResult.movie!}
			compact={true}
			onUpdate={onUpdateCurrentMovie}
			fieldSources={currentResult.field_sources}
			showFieldSources={showFieldScraperSources}
			jobId={jobId}
			resultId={currentResult.result_id}
			nfoDifferences={nfoDifferences}
			favoriteGenres={favoriteGenres}
			favoritedGenreNames={favoritedGenreNames}
			onAddFavorite={handleAddFavorite}
			onRemoveFavorite={handleRemoveFavorite}
			favoriteMutationPending={favoriteMutationPending}
		/>

		{#if nfoDifferences && nfoDifferences.length > 0}
			<NfoDiffSummary nfoDifferences={nfoDifferences} />
		{/if}
	</div>
</Card>
