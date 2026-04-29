<script lang="ts">
	import { flip } from 'svelte/animate';
	import { quintOut } from 'svelte/easing';
	import { fly } from 'svelte/transition';
	import { Pencil, Trash2 } from 'lucide-svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import type { Actress } from '$lib/api/types';

	let {
		actresses,
		itemDelay,
		getDisplayName,
		isSelected,
		onToggleSelection,
		onStartEdit,
		onRemoveActress,
		deletePending
	}: {
		actresses: Actress[];
		itemDelay: (index: number) => number;
		getDisplayName: (actress: Actress) => string;
		isSelected: (actress: Actress) => boolean;
		onToggleSelection: (actress: Actress) => void;
		onStartEdit: (actress: Actress) => void;
		onRemoveActress: (actress: Actress) => void;
		deletePending: boolean;
	} = $props();
</script>

<div class="space-y-2">
	{#each actresses as actress, index (`${actress.id ?? 'na'}-${index}`)}
		<div animate:flip={{ duration: 220, easing: quintOut }} in:fly|local={{ y: 8, duration: 190, delay: itemDelay(index), easing: quintOut }}>
			<Card class="p-3 {isSelected(actress) ? 'ring-2 ring-primary' : ''}">
				<div class="flex items-center gap-3">
					<input
						type="checkbox"
						checked={isSelected(actress)}
						disabled={!actress.id}
						onchange={() => onToggleSelection(actress)}
						aria-label="Select actress for merge"
						class="rounded border-input"
					/>
					<div class="flex-1 min-w-0">
						<div class="flex items-center gap-2 min-w-0">
							<p class="font-medium truncate">{getDisplayName(actress)}</p>
							{#if actress.id}
								<span class="text-xs rounded bg-muted px-2 py-0.5">#{actress.id}</span>
							{/if}
							{#if actress.dmm_id && actress.dmm_id > 0}
								<span class="text-xs rounded bg-muted px-2 py-0.5">DMM {actress.dmm_id}</span>
							{/if}
						</div>
						<p class="text-xs text-muted-foreground truncate">
							{actress.japanese_name || '-'}
						</p>
					</div>
					<div class="flex items-center gap-2">
						<Button variant="outline" size="sm" onclick={() => onStartEdit(actress)}>
							<Pencil class="h-4 w-4" />
						</Button>
						<Button
							variant="outline"
							size="sm"
							onclick={() => onRemoveActress(actress)}
							disabled={deletePending}
							class="text-destructive hover:bg-destructive/10"
						>
							<Trash2 class="h-4 w-4" />
						</Button>
					</div>
				</div>
			</Card>
		</div>
	{/each}
</div>
