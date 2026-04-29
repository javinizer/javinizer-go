<script lang="ts">
	import { flip } from 'svelte/animate';
	import { quintOut } from 'svelte/easing';
	import { fly } from 'svelte/transition';
	import { Pencil, Trash2, ImageOff } from 'lucide-svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import type { Actress } from '$lib/api/types';

	let {
		actresses,
		selectedIds,
		itemDelay,
		getDisplayName,
		isSelected,
		onToggleSelection,
		onStartEdit,
		onRemoveActress,
		deletePending
	}: {
		actresses: Actress[];
		selectedIds: number[];
		itemDelay: (index: number) => number;
		getDisplayName: (actress: Actress) => string;
		isSelected: (actress: Actress) => boolean;
		onToggleSelection: (actress: Actress) => void;
		onStartEdit: (actress: Actress) => void;
		onRemoveActress: (actress: Actress) => void;
		deletePending: boolean;
	} = $props();
</script>

<div class="grid grid-cols-1 md:grid-cols-2 gap-3">
	{#each actresses as actress, index (`${actress.id ?? 'na'}-${index}`)}
		<div animate:flip={{ duration: 220, easing: quintOut }} in:fly|local={{ y: 10, duration: 220, delay: itemDelay(index), easing: quintOut }}>
			<Card class="p-3 h-full {isSelected(actress) ? 'ring-2 ring-primary' : ''}">
				<div class="flex items-start gap-3 h-full">
					<div class="pt-1">
						<input
							type="checkbox"
							checked={isSelected(actress)}
							disabled={!actress.id}
							onchange={() => onToggleSelection(actress)}
							aria-label="Select actress for merge"
							class="rounded border-input"
						/>
					</div>
					{#if actress.thumb_url}
						<img
							src={actress.thumb_url}
							alt={getDisplayName(actress)}
							class="w-20 h-24 rounded object-cover border"
							onerror={(event) => {
								(event.currentTarget as HTMLImageElement).style.display = 'none';
							}}
						/>
					{:else}
						<div class="w-20 h-24 rounded border bg-muted flex items-center justify-center text-muted-foreground">
							<ImageOff class="h-4 w-4" />
						</div>
					{/if}

					<div class="flex-1 min-w-0">
						<div class="flex flex-wrap items-center gap-2">
							<h3 class="font-semibold truncate">{getDisplayName(actress)}</h3>
							{#if actress.id}
								<span class="text-xs rounded bg-muted px-2 py-0.5">#{actress.id}</span>
							{/if}
							{#if actress.dmm_id && actress.dmm_id > 0}
								<span class="text-xs rounded bg-muted px-2 py-0.5">DMM {actress.dmm_id}</span>
							{/if}
						</div>
						{#if actress.japanese_name}
							<p class="text-sm text-muted-foreground truncate">{actress.japanese_name}</p>
						{/if}
						{#if actress.aliases}
							<p class="text-xs text-muted-foreground line-clamp-2 mt-1">Aliases: {actress.aliases}</p>
						{/if}
						<div class="flex items-center gap-2 mt-3">
							<Button variant="outline" size="sm" onclick={() => onStartEdit(actress)}>
								<Pencil class="h-4 w-4" />
								Edit
							</Button>
							<Button
								variant="outline"
								size="sm"
								onclick={() => onRemoveActress(actress)}
								disabled={deletePending}
								class="text-destructive hover:bg-destructive/10"
							>
								<Trash2 class="h-4 w-4" />
								Delete
							</Button>
						</div>
					</div>
				</div>
			</Card>
		</div>
	{/each}
</div>
