<script lang="ts">
	import { cubicOut } from 'svelte/easing';
	import { fade, fly, slide } from 'svelte/transition';
	import { X, Info, Ban } from 'lucide-svelte';
	import { portalToBody } from '$lib/actions/portal';
	import { confirmDialog } from '$lib/stores/dialog.svelte';
	import Card from '../ui/Card.svelte';
	import Button from '../ui/Button.svelte';
	import DraggableList from './DraggableList.svelte';
	import FieldRow from './FieldRow.svelte';
	import type { SettingsConfig, ScraperSettings } from '$lib/api/types';
	import {
		SKIP_FIELD_SENTINEL,
		getGlobalPriority,
		getFieldPriority,
		isFieldOverridden,
		getFieldStatus,
		isSkipField
	} from './priority';

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

	function formatScraperName(name: string): string {
		if (name === 'dmm') return 'DMM/Fanza';
		if (name === 'libredmm') return 'LibreDMM (Fanza, MGStage, SOD, FC2)';
		if (name === 'r18dev') return 'R18.dev';
		if (name === 'javlibrary') return 'JavLibrary';
		if (name === 'javdb') return 'JavDB';
		if (name === 'javbus') return 'JavBus';
		if (name === 'jav321') return 'Jav321';
		if (name === 'tokyohot') return 'Tokyo-Hot';
		if (name === 'aventertainment') return 'AV Entertainment';
		if (name === 'dlgetchu') return 'DLGetchu';
		if (name === 'caribbeancom') return 'Caribbeancom';
		return name;
	}

	// Field priority / override helpers live in ./priority.ts (pure, unit-tested).
	// They take `config` as their first argument and encode the three field
	// states: "inherited" (green), "custom" (orange), "skipped" (grey).

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
		editingPriority = [...getFieldPriority(config, fieldKey)];
	}

	// Save field priority
	function saveFieldPriority() {
		if (!editingField) return;

		if (!config.metadata) config.metadata = {};
		if (!config.metadata.priority) config.metadata.priority = {};

		// Mark this field as touched
		touchedFields.add(editingField);

		const global = getGlobalPriority(config);
		const isSameAsGlobal = JSON.stringify(editingPriority) === JSON.stringify(global);

		if (isSameAsGlobal) {
			// If it matches global, set to empty array (signals "use global")
			config.metadata.priority[editingField] = [];
		} else {
			// Otherwise save the custom priority. This includes the __skip__
			// sentinel, which is an exclusive override that leaves the field
			// empty (no scraper named "__skip__" is ever consulted).
			config.metadata.priority[editingField] = editingPriority;
		}

		// Create a deep clone to trigger reactivity
		onUpdate(JSON.parse(JSON.stringify(config)));
		editingField = null;
	}

	// Reset field to global (clears any override, including a skip)
	function resetFieldToGlobal(fieldKey: string) {
		if (!config.metadata?.priority) return;

		// Mark as touched (user explicitly reset it)
		touchedFields.add(fieldKey);

		// Set to empty array (signals "use global")
		config.metadata.priority[fieldKey] = [];

		// Create a deep clone to trigger reactivity
		onUpdate(JSON.parse(JSON.stringify(config)));
	}

	// Skip this field: stage the __skip__ sentinel so the field is left empty.
	// Confirms with the user before staging; the change persists on Save.
	async function skipField() {
		if (!editingField) return;
		const fieldLabel =
			metadataFields.find((f) => f.key === editingField)?.label ?? editingField;
		const confirmed = await confirmDialog(
			'Skip field',
			`Skip scraping for "${fieldLabel}"? Under exclusive semantics, only the scrapers listed here are consulted — and the skip marker matches none of them, so the field will be left empty. You can re-enable it at any time.`
		);
		if (!confirmed) return;
		editingPriority = [SKIP_FIELD_SENTINEL];
	}

	// Re-enable a skipped field: restore the inherited (global) scraper list so
	// the user can reorder and save. Saving it unchanged restores "inherited".
	function enableField() {
		editingPriority = [...filterEnabledScrapers(getGlobalPriority(config))];
	}

	// Whether the field being edited is currently staged as skipped
	const editingIsSkipped = $derived(isSkipField(editingPriority));

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
			<Info class="h-5 w-5 text-muted-foreground shrink-0 mt-1" />
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
						<h3 class="text-lg font-semibold flex items-center gap-2">
							Edit Priority: {metadataFields.find((f) => f.key === editingField)?.label}
							{#if editingIsSkipped}
								<span class="inline-flex items-center gap-1 text-xs font-medium px-2 py-0.5 rounded-full bg-slate-200 text-slate-700 dark:bg-slate-800 dark:text-slate-300">
									<Ban class="h-3 w-3" />
									Skipped
								</span>
							{/if}
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

				<!-- Draggable List OR Skipped banner -->
				{#if editingIsSkipped}
					<div class="rounded-lg border border-dashed border-slate-300 dark:border-slate-700 bg-slate-50 dark:bg-slate-900/40 p-4 text-sm">
						<div class="flex items-start gap-2">
							<Ban class="h-4 w-4 text-slate-500 mt-0.5 shrink-0" />
							<div class="space-y-1">
								<p class="font-medium text-slate-700 dark:text-slate-300">
									This field is skipped — it will be left empty.
								</p>
								<p class="text-muted-foreground">
									No scrapers will be consulted for this field. Save to apply, or
									re-enable it below to choose scrapers.
								</p>
							</div>
						</div>
					</div>
				{:else}
					<div class="max-h-[50vh] overflow-y-scroll pr-1">
						<DraggableList
							items={filterEnabledScrapers(editingPriority)}
							onReorder={(newPriority) => { editingPriority = newPriority; }}
						>
							{#snippet children({ item })}
								<span class="font-medium">
									{formatScraperName(item)}
								</span>
							{/snippet}
						</DraggableList>
					</div>
				{/if}

				<!-- Info -->
				<div class="bg-accent/50 rounded-lg p-3 text-xs text-muted-foreground space-y-1">
					<p>
						Scrapers are tried top-to-bottom; the first one that returns data for this field is
						used. Only the scrapers listed here are consulted — if none of them provide this
						field, it is left empty (there is no fallback to the global list).
					</p>
					<p>
						Use <span class="font-medium">Skip field</span> to suppress this field entirely.
					</p>
				</div>

				<!-- Actions -->
				<div class="flex items-center gap-3 justify-end">
					<Button variant="outline" onclick={() => (editingField = null)}>
						{#snippet children()}
							Cancel
						{/snippet}
					</Button>
					{#if editingIsSkipped}
						<Button variant="outline" onclick={enableField} aria-label="Re-enable this field">
							{#snippet children()}
								Re-enable field
							{/snippet}
						</Button>
					{:else}
						<Button variant="outline" onclick={skipField} aria-label="Skip this field (leave it empty)">
							{#snippet children()}
								<Ban class="h-4 w-4" />
								Skip field
							{/snippet}
						</Button>
					{/if}
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
