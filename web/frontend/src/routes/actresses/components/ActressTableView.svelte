<script lang="ts">
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

<Card class="overflow-hidden">
	<div class="overflow-x-auto">
		<table class="w-full text-sm">
			<thead class="bg-muted/50">
				<tr class="text-left border-b">
					<th class="px-3 py-2 font-medium w-10">Sel</th>
					<th class="px-3 py-2 font-medium">ID</th>
					<th class="px-3 py-2 font-medium">Name</th>
					<th class="px-3 py-2 font-medium">Japanese Name</th>
					<th class="px-3 py-2 font-medium">DMM ID</th>
					<th class="px-3 py-2 font-medium">Aliases</th>
					<th class="px-3 py-2 font-medium text-right">Actions</th>
				</tr>
			</thead>
			<tbody>
				{#each actresses as actress, index (`${actress.id ?? 'na'}-${index}`)}
					<tr class="border-b last:border-b-0 {isSelected(actress) ? 'bg-primary/5' : ''}" in:fly|local={{ y: 6, duration: 170, delay: itemDelay(index), easing: quintOut }}>
						<td class="px-3 py-2 text-muted-foreground">
							<input
								type="checkbox"
								checked={isSelected(actress)}
								disabled={!actress.id}
								onchange={() => onToggleSelection(actress)}
								aria-label="Select actress for merge"
								class="rounded border-input"
							/>
						</td>
						<td class="px-3 py-2 text-muted-foreground">{actress.id ?? '-'}</td>
						<td class="px-3 py-2 font-medium max-w-44 truncate">{getDisplayName(actress)}</td>
						<td class="px-3 py-2 text-muted-foreground max-w-44 truncate">{actress.japanese_name || '-'}</td>
						<td class="px-3 py-2 text-muted-foreground">{actress.dmm_id && actress.dmm_id > 0 ? actress.dmm_id : '-'}</td>
						<td class="px-3 py-2 text-muted-foreground max-w-52 truncate">{actress.aliases || '-'}</td>
						<td class="px-3 py-2">
							<div class="flex items-center justify-end gap-2">
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
						</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
</Card>
