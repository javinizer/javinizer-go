<script lang="ts">
	import Button from '$lib/components/ui/Button.svelte';
	import { ChevronDown, ChevronUp, Image, LayoutGrid, List, LoaderCircle, Play, RefreshCw, Settings2, X, CheckSquare, Square, Trash2, RotateCcw, MousePointerClick } from 'lucide-svelte';
	import type { CompletenessTier } from '$lib/utils/completeness';

	interface Props {
		isUpdateMode: boolean;
		canOrganize: boolean;
		organizing: boolean;
		movieResultsLength: number;
		destinationPath: string;
		viewMode?: 'detail' | 'grid-poster' | 'grid-cover';
		forceOverwrite?: boolean;
		preserveNfo?: boolean;
		skipNfo?: boolean;
		skipDownload?: boolean;
		selectedCount?: number;
		allSelected?: boolean;
		bulkExcluding?: boolean;
		bulkRescraping?: boolean;
		completenessFilter?: Set<CompletenessTier>;
		tierCounts?: Record<string, number>;
		selectionMode?: boolean;
		onToggleCompletenessTier?: (tier: CompletenessTier) => void;
		onToggleSelectionMode?: () => void;
		onSelectAll?: () => void;
		onDeselectAll?: () => void;
		onBulkExclude?: () => void;
		onBulkRescrape?: () => void;
		onClose: () => void;
		onUpdateAll: () => void;
		onOrganizeAll: () => void;
	}

		let {
		isUpdateMode,
		canOrganize,
		organizing,
		movieResultsLength,
		destinationPath,
		viewMode = $bindable<'detail' | 'grid-poster' | 'grid-cover'>('detail'),
		forceOverwrite = $bindable(false),
		preserveNfo = $bindable(false),
		skipNfo = $bindable(false),
		skipDownload = $bindable(false),
		selectedCount = 0,
		allSelected = false,
		bulkExcluding = false,
		bulkRescraping = false,
		completenessFilter = new Set<CompletenessTier>(['incomplete', 'partial', 'complete']),
		tierCounts = { incomplete: 0, partial: 0, complete: 0 },
		selectionMode = false,
		onToggleCompletenessTier,
		onToggleSelectionMode,
		onSelectAll,
		onDeselectAll,
		onBulkExclude,
		onBulkRescrape,
		onClose,
		onUpdateAll,
		onOrganizeAll
	}: Props = $props();

	$effect(() => {
		if (forceOverwrite) preserveNfo = false;
	});

	$effect(() => {
		if (preserveNfo) forceOverwrite = false;
	});

	let showOptions = $state(false);

	const tierConfig: { tier: CompletenessTier; label: string; dotClass: string }[] = [
		{ tier: 'incomplete', label: 'Incomplete', dotClass: 'bg-red-500' },
		{ tier: 'partial', label: 'Partial', dotClass: 'bg-yellow-500' },
		{ tier: 'complete', label: 'Complete', dotClass: 'bg-green-500' },
	];
</script>

<div class="flex items-center justify-between mb-6">
	<div>
		<h1 class="text-3xl font-bold">Review & Edit Metadata</h1>
		<p class="text-muted-foreground mt-1">
			{#if isUpdateMode}
				Metadata and media files have been updated in place. Review and edit as needed.
			{:else}
				Review and edit scraped metadata before organizing files
			{/if}
		</p>
	</div>
	<div class="flex items-center gap-3">
		<div class="inline-flex rounded-md border border-input p-1">
			<Button
				size="sm"
				variant={viewMode === 'detail' ? 'default' : 'ghost'}
				onclick={() => { viewMode = 'detail'; }}
			>
				{#snippet children()}
					<List class="h-4 w-4 mr-1" />
					Detail
				{/snippet}
			</Button>
			<Button
				size="sm"
				variant={viewMode === 'grid-poster' ? 'default' : 'ghost'}
				onclick={() => { viewMode = 'grid-poster'; }}
			>
				{#snippet children()}
					<LayoutGrid class="h-4 w-4 mr-1" />
					Poster
				{/snippet}
			</Button>
			<Button
				size="sm"
				variant={viewMode === 'grid-cover' ? 'default' : 'ghost'}
				onclick={() => { viewMode = 'grid-cover'; }}
			>
				{#snippet children()}
					<Image class="h-4 w-4 mr-1" />
					Cover
				{/snippet}
			</Button>
		</div>
		<Button variant="outline" onclick={onClose} disabled={organizing}>
			{#snippet children()}
				<X class="h-4 w-4 mr-2" />
				{isUpdateMode ? 'Close' : 'Cancel'}
			{/snippet}
		</Button>
		{#if isUpdateMode}
			<Button onclick={onUpdateAll} disabled={organizing}>
				{#snippet children()}
					{#if organizing}
						<LoaderCircle class="h-4 w-4 mr-2 animate-spin" />
					{:else}
						<RefreshCw class="h-4 w-4 mr-2" />
					{/if}
					{organizing ? 'Updating...' : `Update ${movieResultsLength} File${movieResultsLength !== 1 ? 's' : ''}`}
				{/snippet}
			</Button>
		{:else}
			<Button onclick={onOrganizeAll} disabled={organizing || !canOrganize || !destinationPath.trim()}>
				{#snippet children()}
					{#if organizing}
						<LoaderCircle class="h-4 w-4 mr-2 animate-spin" />
					{:else}
						<Play class="h-4 w-4 mr-2" />
					{/if}
					{organizing ? 'Organizing...' : `Organize ${movieResultsLength} File${movieResultsLength !== 1 ? 's' : ''}`}
				{/snippet}
			</Button>
		{/if}
	</div>
</div>

{#if viewMode === 'grid-poster' || viewMode === 'grid-cover'}
	<div class="flex items-center gap-3 mb-4">
		<Button
			size="sm"
			variant={selectionMode ? 'default' : 'outline'}
			aria-pressed={selectionMode}
			onclick={() => onToggleSelectionMode?.()}
		>
			{#snippet children()}
				<MousePointerClick class="h-4 w-4 mr-1" />
				Select
			{/snippet}
		</Button>
		{#if selectionMode}
			<Button
				size="sm"
				variant="outline"
				onclick={allSelected ? onDeselectAll : onSelectAll}
			>
				{#snippet children()}
					{#if allSelected}
						<CheckSquare class="h-4 w-4 mr-1" />
						Deselect All
					{:else}
						<Square class="h-4 w-4 mr-1" />
						Select All
					{/if}
				{/snippet}
			</Button>
		{/if}
		<div class="h-4 w-px bg-border"></div>
		<div class="inline-flex items-center gap-1">
			{#each tierConfig as { tier, label, dotClass }}
				{@const count = tierCounts[tier] ?? 0}
				{@const isActive = completenessFilter.has(tier)}
				<button
					class="inline-flex items-center gap-1.5 h-9 px-3 text-sm font-medium rounded-md border transition-colors
						{isActive ? 'bg-secondary text-secondary-foreground border-border' : 'bg-transparent text-muted-foreground border-transparent hover:bg-accent hover:text-accent-foreground'}
						{count === 0 ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'}"
					onclick={() => onToggleCompletenessTier?.(tier)}
					disabled={count === 0}
				>
					<span class="w-2 h-2 rounded-full {isActive ? dotClass : 'bg-muted-foreground/30'}"></span>
					{label} ({count})
				</button>
			{/each}
		</div>
	</div>
{/if}

{#if selectedCount > 0}
	<div class="flex items-center gap-3 mb-4 px-1">
		<span class="text-sm font-medium text-muted-foreground">
			{selectedCount} selected
		</span>
		<div class="h-4 w-px bg-border"></div>
		<Button size="sm" variant="outline" onclick={onBulkExclude} disabled={bulkExcluding || bulkRescraping} class="text-orange-600 hover:text-orange-700 dark:text-orange-400 dark:hover:text-orange-300">
			{#snippet children()}
				{#if bulkExcluding}
					<LoaderCircle class="h-4 w-4 mr-1 animate-spin" />
				{:else}
					<Trash2 class="h-4 w-4 mr-1" />
				{/if}
				Remove
			{/snippet}
		</Button>
		<Button size="sm" variant="outline" onclick={onBulkRescrape} disabled={bulkExcluding || bulkRescraping}>
			{#snippet children()}
				{#if bulkRescraping}
					<LoaderCircle class="h-4 w-4 mr-1 animate-spin" />
				{:else}
					<RotateCcw class="h-4 w-4 mr-1" />
				{/if}
				Rescrape
			{/snippet}
		</Button>
	</div>
{/if}

{#if isUpdateMode}
	<div class="mb-4">
		<button
			onclick={() => (showOptions = !showOptions)}
			class="flex items-center gap-2 text-sm font-medium text-muted-foreground hover:text-foreground transition-colors"
		>
			<Settings2 class="h-4 w-4" />
			Options
			{#if showOptions}
				<ChevronUp class="h-3 w-3" />
			{:else}
				<ChevronDown class="h-3 w-3" />
			{/if}
		</button>

		{#if showOptions}
			<div class="grid gap-3 md:grid-cols-4 mt-3">
				<label
					class="flex items-center gap-3 p-3 rounded-lg border border-border bg-background hover:bg-accent/50 cursor-pointer transition-colors"
				>
					<input
						type="checkbox"
						bind:checked={forceOverwrite}
						class="h-4 w-4 rounded border-input text-primary focus:ring-2 focus:ring-primary"
					/>
					<div class="flex-1">
						<span class="text-sm font-medium">Force Overwrite</span>
						<p class="text-xs text-muted-foreground">Ignore existing NFO, use only scraper data</p>
					</div>
				</label>

				<label
					class="flex items-center gap-3 p-3 rounded-lg border border-border bg-background hover:bg-accent/50 cursor-pointer transition-colors"
				>
					<input
						type="checkbox"
						bind:checked={preserveNfo}
						class="h-4 w-4 rounded border-input text-primary focus:ring-2 focus:ring-primary"
					/>
					<div class="flex-1">
						<span class="text-sm font-medium">Preserve NFO</span>
						<p class="text-xs text-muted-foreground">Never overwrite NFO fields, only add missing</p>
					</div>
				</label>

				<label
					class="flex items-center gap-3 p-3 rounded-lg border border-border bg-background hover:bg-accent/50 cursor-pointer transition-colors"
				>
					<input
						type="checkbox"
						bind:checked={skipNfo}
						class="h-4 w-4 rounded border-input text-primary focus:ring-2 focus:ring-primary"
					/>
					<div class="flex-1">
						<span class="text-sm font-medium">Skip NFO</span>
						<p class="text-xs text-muted-foreground">Don't generate NFO metadata files</p>
					</div>
				</label>

				<label
					class="flex items-center gap-3 p-3 rounded-lg border border-border bg-background hover:bg-accent/50 cursor-pointer transition-colors"
				>
					<input
						type="checkbox"
						bind:checked={skipDownload}
						class="h-4 w-4 rounded border-input text-primary focus:ring-2 focus:ring-primary"
					/>
					<div class="flex-1">
						<span class="text-sm font-medium">Skip Download</span>
						<p class="text-xs text-muted-foreground">Don't download cover, poster, and screenshots</p>
					</div>
				</label>
			</div>
		{/if}
	</div>
{/if}
