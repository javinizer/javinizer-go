<script lang="ts">
	import { ChevronDown, ChevronLeft, ChevronRight, CircleAlert, Eye, FileVideo, Trash2 } from 'lucide-svelte';
	import * as m from '$lib/paraglide/messages';
	import Button from '$lib/components/ui/Button.svelte';
	import type { FileResult } from '$lib/api/types';
	import { truncatePath } from '../review-utils';

	interface Props {
		currentMovieIndex: number;
		movieResultsLength: number;
		currentMovieId: string;
		hasChanges: boolean;
		sourceResults: FileResult[];
		primaryFilePath: string;
		showFullSourcePath: boolean;
		showOutputPreview: boolean;
		outputPreviewDisabled: boolean;
		onOpenOutputPreview: () => void;
		onExclude: () => void;
	}

	let {
		currentMovieIndex = $bindable(0),
		movieResultsLength,
		currentMovieId,
		hasChanges,
		sourceResults,
		primaryFilePath,
		showFullSourcePath = $bindable(false),
		showOutputPreview,
		outputPreviewDisabled,
		onOpenOutputPreview,
		onExclude
	}: Props = $props();

	let partsExpanded = $state(false);

	const pageOptions = $derived(
		Array.from({ length: movieResultsLength }, (_, index) => index + 1)
	);

	const isMultiPart = $derived(sourceResults.length > 1);
	const anyLongPath = $derived(
		primaryFilePath.length > 80 || sourceResults.some((r) => r.file_path.length > 80)
	);

	function selectMoviePage(event: Event): void {
		const target = event.currentTarget as HTMLSelectElement;
		const selectedIndex = Number.parseInt(target.value, 10) - 1;
		if (Number.isNaN(selectedIndex)) return;

		currentMovieIndex = Math.min(movieResultsLength - 1, Math.max(0, selectedIndex));
	}
</script>

<div class="rounded-md border border-border bg-card shadow-sm" data-testid="movie-navigation-card">
	<div class="flex h-10 items-center gap-2 px-2">
		<Button
			variant="ghost"
			size="icon"
			onclick={() => (currentMovieIndex = Math.max(0, currentMovieIndex - 1))}
			disabled={currentMovieIndex === 0}
			aria-label={m.review_previous()}
			title={m.review_previous()}
			class="h-8 w-8"
		>
			{#snippet children()}<ChevronLeft class="h-4 w-4" />{/snippet}
		</Button>

		<div class="flex items-center gap-2 min-w-0 flex-1">
			<span class="text-sm font-semibold tabular-nums whitespace-nowrap">
				{currentMovieIndex + 1} / {movieResultsLength}
			</span>
			<select
				id="movie-page-select"
				class="h-7 rounded-md border border-input bg-background px-1.5 text-xs"
				value={currentMovieIndex + 1}
				onchange={selectMoviePage}
				aria-label={m.review_page_label()}
			>
				{#each pageOptions as pageNumber}
					<option value={pageNumber}>{pageNumber}</option>
				{/each}
			</select>
			<span class="text-sm text-muted-foreground truncate font-mono" title={currentMovieId}>{currentMovieId}</span>
			{#if hasChanges}
				<span class="inline-flex items-center gap-1 text-xs text-orange-600 dark:text-orange-400 shrink-0">
					<CircleAlert class="h-3 w-3" />
					{m.review_modified()}
				</span>
			{/if}
		</div>

		<Button
			variant="ghost"
			size="icon"
			onclick={onExclude}
			class="h-8 w-8 text-destructive hover:bg-destructive hover:text-destructive-foreground shrink-0"
			aria-label={m.review_remove()}
			title={m.review_remove()}
		>
			{#snippet children()}<Trash2 class="h-4 w-4" />{/snippet}
		</Button>

		{#if showOutputPreview}
			<Button
				variant="ghost"
				size="icon"
				onclick={onOpenOutputPreview}
				disabled={outputPreviewDisabled}
				class="h-8 w-8 shrink-0"
				aria-label={m.review_output_preview()}
				title={m.review_output_preview()}
			>
				{#snippet children()}<Eye class="h-4 w-4" />{/snippet}
			</Button>
		{/if}

		<Button
			variant="ghost"
			size="icon"
			onclick={() => (currentMovieIndex = Math.min(movieResultsLength - 1, currentMovieIndex + 1))}
			disabled={currentMovieIndex === movieResultsLength - 1}
			aria-label={m.review_next()}
			title={m.review_next()}
			class="h-8 w-8"
		>
			{#snippet children()}<ChevronRight class="h-4 w-4" />{/snippet}
		</Button>
	</div>

	{#if primaryFilePath}
		<div class="border-t border-border px-3 py-1.5">
			{#if isMultiPart}
				<button
					onclick={() => (partsExpanded = !partsExpanded)}
					class="flex w-full items-center gap-2 text-xs text-muted-foreground hover:text-foreground transition-colors cursor-pointer min-w-0"
				>
					<FileVideo class="h-3.5 w-3.5 shrink-0" />
					<span class="shrink-0">{m.review_source_files_count({ count: sourceResults.length })}</span>
					<ChevronDown class="h-3.5 w-3.5 shrink-0 transition-transform {partsExpanded ? 'rotate-180' : ''}" />
					{#if anyLongPath}
						<span
							role="button"
							tabindex="0"
							onclick={(e: MouseEvent) => { e.stopPropagation(); showFullSourcePath = !showFullSourcePath; }}
							onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') { e.stopPropagation(); showFullSourcePath = !showFullSourcePath; } }}
							class="ml-auto text-primary hover:text-primary/80 shrink-0"
						>
							{showFullSourcePath ? m.review_hide_path() : m.review_show_full_path()}
						</span>
					{/if}
				</button>
				{#if partsExpanded}
					<div class="mt-1.5 space-y-1">
						{#each sourceResults as result, index}
							<div class="bg-accent rounded px-2 py-1 {showFullSourcePath ? 'overflow-x-auto' : ''}">
								<code class="text-xs block {showFullSourcePath ? 'whitespace-nowrap' : 'truncate'}" title={result.file_path}>
									<span class="text-muted-foreground mr-2">{m.review_part_n({ n: index + 1 })}</span>
									{showFullSourcePath ? result.file_path : truncatePath(result.file_path)}
								</code>
							</div>
						{/each}
					</div>
				{/if}
			{:else}
				<div class="flex items-center gap-2 min-w-0">
					<FileVideo class="h-3.5 w-3.5 text-muted-foreground shrink-0" />
					<code
						class="text-xs text-muted-foreground font-mono min-w-0 {showFullSourcePath ? 'break-all' : 'truncate'}"
						title={primaryFilePath}
					>
						{showFullSourcePath ? primaryFilePath : truncatePath(primaryFilePath)}
					</code>
					{#if primaryFilePath.length > 80}
						<button
							onclick={() => (showFullSourcePath = !showFullSourcePath)}
							class="text-xs text-primary hover:text-primary/80 transition-colors cursor-pointer shrink-0 ml-auto"
						>
							{showFullSourcePath ? m.review_hide_path() : m.review_show_full_path()}
						</button>
					{/if}
				</div>
			{/if}
		</div>
	{/if}
</div>