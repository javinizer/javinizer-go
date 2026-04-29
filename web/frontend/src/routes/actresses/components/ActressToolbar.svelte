<script lang="ts">
	import { fly } from 'svelte/transition';
	import { cubicOut } from 'svelte/easing';
	import { Search, ArrowUpDown, GitMerge } from 'lucide-svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';

	let {
		queryInput = $bindable(),
		activeQuery,
		viewMode = $bindable(),
		sortBy = $bindable(),
		sortOrder,
		selectedIds,
		total,
		actressesCount,
		isRefreshing,
		onApplySearch,
		onClearSearch,
		onToggleSortOrder,
		onSelectCurrentPage,
		onClearSelection,
		onStartMergeSelected
	}: {
		queryInput: string;
		activeQuery: string;
		viewMode: 'cards' | 'compact' | 'table';
		sortBy: string;
		sortOrder: 'asc' | 'desc';
		selectedIds: number[];
		total: number;
		actressesCount: number;
		isRefreshing: boolean;
		onApplySearch: () => void;
		onClearSearch: () => void;
		onToggleSortOrder: () => void;
		onSelectCurrentPage: () => void;
		onClearSelection: () => void;
		onStartMergeSelected: () => void;
	} = $props();
</script>

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
							if (event.key === 'Enter') onApplySearch();
						}}
						placeholder="Search by English or Japanese name"
						class="w-full rounded-md border border-input bg-background pl-9 pr-3 py-2 text-sm"
					/>
				</div>
			</div>
			<Button onclick={onApplySearch}>Search</Button>
			<Button variant="outline" onclick={onClearSearch}>Clear</Button>
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
				<Button variant="outline" size="sm" onclick={onToggleSortOrder} title="Toggle sort direction">
					<ArrowUpDown class="h-4 w-4" />
					{sortOrder === 'asc' ? 'Asc' : 'Desc'}
				</Button>
			</div>
		</div>
		<div class="mt-3 text-sm text-muted-foreground">
			Showing {actressesCount} of {total} actress records
			{#if activeQuery}
				for "{activeQuery}"
			{/if}
		</div>
		<div class="mt-3 flex flex-wrap items-center gap-2 rounded-md border border-input bg-muted/20 px-3 py-2">
			<span class="text-sm">
				{selectedIds.length} selected
			</span>
			<Button variant="outline" size="sm" onclick={onSelectCurrentPage}>Select Page</Button>
			<Button variant="outline" size="sm" onclick={onClearSelection} disabled={selectedIds.length === 0}>
				Clear
			</Button>
			<Button size="sm" onclick={onStartMergeSelected} disabled={selectedIds.length < 2}>
				<GitMerge class="h-4 w-4" />
				Merge Selected
			</Button>
		</div>
	</Card>
</div>
