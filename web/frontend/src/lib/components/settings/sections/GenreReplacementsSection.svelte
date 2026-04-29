<script lang="ts">
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { apiClient } from '$lib/api/client';
	import type { GenreReplacement, GenreReplacementUpdateRequest } from '$lib/api/types';
	import { toastStore } from '$lib/stores/toast';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import { Trash2, Plus, Loader2, Search, X, Check, Pencil, ArrowDownUp, ChevronsDownUp } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { createGenreReplacementsQuery } from '$lib/query/queries';

	const queryClient = useQueryClient();

	const replacementsQuery = createGenreReplacementsQuery();
	let replacements = $derived<GenreReplacement[]>(replacementsQuery.data?.replacements ?? []);
	let loading = $derived(replacementsQuery.isPending);
	let error = $derived<string | null>(replacementsQuery.error?.message ?? null);

	let newOriginal = $state('');
	let newReplacement = $state('');
	let searchQuery = $state('');
	let sortDirection = $state<'asc' | 'desc'>('asc');

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
		mutationFn: (original: string) => apiClient.deleteGenreReplacement(original),
		onSuccess: (_, original) => {
			toastStore.success(`Genre replacement "${original}" removed`, 3000);
			void queryClient.invalidateQueries({ queryKey: ['genre-replacements'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message || 'Failed to delete genre replacement', 4000);
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

	function handleDelete(original: string) {
		deleteMutation.mutate(original);
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
		const o = editOriginal.trim();
		const r = editReplacement.trim();
		if (!o || !r) {
			toastStore.error('Both fields are required', 4000);
			return;
		}
		updateMutation.mutate({ original: o, replacement: r });
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
</script>

<SettingsSection
	title="Genre Replacements"
	description="Manage genre name replacements that are applied during scraping"
	defaultExpanded={false}
>
	{#if loading}
		<div class="flex items-center justify-center py-8 text-muted-foreground">
			<Loader2 class="h-5 w-5 animate-spin mr-2" />
			Loading...
		</div>
	{:else if error}
		<div class="text-destructive text-sm py-4">
			Failed to load genre replacements: {error}
		</div>
	{:else}
		<div class="space-y-4">
			{#if replacements.length > 10}
				<div class="flex items-center gap-2">
					<div class="relative flex-1">
						<Search class="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
						<input
							type="text"
							bind:value={searchQuery}
							placeholder="Search genres..."
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
			{/if}

			{#if replacements.length === 0}
				<p class="text-sm text-muted-foreground py-4">
					No genre replacements configured. Add one below.
				</p>
			{:else}
				<div class="relative" style="max-height: 400px; overflow-y: auto; border: 1px solid var(--border); border-radius: 0.5rem;">
					<div class="sticky top-0 z-10 bg-card border-b border-border">
						<div class="grid grid-cols-[1fr_1fr_auto] gap-0 text-sm py-2 px-3 font-medium text-muted-foreground">
							<div>Original</div>
							<div>Replacement</div>
							<div class="w-12 text-center"></div>
						</div>
					</div>
					<div class="min-h-0">
						{#if filteredAndSorted.length === 0 && searchQuery.trim()}
							<div class="py-8 text-center text-muted-foreground text-sm">
								No replacements match "{searchQuery}"
							</div>
						{:else}
							{#each filteredAndSorted as rep (rep.id)}
								<div class="grid grid-cols-[1fr_1fr_auto] gap-0 text-sm border-b border-border/50 hover:bg-accent/30 transition-colors">
									{#if editingId === rep.id}
										<div class="py-1.5 px-3">
											<input
												type="text"
												bind:value={editOriginal}
												onkeydown={handleEditKeydown}
												class="w-full rounded border border-input bg-background px-2 py-1 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-ring"
												autofocus
											/>
										</div>
										<div class="py-1.5 px-3 space-y-1">
											<input
												type="text"
												bind:value={editReplacement}
												onkeydown={handleEditKeydown}
												class="w-full rounded border border-input bg-background px-2 py-1 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-ring"
											/>
											<div class="flex gap-1">
												<button
													type="button"
													class="inline-flex items-center gap-0.5 px-2 py-0.5 text-xs bg-primary text-primary-foreground rounded hover:bg-primary/90"
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
													class="inline-flex items-center gap-0.5 px-2 py-0.5 text-xs border border-input rounded hover:bg-accent transition-colors"
													onclick={cancelEdit}
												>
													<X class="h-3 w-3" />
													Cancel
												</button>
											</div>
										</div>
										<div class="py-1.5 px-3"></div>
									{:else}
										<div class="py-2 px-3 font-mono text-sm whitespace-nowrap overflow-hidden text-ellipsis max-w-[180px]" title={rep.original}>
											{rep.original}
										</div>
										<div class="py-2 px-3 font-mono text-sm whitespace-nowrap overflow-hidden text-ellipsis max-w-[180px]" title={rep.replacement}>
											{rep.replacement}
										</div>
										<div class="py-2 px-3 flex items-center justify-center gap-0.5">
											<button
												type="button"
												class="text-muted-foreground hover:text-foreground transition-colors p-1 rounded"
												title="Edit replacement"
												onclick={() => startEdit(rep)}
											>
												<Pencil class="h-4 w-4" />
											</button>
											<button
												type="button"
												class="text-muted-foreground hover:text-destructive transition-colors p-1 rounded"
												title="Remove replacement"
												onclick={() => handleDelete(rep.original)}
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
				{#if searchQuery.trim()}
					<p class="text-xs text-muted-foreground">
						Showing {filteredAndSorted.length} of {replacements.length} replacements
					</p>
				{:else}
					<p class="text-xs text-muted-foreground">
						{replacements.length} replacement{replacements.length !== 1 ? 's' : ''} configured
					</p>
				{/if}
			{/if}

			<div class="border-t pt-4">
				<p class="text-xs text-muted-foreground mb-3">Add a new genre replacement rule:</p>
				<div class="flex items-end gap-2">
					<div class="flex-1">
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
					<div class="flex-1">
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

			<p class="text-xs text-muted-foreground">
				Replacements take effect on the next scrape. Existing movies are not retroactively updated.
			</p>
		</div>
	{/if}
</SettingsSection>
