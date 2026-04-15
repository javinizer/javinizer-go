<script lang="ts">
	import { onMount } from 'svelte';
	import { apiClient } from '$lib/api/client';
	import type { GenreReplacement } from '$lib/api/types';
	import { toastStore } from '$lib/stores/toast';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import { Trash2, Plus, Loader2 } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';

	let replacements = $state<GenreReplacement[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let newOriginal = $state('');
	let newReplacement = $state('');
	let adding = $state(false);

	async function loadReplacements() {
		try {
			loading = true;
			error = null;
			const resp = await apiClient.listGenreReplacements();
			replacements = resp.replacements;
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		} finally {
			loading = false;
		}
	}

	async function handleAdd() {
		const original = newOriginal.trim();
		const replacement = newReplacement.trim();
		if (!original || !replacement) {
			toastStore.error('Both original and replacement fields are required', 4000);
			return;
		}

		try {
			adding = true;
			await apiClient.createGenreReplacement({ original, replacement });
			newOriginal = '';
			newReplacement = '';
			toastStore.success(`Genre replacement "${original}" → "${replacement}" added`, 3000);
			await loadReplacements();
		} catch (e) {
			toastStore.error(e instanceof Error ? e.message : 'Failed to add genre replacement', 4000);
		} finally {
			adding = false;
		}
	}

	async function handleDelete(original: string) {
		try {
			await apiClient.deleteGenreReplacement(original);
			toastStore.success(`Genre replacement "${original}" removed`, 3000);
			await loadReplacements();
		} catch (e) {
			toastStore.error(e instanceof Error ? e.message : 'Failed to delete genre replacement', 4000);
		}
	}

	onMount(() => {
		loadReplacements();
	});
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
		{#if replacements.length === 0}
			<p class="text-sm text-muted-foreground py-4">
				No genre replacements configured. Add one below.
			</p>
		{:else}
			<div class="overflow-x-auto">
				<table class="w-full text-sm">
					<thead>
						<tr class="border-b border-border">
							<th class="text-left py-2 px-3 font-medium text-muted-foreground">Original</th>
							<th class="text-left py-2 px-3 font-medium text-muted-foreground">Replacement</th>
							<th class="w-12 py-2 px-3"></th>
						</tr>
					</thead>
					<tbody>
						{#each replacements as rep}
							<tr class="border-b border-border/50 hover:bg-accent/30 transition-colors">
								<td class="py-2 px-3 font-mono text-sm">{rep.original}</td>
								<td class="py-2 px-3 font-mono text-sm">{rep.replacement}</td>
								<td class="py-2 px-3 text-center">
									<button
										type="button"
										class="text-muted-foreground hover:text-destructive transition-colors p-1 rounded"
										title="Remove replacement"
										onclick={() => handleDelete(rep.original)}
									>
										<Trash2 class="h-4 w-4" />
									</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}

		<div class="pt-4 border-t mt-4">
			<p class="text-xs text-muted-foreground mb-3">Add a new genre replacement rule:</p>
			<div class="flex items-end gap-2">
				<div class="flex-1">
					<label for="genre-original" class="block text-xs font-medium text-muted-foreground mb-1">Original</label>
					<input
						id="genre-original"
						type="text"
						bind:value={newOriginal}
						placeholder="e.g., HD"
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
						class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
					/>
				</div>
				<Button
					type="button"
					size="sm"
					onclick={handleAdd}
					disabled={adding || !newOriginal.trim() || !newReplacement.trim()}
				>
					{#if adding}
						<Loader2 class="h-4 w-4 animate-spin mr-1" />
					{:else}
						<Plus class="h-4 w-4 mr-1" />
					{/if}
					Add
				</Button>
			</div>
		</div>

		<p class="text-xs text-muted-foreground mt-3">
			Replacements take effect on the next scrape. Existing movies are not retroactively updated.
		</p>
	{/if}
</SettingsSection>
