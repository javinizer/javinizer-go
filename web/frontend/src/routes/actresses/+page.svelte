<script lang="ts">
	import { flip } from 'svelte/animate';
	import { cubicOut, quintOut } from 'svelte/easing';
	import { fade, fly, scale } from 'svelte/transition';
	import {
		createQuery,
		createMutation,
		useQueryClient,
		keepPreviousData
	} from '@tanstack/svelte-query';
	import {
		Plus,
		RefreshCw,
		Search,
		Save,
		Trash2,
		Pencil,
		ImageOff,
		ChevronLeft,
		ChevronRight,
		ArrowUpDown,
		GitMerge
	} from 'lucide-svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { apiClient } from '$lib/api/client';
	import { toastStore } from '$lib/stores/toast';
	import type {
		Actress,
		ActressUpsertRequest,
		ActressMergePreviewResponse,
		ActressMergeResolution
	} from '$lib/api/types';

	type ActressForm = {
		dmm_id: string;
		first_name: string;
		last_name: string;
		japanese_name: string;
		thumb_url: string;
		aliases: string;
	};

	const DEFAULT_LIMIT = 20;

	const queryClient = useQueryClient();

	let queryInput = $state('');
	let activeQuery = $state('');
	let limit = $state(DEFAULT_LIMIT);
	let offset = $state(0);
	let viewMode = $state<'cards' | 'compact' | 'table'>('cards');
	let sortBy = $state<'name' | 'japanese_name' | 'id' | 'dmm_id' | 'updated_at' | 'created_at'>('name');
	let sortOrder = $state<'asc' | 'desc'>('asc');

	let editingId = $state<number | null>(null);
	let form = $state<ActressForm>(emptyForm());
	let formError = $state<string | null>(null);
	let selectedIds = $state<number[]>([]);

	const actressesQuery = createQuery(() => ({
		queryKey: ['actresses', { limit, offset, q: activeQuery, sort_by: sortBy, sort_order: sortOrder }],
		queryFn: () => apiClient.listActresses({
			limit,
			offset,
			q: activeQuery || undefined,
			sort_by: sortBy,
			sort_order: sortOrder
		}),
		placeholderData: keepPreviousData
	}));

	let actresses = $derived(actressesQuery.data?.actresses ?? []);
	let total = $derived(actressesQuery.data?.total ?? 0);
	let loading = $derived(actressesQuery.isPending && !actressesQuery.data);
	let error = $derived(actressesQuery.error?.message ?? null);
	let isRefreshing = $derived(actressesQuery.isFetching && !!actressesQuery.data);

	const saveActressMutation = createMutation(() => ({
		mutationFn: async (payload: ActressUpsertRequest) => {
			if (editingId !== null) {
				return apiClient.updateActress(editingId, payload);
			}
			return apiClient.createActress(payload);
		},
		onSuccess: () => {
			if (editingId !== null) {
				toastStore.success('Actress updated');
			} else {
				toastStore.success('Actress created');
			}
			queryClient.invalidateQueries({ queryKey: ['actresses'] });
			resetForm();
		},
		onError: (err: Error) => {
			formError = err.message;
		}
	}));

	const deleteActressMutation = createMutation(() => ({
		mutationFn: (id: number) => apiClient.deleteActress(id),
		onSuccess: (_data, id) => {
			toastStore.success('Actress deleted');
			selectedIds = selectedIds.filter((sid) => sid !== id);
			const currentActresses = actressesQuery.data?.actresses ?? [];
			if (currentActresses.length === 1 && offset > 0) {
				offset = Math.max(0, offset - limit);
			}
			if (editingId === id) {
				resetForm();
			}
			queryClient.invalidateQueries({ queryKey: ['actresses'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message);
		}
	}));

	$effect(() => {
		const data = actressesQuery.data;
		if (data && !showMergeModal) {
			const pageIDs = new Set(
				data.actresses.map((actress) => actress.id).filter((id): id is number => id !== undefined)
			);
			selectedIds = selectedIds.filter((id) => pageIDs.has(id));
		}
	});

	let showMergeModal = $state(false);
	let mergePrimaryId = $state<number | null>(null);
	let mergeSourceQueue = $state<number[]>([]);
	let mergeCurrentSourceId = $state<number | null>(null);
	let mergePreview = $state<ActressMergePreviewResponse | null>(null);
	let mergeResolutions = $state<Record<string, ActressMergeResolution>>({});
	let mergeLoadingPreview = $state(false);
	let mergeRunning = $state(false);
	let mergeSummary = $state<{ success: number; failed: number; messages: string[] }>({
		success: 0,
		failed: 0,
		messages: []
	});

	const currentPage = $derived(Math.floor(offset / limit) + 1);
	const totalPages = $derived(Math.max(1, Math.ceil(total / limit)));
	const canGoPrev = $derived(offset > 0);
	const canGoNext = $derived(offset + limit < total);

	function itemDelay(index: number): number {
		return Math.min(index * 35, 280);
	}

	function emptyForm(): ActressForm {
		return {
			dmm_id: '',
			first_name: '',
			last_name: '',
			japanese_name: '',
			thumb_url: '',
			aliases: ''
		};
	}

	function getDisplayName(actress: Actress): string {
		if (actress.last_name && actress.first_name) return `${actress.last_name} ${actress.first_name}`;
		if (actress.first_name) return actress.first_name;
		if (actress.japanese_name) return actress.japanese_name;
		return 'Unnamed';
	}

	function getActressLabelByID(id: number): string {
		const actress = actresses.find((item) => item.id === id);
		if (!actress) return `Actress #${id}`;
		return `#${id} - ${getDisplayName(actress)}`;
	}

	function isSelected(actress: Actress): boolean {
		return actress.id !== undefined && selectedIds.includes(actress.id);
	}

	function toggleSelection(actress: Actress) {
		if (!actress.id) return;
		if (selectedIds.includes(actress.id)) {
			selectedIds = selectedIds.filter((id) => id !== actress.id);
			return;
		}
		selectedIds = [...selectedIds, actress.id];
	}

	function selectCurrentPage() {
		const ids = actresses.map((actress) => actress.id).filter((id): id is number => id !== undefined);
		selectedIds = [...new Set([...selectedIds, ...ids])];
	}

	function clearSelection() {
		selectedIds = [];
	}

	function toPayload(): ActressUpsertRequest {
		const dmmID = Number.parseInt(form.dmm_id, 10);
		return {
			dmm_id: Number.isNaN(dmmID) ? undefined : dmmID,
			first_name: form.first_name.trim(),
			last_name: form.last_name.trim(),
			japanese_name: form.japanese_name.trim(),
			thumb_url: form.thumb_url.trim(),
			aliases: form.aliases.trim()
		};
	}

	function validateForm(): string | null {
		const payload = toPayload();
		if (payload.dmm_id !== undefined && payload.dmm_id < 0) {
			return 'DMM ID must be greater than or equal to 0';
		}
		if (!payload.first_name && !payload.japanese_name) {
			return 'Either First Name or Japanese Name is required';
		}
		return null;
	}

	function resetForm() {
		editingId = null;
		form = emptyForm();
		formError = null;
	}

	function startEdit(actress: Actress) {
		editingId = actress.id ?? null;
		form = {
			dmm_id: actress.dmm_id && actress.dmm_id > 0 ? actress.dmm_id.toString() : '',
			first_name: actress.first_name ?? '',
			last_name: actress.last_name ?? '',
			japanese_name: actress.japanese_name ?? '',
			thumb_url: actress.thumb_url ?? '',
			aliases: actress.aliases ?? ''
		};
		formError = null;
	}

	function saveActress() {
		formError = validateForm();
		if (formError) return;
		saveActressMutation.mutate(toPayload());
	}

	function removeActress(actress: Actress) {
		if (!actress.id) return;
		const name = getDisplayName(actress);
		if (!confirm(`Delete actress "${name}"?`)) return;
		deleteActressMutation.mutate(actress.id);
	}

	function applySearch() {
		activeQuery = queryInput.trim();
		offset = 0;
	}

	function clearSearch() {
		queryInput = '';
		activeQuery = '';
		offset = 0;
	}

	function applySort() {
		offset = 0;
	}

	function toggleSortOrder() {
		sortOrder = sortOrder === 'asc' ? 'desc' : 'asc';
		applySort();
	}

	function prevPage() {
		if (!canGoPrev) return;
		offset = Math.max(0, offset - limit);
	}

	function nextPage() {
		if (!canGoNext) return;
		offset += limit;
	}

	function resetMergeState() {
		mergeSourceQueue = [];
		mergeCurrentSourceId = null;
		mergePreview = null;
		mergeResolutions = {};
		mergeLoadingPreview = false;
		mergeRunning = false;
		mergeSummary = { success: 0, failed: 0, messages: [] };
	}

	function closeMergeModal() {
		showMergeModal = false;
		mergePrimaryId = null;
		resetMergeState();
	}

	function startMergeSelected() {
		if (selectedIds.length < 2) {
			toastStore.warning('Select at least 2 actresses to merge');
			return;
		}
		mergePrimaryId = selectedIds[0];
		showMergeModal = true;
		resetMergeState();
		void resetMergeQueueAndPreview();
	}

	async function resetMergeQueueAndPreview() {
		if (!mergePrimaryId) return;
		mergeSourceQueue = selectedIds.filter((id) => id !== mergePrimaryId);
		await loadNextMergePreview();
	}

	function formatMergeValue(value: unknown): string {
		if (value === null || value === undefined || value === '') return 'Empty';
		return String(value);
	}

	function setResolution(field: string, decision: ActressMergeResolution) {
		mergeResolutions = { ...mergeResolutions, [field]: decision };
	}

	async function loadNextMergePreview() {
		if (!mergePrimaryId || mergeSourceQueue.length === 0) {
			mergeCurrentSourceId = null;
			mergePreview = null;
			return;
		}

		const sourceID = mergeSourceQueue[0];
		mergeCurrentSourceId = sourceID;
		mergeLoadingPreview = true;
		try {
			const preview = await apiClient.previewActressMerge({
				target_id: mergePrimaryId,
				source_id: sourceID
			});
			mergePreview = preview;
			mergeResolutions = { ...preview.default_resolutions };
		} catch (e) {
			const message = e instanceof Error ? e.message : 'Failed to preview merge';
			mergeSummary = {
				...mergeSummary,
				failed: mergeSummary.failed + 1,
				messages: [...mergeSummary.messages, `Skipped #${sourceID}: ${message}`]
			};
			mergeSourceQueue = mergeSourceQueue.slice(1);
			await loadNextMergePreview();
		} finally {
			mergeLoadingPreview = false;
		}
	}

	async function applyCurrentMerge() {
		if (!mergePrimaryId || !mergeCurrentSourceId || !mergePreview || mergeRunning) return;
		mergeRunning = true;
		try {
			const result = await apiClient.mergeActresses({
				target_id: mergePrimaryId,
				source_id: mergeCurrentSourceId,
				resolutions: mergeResolutions
			});
			mergeSummary = {
				...mergeSummary,
				success: mergeSummary.success + 1,
				messages: [...mergeSummary.messages, `Merged #${result.merged_from_id} into #${mergePrimaryId}`]
			};
			selectedIds = selectedIds.filter((id) => id !== mergeCurrentSourceId);
			mergeSourceQueue = mergeSourceQueue.slice(1);
			await queryClient.invalidateQueries({ queryKey: ['actresses'] });
			await loadNextMergePreview();
		} catch (e) {
			const message = e instanceof Error ? e.message : 'Failed to merge actress';
			mergeSummary = {
				...mergeSummary,
				failed: mergeSummary.failed + 1,
				messages: [...mergeSummary.messages, `Failed #${mergeCurrentSourceId}: ${message}`]
			};
			mergeSourceQueue = mergeSourceQueue.slice(1);
			await loadNextMergePreview();
		} finally {
			mergeRunning = false;
		}
	}

	async function skipCurrentMerge() {
		if (!mergeCurrentSourceId || mergeRunning) return;
		mergeSummary = {
			...mergeSummary,
			messages: [...mergeSummary.messages, `Skipped #${mergeCurrentSourceId}`]
		};
		mergeSourceQueue = mergeSourceQueue.slice(1);
		await loadNextMergePreview();
	}
</script>

<div class="container mx-auto px-4 py-8">
	<div class="max-w-7xl mx-auto space-y-6">
		<div
			class="flex flex-wrap items-center justify-between gap-3"
			in:fly|local={{ y: -10, duration: 240, easing: cubicOut }}
		>
			<div>
				<h1 class="text-3xl font-bold">Actress Database</h1>
				<p class="text-muted-foreground mt-1">Create, update, and remove actress records stored in the database.</p>
			</div>
				<div class="flex items-center gap-2">
					<Button variant="outline" onclick={() => queryClient.invalidateQueries({ queryKey: ['actresses'] })}>
						<RefreshCw class="h-4 w-4 {isRefreshing ? 'animate-spin' : ''}" />
						Refresh
					</Button>
				<Button onclick={resetForm}>
					<Plus class="h-4 w-4" />
					New Actress
				</Button>
			</div>
		</div>

		<div class="grid grid-cols-1 xl:grid-cols-5 gap-6" in:fade|local={{ duration: 240 }}>
			<div class="xl:col-span-2 xl:self-start xl:sticky xl:top-20">
				<div in:fade|local={{ duration: 220 }}>
					<Card
						class={`p-5 space-y-4 transition-colors ${
							editingId
								? 'border-primary/40 bg-primary/5'
								: 'border-dashed border-input/70 bg-card'
						}`}
					>
						<div class="flex items-center justify-between gap-2">
							<h2 class="text-lg font-semibold flex items-center gap-2">
								{#if editingId}
									<Pencil class="h-5 w-5 text-primary" />
									Edit Actress
								{:else}
									<Plus class="h-5 w-5 text-muted-foreground" />
									Create Actress
								{/if}
							</h2>
							<span
								class={`text-xs font-medium rounded-full px-2.5 py-1 ${
									editingId ? 'bg-primary/15 text-primary' : 'bg-muted text-muted-foreground'
								}`}
							>
								{editingId ? 'Edit Mode' : 'Create Mode'}
							</span>
						</div>
						<p class={`text-sm ${editingId ? 'text-primary/90' : 'text-muted-foreground'}`}>
							{editingId
								? 'You are editing an existing actress record.'
								: 'Fill in details to add a new actress record.'}
						</p>

					{#if formError}
						<div class="rounded-md border border-destructive bg-destructive/10 p-3 text-sm text-destructive">
							{formError}
						</div>
					{/if}

					<div class="space-y-3">
						<div>
							<label class="text-sm font-medium" for="dmm-id">DMM ID</label>
							<input
								id="dmm-id"
								type="number"
								min="0"
								bind:value={form.dmm_id}
								placeholder="e.g. 1092662"
								class="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
							/>
							<p class="mt-1 text-xs text-muted-foreground">Optional. Leave blank if unknown.</p>
						</div>

						<div class="grid grid-cols-2 gap-3">
							<div>
								<label class="text-sm font-medium" for="first-name">First Name</label>
								<input
									id="first-name"
									type="text"
									bind:value={form.first_name}
									placeholder="Yui"
									class="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
								/>
							</div>
							<div>
								<label class="text-sm font-medium" for="last-name">Last Name</label>
								<input
									id="last-name"
									type="text"
									bind:value={form.last_name}
									placeholder="Hatano"
									class="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
								/>
							</div>
						</div>

						<div>
							<label class="text-sm font-medium" for="ja-name">Japanese Name</label>
							<input
								id="ja-name"
								type="text"
								bind:value={form.japanese_name}
								placeholder="波多野結衣"
								class="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
							/>
						</div>

						<div>
							<label class="text-sm font-medium" for="thumb-url">Thumbnail URL</label>
							<input
								id="thumb-url"
								type="url"
								bind:value={form.thumb_url}
								placeholder="https://example.com/actress.jpg"
								class="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
							/>
						</div>

						<div>
							<label class="text-sm font-medium" for="aliases">Aliases</label>
							<input
								id="aliases"
								type="text"
								bind:value={form.aliases}
								placeholder="Alias1|Alias2"
								class="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
							/>
						</div>
					</div>

						<div class="flex items-center gap-2 pt-2">
							<Button onclick={saveActress} disabled={saveActressMutation.isPending}>
								<Save class="h-4 w-4" />
								{saveActressMutation.isPending ? 'Saving...' : editingId ? 'Update' : 'Create'}
							</Button>
							<Button variant="outline" onclick={resetForm} disabled={saveActressMutation.isPending}>Clear</Button>
						</div>
					</Card>
				</div>
			</div>

			<div class="xl:col-span-3 space-y-4">
				<div in:fly|local={{ x: 14, duration: 260, easing: cubicOut }}>
					<Card class="p-4">
						<div class="flex flex-wrap items-center gap-2">
						<div class="flex-1 min-w-55">
							<label class="sr-only" for="search">Search actresses</label>
							<div class="relative">
								<Search class="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
								<input
									id="search"
									type="text"
									bind:value={queryInput}
									onkeydown={(event) => {
										if (event.key === 'Enter') applySearch();
									}}
									placeholder="Search by English or Japanese name"
									class="w-full rounded-md border border-input bg-background pl-9 pr-3 py-2 text-sm"
								/>
							</div>
						</div>
						<Button onclick={applySearch}>Search</Button>
						<Button variant="outline" onclick={clearSearch}>Clear</Button>
					</div>
					<div class="mt-3 flex flex-wrap items-center justify-between gap-3">
						<div class="inline-flex rounded-md border border-input p-1">
							<Button
								size="sm"
								variant={viewMode === 'cards' ? 'default' : 'ghost'}
								onclick={() => {
									viewMode = 'cards';
								}}
							>
								Cards
							</Button>
							<Button
								size="sm"
								variant={viewMode === 'compact' ? 'default' : 'ghost'}
								onclick={() => {
									viewMode = 'compact';
								}}
							>
								Compact
							</Button>
							<Button
								size="sm"
								variant={viewMode === 'table' ? 'default' : 'ghost'}
								onclick={() => {
									viewMode = 'table';
								}}
							>
								Table
							</Button>
						</div>
						<div class="flex items-center gap-2">
							<select
								bind:value={sortBy}
								onchange={applySort}
								class="rounded-md border border-input bg-background px-3 py-2 text-sm"
								aria-label="Sort actresses by"
							>
								<option value="name">Sort: Name</option>
								<option value="japanese_name">Sort: Japanese Name</option>
								<option value="id">Sort: Database ID</option>
								<option value="dmm_id">Sort: DMM ID</option>
								<option value="updated_at">Sort: Updated Time</option>
								<option value="created_at">Sort: Created Time</option>
							</select>
							<Button variant="outline" size="sm" onclick={toggleSortOrder} title="Toggle sort direction">
								<ArrowUpDown class="h-4 w-4" />
								{sortOrder === 'asc' ? 'Asc' : 'Desc'}
							</Button>
						</div>
					</div>
						<div class="mt-3 text-sm text-muted-foreground">
							Showing {actresses.length} of {total} actress records
							{#if activeQuery}
								for "{activeQuery}"
							{/if}
						</div>
						<div class="mt-3 flex flex-wrap items-center gap-2 rounded-md border border-input bg-muted/20 px-3 py-2">
							<span class="text-sm">
								{selectedIds.length} selected
							</span>
							<Button variant="outline" size="sm" onclick={selectCurrentPage}>Select Page</Button>
							<Button variant="outline" size="sm" onclick={clearSelection} disabled={selectedIds.length === 0}>
								Clear
							</Button>
							<Button size="sm" onclick={startMergeSelected} disabled={selectedIds.length < 2}>
								<GitMerge class="h-4 w-4" />
								Merge Selected
							</Button>
						</div>
					</Card>
				</div>

				{#if error}
					<div in:fly|local={{ y: 8, duration: 180 }}>
						<Card class="p-4 border-destructive bg-destructive/10 text-destructive">
							{error}
						</Card>
					</div>
				{/if}

				{#if loading}
					<div in:fade|local={{ duration: 180 }}>
						<Card class="p-8 text-center text-muted-foreground">Loading actresses...</Card>
					</div>
				{:else if actresses.length === 0}
					<div in:fade|local={{ duration: 180 }}>
						<Card class="p-8 text-center">
							<p class="text-muted-foreground">No actresses found.</p>
						</Card>
					</div>
				{:else}
					{#key viewMode}
						<div in:scale|local={{ start: 0.98, duration: 180, easing: quintOut }} out:fade|local={{ duration: 120 }}>
							{#if viewMode === 'cards'}
								<div class="grid grid-cols-1 md:grid-cols-2 gap-3">
									{#each actresses as actress, index (`${actress.id ?? 'na'}-${index}`)}
										<div animate:flip={{ duration: 220, easing: quintOut }} in:fly|local={{ y: 10, duration: 220, delay: itemDelay(index), easing: quintOut }}>
											<Card class="p-3 h-full {isSelected(actress) ? 'ring-2 ring-primary' : ''}">
												<div class="flex items-start gap-3 h-full">
													<div class="pt-1">
														<input
															type="checkbox"
															checked={isSelected(actress)}
															disabled={!actress.id}
															onchange={() => toggleSelection(actress)}
															aria-label="Select actress for merge"
															class="rounded border-input"
														/>
													</div>
													{#if actress.thumb_url}
														<img
															src={actress.thumb_url}
															alt={getDisplayName(actress)}
															class="w-20 h-24 rounded object-cover border"
															onerror={(event) => {
																(event.currentTarget as HTMLImageElement).style.display = 'none';
															}}
														/>
													{:else}
														<div class="w-20 h-24 rounded border bg-muted flex items-center justify-center text-muted-foreground">
															<ImageOff class="h-4 w-4" />
														</div>
													{/if}

													<div class="flex-1 min-w-0">
														<div class="flex flex-wrap items-center gap-2">
															<h3 class="font-semibold truncate">{getDisplayName(actress)}</h3>
															{#if actress.id}
																<span class="text-xs rounded bg-muted px-2 py-0.5">#{actress.id}</span>
															{/if}
															{#if actress.dmm_id && actress.dmm_id > 0}
																<span class="text-xs rounded bg-muted px-2 py-0.5">DMM {actress.dmm_id}</span>
															{/if}
														</div>
														{#if actress.japanese_name}
															<p class="text-sm text-muted-foreground truncate">{actress.japanese_name}</p>
														{/if}
														{#if actress.aliases}
															<p class="text-xs text-muted-foreground line-clamp-2 mt-1">Aliases: {actress.aliases}</p>
														{/if}
														<div class="flex items-center gap-2 mt-3">
															<Button variant="outline" size="sm" onclick={() => startEdit(actress)}>
																<Pencil class="h-4 w-4" />
																Edit
															</Button>
															<Button
																variant="outline"
																size="sm"
																onclick={() => removeActress(actress)}
																disabled={deleteActressMutation.isPending}
																class="text-destructive hover:bg-destructive/10"
															>
																<Trash2 class="h-4 w-4" />
																Delete
															</Button>
														</div>
													</div>
												</div>
											</Card>
										</div>
									{/each}
								</div>
							{:else if viewMode === 'compact'}
								<div class="space-y-2">
									{#each actresses as actress, index (`${actress.id ?? 'na'}-${index}`)}
										<div animate:flip={{ duration: 220, easing: quintOut }} in:fly|local={{ y: 8, duration: 190, delay: itemDelay(index), easing: quintOut }}>
											<Card class="p-3 {isSelected(actress) ? 'ring-2 ring-primary' : ''}">
												<div class="flex items-center gap-3">
													<input
														type="checkbox"
														checked={isSelected(actress)}
														disabled={!actress.id}
														onchange={() => toggleSelection(actress)}
														aria-label="Select actress for merge"
														class="rounded border-input"
													/>
													<div class="flex-1 min-w-0">
														<div class="flex items-center gap-2 min-w-0">
															<p class="font-medium truncate">{getDisplayName(actress)}</p>
															{#if actress.id}
																<span class="text-xs rounded bg-muted px-2 py-0.5">#{actress.id}</span>
															{/if}
															{#if actress.dmm_id && actress.dmm_id > 0}
																<span class="text-xs rounded bg-muted px-2 py-0.5">DMM {actress.dmm_id}</span>
															{/if}
														</div>
														<p class="text-xs text-muted-foreground truncate">
															{actress.japanese_name || '-'}
														</p>
													</div>
													<div class="flex items-center gap-2">
														<Button variant="outline" size="sm" onclick={() => startEdit(actress)}>
															<Pencil class="h-4 w-4" />
														</Button>
														<Button
															variant="outline"
															size="sm"
															onclick={() => removeActress(actress)}
															disabled={deleteActressMutation.isPending}
															class="text-destructive hover:bg-destructive/10"
														>
															<Trash2 class="h-4 w-4" />
														</Button>
													</div>
												</div>
											</Card>
										</div>
									{/each}
								</div>
							{:else}
								<Card class="overflow-hidden">
									<div class="overflow-x-auto">
										<table class="w-full text-sm">
											<thead class="bg-muted/50">
												<tr class="text-left border-b">
													<th class="px-3 py-2 font-medium w-10">Sel</th>
													<th class="px-3 py-2 font-medium">ID</th>
													<th class="px-3 py-2 font-medium">Name</th>
													<th class="px-3 py-2 font-medium">Japanese Name</th>
													<th class="px-3 py-2 font-medium">DMM ID</th>
													<th class="px-3 py-2 font-medium">Aliases</th>
													<th class="px-3 py-2 font-medium text-right">Actions</th>
												</tr>
											</thead>
											<tbody>
												{#each actresses as actress, index (`${actress.id ?? 'na'}-${index}`)}
													<tr class="border-b last:border-b-0 {isSelected(actress) ? 'bg-primary/5' : ''}" in:fly|local={{ y: 6, duration: 170, delay: itemDelay(index), easing: quintOut }}>
														<td class="px-3 py-2 text-muted-foreground">
															<input
																type="checkbox"
																checked={isSelected(actress)}
																disabled={!actress.id}
																onchange={() => toggleSelection(actress)}
																aria-label="Select actress for merge"
																class="rounded border-input"
															/>
														</td>
														<td class="px-3 py-2 text-muted-foreground">{actress.id ?? '-'}</td>
														<td class="px-3 py-2 font-medium max-w-44 truncate">{getDisplayName(actress)}</td>
														<td class="px-3 py-2 text-muted-foreground max-w-44 truncate">{actress.japanese_name || '-'}</td>
														<td class="px-3 py-2 text-muted-foreground">{actress.dmm_id && actress.dmm_id > 0 ? actress.dmm_id : '-'}</td>
														<td class="px-3 py-2 text-muted-foreground max-w-52 truncate">{actress.aliases || '-'}</td>
														<td class="px-3 py-2">
															<div class="flex items-center justify-end gap-2">
																<Button variant="outline" size="sm" onclick={() => startEdit(actress)}>
																	<Pencil class="h-4 w-4" />
																</Button>
																<Button
																	variant="outline"
																	size="sm"
																	onclick={() => removeActress(actress)}
																	disabled={deleteActressMutation.isPending}
																	class="text-destructive hover:bg-destructive/10"
																>
																	<Trash2 class="h-4 w-4" />
																</Button>
															</div>
														</td>
													</tr>
												{/each}
											</tbody>
										</table>
									</div>
								</Card>
							{/if}
						</div>
					{/key}
				{/if}

				<Card class="p-3">
					<div class="flex items-center justify-between text-sm">
						<div class="text-muted-foreground">
							Page {currentPage} of {totalPages}
						</div>
						<div class="flex items-center gap-2">
							<Button variant="outline" size="sm" onclick={prevPage} disabled={!canGoPrev}>
								<ChevronLeft class="h-4 w-4" />
								Prev
							</Button>
							<Button variant="outline" size="sm" onclick={nextPage} disabled={!canGoNext}>
								Next
								<ChevronRight class="h-4 w-4" />
							</Button>
						</div>
					</div>
				</Card>
			</div>
		</div>
	</div>
</div>

{#if showMergeModal}
	<div class="fixed inset-0 z-50 bg-black/50 p-4 flex items-center justify-center">
		<Card class="w-full max-w-3xl max-h-[90vh] overflow-hidden">
			<div class="p-4 border-b flex items-center justify-between">
				<h2 class="text-lg font-semibold">Merge Selected Actresses</h2>
				<Button variant="outline" size="sm" onclick={closeMergeModal}>Close</Button>
			</div>

			<div class="p-4 space-y-4 overflow-auto max-h-[70vh]">
				{#if selectedIds.length < 2}
					<p class="text-sm text-muted-foreground">Select at least two actresses from the current page to merge.</p>
				{:else}
					<div class="space-y-2">
						<label class="text-sm font-medium" for="merge-primary">Primary actress to keep</label>
						<select
							id="merge-primary"
							value={mergePrimaryId ?? ''}
							class="rounded-md border border-input bg-background px-3 py-2 text-sm"
							onchange={(event) => {
								const value = Number.parseInt((event.currentTarget as HTMLSelectElement).value, 10);
								mergePrimaryId = Number.isNaN(value) ? null : value;
								void resetMergeQueueAndPreview();
							}}
						>
							{#each selectedIds as selectedID}
								<option value={selectedID}>{getActressLabelByID(selectedID)}</option>
							{/each}
						</select>
					</div>

					<div class="rounded-md border border-input bg-muted/20 px-3 py-2 text-sm">
						Queue: {mergeSourceQueue.length} remaining
						{#if mergeCurrentSourceId}
							• processing source #{mergeCurrentSourceId}
						{/if}
					</div>

					{#if mergeLoadingPreview}
						<p class="text-sm text-muted-foreground">Loading merge preview...</p>
					{:else if mergePreview && mergeCurrentSourceId}
						<div class="space-y-3">
							<div class="text-sm">
								Review conflicts for <span class="font-medium">#{mergeCurrentSourceId}</span> -> <span class="font-medium">#{mergePrimaryId}</span>
							</div>

							{#if mergePreview.conflicts.length === 0}
								<div class="rounded-md border border-input bg-muted/20 px-3 py-2 text-sm">
									No field conflicts. Safe to merge with defaults.
								</div>
							{:else}
								<div class="space-y-2">
									{#each mergePreview.conflicts as conflict}
										<div class="rounded-md border border-input p-3 space-y-2">
											<div class="font-medium text-sm">{conflict.field}</div>
											<div class="grid grid-cols-1 md:grid-cols-2 gap-3 text-sm">
												<label class="rounded-md border border-input p-2 flex items-start gap-2 cursor-pointer">
													<input
														type="radio"
														name={`conflict-${conflict.field}`}
														checked={(mergeResolutions[conflict.field] || conflict.default_resolution) === 'target'}
														onchange={() => setResolution(conflict.field, 'target')}
													/>
													<span>
														<span class="font-medium">Keep target</span><br />
														<span class="text-muted-foreground">{formatMergeValue(conflict.target_value)}</span>
													</span>
												</label>
												<label class="rounded-md border border-input p-2 flex items-start gap-2 cursor-pointer">
													<input
														type="radio"
														name={`conflict-${conflict.field}`}
														checked={(mergeResolutions[conflict.field] || conflict.default_resolution) === 'source'}
														onchange={() => setResolution(conflict.field, 'source')}
													/>
													<span>
														<span class="font-medium">Use source</span><br />
														<span class="text-muted-foreground">{formatMergeValue(conflict.source_value)}</span>
													</span>
												</label>
											</div>
										</div>
									{/each}
								</div>
							{/if}

							<div class="flex items-center gap-2">
								<Button variant="outline" onclick={skipCurrentMerge} disabled={mergeRunning}>
									Skip
								</Button>
								<Button onclick={applyCurrentMerge} disabled={mergeRunning}>
									{mergeRunning ? 'Merging...' : 'Apply Merge'}
								</Button>
							</div>
						</div>
					{:else}
						<div class="rounded-md border border-input bg-green-500/10 px-3 py-2 text-sm">
							Queue complete.
						</div>
					{/if}
				{/if}

				{#if mergeSummary.messages.length > 0}
					<div class="space-y-2">
						<div class="text-sm font-medium">
							Summary: {mergeSummary.success} succeeded, {mergeSummary.failed} failed
						</div>
						<div class="max-h-40 overflow-auto rounded-md border border-input p-2 text-xs space-y-1">
							{#each mergeSummary.messages as message, idx (`merge-log-${idx}`)}
								<div>{message}</div>
							{/each}
						</div>
					</div>
				{/if}
			</div>
		</Card>
	</div>
{/if}
