<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import { cubicOut } from 'svelte/easing';
	import { fade, fly } from 'svelte/transition';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { apiClient } from '$lib/api/client';
	import type {
		WordReplacement,
		WordReplacementUpdateRequest,
		ImportResponse,
	} from '$lib/api/types';
	import { toastStore } from '$lib/stores/toast';
	import {
		Trash2,
		Plus,
		Loader2,
		Search,
		X,
		Check,
		Pencil,
		ArrowDownUp,
		ChevronsDownUp,
		ArrowLeft,
		Type,
		Download,
		Upload,
	} from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import { createWordReplacementsQuery } from '$lib/query/queries';

	const queryClient = useQueryClient();

	const replacementsQuery = createWordReplacementsQuery();
	let replacements = $derived<WordReplacement[]>(
		replacementsQuery.data?.replacements ?? [],
	);
	let loading = $derived(replacementsQuery.isPending);
	let error = $derived<string | null>(
		replacementsQuery.error?.message ?? null,
	);

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
				(r) =>
					r.original.toLowerCase().includes(q) ||
					r.replacement.toLowerCase().includes(q),
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
			apiClient.createWordReplacement({ original, replacement }),
		onSuccess: (_data, { original, replacement }) => {
			newOriginal = '';
			newReplacement = '';
			toastStore.success(m.words_added({ original, replacement }), 3000);
			void queryClient.invalidateQueries({ queryKey: ['word-replacements'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message || m.words_add_failed(), 4000);
		}
	}));

	const updateMutation = createMutation(() => ({
		mutationFn: (req: WordReplacementUpdateRequest) => apiClient.updateWordReplacement(req),
		onSuccess: (_data, { original, replacement }) => {
			editingId = null;
			toastStore.success(m.words_updated({ original, replacement }), 3000);
			void queryClient.invalidateQueries({ queryKey: ['word-replacements'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message || m.words_update_failed(), 4000);
		}
	}));

		const deleteMutation = createMutation(() => ({
		mutationFn: (id: number) => apiClient.deleteWordReplacement(id),
		onSuccess: () => {
			toastStore.success(m.words_removed(), 3000);
			void queryClient.invalidateQueries({ queryKey: ['word-replacements'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message || m.words_delete_failed(), 4000);
		}
	}));

	const exportMutation = createMutation(() => ({
		mutationFn: () => apiClient.exportWordReplacements(),
		onSuccess: async (data) => {
			const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
			const url = URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			a.download = 'word-replacements.json';
			document.body.appendChild(a);
			a.click();
			document.body.removeChild(a);
			URL.revokeObjectURL(url);
			toastStore.success(m.words_exported({ count: data.length }), 3000);
		},
		onError: (err: Error) => {
			toastStore.error(err.message || m.words_export_failed(), 4000);
		}
	}));

	const importMutation = createMutation(() => ({
		mutationFn: (payload: { replacements: { original: string; replacement: string }[]; includeDefaults: boolean }) =>
			apiClient.importWordReplacements(payload),
		onSuccess: (res: ImportResponse) => {
			toastStore.success(m.words_import_complete({ imported: res.imported, skipped: res.skipped, errors: res.errors }), 5000);
			void queryClient.invalidateQueries({ queryKey: ['word-replacements'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message || m.words_import_failed(), 4000);
		}
	}));

	function handleAdd() {
		const original = newOriginal.trim();
		const replacement = newReplacement.trim();
		if (!original || !replacement) {
			toastStore.error(m.words_both_fields_required(), 4000);
			return;
		}
		addMutation.mutate({ original, replacement });
	}

	function handleDelete(id: number) {
		deleteMutation.mutate(id);
	}

	function startEdit(rep: WordReplacement) {
		editingId = rep.id;
		editOriginal = rep.original;
		editReplacement = rep.replacement;
	}

	function cancelEdit() {
		editingId = null;
		editOriginal = '';
		editReplacement = '';
	}

	function saveEdit(rep: WordReplacement) {
		const r = editReplacement.trim();
		if (!r) {
			toastStore.error(m.words_both_fields_required_edit(), 4000);
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
			const parsed: WordReplacement[] = JSON.parse(text);
			if (!Array.isArray(parsed)) throw new Error(m.words_expected_json_array());

			const replacements = parsed
				.filter(r => r.original && r.original.trim())
				.map(r => ({ original: r.original.trim(), replacement: (r.replacement || '').trim() }));

			if (replacements.length === 0) {
				toastStore.error(m.words_no_valid_in_file(), 4000);
				return;
			}

			const includeDefaults = confirm(m.words_import_defaults_confirm());

			if (!confirm(m.words_import_confirm({ count: replacements.length }))) return;

			importMutation.mutate({ replacements, includeDefaults });
		} catch (err) {
			toastStore.error(m.words_invalid_json({ error: err instanceof Error ? err.message : String(err) }), 4000);
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
						<Type class="h-6 w-6 text-primary" />
						<h1 class="text-3xl font-bold">{m.words_title()}</h1>
					</div>
					<p class="text-muted-foreground mt-1">
						{m.words_subtitle()}
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
					{m.words_export()}
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
					{m.words_import()}
				</Button>
			</div>
		</div>

		{#if error}
			<div in:fly|local={{ y: 8, duration: 180 }}>
				<Card class="p-4 border-destructive bg-destructive/10 text-destructive">
					{m.words_load_failed({ error })}
				</Card>
			</div>
		{:else}
			<div in:fly|local={{ y: 8, duration: 180, delay: 60 }}>
				<Card class="p-5">
					<p class="text-sm font-medium mb-3">{m.words_add_rule_heading()}</p>
					<div class="flex flex-col sm:flex-row items-start gap-3">
						<div class="flex-1 w-full sm:w-auto">
							<label for="word-original" class="block text-xs font-medium text-muted-foreground mb-1">{m.words_original_label()}</label>
							<input
								id="word-original"
								type="text"
								bind:value={newOriginal}
								placeholder={m.words_original_ph()}
								onkeydown={handleAddKeydown}
								class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
							/>
						</div>
						<div class="flex-1 w-full sm:w-auto">
							<label for="word-replacement" class="block text-xs font-medium text-muted-foreground mb-1">{m.words_replacement_label()}</label>
							<input
								id="word-replacement"
								type="text"
								bind:value={newReplacement}
								placeholder={m.words_replacement_ph()}
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
								{m.common_add()}
							</Button>
						</div>
					</div>
				</Card>
			</div>

			<div in:fade|local={{ duration: 240, delay: 120 }}>
				{#if loading}
					<Card class="p-8 text-center text-muted-foreground">
						<Loader2 class="h-5 w-5 animate-spin mx-auto mb-2" />
						{m.words_loading_replacements()}
					</Card>
				{:else if replacements.length === 0}
					<Card class="p-8 text-center">
						<p class="text-muted-foreground">{m.words_none_configured()}</p>
					</Card>
				{:else}
					<div class="flex flex-col sm:flex-row items-start sm:items-center gap-3 mb-3">
						<div class="relative flex-1">
							<Search class="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
							<input
								type="text"
								bind:value={searchQuery}
								placeholder={m.words_search_ph()}
								class="w-full pl-9 pr-8 rounded-md border border-input bg-background py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
							/>
							{#if searchQuery}
								<button
									type="button"
									class="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground p-0.5"
									onclick={clearSearch}
									title={m.words_clear_search()}
								>
									<X class="h-3.5 w-3.5" />
								</button>
							{/if}
						</div>
						<button
							type="button"
							class="inline-flex items-center gap-1 px-2.5 py-2 text-sm border border-input rounded-md bg-background hover:bg-accent transition-colors text-muted-foreground hover:text-foreground"
							onclick={toggleSort}
							title={m.words_toggle_sort()}
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
									<div>{m.words_col_original()}</div>
									<div>{m.words_col_replacement()}</div>
									<div class="w-20 text-center">{m.words_col_actions()}</div>
								</div>
							</div>
							<div class="min-h-0">
								{#if filteredAndSorted.length === 0 && searchQuery.trim()}
									<div class="py-12 text-center text-muted-foreground text-sm">
										{m.words_no_match_search({ query: searchQuery })}
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
															{m.words_save()}
														</button>
														<button
															type="button"
															class="inline-flex items-center gap-0.5 px-2.5 py-1 text-xs border border-input rounded-md hover:bg-accent transition-colors"
															onclick={cancelEdit}
														>
															<X class="h-3 w-3" />
															{m.common_cancel()}
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
														title={m.words_edit()}
														onclick={() => startEdit(rep)}
													>
														<Pencil class="h-4 w-4" />
													</button>
													<button
														type="button"
														class="text-muted-foreground hover:text-destructive transition-colors p-1 rounded"
														title={m.words_delete()}
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
							{m.words_showing_of({ shown: filteredAndSorted.length, total: replacements.length })}
						</p>
					{:else}
						<p class="text-xs text-muted-foreground pt-1">
							{m.words_replacements_configured({ count: replacements.length })}
						</p>
					{/if}
				{/if}
			</div>

			<div class="rounded-lg border border-border/60 bg-muted/20 px-4 py-3">
				<p class="text-xs text-muted-foreground">
					{m.words_next_scrape_note()}
				</p>
			</div>
		{/if}
	</div>
</div>