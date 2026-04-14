<script lang="ts">
	import { flip } from 'svelte/animate';
	import { cubicOut } from 'svelte/easing';
	import type { Scraper } from '$lib/api/types';
	import Button from './ui/Button.svelte';
	import { GripVertical, ChevronUp, ChevronDown, X } from 'lucide-svelte';
	import { fade, fly } from 'svelte/transition';

	let { scrapers = [], selected = $bindable([]), disabled = false }: {
		scrapers?: Scraper[];
		selected?: string[];
		disabled?: boolean;
	} = $props();

	let draggedIndex = $state<number | null>(null);
	let dragOverIndex = $state<number | null>(null);

	// Get scraper by name
	function getScraperByName(name: string): Scraper | undefined {
		return scrapers.find((s) => s.name === name);
	}

	// Get selected scrapers in order
	const selectedScrapers = $derived(
		selected
			.map((name) => getScraperByName(name))
			.filter((s): s is Scraper => s !== undefined)
	);

	// Get unselected enabled scrapers
	const unselectedScrapers = $derived(
		scrapers.filter((s) => s.enabled && !selected.includes(s.name))
	);

	function addScraper(name: string) {
		if (!selected.includes(name)) {
			selected = [...selected, name];
		}
	}

	function removeScraper(name: string) {
		selected = selected.filter((s) => s !== name);
	}

	function moveUp(index: number) {
		if (index > 0) {
			const newSelected = [...selected];
			[newSelected[index - 1], newSelected[index]] = [
				newSelected[index],
				newSelected[index - 1]
			];
			selected = newSelected;
		}
	}

	function moveDown(index: number) {
		if (index < selected.length - 1) {
			const newSelected = [...selected];
			[newSelected[index], newSelected[index + 1]] = [
				newSelected[index + 1],
				newSelected[index]
			];
			selected = newSelected;
		}
	}

	function selectAll() {
		selected = scrapers.filter((s) => s.enabled).map((s) => s.name);
	}

	function selectNone() {
		selected = [];
	}

	// Drag and drop handlers
	function handleDragStart(index: number) {
		if (!disabled) {
			draggedIndex = index;
		}
	}

	function handleDragOver(event: DragEvent, index: number) {
		event.preventDefault();
		if (!disabled) {
			dragOverIndex = index;
		}
	}

	function handleDrop(event: DragEvent, dropIndex: number) {
		event.preventDefault();
		if (draggedIndex !== null && draggedIndex !== dropIndex && !disabled) {
			const newSelected = [...selected];
			const [removed] = newSelected.splice(draggedIndex, 1);
			newSelected.splice(dropIndex, 0, removed);
			selected = newSelected;
		}
		draggedIndex = null;
		dragOverIndex = null;
	}

	function handleDragEnd() {
		draggedIndex = null;
		dragOverIndex = null;
	}
</script>

<div class="border rounded-lg p-4">
	<div class="flex items-center justify-between mb-4">
		<h3 class="text-sm font-medium">Select Scrapers & Set Priority</h3>
		<div class="flex gap-2">
			<Button variant="outline" size="sm" onclick={selectAll} {disabled}>
				{#snippet children()}All{/snippet}
			</Button>
			<Button variant="outline" size="sm" onclick={selectNone} {disabled}>
				{#snippet children()}None{/snippet}
			</Button>
		</div>
	</div>

	{#if selected.length > 0}
		<!-- Selected scrapers with priority order -->
		<div class="mb-4">
			<div class="text-xs text-muted-foreground mb-2">
				Priority Order (drag to reorder, higher = more priority)
			</div>
			<div class="space-y-2">
				{#each selectedScrapers as scraper, index (scraper.name)}
					<div
						animate:flip={{ duration: 220, easing: cubicOut }}
						in:fly|local={{ y: 8, duration: 170, easing: cubicOut }}
						out:fade|local={{ duration: 120 }}
						role="listitem"
						draggable={!disabled}
						ondragstart={() => handleDragStart(index)}
						ondragover={(e) => handleDragOver(e, index)}
						ondrop={(e) => handleDrop(e, index)}
						ondragend={handleDragEnd}
						class="flex items-center gap-2 p-2 bg-background border rounded-lg transition-all {draggedIndex ===
						index
							? 'opacity-45 scale-[0.99]'
							: ''} {dragOverIndex === index ? 'border-primary bg-primary/5 shadow-sm scale-[1.01]' : ''} {disabled
							? 'cursor-not-allowed opacity-50'
							: 'cursor-move hover:border-primary/50'}"
					>
						<!-- Drag handle -->
						<GripVertical
							class="h-4 w-4 text-muted-foreground {disabled ? 'opacity-50' : ''}"
						/>

						<!-- Priority number -->
						<div
							class="flex items-center justify-center w-6 h-6 rounded-full bg-primary text-primary-foreground text-xs font-semibold"
						>
							{index + 1}
						</div>

						<!-- Scraper name -->
						<span class="flex-1 text-sm font-medium">{scraper.display_title}</span>

						<!-- Move buttons -->
						<div class="flex gap-1">
							<button
								onclick={() => moveUp(index)}
								disabled={disabled || index === 0}
								class="p-1 hover:bg-accent rounded transition-colors {disabled || index === 0
									? 'opacity-30 cursor-not-allowed'
									: 'cursor-pointer'}"
								title="Move up"
							>
								<ChevronUp class="h-4 w-4" />
							</button>
							<button
								onclick={() => moveDown(index)}
								disabled={disabled || index === selected.length - 1}
								class="p-1 hover:bg-accent rounded transition-colors {disabled ||
								index === selected.length - 1
									? 'opacity-30 cursor-not-allowed'
									: 'cursor-pointer'}"
								title="Move down"
							>
								<ChevronDown class="h-4 w-4" />
							</button>
							<button
								onclick={() => removeScraper(scraper.name)}
								disabled={disabled}
								class="p-1 hover:bg-red-100 dark:hover:bg-red-900/30 text-red-600 rounded transition-colors {disabled
									? 'opacity-30 cursor-not-allowed'
									: 'cursor-pointer'}"
								title="Remove"
							>
								<X class="h-4 w-4" />
							</button>
						</div>
					</div>
				{/each}
			</div>
		</div>
	{/if}

	{#if unselectedScrapers.length > 0}
		<!-- Available scrapers to add -->
		<div>
			<div class="text-xs text-muted-foreground mb-2">
				{selected.length > 0 ? 'Add More Scrapers' : 'Available Scrapers'}
			</div>
			<div class="space-y-1">
				{#each unselectedScrapers as scraper}
					<button
						in:fade|local={{ duration: 150 }}
						out:fade|local={{ duration: 110 }}
						onclick={() => addScraper(scraper.name)}
						disabled={disabled}
						class="w-full flex items-center p-2 rounded-md hover:bg-accent cursor-pointer transition-colors text-left {disabled
							? 'opacity-50 cursor-not-allowed'
							: ''}"
					>
						<span class="text-sm text-muted-foreground mr-2">+</span>
						<span class="flex-1 text-sm">{scraper.display_title}</span>
					</button>
				{/each}
			</div>
		</div>
	{/if}

	{#if selected.length === 0 && unselectedScrapers.length === 0}
		<div class="text-center py-4 text-sm text-muted-foreground">
			No scrapers available. Please enable scrapers in settings.
		</div>
	{/if}
</div>
