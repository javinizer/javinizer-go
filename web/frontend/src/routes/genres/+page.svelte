<script lang="ts">
	import { cubicOut } from 'svelte/easing';
	import { fade, fly } from 'svelte/transition';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { apiClient } from '$lib/api/client';
	import type { GenreReplacement, GenreReplacementUpdateRequest, ImportResponse } from '$lib/api/types';
	import { toastStore } from '$lib/stores/toast';
	import { Trash2, Plus, Loader2, Search, X, Check, Pencil, ArrowDownUp, ChevronsDownUp, ArrowLeft, Tags, Download, Upload, Ban, Star } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import { createGenreReplacementsQuery, createIgnoredGenresQuery, createFavoriteGenresQuery } from '$lib/query/queries';

	const queryClient = useQueryClient();

	const replacementsQuery = createGenreReplacementsQuery();
	let replacements = $derived<GenreReplacement[]>(replacementsQuery.data?.replacements ?? []);
	let loading = $derived(replacementsQuery.isPending);
	let error = $derived<string | null>(replacementsQuery.error?.message ?? null);

	let newOriginal = $state('');
	let newReplacement = $state('');
	let searchQuery = $state('');
	let sortDirection = $state<'asc' | 'desc'>('asc');
	let importFile = $state<HTMLInputElement | null>(null);

	let filteredAndSorted = $derived.by(() => {
		let result = replacements;
		if (searchQuery.trim()) {
			const q = searchQuery.trim().toLowerCase();
			result = result.filter(
				r => r.original.toLowerCase().includes(q) || r.replacement.toLowerCase().includes(q)
			);
		}
		result = [...result].sort((a, b) => {
			return sortDirection === 'asc'
				? a.original.localeCompare(b.original)
				: b.original.localeCompare(a.original);
		});
		return result;
	});

	let editingId = $state<number | null>(null);
	let editOriginal = $state('');
	let editReplacement = $state('');

	const ignoredQuery = createIgnoredGenresQuery();
	let ignoredGenres = $derived<string[]>(ignoredQuery.data?.ignored_genres ?? []);
	let ignoredLoading = $derived(ignoredQuery.isPending);
	let ignoredError = $derived<string | null>(ignoredQuery.error?.message ?? null);
	let newIgnored = $state('');

	const favoritesQuery = createFavoriteGenresQuery();
	let favoriteGenres = $derived<string[]>(favoritesQuery.data?.favorites ?? []);
	let favoritesLoading = $derived(favoritesQuery.isPending);
	let favoritesError = $derived<string | null>(favoritesQuery.error?.message ?? null);
	let newFavorite = $state('');
	let bulkFavorites = $state('');

	const addMutation = createMutation(() => ({
		mutationFn: ({ original, replacement }: { original: string; replacement: string }) =>
			apiClient.createGenreReplacement({ original, replacement }),
		onSuccess: (_data, { original, replacement }) => {
			newOriginal = '';
			newReplacement = '';
			toastStore.success(`Genre replacement "${original}" → "${replacement}" added`, 3000);
			void queryClient.invalidateQueries({ queryKey: ['genre-replacements'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message || 'Failed to add genre replacement', 4000);
		}
	}));

	const updateMutation = createMutation(() => ({
		mutationFn: (req: GenreReplacementUpdateRequest) => apiClient.updateGenreReplacement(req),
		onSuccess: (_data, { original, replacement }) => {
			editingId = null;
			toastStore.success(`Genre replacement updated: "${original}" → "${replacement}"`, 3000);
			void queryClient.invalidateQueries({ queryKey: ['genre-replacements'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message || 'Failed to update genre replacement', 4000);
		}
	}));

		const deleteMutation = createMutation(() => ({
		mutationFn: (id: number) => apiClient.deleteGenreReplacement(id),
		onSuccess: () => {
			toastStore.success('Genre replacement removed', 3000);
			void queryClient.invalidateQueries({ queryKey: ['genre-replacements'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message || 'Failed to delete genre replacement', 4000);
		}
	}));

	const exportMutation = createMutation(() => ({
		mutationFn: () => apiClient.exportGenreReplacements(),
		onSuccess: async (data) => {
			const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
			const url = URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			a.download = 'genre-replacements.json';
			document.body.appendChild(a);
			a.click();
			document.body.removeChild(a);
			URL.revokeObjectURL(url);
			toastStore.success(`Exported ${data.length} genre replacement(s)`, 3000);
		},
		onError: (err: Error) => {
			toastStore.error(err.message || 'Failed to export genre replacements', 4000);
		}
	}));

	const importMutation = createMutation(() => ({
		mutationFn: (payload: { replacements: { original: string; replacement: string }[] }) =>
			apiClient.importGenreReplacements(payload),
		onSuccess: (res: ImportResponse) => {
			toastStore.success(`Import complete — Imported: ${res.imported}, Skipped: ${res.skipped}, Errors: ${res.errors}`, 5000);
			void queryClient.invalidateQueries({ queryKey: ['genre-replacements'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message || 'Failed to import genre replacements', 4000);
		}
	}));

	const addIgnoredMutation = createMutation(() => ({
		mutationFn: (genre: string) => apiClient.addIgnoredGenre({ genre }),
		onSuccess: () => {
			newIgnored = '';
			void queryClient.invalidateQueries({ queryKey: ['genre-ignored'] });
			void queryClient.invalidateQueries({ queryKey: ['config'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message || 'Failed to ignore genre', 4000);
		}
	}));

	const deleteIgnoredMutation = createMutation(() => ({
		mutationFn: (genre: string) => apiClient.deleteIgnoredGenre(genre),
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: ['genre-ignored'] });
			void queryClient.invalidateQueries({ queryKey: ['config'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message || 'Failed to remove ignored genre', 4000);
		}
	}));

	const addFavoriteMutation = createMutation(() => ({
		mutationFn: (genre: string) => apiClient.addFavoriteGenre({ genre }),
		onSuccess: () => {
			newFavorite = '';
			void queryClient.invalidateQueries({ queryKey: ['genre-favorites'] });
			void queryClient.invalidateQueries({ queryKey: ['config'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message || 'Failed to add favorite', 4000);
		}
	}));

	const deleteFavoriteMutation = createMutation(() => ({
		mutationFn: (genre: string) => apiClient.deleteFavoriteGenre(genre),
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: ['genre-favorites'] });
			void queryClient.invalidateQueries({ queryKey: ['config'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message || 'Failed to remove favorite', 4000);
		}
	}));

	const replaceFavoritesMutation = createMutation(() => ({
		mutationFn: (genres: string[]) => apiClient.replaceFavoriteGenres({ genres }),
		onSuccess: (res) => {
			bulkFavorites = '';
			toastStore.success(`Saved ${res.count} favorite genre(s)`, 3000);
			void queryClient.invalidateQueries({ queryKey: ['genre-favorites'] });
			void queryClient.invalidateQueries({ queryKey: ['config'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message || 'Failed to save favorites', 4000);
		}
	}));

	function handleAdd() {
		const original = newOriginal.trim();
		const replacement = newReplacement.trim();
		if (!original || !replacement) {
			toastStore.error('Both original and replacement fields are required', 4000);
			return;
		}
		addMutation.mutate({ original, replacement });
	}

	function handleDelete(id: number) {
		deleteMutation.mutate(id);
	}

	function startEdit(rep: GenreReplacement) {
		editingId = rep.id;
		editOriginal = rep.original;
		editReplacement = rep.replacement;
	}

	function cancelEdit() {
		editingId = null;
		editOriginal = '';
		editReplacement = '';
	}

	function saveEdit(rep: GenreReplacement) {
		const r = editReplacement.trim();
		if (!r) {
			toastStore.error('Both fields are required', 4000);
			return;
		}
		updateMutation.mutate({ original: rep.original, replacement: r });
	}

	function toggleSort() {
		sortDirection = sortDirection === 'asc' ? 'desc' : 'asc';
	}

	function clearSearch() {
		searchQuery = '';
	}

	function handleAddKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter') {
			e.preventDefault();
			handleAdd();
		}
	}

	function handleEditKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter') {
			e.preventDefault();
			const rep = replacements.find(r => r.id === editingId);
			if (rep) saveEdit(rep);
		} else if (e.key === 'Escape') {
			cancelEdit();
		}
	}

	function handleAddIgnored() {
		const genre = newIgnored.trim();
		if (!genre) {
			toastStore.error('Enter a genre to ignore', 3000);
			return;
		}
		addIgnoredMutation.mutate(genre);
	}

	function handleAddFavorite() {
		const genre = newFavorite.trim();
		if (!genre) {
			toastStore.error('Enter a genre to favorite', 3000);
			return;
		}
		addFavoriteMutation.mutate(genre);
	}

	function parseBulkGenres(input: string): string[] {
		return input
			.split(/[\n,]+/)
			.map((s) => s.trim())
			.filter((s) => s.length > 0);
	}

	function handleBulkAddFavorites() {
		const added = parseBulkGenres(bulkFavorites);
		if (added.length === 0) {
			toastStore.error('Enter one or more genres to add', 3000);
			return;
		}
		const merged: string[] = [];
		for (const g of [...favoriteGenres, ...added]) {
			if (!merged.includes(g)) merged.push(g);
		}
		replaceFavoritesMutation.mutate(merged);
	}

	function handleAddIgnoredKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter') {
			e.preventDefault();
			handleAddIgnored();
		}
	}

	function handleAddFavoriteKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter') {
			e.preventDefault();
			handleAddFavorite();
		}
	}

	function handleExport() {
		exportMutation.mutate();
	}

	function handleImportClick() {
		importFile?.click();
	}

	async function handleImportChange(e: Event) {
		const target = e.target as HTMLInputElement;
		const file = target.files?.[0];
		if (!file) return;

		try {
			const text = await file.text();
			const parsed: GenreReplacement[] = JSON.parse(text);
			if (!Array.isArray(parsed)) throw new Error('Expected a JSON array');

			const replacements = parsed
				.filter(r => r.original && r.original.trim())
				.map(r => ({ original: r.original.trim(), replacement: (r.replacement || '').trim() }));

			if (replacements.length === 0) {
				toastStore.error('No valid replacements in file', 4000);
				return;
			}

			if (!confirm(`Import ${replacements.length} genre replacement(s)?`)) return;

			importMutation.mutate({ replacements });
		} catch (err) {
			toastStore.error(`Invalid JSON file: ${err instanceof Error ? err.message : String(err)}`, 4000);
		}

		target.value = '';
	}
</script>

<div class="container mx-auto px-4 py-8">
	<div class="max-w-7xl mx-auto space-y-6">
		<div
			class="flex flex-wrap items-center justify-between gap-3"
			in:fly|local={{ y: -10, duration: 240, easing: cubicOut }}
		>
			<div class="flex items-center gap-3">
				<a href="/settings">
					<Button variant="ghost" size="icon">
						{#snippet children()}
							<ArrowLeft class="h-5 w-5" />
						{/snippet}
					</Button>
				</a>
				<div>
					<div class="flex items-center gap-2">
						<Tags class="h-6 w-6 text-primary" />
						<h1 class="text-3xl font-bold">Genres</h1>
					</div>
					<p class="text-muted-foreground mt-1">
						Manage ignored genres, favorites, and genre name replacements
					</p>
				</div>
			</div>
			<div class="flex items-center gap-2">
				<input
					type="file"
					accept=".json"
					bind:this={importFile}
					onchange={handleImportChange}
					class="hidden"
				/>
				<Button
					variant="outline"
					size="sm"
					onclick={handleExport}
					disabled={exportMutation.isPending}
				>
					{#if exportMutation.isPending}
						<Loader2 class="h-4 w-4 animate-spin mr-1" />
					{:else}
						<Download class="h-4 w-4 mr-1" />
					{/if}
					Export
				</Button>
				<Button
					variant="outline"
					size="sm"
					onclick={handleImportClick}
					disabled={importMutation.isPending}
				>
					{#if importMutation.isPending}
						<Loader2 class="h-4 w-4 animate-spin mr-1" />
					{:else}
						<Upload class="h-4 w-4 mr-1" />
					{/if}
					Import
				</Button>
			</div>
		</div>

		<div in:fly|local={{ y: 8, duration: 180, delay: 40 }}>
			<Card class="p-5">
				<div class="flex items-center gap-2 mb-1">
					<Ban class="h-4 w-4 text-primary" />
					<h2 class="text-lg font-semibold">Ignored Genres</h2>
					<span class="text-xs text-muted-foreground">{ignoredGenres.length} excluded</span>
				</div>
				<p class="text-xs text-muted-foreground mb-3">
					Genres in this list are excluded from scraping/processing. Takes effect on the next scrape.
				</p>
				{#if ignoredError}
					<p class="text-sm text-destructive mb-2">Failed to load: {ignoredError}</p>
				{:else if ignoredLoading}
					<div class="flex items-center gap-2 text-sm text-muted-foreground">
						<Loader2 class="h-4 w-4 animate-spin" /> Loading...
					</div>
				{:else}
					<div class="flex flex-wrap gap-2 mb-3">
						{#if ignoredGenres.length === 0}
							<span class="text-sm text-muted-foreground">No ignored genres. Add one below.</span>
						{:else}
							{#each ignoredGenres as genre (genre)}
								<span class="inline-flex items-center gap-1 rounded-full border border-border bg-muted/40 py-1 pl-3 pr-1 text-sm">
									{genre}
									<button
										type="button"
										class="ml-0.5 rounded-full p-0.5 text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors"
										title="Stop ignoring"
										onclick={() => deleteIgnoredMutation.mutate(genre)}
										disabled={deleteIgnoredMutation.isPending}
									>
										<X class="h-3.5 w-3.5" />
									</button>
								</span>
							{/each}
						{/if}
					</div>
					<div class="flex items-center gap-2">
						<input
							type="text"
							bind:value={newIgnored}
							placeholder="Genre to ignore..."
							onkeydown={handleAddIgnoredKeydown}
							class="flex-1 rounded-md border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
						/>
						<Button
							type="button"
							size="sm"
							onclick={handleAddIgnored}
							disabled={addIgnoredMutation.isPending || !newIgnored.trim()}
						>
							{#if addIgnoredMutation.isPending}
								<Loader2 class="h-4 w-4 animate-spin mr-1" />
							{:else}
								<Plus class="h-4 w-4 mr-1" />
							{/if}
							Ignore
						</Button>
					</div>
				{/if}
			</Card>
		</div>

		<div in:fly|local={{ y: 8, duration: 180, delay: 80 }}>
			<Card class="p-5">
				<div class="flex items-center gap-2 mb-1">
					<Star class="h-4 w-4 text-primary" />
					<h2 class="text-lg font-semibold">Favorite Genres</h2>
					<span class="text-xs text-muted-foreground">{favoriteGenres.length} saved</span>
				</div>
				<p class="text-xs text-muted-foreground mb-3">
					Save a curated set of favorite genres here for later use. Favorites are a saved list you can reference when organizing your collection.
				</p>
				{#if favoritesError}
					<p class="text-sm text-destructive mb-2">Failed to load: {favoritesError}</p>
				{:else if favoritesLoading}
					<div class="flex items-center gap-2 text-sm text-muted-foreground">
						<Loader2 class="h-4 w-4 animate-spin" /> Loading...
					</div>
				{:else}
					<div class="flex flex-wrap gap-2 mb-3">
						{#if favoriteGenres.length === 0}
							<span class="text-sm text-muted-foreground">No favorites yet. Add one below or paste several at once.</span>
						{:else}
							{#each favoriteGenres as genre (genre)}
								<span class="inline-flex items-center gap-1 rounded-full border border-border bg-primary/10 py-1 pl-3 pr-1 text-sm">
									{genre}
									<button
										type="button"
										class="ml-0.5 rounded-full p-0.5 text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors"
										title="Remove favorite"
										onclick={() => deleteFavoriteMutation.mutate(genre)}
										disabled={deleteFavoriteMutation.isPending}
									>
										<X class="h-3.5 w-3.5" />
									</button>
								</span>
							{/each}
						{/if}
					</div>
					<div class="flex items-center gap-2 mb-3">
						<input
							type="text"
							bind:value={newFavorite}
							placeholder="Favorite genre..."
							onkeydown={handleAddFavoriteKeydown}
							class="flex-1 rounded-md border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
						/>
						<Button
							type="button"
							size="sm"
							onclick={handleAddFavorite}
							disabled={addFavoriteMutation.isPending || !newFavorite.trim()}
						>
							{#if addFavoriteMutation.isPending}
								<Loader2 class="h-4 w-4 animate-spin mr-1" />
							{:else}
								<Plus class="h-4 w-4 mr-1" />
							{/if}
							Add
						</Button>
					</div>
					<div class="rounded-md border border-border/60 bg-muted/20 p-3">
						<p class="text-xs font-medium text-muted-foreground mb-2">Bulk add &amp; save</p>
						<textarea
							bind:value={bulkFavorites}
							placeholder="Paste genres separated by commas or new lines..."
							rows="2"
							class="w-full rounded border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring resize-y"
						></textarea>
						<div class="flex justify-end mt-2">
							<Button
								type="button"
								size="sm"
								onclick={handleBulkAddFavorites}
								disabled={replaceFavoritesMutation.isPending || !bulkFavorites.trim()}
							>
								{#if replaceFavoritesMutation.isPending}
									<Loader2 class="h-4 w-4 animate-spin mr-1" />
								{:else}
									<Check class="h-4 w-4 mr-1" />
								{/if}
								Save Favorites
							</Button>
						</div>
					</div>
				{/if}
			</Card>
		</div>

		{#if error}
			<div in:fly|local={{ y: 8, duration: 180 }}>
				<Card class="p-4 border-destructive bg-destructive/10 text-destructive">
					Failed to load genre replacements: {error}
				</Card>
			</div>
		{:else}
			<div in:fly|local={{ y: 8, duration: 180, delay: 60 }}>
				<Card class="p-5">
					<p class="text-sm font-medium mb-3">Add a new genre replacement rule</p>
					<div class="flex flex-col sm:flex-row items-start gap-3">
						<div class="flex-1 w-full sm:w-auto">
							<label for="genre-original" class="block text-xs font-medium text-muted-foreground mb-1">Original</label>
							<input
								id="genre-original"
								type="text"
								bind:value={newOriginal}
								placeholder="e.g., HD"
								onkeydown={handleAddKeydown}
								class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
							/>
						</div>
						<div class="flex-1 w-full sm:w-auto">
							<label for="genre-replacement" class="block text-xs font-medium text-muted-foreground mb-1">Replacement</label>
							<input
								id="genre-replacement"
								type="text"
								bind:value={newReplacement}
								placeholder="e.g., High Definition"
								onkeydown={handleAddKeydown}
								class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
							/>
						</div>
						<div class="flex items-end">
							<Button
								type="button"
								size="sm"
								onclick={handleAdd}
								disabled={addMutation.isPending || !newOriginal.trim() || !newReplacement.trim()}
							>
								{#if addMutation.isPending}
									<Loader2 class="h-4 w-4 animate-spin mr-1" />
								{:else}
									<Plus class="h-4 w-4 mr-1" />
								{/if}
								Add
							</Button>
						</div>
					</div>
				</Card>
			</div>

			<div in:fade|local={{ duration: 240, delay: 120 }}>
				{#if loading}
					<Card class="p-8 text-center text-muted-foreground">
						<Loader2 class="h-5 w-5 animate-spin mx-auto mb-2" />
						Loading genre replacements...
					</Card>
				{:else if replacements.length === 0}
					<Card class="p-8 text-center">
						<p class="text-muted-foreground">No genre replacements configured yet. Add one above.</p>
					</Card>
				{:else}
					<div class="flex flex-col sm:flex-row items-start sm:items-center gap-3 mb-3">
						<div class="relative flex-1">
							<Search class="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
							<input
								type="text"
								bind:value={searchQuery}
								placeholder="Search by original or replacement..."
								class="w-full pl-9 pr-8 rounded-md border border-input bg-background py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
							/>
							{#if searchQuery}
								<button
									type="button"
									class="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground p-0.5"
									onclick={clearSearch}
									title="Clear search"
								>
									<X class="h-3.5 w-3.5" />
								</button>
							{/if}
						</div>
						<button
							type="button"
							class="inline-flex items-center gap-1 px-2.5 py-2 text-sm border border-input rounded-md bg-background hover:bg-accent transition-colors text-muted-foreground hover:text-foreground"
							onclick={toggleSort}
							title="Toggle sort order"
						>
							{#if sortDirection === 'asc'}
								<ArrowDownUp class="h-4 w-4" />
							{:else}
								<ChevronsDownUp class="h-4 w-4" />
							{/if}
							<span class="text-xs">{sortDirection === 'asc' ? 'A-Z' : 'Z-A'}</span>
						</button>
					</div>

					<div class="rounded-lg border bg-card overflow-hidden">
						<div class="relative" style="max-height: 560px; overflow-y: auto;">
							<div class="sticky top-0 z-10">
								<div class="grid grid-cols-[1fr_1fr_auto] gap-0 text-sm py-3 px-4 font-medium text-muted-foreground border-b border-border bg-card/95 backdrop-blur">
									<div>Original</div>
									<div>Replacement</div>
									<div class="w-20 text-center">Actions</div>
								</div>
							</div>
							<div class="min-h-0">
								{#if filteredAndSorted.length === 0 && searchQuery.trim()}
									<div class="py-12 text-center text-muted-foreground text-sm">
										No replacements match "{searchQuery}"
									</div>
								{:else}
									{#each filteredAndSorted as rep (rep.id)}
										<div class="grid grid-cols-[1fr_1fr_auto] gap-0 text-sm border-b border-border/50 last:border-b-0 hover:bg-accent/30 transition-colors">
											{#if editingId === rep.id}
												<div class="py-2 px-4">
													<input
														type="text"
														value={rep.original}
														disabled
														class="w-full rounded border border-input bg-muted/50 px-2 py-1 text-sm font-mono text-muted-foreground cursor-not-allowed"
													/>
												</div>
												<div class="py-2 px-4 space-y-1">
													<input
														type="text"
														bind:value={editReplacement}
														onkeydown={handleEditKeydown}
														class="w-full rounded border border-input bg-background px-2 py-1 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-ring"
													/>
													<div class="flex gap-1">
														<button
															type="button"
															class="inline-flex items-center gap-0.5 px-2.5 py-1 text-xs bg-primary text-primary-foreground rounded-md hover:bg-primary/90"
															onclick={() => saveEdit(rep)}
															disabled={updateMutation.isPending}
														>
															{#if updateMutation.isPending}
																<Loader2 class="h-3 w-3 animate-spin" />
															{:else}
																<Check class="h-3 w-3" />
															{/if}
															Save
														</button>
														<button
															type="button"
															class="inline-flex items-center gap-0.5 px-2.5 py-1 text-xs border border-input rounded-md hover:bg-accent transition-colors"
															onclick={cancelEdit}
														>
															<X class="h-3 w-3" />
															Cancel
														</button>
													</div>
												</div>
												<div class="py-2 px-4"></div>
											{:else}
												<div class="py-2.5 px-4 font-mono text-sm whitespace-nowrap overflow-hidden text-ellipsis max-w-[200px]" title={rep.original}>
													{rep.original}
												</div>
												<div class="py-2.5 px-4 font-mono text-sm whitespace-nowrap overflow-hidden text-ellipsis max-w-[200px]" title={rep.replacement}>
													{rep.replacement}
												</div>
												<div class="py-2.5 px-4 flex items-center justify-center gap-0.5">
													<button
														type="button"
														class="text-muted-foreground hover:text-foreground transition-colors p-1 rounded"
														title="Edit"
														onclick={() => startEdit(rep)}
													>
														<Pencil class="h-4 w-4" />
													</button>
													<button
														type="button"
														class="text-muted-foreground hover:text-destructive transition-colors p-1 rounded"
														title="Delete"
																				onclick={() => handleDelete(rep.id)}
													>
														<Trash2 class="h-4 w-4" />
													</button>
												</div>
											{/if}
										</div>
									{/each}
								{/if}
							</div>
						</div>
					</div>

					{#if searchQuery.trim()}
						<p class="text-xs text-muted-foreground pt-1">
							Showing {filteredAndSorted.length} of {replacements.length} replacements
						</p>
					{:else}
						<p class="text-xs text-muted-foreground pt-1">
							{replacements.length} replacement{replacements.length !== 1 ? 's' : ''} configured
						</p>
					{/if}
				{/if}
			</div>

			<div class="rounded-lg border border-border/60 bg-muted/20 px-4 py-3">
				<p class="text-xs text-muted-foreground">
					Replacements take effect on the next scrape. Existing movies are not retroactively updated.
				</p>
			</div>
		{/if}
	</div>
</div>