<script lang="ts">
	import { cubicOut } from 'svelte/easing';
	import { fade, fly, slide } from 'svelte/transition';
	import { X, Info } from 'lucide-svelte';
	import { portalToBody } from '$lib/actions/portal';
	import Card from '../ui/Card.svelte';
	import Button from '../ui/Button.svelte';
	import DraggableList from './DraggableList.svelte';
	import FieldRow from './FieldRow.svelte';

	interface Props {
		config: any;
		onUpdate: (config: any) => void;
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

	// Helper to get global priority from config
	function getGlobalPriority(): string[] {
		return config?.scrapers?.priority || [];
	}

	function formatScraperName(name: string): string {
		if (name === 'dmm') return 'DMM/Fanza';
		if (name === 'libredmm') return 'LibreDMM';
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

	// Helper to get field priority (either custom or global)
	// Empty arrays mean "use global", same as undefined
	function getFieldPriority(fieldKey: string): string[] {
		const fieldConfig = config?.metadata?.priority?.[fieldKey];
		// If field config is undefined, null, or empty array, use global priority
		if (!fieldConfig || fieldConfig.length === 0) {
			return getGlobalPriority();
		}
		return fieldConfig;
	}

	// Check if field has custom override (either touched by user or already in config)
	// Empty arrays are treated the same as undefined (not overridden)
	function isFieldOverridden(fieldKey: string): boolean {
		const fieldConfig = config?.metadata?.priority?.[fieldKey];
		const globalPriority = getGlobalPriority();

		// Empty or undefined means "use global" (not overridden)
		if (!fieldConfig || fieldConfig.length === 0) {
			return false;
		}

		// Only consider it overridden if it's different from global
		return JSON.stringify(fieldConfig) !== JSON.stringify(globalPriority);
	}

	// Count override count
	function getOverrideCount(): number {
		if (!config?.metadata?.priority) return 0;
		return metadataFields.filter((field) => isFieldOverridden(field.key)).length;
	}

	// Get scraper usage count (how many fields use this scraper in their priority)
	function getScraperUsageCount(scraperName: string): number {
		let count = 0;

		// Count fields using this scraper (either in global or field-specific priority)
		metadataFields.forEach((field) => {
			const fieldPriority = getFieldPriority(field.key);
			if (fieldPriority.includes(scraperName)) {
				count++;
			}
		});

		return count;
	}

	// Get list of fields using a specific scraper
	function getFieldsUsingScaper(scraperName: string): string[] {
		return metadataFields
			.filter((field) => getFieldPriority(field.key).includes(scraperName))
			.map((field) => field.label);
	}

	// Get list of enabled scrapers
	function getEnabledScrapers(): string[] {
		const allScrapers = getGlobalPriority();
		return allScrapers.filter((scraperName) => {
			return config?.scrapers?.[scraperName]?.enabled !== false;
		});
	}

	// Filter priority list to only include enabled scrapers
	function filterEnabledScrapers(priority: string[]): string[] {
		return priority.filter((scraperName) => {
			return config?.scrapers?.[scraperName]?.enabled !== false;
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
		editingPriority = [...getFieldPriority(fieldKey)];
	}

	// Save field priority
	function saveFieldPriority() {
		if (!editingField) return;

		if (!config.metadata) config.metadata = {};
		if (!config.metadata.priority) config.metadata.priority = {};

		// Mark this field as touched
		touchedFields.add(editingField);

		const global = getGlobalPriority();
		const isSameAsGlobal = JSON.stringify(editingPriority) === JSON.stringify(global);

		if (isSameAsGlobal) {
			// If it matches global, set to empty array (signals "use global")
			config.metadata.priority[editingField] = [];
		} else {
			// Otherwise save the custom priority
			config.metadata.priority[editingField] = editingPriority;
		}

		// Create a deep clone to trigger reactivity
		onUpdate(JSON.parse(JSON.stringify(config)));
		editingField = null;
	}

	// Reset field to global
	function resetFieldToGlobal(fieldKey: string) {
		if (!config.metadata?.priority) return;

		// Mark as touched (user explicitly reset it)
		touchedFields.add(fieldKey);

		// Set to empty array (signals "use global")
		config.metadata.priority[fieldKey] = [];

		// Create a deep clone to trigger reactivity
		onUpdate(JSON.parse(JSON.stringify(config)));
	}

	// Switch to Advanced mode warning
	function switchToAdvanced() {
		mode = 'advanced';
	}

	// Switch to Simple mode warning
	function switchToSimple() {
		const overrideCount = getOverrideCount();
		if (overrideCount > 0) {
			const confirmed = confirm(
				`You have ${overrideCount} field override(s). Switching to Simple mode will hide these settings but not delete them. Continue?`
			);
			if (!confirmed) return;
		}
		mode = 'simple';
	}

	// Filtered fields based on showOnlyOverrides
	const filteredFields = $derived(() => {
		if (!showOnlyOverrides) return metadataFields;
		return metadataFields.filter((field) => isFieldOverridden(field.key));
	});

	// Group fields by category
	const groupedFields = $derived(() => {
		const fields = filteredFields();
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
				items={filterEnabledScrapers(getGlobalPriority())}
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

				{#each Object.entries(groupedFields()) as [category, fields] (category)}
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
										priority={getFieldPriority(field.key)}
										globalPriority={getGlobalPriority()}
										isOverridden={isFieldOverridden(field.key)}
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
					<Button variant="ghost" size="icon" onclick={() => (editingField = null)}>
						{#snippet children()}
							<X class="h-4 w-4" />
						{/snippet}
					</Button>
				</div>

				<!-- Draggable List -->
				<div>
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

				<!-- Info -->
				<div class="bg-accent/50 rounded-lg p-3 text-xs text-muted-foreground">
					<p>
						Scrapers are tried in order from top to bottom. The first scraper that returns data
						for this field will be used.
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
