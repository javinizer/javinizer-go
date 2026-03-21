<script lang="ts">
	import { flip } from 'svelte/animate';
	import { quintOut } from 'svelte/easing';
	import { fade, scale } from 'svelte/transition';
	import { portalToBody } from '$lib/actions/portal';
	import type { Movie, Actress } from '$lib/api/types';
	import Button from './ui/Button.svelte';
	import Card from './ui/Card.svelte';
	import { Plus, SquarePen, Trash2, X, Save, Search } from 'lucide-svelte';

	interface Props {
		movie: Movie;
		onUpdate: (movie: Movie) => void;
		actressSources?: Record<string, string>;
		showFieldSources?: boolean;
	}

	let { movie, onUpdate, actressSources, showFieldSources = false }: Props = $props();

	let actresses = $state<Actress[]>([]);
	let showEditModal = $state(false);
	let editingIndex = $state<number | null>(null);
	let editingActress = $state<Actress>({
		first_name: '',
		last_name: '',
		japanese_name: '',
		thumb_url: ''
	});

	// Autocomplete state
	let searchQuery = $state('');
	let allActresses = $state<Actress[]>([]);
	let showSearchResults = $state(false);
	let isSearchFocused = $state(false);

	// Filtered results based on search query (client-side filtering)
	const filteredActresses = $derived(() => {
		if (!searchQuery || searchQuery.trim().length === 0) {
			return allActresses;
		}
		const query = searchQuery.toLowerCase();
		return allActresses.filter(actress => {
			const fullName = getFullName(actress).toLowerCase();
			const japaneseName = (actress.japanese_name || '').toLowerCase();
			return fullName.includes(query) || japaneseName.includes(query);
		});
	});

	// Sync actresses when movie prop changes
	$effect(() => {
		actresses = movie.actresses || [];
	});

	// Helper to get full name
	function getFullName(actress: Actress): string {
		if (actress.last_name && actress.first_name) {
			return `${actress.last_name} ${actress.first_name}`;
		}
		if (actress.first_name) {
			return actress.first_name;
		}
		return actress.japanese_name || 'Unknown';
	}

	// Update parent when actresses change
	$effect(() => {
		onUpdate({ ...movie, actresses });
	});

	function openAddActress() {
		editingIndex = null;
		editingActress = {
			first_name: '',
			last_name: '',
			japanese_name: '',
			thumb_url: ''
		};
		showEditModal = true;
		loadAllActresses(); // Load actresses when opening modal
	}

	function openEditActress(index: number) {
		editingIndex = index;
		editingActress = { ...actresses[index] };
		showEditModal = true;
		loadAllActresses(); // Load actresses when opening modal
	}

	function saveActress() {
		if (!editingActress.first_name?.trim() && !editingActress.japanese_name?.trim()) {
			alert('At least first name or Japanese name is required');
			return;
		}

		if (editingIndex !== null) {
			// Edit existing
			actresses[editingIndex] = editingActress;
		} else {
			// Add new
			actresses = [...actresses, editingActress];
		}

		showEditModal = false;
	}

	function removeActress(index: number) {
		if (confirm('Remove this actress?')) {
			actresses = actresses.filter((_, i) => i !== index);
		}
	}

	function cancelEdit() {
		showEditModal = false;
		// Reset search state when closing modal
		searchQuery = '';
		showSearchResults = false;
	}

	// Load all actresses on modal open
	async function loadAllActresses() {
		try {
			const response = await fetch(`/api/v1/actresses/search?q=`);
			if (response.ok) {
				allActresses = await response.json();
			}
		} catch (error) {
			console.error('Failed to load actresses:', error);
			allActresses = [];
		}
	}

	// Select an actress from search results
	function selectActressFromSearch(actress: Actress) {
		editingActress = { ...actress };
		searchQuery = getFullName(actress); // Show selected name in input
		showSearchResults = false;
	}

	// Handle input focus
	function handleSearchFocus() {
		showSearchResults = true;
	}

	// Handle input blur (with delay to allow click on dropdown)
	function handleSearchBlur() {
		setTimeout(() => {
			showSearchResults = false;
		}, 200);
	}

	function normalizeName(value: string | undefined): string {
		if (!value) return '';
		return value.trim().toLowerCase().split(/\s+/).filter(Boolean).join(' ');
	}

	function actressKey(actress: Actress): string {
		if (actress.dmm_id && actress.dmm_id > 0) return `dmmid:${actress.dmm_id}`;
		const japanese = normalizeName(actress.japanese_name);
		if (japanese) return `name:${japanese}`;
		const firstLast = normalizeName(`${actress.first_name || ''} ${actress.last_name || ''}`);
		if (firstLast) return `name:${firstLast}`;
		const lastFirst = normalizeName(`${actress.last_name || ''} ${actress.first_name || ''}`);
		if (lastFirst) return `name:${lastFirst}`;
		return '';
	}

	function sourceText(rawSource: string | undefined): string | null {
		if (!rawSource) return null;
		const source = rawSource.trim();
		if (!source) return null;

		const normalized = source.toLowerCase();
		if (normalized === 'nfo') return 'via NFO';
		if (normalized === 'merged') return 'via merged data';
		if (normalized === 'empty') return 'empty';
		return `via ${source}`;
	}

	function sourceForActress(actress: Actress): string | null {
		if (!showFieldSources || !actressSources) return null;

		const primaryKey = actressKey(actress);
		const rawSource = primaryKey ? actressSources[primaryKey] : undefined;
		if (rawSource) return sourceText(rawSource);

		// Fallback checks for alternate name-key combinations when backend/frontend
		// normalization differs slightly after user edits.
		const altKeys = [
			`name:${normalizeName(actress.japanese_name)}`,
			`name:${normalizeName(`${actress.first_name || ''} ${actress.last_name || ''}`)}`,
			`name:${normalizeName(`${actress.last_name || ''} ${actress.first_name || ''}`)}`
		].filter((key) => key !== 'name:');

		for (const key of altKeys) {
			const altSource = actressSources[key];
			if (altSource) return sourceText(altSource);
		}

		return null;
	}
</script>

<div class="space-y-4">
	<div class="flex items-center justify-between">
		<h3 class="text-lg font-semibold">Actresses ({actresses.length})</h3>
		<Button onclick={openAddActress} size="sm">
			{#snippet children()}
				<Plus class="h-4 w-4 mr-2" />
				Add Actress
			{/snippet}
		</Button>
	</div>

	{#if actresses.length === 0}
		<div class="text-center py-8 text-muted-foreground border-2 border-dashed rounded-lg">
			<p>No actresses added</p>
			<Button onclick={openAddActress} size="sm" class="mt-2">
				{#snippet children()}
					<Plus class="h-4 w-4 mr-2" />
					Add First Actress
				{/snippet}
			</Button>
		</div>
	{:else}
		<div class="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
			{#each actresses as actress, index (actress.id || `${actress.first_name}-${actress.last_name}-${actress.japanese_name}-${index}`)}
				<div animate:flip={{ duration: 220, easing: quintOut }}>
					<Card class="p-3 hover:shadow-md transition-shadow">
					<div class="space-y-2">
						{#if actress.thumb_url}
							<img
								src={actress.thumb_url}
								alt={getFullName(actress)}
								class="w-full aspect-2/3 object-cover rounded"
								onerror={(e) => {
									(e.currentTarget as HTMLImageElement).src =
										'https://via.placeholder.com/200x300?text=No+Image';
								}}
							/>
						{:else}
							<div
								class="w-full aspect-2/3 bg-accent rounded flex items-center justify-center text-xs text-muted-foreground"
							>
								No Image
							</div>
						{/if}

						<div class="space-y-1">
							<p class="font-medium text-sm truncate" title={getFullName(actress)}>
								{getFullName(actress)}
							</p>
							{#if actress.japanese_name}
								<p class="text-xs text-muted-foreground truncate" title={actress.japanese_name}>
									{actress.japanese_name}
								</p>
							{/if}
							{#if sourceForActress(actress)}
								<p class="text-xs text-muted-foreground">{sourceForActress(actress)}</p>
							{/if}
						</div>

						<div class="flex gap-1">
							<Button variant="outline" size="sm" onclick={() => openEditActress(index)} class="flex-1">
								{#snippet children()}
									<SquarePen class="h-3 w-3" />
								{/snippet}
							</Button>
							<Button
								variant="outline"
								size="sm"
								onclick={() => removeActress(index)}
								class="flex-1 text-destructive hover:bg-destructive/10"
							>
								{#snippet children()}
									<Trash2 class="h-3 w-3" />
								{/snippet}
							</Button>
						</div>
					</div>
					</Card>
				</div>
			{/each}
		</div>
	{/if}
</div>

<!-- Edit/Add Actress Modal -->
{#if showEditModal}
	<div class="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4" use:portalToBody in:fade|local={{ duration: 140 }} out:fade|local={{ duration: 120 }}>
		<div in:scale|local={{ start: 0.97, duration: 180, easing: quintOut }} out:scale|local={{ start: 1, opacity: 0.75, duration: 130, easing: quintOut }} class="w-full max-w-2xl">
			<Card class="w-full flex flex-col max-h-[90vh]">
			<!-- Header -->
			<div class="p-6 border-b flex items-center justify-between">
				<h2 class="text-xl font-bold">
					{editingIndex !== null ? 'Edit Actress' : 'Add Actress'}
				</h2>
				<Button variant="ghost" size="icon" onclick={cancelEdit}>
					{#snippet children()}
						<X class="h-4 w-4" />
					{/snippet}
				</Button>
			</div>

			<!-- Body -->
			<div class="flex-1 overflow-auto p-6 space-y-4">
				<!-- Search Section -->
				<div class="space-y-2">
					<label class="text-sm font-medium flex items-center gap-2">
						<Search class="h-4 w-4" />
						Select or Search Actress
					</label>
					<div class="relative">
						<input
							type="text"
							bind:value={searchQuery}
							onfocus={handleSearchFocus}
							onblur={handleSearchBlur}
							placeholder="Click to see all actresses or type to search..."
							class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all"
						/>

						{#if showSearchResults}
							{#if filteredActresses().length > 0}
								<div class="absolute z-10 w-full mt-1 bg-background border rounded-md shadow-lg max-h-64 overflow-y-auto">
									{#each filteredActresses() as actress}
										<button
											type="button"
											onclick={() => selectActressFromSearch(actress)}
											class="w-full px-3 py-2 hover:bg-gray-100 dark:hover:bg-gray-800 text-left flex items-center gap-3 border-b last:border-b-0 transition-colors cursor-pointer"
										>
											{#if actress.thumb_url}
												<img
													src={actress.thumb_url}
													alt={getFullName(actress)}
													class="w-12 h-16 object-cover rounded"
												/>
											{:else}
												<div class="w-12 h-16 bg-accent rounded flex items-center justify-center text-xs">
													No Img
												</div>
											{/if}
											<div class="flex-1">
												<p class="font-medium text-sm">{getFullName(actress)}</p>
												{#if actress.japanese_name}
													<p class="text-xs text-muted-foreground">{actress.japanese_name}</p>
												{/if}
											</div>
										</button>
									{/each}
								</div>
							{:else if allActresses.length === 0}
								<div class="absolute z-10 w-full mt-1 bg-background border rounded-md shadow-lg p-3 text-sm text-muted-foreground text-center">
									No actresses in database yet. Add your first one below!
								</div>
							{:else}
								<div class="absolute z-10 w-full mt-1 bg-background border rounded-md shadow-lg p-3 text-sm text-muted-foreground text-center">
									No matches found
								</div>
							{/if}
						{/if}
					</div>
					<p class="text-xs text-muted-foreground">
						{#if allActresses.length > 0}
							{allActresses.length} actress{allActresses.length === 1 ? '' : 'es'} in database - select from dropdown or enter details manually below
						{:else}
							Enter actress details manually below
						{/if}
					</p>
				</div>

				<div class="relative">
					<div class="absolute inset-0 flex items-center">
						<div class="w-full border-t"></div>
					</div>
					<div class="relative flex justify-center text-xs uppercase">
						<span class="bg-background px-2 text-muted-foreground">Or enter manually</span>
					</div>
				</div>

				<div class="grid md:grid-cols-2 gap-6">
					<!-- Left: Form -->
					<div class="space-y-4">
						<div>
							<label class="text-sm font-medium mb-1 block" for="actress-first-name">First Name</label>
							<input
								id="actress-first-name"
								type="text"
								bind:value={editingActress.first_name}
								placeholder="e.g., Yume"
								class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all"
							/>
						</div>

						<div>
							<label class="text-sm font-medium mb-1 block" for="actress-last-name">Last Name</label>
							<input
								id="actress-last-name"
								type="text"
								bind:value={editingActress.last_name}
								placeholder="e.g., Nishimiya"
								class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all"
							/>
						</div>

						<div>
							<label class="text-sm font-medium mb-1 block" for="actress-japanese-name">Japanese Name</label>
							<input
								id="actress-japanese-name"
								type="text"
								bind:value={editingActress.japanese_name}
								placeholder="e.g., 西宮ゆめ"
								class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all"
							/>
						</div>

						<div>
							<label class="text-sm font-medium mb-1 block" for="actress-thumb-url">Thumbnail URL</label>
							<input
								id="actress-thumb-url"
								type="url"
								bind:value={editingActress.thumb_url}
								placeholder="https://..."
								class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono text-sm"
							/>
						</div>
					</div>

					<!-- Right: Preview -->
					<div>
						<span class="text-sm font-medium mb-1 block">Preview</span>
						<Card class="p-3">
							{#if editingActress.thumb_url}
								<img
									src={editingActress.thumb_url}
									alt={getFullName(editingActress) || 'Preview'}
									class="w-full aspect-2/3 object-cover rounded mb-2"
									onerror={(e) => {
										const target = e.currentTarget as HTMLImageElement; target.style.display = 'none';
									}}
								/>
							{:else}
								<div
									class="w-full aspect-2/3 bg-accent rounded flex items-center justify-center text-sm text-muted-foreground mb-2"
								>
									No Thumbnail
								</div>
							{/if}
							<p class="font-medium text-sm truncate">
								{getFullName(editingActress) || 'Name'}
							</p>
							{#if editingActress.japanese_name}
								<p class="text-xs text-muted-foreground truncate">
									{editingActress.japanese_name}
								</p>
							{/if}
						</Card>
					</div>
				</div>
			</div>

			<!-- Footer -->
			<div class="p-6 border-t flex items-center justify-end gap-3">
				<Button variant="outline" onclick={cancelEdit}>
					{#snippet children()}Cancel{/snippet}
				</Button>
				<Button onclick={saveActress}>
					{#snippet children()}
						<Save class="h-4 w-4 mr-2" />
						{editingIndex !== null ? 'Save Changes' : 'Add Actress'}
					{/snippet}
				</Button>
			</div>
			</Card>
		</div>
	</div>
{/if}
