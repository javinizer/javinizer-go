<script lang="ts">
	import Button from '$lib/components/ui/Button.svelte';
	import { ChevronDown, ChevronUp, Image, LayoutGrid, List, LoaderCircle, Play, RefreshCw, Settings2, X, CheckSquare, Square, Trash2, RotateCcw, MousePointerClick, Save } from 'lucide-svelte';
	import type { CompletenessTier } from '$lib/utils/completeness';
	import type { ArrayMergeStrategy, MergePreset, ScalarMergeStrategy } from '$lib/api/types';
	import * as m from '$lib/paraglide/messages';
	import { withCustomReviewMergeStrategy } from '../stores/review-state.svelte';

	interface Props {
		isUpdateMode: boolean;
		canOrganize: boolean;
		organizing: boolean;
		movieResultsLength: number;
		destinationPath: string;
		operationMode?: string;
		applyInvalid?: boolean;
		viewMode?: 'detail' | 'grid-poster' | 'grid-cover';
		forceOverwrite?: boolean;
		preserveNfo?: boolean;
		skipNfo?: boolean;
		skipDownload?: boolean;
		overwriteExistingMedia?: boolean;
		applyPreset?: MergePreset;
		applyScalarStrategy?: ScalarMergeStrategy;
		applyArrayStrategy?: ArrayMergeStrategy;
		usesLegacyApplyDefaults?: boolean;
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
		onSaveAll: () => void | Promise<void>;
		hasEdits: boolean;
		editCount: number;
		savingEdits: boolean;
	}

		let {
		isUpdateMode,
		canOrganize,
		organizing,
		movieResultsLength,
		destinationPath,
		operationMode = 'organize',
		applyInvalid = false,
		viewMode = $bindable<'detail' | 'grid-poster' | 'grid-cover'>('detail'),
		forceOverwrite = $bindable(false),
		preserveNfo = $bindable(false),
		skipNfo = $bindable(false),
		skipDownload = $bindable(false),
		overwriteExistingMedia = $bindable(false),
		applyPreset = $bindable<MergePreset | undefined>(undefined),
		applyScalarStrategy = $bindable<ScalarMergeStrategy>('prefer-nfo'),
		applyArrayStrategy = $bindable<ArrayMergeStrategy>('merge'),
		usesLegacyApplyDefaults = false,
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
		onOrganizeAll,
		onSaveAll,
		hasEdits,
		editCount,
		savingEdits
	}: Props = $props();

	$effect(() => {
		if (forceOverwrite) preserveNfo = false;
	});

	$effect(() => {
		if (preserveNfo) forceOverwrite = false;
	});

	// A preset implies its fixed strategy pair; the backend rejects a request
	// carrying BOTH (preset contradicts strategies). Hand-editing a strategy
	// must therefore clear the preset — this mirrors
	// withCustomReviewMergeStrategy, the store-level rule the action bar
	// already follows.
	function chooseMergeStrategy(change: { scalar?: ScalarMergeStrategy; array?: ArrayMergeStrategy }) {
		const next = withCustomReviewMergeStrategy(
			{ destinationPath, forceOverwrite, preserveNfo, skipNfo, skipDownload, overwriteExistingMedia, applyPreset, applyScalarStrategy, applyArrayStrategy },
			change
		);
		applyPreset = next.applyPreset;
		applyScalarStrategy = next.applyScalarStrategy;
		applyArrayStrategy = next.applyArrayStrategy;
	}

	function choosePreset(value: MergePreset | undefined) {
		if (value === 'conservative') { applyScalarStrategy = 'preserve-existing'; applyArrayStrategy = 'merge'; }
		if (value === 'gap-fill') { applyScalarStrategy = 'fill-missing-only'; applyArrayStrategy = 'merge'; }
		if (value === 'aggressive') { applyScalarStrategy = 'prefer-scraper'; applyArrayStrategy = 'replace'; }
		applyPreset = value;
	}

	let showOptions = $state(false);

	const tierConfig: { tier: CompletenessTier; label: string; dotClass: string }[] = [
		{ tier: 'incomplete', label: m.review_tier_incomplete(), dotClass: 'bg-red-500' },
		{ tier: 'partial', label: m.review_tier_partial(), dotClass: 'bg-yellow-500' },
		{ tier: 'complete', label: m.review_tier_complete(), dotClass: 'bg-green-500' },
	];
</script>

<div class="flex items-center justify-between mb-6">
	<div>
		<h1 class="text-3xl font-bold">{m.review_title()}</h1>
		<p class="text-muted-foreground mt-1">
			{#if isUpdateMode}
				{m.review_subtitle_update()}
			{:else}
				{m.review_subtitle_organize()}
			{/if}
		</p>
	</div>
	<div class="flex items-center gap-3">
		<div class="inline-flex rounded-md border border-input p-1">
			<Button
				size="sm"
				variant={viewMode === 'detail' ? 'default' : 'ghost'}
				class="w-24 justify-center"
				onclick={() => { viewMode = 'detail'; }}
			>
				{#snippet children()}
					<List class="h-4 w-4 mr-1" />
					{m.review_view_detail()}
				{/snippet}
			</Button>
			<Button
				size="sm"
				variant={viewMode === 'grid-poster' ? 'default' : 'ghost'}
				class="w-24 justify-center"
				onclick={() => { viewMode = 'grid-poster'; }}
			>
				{#snippet children()}
					<LayoutGrid class="h-4 w-4 mr-1" />
					{m.review_poster()}
				{/snippet}
			</Button>
			<Button
				size="sm"
				variant={viewMode === 'grid-cover' ? 'default' : 'ghost'}
				class="w-24 justify-center"
				onclick={() => { viewMode = 'grid-cover'; }}
			>
				{#snippet children()}
					<Image class="h-4 w-4 mr-1" />
					{m.review_view_cover()}
				{/snippet}
			</Button>
		</div>
		<div class="h-8 w-px bg-border"></div>
		{#if hasEdits}
			<Button onclick={() => { void Promise.resolve(onSaveAll()).catch(() => {}); }} disabled={savingEdits || organizing} title={m.review_save_changes_title()}>
				{#snippet children()}
					{#if savingEdits}
						<LoaderCircle class="h-4 w-4 mr-2 animate-spin" />
					{:else}
						<Save class="h-4 w-4 mr-2" />
					{/if}
					{savingEdits ? m.common_saving() : (editCount > 1 ? m.review_save_changes_with_count({ count: editCount }) : m.review_save_changes())}
				{/snippet}
			</Button>
		{/if}
		<Button variant="outline" onclick={onClose} disabled={organizing}>
			{#snippet children()}
				<X class="h-4 w-4 mr-2" />
				{isUpdateMode ? 'Close' : 'Cancel'}
			{/snippet}
		</Button>
		{#if isUpdateMode}
			<Button onclick={onUpdateAll} disabled={organizing || applyInvalid}>
				{#snippet children()}
					{#if organizing}
						<LoaderCircle class="h-4 w-4 mr-2 animate-spin" />
					{:else}
						<RefreshCw class="h-4 w-4 mr-2" />
					{/if}
					{organizing ? m.review_updating() : m.review_update_files_button({ count: movieResultsLength })}
				{/snippet}
			</Button>
		{:else}
			<!-- applyInvalid mirrors the sticky action bar + update-mode button:
			with both NFO output and media downloads skipped, submitting would
			send a plan the back-end rejects. -->
			<Button onclick={onOrganizeAll} disabled={organizing || applyInvalid || !canOrganize || (operationMode === 'organize' && !destinationPath.trim())}>
				{#snippet children()}
					{#if organizing}
						<LoaderCircle class="h-4 w-4 mr-2 animate-spin" />
					{:else}
						<Play class="h-4 w-4 mr-2" />
					{/if}
					{organizing ? m.review_organizing() : m.review_organize_files_button({ count: movieResultsLength })}
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
				{m.review_select()}
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
						{m.review_deselect_all()}
					{:else}
						<Square class="h-4 w-4 mr-1" />
						{m.review_select_all()}
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
		{#if selectedCount > 0}
			<div class="ml-auto flex items-center gap-3">
				<span class="text-sm font-medium text-muted-foreground whitespace-nowrap">
					{m.review_selected_count({ count: selectedCount })}
				</span>
				<Button
					size="sm"
					variant="outline"
					onclick={onBulkExclude}
					disabled={bulkExcluding || bulkRescraping}
					class="text-orange-600 hover:text-orange-700 dark:text-orange-400 dark:hover:text-orange-300"
				>
					{#snippet children()}
						{#if bulkExcluding}
							<LoaderCircle class="h-4 w-4 mr-1 animate-spin" />
						{:else}
							<Trash2 class="h-4 w-4 mr-1" />
						{/if}
						{m.review_remove()}
					{/snippet}
				</Button>
				<Button
					size="sm"
					variant="outline"
					onclick={onBulkRescrape}
					disabled={bulkExcluding || bulkRescraping}
				>
					{#snippet children()}
						{#if bulkRescraping}
							<LoaderCircle class="h-4 w-4 mr-1 animate-spin" />
						{:else}
							<RotateCcw class="h-4 w-4 mr-1" />
						{/if}
						{m.review_rescrape()}
					{/snippet}
				</Button>
			</div>
		{/if}
	</div>
{/if}

{#if isUpdateMode || canOrganize}
	<div class="mb-4">
		<button
			onclick={() => (showOptions = !showOptions)}
			class="flex items-center gap-2 text-sm font-medium text-muted-foreground hover:text-foreground transition-colors"
		>
			<Settings2 class="h-4 w-4" />
			{m.review_options()}
			{#if showOptions}
				<ChevronUp class="h-3 w-3" />
			{:else}
				<ChevronDown class="h-3 w-3" />
			{/if}
		</button>

		{#if showOptions}
			{#if applyInvalid}<p class="mt-3 text-sm text-destructive" role="alert">{m.review_choose_output_alert()}</p>{/if}
			<div class="grid gap-3 md:grid-cols-4 mt-3">
				{#if isUpdateMode}
				<label
					class="flex items-center gap-3 p-3 rounded-lg border border-border bg-background hover:bg-accent/50 cursor-pointer transition-colors"
				>
					<input
						type="checkbox"
						bind:checked={forceOverwrite}
						class="h-4 w-4 rounded border-input text-primary focus:ring-2 focus:ring-primary"
					/>
					<div class="flex-1">
						<span class="text-sm font-medium">{m.review_force_overwrite()}</span>
						<p class="text-xs text-muted-foreground">{m.review_force_overwrite_desc()}</p>
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
						<span class="text-sm font-medium">{m.review_preserve_nfo()}</span>
						<p class="text-xs text-muted-foreground">{m.review_preserve_nfo_desc()}</p>
					</div>
				</label>

				{/if}
				<label
					class="flex items-center gap-3 p-3 rounded-lg border border-border bg-background hover:bg-accent/50 cursor-pointer transition-colors"
				>
					<input
						type="checkbox"
						bind:checked={skipNfo}
						class="h-4 w-4 rounded border-input text-primary focus:ring-2 focus:ring-primary"
					/>
					<div class="flex-1">
						<span class="text-sm font-medium">{m.review_skip_nfo()}</span>
						<p class="text-xs text-muted-foreground">{m.review_skip_nfo_desc()}</p>
					</div>
				</label>

				<label
					class="flex items-center gap-3 p-3 rounded-lg border border-border bg-background hover:bg-accent/50 cursor-pointer transition-colors"
				>
					<input
						type="checkbox"
						checked={skipDownload}
						onchange={(e) => {
							skipDownload = e.currentTarget.checked;
							// "Skip downloads" and "Replace existing media" are mutually
							// exclusive: leaving both set sends skip_download +
							// overwrite_existing_media, a 400 from the backend.
							if (skipDownload) overwriteExistingMedia = false;
						}}
						class="h-4 w-4 rounded border-input text-primary focus:ring-2 focus:ring-primary"
					/>
					<div class="flex-1">
						<span class="text-sm font-medium">{m.review_skip_download()}</span>
						<p class="text-xs text-muted-foreground">{m.review_skip_download_desc()}</p>
					</div>
				</label>

				{#if isUpdateMode}
				<label
					class="flex items-center gap-3 p-3 rounded-lg border border-border bg-background hover:bg-accent/50 cursor-pointer transition-colors"
				>
					<input type="checkbox" bind:checked={overwriteExistingMedia} disabled={skipDownload} class="h-4 w-4 rounded border-input text-primary focus:ring-2 focus:ring-primary" />
					<div class="flex-1"><span class="text-sm font-medium">{m.review_replace_existing_media()}</span><p class="text-xs text-muted-foreground">{m.review_replace_existing_media_desc()}</p></div>
				</label>

				<div class="grid gap-3 rounded-lg border bg-background p-3 md:col-span-4 md:grid-cols-3">
					<label class="text-xs text-muted-foreground">{m.browse_quick_presets()}
						<select class="mt-1 h-10 w-full rounded-md border bg-background px-2 text-sm" value={applyPreset ?? ''} disabled={forceOverwrite || preserveNfo} onchange={(e) => choosePreset((e.currentTarget.value || undefined) as MergePreset | undefined)}><option value="">{m.browse_clear_preset()}</option><option value="conservative">{m.browse_preset_conservative()}</option><option value="gap-fill">{m.browse_preset_gap_fill()}</option><option value="aggressive">{m.browse_preset_aggressive()}</option></select>
					</label>
					<label class="text-xs text-muted-foreground">{m.browse_scalar_fields()}
						<select class="mt-1 h-10 w-full rounded-md border bg-background px-2 text-sm" value={applyScalarStrategy} disabled={forceOverwrite || preserveNfo} onchange={(e) => chooseMergeStrategy({ scalar: e.currentTarget.value as ScalarMergeStrategy })}><option value="prefer-nfo">{m.browse_prefer_nfo()}</option><option value="prefer-scraper">{m.browse_prefer_scraped()}</option><option value="preserve-existing">{m.browse_preserve_existing()}</option><option value="fill-missing-only">{m.browse_fill_missing_only()}</option></select>
					</label>
					<label class="text-xs text-muted-foreground">{m.browse_array_fields()}
						<select class="mt-1 h-10 w-full rounded-md border bg-background px-2 text-sm" value={applyArrayStrategy} disabled={forceOverwrite || preserveNfo} onchange={(e) => chooseMergeStrategy({ array: e.currentTarget.value as ArrayMergeStrategy })}><option value="merge">{m.browse_merge()}</option><option value="replace">{m.browse_replace()}</option></select>
					</label>
					{#if usesLegacyApplyDefaults}<p class="text-xs text-muted-foreground md:col-span-3">{m.review_legacy_defaults_note()}</p>{/if}
				</div>
				{/if}
			</div>
		{/if}
	</div>
{/if}
