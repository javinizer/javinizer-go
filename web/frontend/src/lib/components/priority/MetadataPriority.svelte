<script lang="ts">
	import { cubicOut } from 'svelte/easing';
	import { fade, fly, slide } from 'svelte/transition';
	import { X, Info } from 'lucide-svelte';
	import { portalToBody } from '$lib/actions/portal';
	import { confirmDialog } from '$lib/stores/dialog.svelte';
	import Card from '../ui/Card.svelte';
	import Button from '../ui/Button.svelte';
	import DraggableList from './DraggableList.svelte';
	import FieldRow from './FieldRow.svelte';
	import type { SettingsConfig, ScraperSettings } from '$lib/api/types';
	import {
		getGlobalPriority,
		getFieldPriority,
		isFieldOverridden,
		getFieldStatus,
		applyEnabledReorderToFull,
		buildFieldPriorityOverride,
		SKIP_SENTINEL
	} from './priority';
	import { formatScraperName } from './scraperNames';

	interface Props {
		config: SettingsConfig;
		onUpdate: (config: SettingsConfig) => void;
		onScraperUsageQuery?: (scraperName: string) => { count: number; fields: string[] };
	}

	let { config, onUpdate, onScraperUsageQuery }: Props = $props();

	type PriorityMode = 'simple' | 'advanced';
	let mode = $state<PriorityMode>('simple');
	let showOnlyOverrides = $state(false);
	let editingField = $state<string | null>(null);
	let editingPriority = $state<string[]>([]);

	// Track which fields have been explicitly modified by the user
	let touchedFields = $state<Set<string>>(new Set());

	// --- Priority mode help popover (the "(i)" icon in the header) ---
	// Follows the CompletenessBreakdownTooltip pattern (ReviewGridCard): hover
	// shows after a short delay, click toggles, Escape + click-outside close.
	// The popover stays in the DOM so `aria-describedby` always resolves, but
	// is invisible + pointer-events-none while hidden.
	const priorityModeHelpTooltipId = 'priority-mode-help-tooltip';
	let showInfo = $state(false);
	// Whether the popover is pinned open by a click. A pinned popover stays open
	// when the pointer leaves the trigger (so the user can move into it to read
	// the help text) — only Esc or click-outside closes it. Without this, the
	// `mt-2` gap between icon and popover means leaving the icon fires mouseleave
	// and snaps a click-opened popover shut, defeating click-to-pin.
	let pinned = $state(false);
	let hoverTimeout: ReturnType<typeof setTimeout> | null = null;
	let infoButtonEl: HTMLButtonElement | null = $state(null);
	let infoPopoverEl: HTMLDivElement | null = $state(null);

	function onInfoEnter() {
		// Don't re-trigger hover-show once the user has pinned it open.
		if (pinned) return;
		hoverTimeout = setTimeout(() => {
			showInfo = true;
		}, 175);
	}

	function onInfoLeave() {
		if (hoverTimeout) {
			clearTimeout(hoverTimeout);
			hoverTimeout = null;
		}
		// A pinned popover survives the pointer leaving the trigger.
		if (!pinned) showInfo = false;
	}

	function toggleInfo() {
		// Cancel any pending hover-show so a delayed timeout can't reopen the
		// popover right after the user clicked it closed.
		if (hoverTimeout) {
			clearTimeout(hoverTimeout);
			hoverTimeout = null;
		}
		pinned = !pinned;
		showInfo = pinned;
	}

	// Close on Escape (returning focus to the trigger) and on click-outside.
	// Attached only while open.
	$effect(() => {
		if (!showInfo) return;
		function onDocClick(event: MouseEvent) {
			const target = event.target as Node | null;
			if (target && (infoButtonEl?.contains(target) || infoPopoverEl?.contains(target))) {
				return;
			}
			pinned = false;
			showInfo = false;
		}
		function onDocKey(event: KeyboardEvent) {
			if (event.key === 'Escape') {
				event.stopPropagation();
				pinned = false;
				showInfo = false;
				infoButtonEl?.focus();
			}
		}
		document.addEventListener('click', onDocClick, true);
		document.addEventListener('keydown', onDocKey);
		return () => {
			document.removeEventListener('click', onDocClick, true);
			document.removeEventListener('keydown', onDocKey);
		};
	});

	// Clear any pending hover timer on teardown.
	$effect(() => {
		return () => {
			if (hoverTimeout) clearTimeout(hoverTimeout);
		};
	});

	// Metadata field definitions with descriptions (using snake_case keys to match API)
	const metadataFields = [
		{ key: 'id', label: 'Movie ID', category: 'Primary', description: 'Primary movie identifier (e.g., IPX-123)' },
		{ key: 'title', label: 'Title', category: 'Primary', description: 'Movie title in English or romanized form' },
		{ key: 'original_title', label: 'Original Title', category: 'Primary', description: 'Original Japanese title' },
		{ key: 'description', label: 'Description', category: 'Primary', description: 'Movie plot summary' },
		{ key: 'release_date', label: 'Release Date', category: 'Primary', description: 'Official release date' },
		{ key: 'runtime', label: 'Runtime', category: 'Primary', description: 'Movie duration in minutes' },
		{ key: 'content_id', label: 'Content ID', category: 'Primary', description: 'Alternative content identifier' },
		{ key: 'actress', label: 'Actresses', category: 'Metadata', description: 'Cast members and performers' },
		{ key: 'genre', label: 'Genres', category: 'Metadata', description: 'Movie categories and tags' },
		{ key: 'director', label: 'Director', category: 'Metadata', description: 'Movie director' },
		{ key: 'maker', label: 'Studio/Maker', category: 'Metadata', description: 'Production studio' },
		{ key: 'label', label: 'Label', category: 'Metadata', description: 'Distribution label' },
		{ key: 'series', label: 'Series', category: 'Metadata', description: 'Series or collection name' },
		{ key: 'rating', label: 'Rating', category: 'Metadata', description: 'User rating or score' },
		{ key: 'cover_url', label: 'Cover Image', category: 'Media', description: 'Front cover artwork URL' },
		{ key: 'poster_url', label: 'Poster Image', category: 'Media', description: 'Poster or fanart URL' },
		{ key: 'screenshot_url', label: 'Screenshots', category: 'Media', description: 'Scene screenshot URLs' },
		{ key: 'trailer_url', label: 'Trailer', category: 'Media', description: 'Preview video URL' }
	];

	// Field priority / override helpers live in ./priority.ts (pure, unit-tested).
	// They take `config` as their first argument and encode the two field
	// states: "inherited" (green) and "custom" (orange).
	// formatScraperName lives in ./scraperNames.ts (shared with FieldRow).

	// Get list of enabled scrapers
	function getEnabledScrapers(): string[] {
		const allScrapers = getGlobalPriority(config);
		return allScrapers.filter((scraperName) => {
			const scraperCfg = config?.scrapers?.[scraperName];
			return (scraperCfg as ScraperSettings)?.enabled !== false;
		});
	}

	// Filter priority list to only include enabled scrapers
	function filterEnabledScrapers(priority: string[]): string[] {
		return priority.filter((scraperName) => {
			const scraperCfg = config?.scrapers?.[scraperName];
			return (scraperCfg as ScraperSettings)?.enabled !== false;
		});
	}

	// Update global priority
	function updateGlobalPriority(newPriority: string[]) {
		if (!config.scrapers) config.scrapers = {};
		config.scrapers.priority = newPriority;
		// Create a deep clone to trigger reactivity
		onUpdate(JSON.parse(JSON.stringify(config)));
	}

	// Open field editor
	function openFieldEditor(fieldKey: string) {
		editingField = fieldKey;
		// When opening a 'skipped' field (stored ["__skip__"]), start with an
		// empty list so the user sees an empty editor and can add scrapers back.
		// Saving an empty list re-emits ["__skip__"] via buildFieldPriorityOverride.
		const stored = config?.metadata?.priority?.[fieldKey];
		if (stored && stored.length === 1 && stored[0] === SKIP_SENTINEL) {
			editingPriority = [];
		} else {
			editingPriority = [...getFieldPriority(config, fieldKey)];
		}
	}

	// Save field priority
	function saveFieldPriority() {
		if (!editingField) return;

		if (!config.metadata) config.metadata = {};

		// Mark this field as touched
		touchedFields.add(editingField);

		// Delegate to the canonical, unit-tested helper: when the resolved
		// priority equals the global list it DELETES the key (restoring
		// "inherited" = key absent); when the priority is EMPTY (Remove all + Save)
		// it stores ["__skip__"] (the skip sentinel — deliberate suppression,
		// since [] now means inherit under World A); otherwise it stores the full
		// list verbatim (including disabled scrapers preserved through onReorder).
		config.metadata.priority = buildFieldPriorityOverride(
			config,
			editingField,
			editingPriority
		);

		// Create a deep clone to trigger reactivity
		onUpdate(JSON.parse(JSON.stringify(config)));
		editingField = null;
	}

	// Reset field to global (clears any override). Inherit = key ABSENT, so we
	// DELETE the key rather than storing []. A present [] is LEGACY and folds to
	// inherit on read under World A; a stored ["__skip__"] means suppression —
	// distinct from inherit — so either way we delete the key to restore
	// inheritance.
	function resetFieldToGlobal(fieldKey: string) {
		if (!config.metadata?.priority) return;

		// Mark as touched (user explicitly reset it)
		touchedFields.add(fieldKey);

		if (fieldKey in config.metadata.priority) {
			delete config.metadata.priority[fieldKey];

			// Create a deep clone to trigger reactivity
			onUpdate(JSON.parse(JSON.stringify(config)));
		}
	}

	// Remove a scraper from the field being edited (the per-item X button).
	// The list stays in order; the removed scraper can be added back from the
	// "available scrapers" chip row below the list.
	function removeScraperFromField(name: string) {
		editingPriority = editingPriority.filter((s) => s !== name);
	}

	// Add a single scraper back into the field being edited (appended at the
	// end — the user can reorder afterward).
	function addScraperToField(name: string) {
		if (!editingPriority.includes(name)) {
			editingPriority = [...editingPriority, name];
		}
	}

	// Shortcut: add every global scraper not already in the field's list.
	function addAllScrapers() {
		const global = getGlobalPriority(config);
		const present = new Set(editingPriority);
		editingPriority = [...editingPriority, ...global.filter((s) => !present.has(s))];
	}

	// Shortcut: remove every scraper from the field's list. Saving the emptied
	// list stores ["__skip__"] (the skip sentinel) via buildFieldPriorityOverride —
	// under World A [] means inherit, so the skip sentinel is the only encoding
	// for deliberate suppression. To inherit the global list instead, use Reset to
	// global (which deletes the key).
	function removeAllScrapers() {
		editingPriority = [];
	}

	// Scrapers available to add back: global scrapers not currently in the
	// editing list (enabled ones first for relevance, but disabled ones are
	// offered too so a user can re-add a since-disabled scraper).
	const availableScrapersToAdd = $derived(
		editingField
			? getGlobalPriority(config).filter((s) => !editingPriority.includes(s))
			: []
	);

	// Count override count
	function getOverrideCount(): number {
		if (!config?.metadata?.priority) return 0;
		return metadataFields.filter((field) => isFieldOverridden(config, field.key)).length;
	}

	// Get scraper usage count (how many fields use this scraper in their priority)
	function getScraperUsageCount(scraperName: string): number {
		let count = 0;

		// Count fields using this scraper (either in global or field-specific priority)
		metadataFields.forEach((field) => {
			const fieldPriority = getFieldPriority(config, field.key);
			if (fieldPriority.includes(scraperName)) {
				count++;
			}
		});

		return count;
	}

	// Get list of fields using a specific scraper
	function getFieldsUsingScaper(scraperName: string): string[] {
		return metadataFields
			.filter((field) => getFieldPriority(config, field.key).includes(scraperName))
			.map((field) => field.label);
	}

	// Switch to Advanced mode warning
	function switchToAdvanced() {
		mode = 'advanced';
	}

	// Switch to Simple mode warning
	async function switchToSimple() {
		const overrideCount = getOverrideCount();
		if (overrideCount > 0) {
			if (!(await confirmDialog(
				'Switch to Simple Mode',
				`You have ${overrideCount} field override(s). Switching to Simple mode will hide these settings but not delete them. Continue?`
			))) return;
		}
		mode = 'simple';
	}

	// Filtered fields based on showOnlyOverrides
	const filteredFields = $derived.by(() => {
		if (!showOnlyOverrides) return metadataFields;
		return metadataFields.filter((field) => isFieldOverridden(config, field.key));
	});

	// Group fields by category
	const groupedFields = $derived.by(() => {
		const fields = filteredFields;
		const groups: Record<string, typeof metadataFields> = {};
		fields.forEach((field) => {
			if (!groups[field.category]) groups[field.category] = [];
			groups[field.category].push(field);
		});
		return groups;
	});
</script>

<div class="space-y-6">
		<!-- Mode Toggle -->
		<div class="flex items-start gap-4 p-4 bg-accent/30 rounded-lg">
			<div class="flex-1">
				<div class="flex items-center gap-2 mb-2">
					<div class="inline-flex rounded-lg border p-1 bg-background">
						<button
							type="button"
							onclick={switchToSimple}
							class="px-4 py-1.5 text-sm font-medium rounded transition-colors {mode ===
							'simple'
								? 'bg-primary text-primary-foreground'
								: 'hover:bg-accent'}"
						>
							Simple
						</button>
						<button
							type="button"
							onclick={switchToAdvanced}
							class="px-4 py-1.5 text-sm font-medium rounded transition-colors {mode ===
							'advanced'
								? 'bg-primary text-primary-foreground'
								: 'hover:bg-accent'}"
						>
							Advanced
							{#if getOverrideCount() > 0}
								<span class="ml-1 text-xs">({getOverrideCount()})</span>
							{/if}
						</button>
					</div>
				</div>
				<p class="text-xs text-muted-foreground">
					{#if mode === 'simple'}
						Simple: One priority list applies to all metadata fields
					{:else}
						Advanced: Customize priority for individual fields
					{/if}
				</p>
			</div>
			<!-- svelte-ignore a11y_no_static_element_interactions -->
		<div
			class="relative shrink-0 mt-1"
			onmouseenter={onInfoEnter}
			onmouseleave={onInfoLeave}
		>
			<button
				type="button"
				bind:this={infoButtonEl}
				aria-label="Priority mode help"
				aria-describedby={priorityModeHelpTooltipId}
				aria-expanded={showInfo}
				onclick={toggleInfo}
				class="inline-flex items-center justify-center rounded-md p-0.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 ring-offset-background transition-colors"
			>
				<Info class="h-5 w-5" />
			</button>
			<div
				role="tooltip"
				id={priorityModeHelpTooltipId}
				bind:this={infoPopoverEl}
				class="absolute top-full right-0 mt-2 w-72 bg-background text-foreground border rounded-lg px-3 py-2 shadow-lg z-20"
				class:pointer-events-none={!showInfo}
				class:invisible={!showInfo}
			>
				{#if showInfo}
					<div transition:fade={{ duration: 150 }} class="space-y-1.5 text-xs">
						<p class="font-semibold">Metadata priority modes</p>
						<p>
							<span class="font-medium">Simple</span> — one priority list applies to every
							metadata field.
						</p>
						<p>
							<span class="font-medium">Advanced</span> — set a custom scraper order for
							individual fields.
						</p>
						<p class="text-muted-foreground">
							Per-field lists are exclusive: only the listed scrapers are consulted
							— there is no fallback to the global list.
						</p>
					</div>
				{/if}
			</div>
		</div>
		</div>

		<!-- Global Priority -->
		<div>
			<span class="block text-sm font-medium mb-3">
				Global Scraper Priority
				{#if mode === 'simple'}
					<span class="text-xs text-muted-foreground ml-2">
						(applies to all fields)
					</span>
				{/if}
			</span>
			<DraggableList
				items={filterEnabledScrapers(getGlobalPriority(config))}
				onReorder={updateGlobalPriority}
			>
				{#snippet children({ item })}
					<span class="font-medium">
						{formatScraperName(item)}
					</span>
				{/snippet}
			</DraggableList>
		</div>

		<!-- Advanced Mode: Per-Field Overrides -->
		{#if mode === 'advanced'}
			<div class="space-y-4" transition:slide|local={{ duration: 220, easing: cubicOut }}>
				<div class="flex items-center justify-between">
					<h3 class="text-sm font-medium">Per-Field Overrides</h3>
					<label class="flex items-center gap-2 text-sm">
						<input type="checkbox" bind:checked={showOnlyOverrides} class="rounded" />
						<span class="text-muted-foreground">Show only overridden</span>
					</label>
				</div>

				{#each Object.entries(groupedFields) as [category, fields] (category)}
					<div class="space-y-2" in:fly|local={{ y: 6, duration: 180, easing: cubicOut }} out:fade|local={{ duration: 120 }}>
						<h4 class="text-xs font-semibold text-muted-foreground uppercase tracking-wide">
							{category}
						</h4>
						<div class="space-y-2">
							{#each fields as field (field.key)}
								<div in:fade|local={{ duration: 160 }} out:fade|local={{ duration: 110 }}>
									<FieldRow
										fieldName={field.key}
										fieldLabel={field.label}
										priority={getFieldPriority(config, field.key)}
										globalPriority={getGlobalPriority(config)}
										status={getFieldStatus(config, field.key)}
										onEdit={() => openFieldEditor(field.key)}
										onReset={() => resetFieldToGlobal(field.key)}
									/>
								</div>
							{/each}
						</div>
					</div>
				{/each}

				{#if showOnlyOverrides && getOverrideCount() === 0}
					<div class="text-center py-8 text-muted-foreground">
						<p class="text-sm">No field overrides configured</p>
						<p class="text-xs mt-1">All fields use the global priority</p>
					</div>
				{/if}
			</div>
		{/if}
</div>

<!-- Field Editor Modal -->
{#if editingField}
	<div class="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4 animate-fade-in" use:portalToBody>
		<Card class="w-full max-w-md animate-scale-in">
			<div class="p-6 space-y-4">
				<!-- Header -->
				<div class="flex items-start justify-between">
					<div>
						<h3 class="text-lg font-semibold">
							Edit Priority: {metadataFields.find((f) => f.key === editingField)?.label}
						</h3>
						<p class="text-sm text-muted-foreground mt-1">
							{metadataFields.find((f) => f.key === editingField)?.description}
						</p>
					</div>
					<Button variant="ghost" size="icon" onclick={() => (editingField = null)} aria-label="Close editor">
						{#snippet children()}
							<X class="h-4 w-4" />
						{/snippet}
					</Button>
				</div>

				<!-- Draggable list: each scraper has an X to remove it from this field -->
				<div class="max-h-[40vh] overflow-y-scroll pr-1">
					{#if filterEnabledScrapers(editingPriority).length > 0}
						<DraggableList
							items={filterEnabledScrapers(editingPriority)}
							onReorder={(newEnabledOrder) => {
								// Reorder within the FULL list: re-apply the enabled-only
								// reordering back onto editingPriority, preserving any
								// disabled scrapers the DraggableList hid from display
								// (appended after the enabled ones, in their original
								// relative order). Writing newEnabledOrder straight back
								// would silently drop disabled scrapers on the first drag.
								editingPriority = applyEnabledReorderToFull(editingPriority, newEnabledOrder);
							}}
							onRemove={(name) => removeScraperFromField(name)}
						>
							{#snippet children({ item })}
								<span class="font-medium">
									{formatScraperName(item)}
								</span>
							{/snippet}
						</DraggableList>
					{:else}
						<p class="text-sm text-muted-foreground italic py-4 text-center">
							No scrapers in this field's list. Save with an empty list to suppress this
							field (stores the <code class="bg-muted px-1 rounded">"__skip__"</code>
							sentinel — no scraper is consulted), or add some below.
						</p>
					{/if}
				</div>

				<!-- Shortcuts: add all / remove all -->
				<div class="flex items-center gap-2 flex-wrap">
					<Button variant="outline" size="sm" onclick={addAllScrapers} aria-label="Add all scrapers to this field">
						{#snippet children()}
							Add all
						{/snippet}
					</Button>
					<Button variant="outline" size="sm" onclick={removeAllScrapers} aria-label="Remove all scrapers from this field">
						{#snippet children()}
							Remove all
						{/snippet}
					</Button>
				</div>

				<!-- Available scrapers to add back (those not currently in the list) -->
				{#if availableScrapersToAdd.length > 0}
					<div class="space-y-1.5">
						<p class="text-xs font-medium text-muted-foreground">Available scrapers</p>
						<div class="flex flex-wrap gap-1.5">
							{#each availableScrapersToAdd as name}
								<button
									type="button"
									onclick={() => addScraperToField(name)}
									class="inline-flex items-center gap-1 px-2 py-1 text-xs rounded-full border border-dashed border-border hover:border-primary hover:bg-primary/5 transition-colors"
									aria-label="Add {formatScraperName(name)} to this field"
								>
									<span class="text-lg leading-none">+</span>
									{formatScraperName(name)}
								</button>
							{/each}
						</div>
					</div>
				{/if}

				<!-- Info -->
				<div class="bg-accent/50 rounded-lg p-3 text-xs text-muted-foreground space-y-1">
					<p>
						Scrapers are tried top-to-bottom; the first one that returns data for this field is
						used. Only the scrapers listed here are consulted — there is no fallback to the
						global list. Remove a scraper with its <span class="font-medium">✕</span> button; add
						one back from the chips above.
					</p>
					<p>
						To leave a field empty, remove all scrapers and Save (stores
						<code class="bg-muted px-1 rounded">series: ["__skip__"]</code> — the
						skip sentinel, no scraper is consulted), or point it at a scraper that
						doesn't provide it (e.g.
						<code class="bg-muted px-1 rounded">series: [tokyohot]</code>). A legacy
						<code class="bg-muted px-1 rounded">series: []</code> is treated as
						inherit-global, not suppression — so "Remove all + Save" stores the
						<code class="bg-muted px-1 rounded">__skip__</code> sentinel instead.
					</p>
				</div>

				<!-- Actions -->
				<div class="flex items-center gap-3 justify-end">
					<Button variant="outline" onclick={() => (editingField = null)}>
						{#snippet children()}
							Cancel
						{/snippet}
					</Button>
					<Button onclick={saveFieldPriority}>
						{#snippet children()}
							Save Priority
						{/snippet}
					</Button>
				</div>
			</div>
		</Card>
	</div>
{/if}

<style>
	@keyframes fade-in {
		from {
			opacity: 0;
		}
		to {
			opacity: 1;
		}
	}

	@keyframes scale-in {
		from {
			transform: scale(0.95);
			opacity: 0;
		}
		to {
			transform: scale(1);
			opacity: 1;
		}
	}

	.animate-fade-in {
		animation: fade-in 0.2s ease-out;
	}

	:global(.animate-scale-in) {
		animation: scale-in 0.3s ease-out;
	}
</style>
