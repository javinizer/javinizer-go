<script lang="ts">
	import { GripVertical } from 'lucide-svelte';
	import type { Snippet } from 'svelte';

	interface Props {
		items: string[];
		onReorder?: (items: string[]) => void;
		disabled?: boolean;
		children?: Snippet<[{ item: string; index: number }]>;
	}

	let { items = $bindable(), onReorder, disabled = false, children }: Props = $props();

	let draggedIndex = $state<number | null>(null);
	let dragOverIndex = $state<number | null>(null);

	function handleDragStart(index: number) {
		if (disabled) return;
		draggedIndex = index;
	}

	function handleDragOver(event: DragEvent, index: number) {
		if (disabled || draggedIndex === null) return;
		event.preventDefault();
		dragOverIndex = index;
	}

	function handleDrop(index: number) {
		if (disabled || draggedIndex === null) return;

		const newItems = [...items];
		const [removed] = newItems.splice(draggedIndex, 1);
		newItems.splice(index, 0, removed);

		items = newItems;
		draggedIndex = null;
		dragOverIndex = null;

		if (onReorder) {
			onReorder(newItems);
		}
	}

	function handleDragEnd() {
		draggedIndex = null;
		dragOverIndex = null;
	}

	function moveUp(index: number) {
		if (index === 0 || disabled) return;
		const newItems = [...items];
		[newItems[index], newItems[index - 1]] = [newItems[index - 1], newItems[index]];
		items = newItems;
		if (onReorder) {
			onReorder(newItems);
		}
	}

	function moveDown(index: number) {
		if (index === items.length - 1 || disabled) return;
		const newItems = [...items];
		[newItems[index], newItems[index + 1]] = [newItems[index + 1], newItems[index]];
		items = newItems;
		if (onReorder) {
			onReorder(newItems);
		}
	}
</script>

<div class="space-y-2">
	{#each items as item, index (item)}
		<div
			class="flex items-center gap-2 p-3 bg-background border rounded-lg transition-all {draggedIndex ===
			index
				? 'opacity-50'
				: ''} {dragOverIndex === index ? 'border-primary' : ''} {disabled
				? 'cursor-not-allowed opacity-60'
				: 'hover:border-primary/50'}"
			draggable={!disabled}
			ondragstart={() => handleDragStart(index)}
			ondragover={(e) => handleDragOver(e, index)}
			ondrop={() => handleDrop(index)}
			ondragend={handleDragEnd}
			role="listitem"
		>
			{#if !disabled}
				<GripVertical class="h-5 w-5 text-muted-foreground cursor-grab active:cursor-grabbing" />
			{/if}

			<div class="flex-1">
				{#if children}
					{@render children({ item, index })}
				{:else}
					<span class="font-medium">{item}</span>
				{/if}
			</div>

			{#if !disabled}
				<div class="flex gap-1">
					<button
						type="button"
						onclick={() => moveUp(index)}
						disabled={index === 0}
						class="px-2 py-1 text-sm rounded hover:bg-accent disabled:opacity-30"
						aria-label="Move up"
					>
						↑
					</button>
					<button
						type="button"
						onclick={() => moveDown(index)}
						disabled={index === items.length - 1}
						class="px-2 py-1 text-sm rounded hover:bg-accent disabled:opacity-30"
						aria-label="Move down"
					>
						↓
					</button>
				</div>
			{/if}
		</div>
	{/each}
</div>
