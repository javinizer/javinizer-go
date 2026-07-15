<script lang="ts">
	import { fly } from 'svelte/transition';
	import { cubicOut } from 'svelte/easing';
	import { Search, ArrowUpDown, GitMerge } from 'lucide-svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import * as m from '$lib/paraglide/messages';

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
				<label class="sr-only" for="search">{m.actresses_search_label()}</label>
				<div class="relative">
					<Search class="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
					<input
						id="search"
						type="text"
						bind:value={queryInput}
						onkeydown={(event) => {
							if (event.key === 'Enter') onApplySearch();
						}}
						placeholder={m.actresses_search_placeholder()}
						class="w-full rounded-md border border-input bg-background pl-9 pr-3 py-2 text-sm"
					/>
				</div>
			</div>
			<Button onclick={onApplySearch}>{m.common_search()}</Button>
			<Button variant="outline" onclick={onClearSearch}>{m.common_clear()}</Button>
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
					{m.actresses_view_cards()}
				</Button>
				<Button
					size="sm"
					variant={viewMode === 'compact' ? 'default' : 'ghost'}
					onclick={() => {
						viewMode = 'compact';
					}}
				>
					{m.actresses_view_compact()}
				</Button>
				<Button
					size="sm"
					variant={viewMode === 'table' ? 'default' : 'ghost'}
					onclick={() => {
						viewMode = 'table';
					}}
				>
					{m.actresses_view_table()}
				</Button>
			</div>
			<div class="flex items-center gap-2">
				<select
					bind:value={sortBy}
					class="rounded-md border border-input bg-background px-3 py-2 text-sm"
					aria-label={m.actresses_sort_aria()}
				>
					<option value="name">{m.actresses_sort_name()}</option>
					<option value="japanese_name">{m.actresses_sort_japanese_name()}</option>
					<option value="id">{m.actresses_sort_id()}</option>
					<option value="dmm_id">{m.actresses_sort_dmm_id()}</option>
					<option value="updated_at">{m.actresses_sort_updated_at()}</option>
					<option value="created_at">{m.actresses_sort_created_at()}</option>
				</select>
				<Button variant="outline" size="sm" onclick={onToggleSortOrder} title={m.actresses_toggle_sort_title()}>
					<ArrowUpDown class="h-4 w-4" />
					{sortOrder === 'asc' ? m.actresses_asc() : m.actresses_desc()}
				</Button>
			</div>
		</div>
		<div class="mt-3 text-sm text-muted-foreground">
			{m.actresses_showing_count({ shown: actressesCount, total })}
			{#if activeQuery}
				for "{activeQuery}"
			{/if}
		</div>
		<div class="mt-3 flex flex-wrap items-center gap-2 rounded-md border border-input bg-muted/20 px-3 py-2">
			<span class="text-sm">
				{m.actresses_selected_count({ count: selectedIds.length })}
			</span>
			<Button variant="outline" size="sm" onclick={onSelectCurrentPage}>{m.actresses_select_page()}</Button>
			<Button variant="outline" size="sm" onclick={onClearSelection} disabled={selectedIds.length === 0}>
				{m.common_clear()}
			</Button>
			<Button size="sm" onclick={onStartMergeSelected} disabled={selectedIds.length < 2}>
				<GitMerge class="h-4 w-4" />
				{m.actresses_merge_selected_button()}
			</Button>
		</div>
	</Card>
</div>
