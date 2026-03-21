<script lang="ts">
	import { SquarePen, RotateCcw } from 'lucide-svelte';
	import Button from '../ui/Button.svelte';

	interface Props {
		fieldName: string;
		fieldLabel: string;
		priority: string[];
		globalPriority: string[];
		isOverridden: boolean;
		onEdit: () => void;
		onReset: () => void;
	}

	let { fieldName, fieldLabel, priority, globalPriority, isOverridden, onEdit, onReset }: Props =
		$props();

	// Helper to format scraper names
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
		return name.charAt(0).toUpperCase() + name.slice(1);
	}
</script>

<div
	class="flex items-center gap-3 p-3 rounded-lg border {isOverridden
		? 'bg-orange-50/50 border-orange-200 dark:bg-orange-950/20 dark:border-orange-900'
		: 'bg-background'}"
>
	<!-- Status Indicator -->
	<div
		class="w-2 h-2 rounded-full shrink-0 {isOverridden
			? 'bg-orange-500'
			: 'bg-green-500'}"
		aria-label={isOverridden ? 'Custom priority' : 'Inherited from global'}
	></div>

	<!-- Field Name -->
	<div class="flex-1 min-w-0">
		<div class="font-medium text-sm">{fieldLabel}</div>
		<div class="text-xs text-muted-foreground truncate">
			{#each priority as scraper, index}
				<span class="inline-flex items-center">
					{formatScraperName(scraper)}
					{#if index < priority.length - 1}
						<span class="mx-1 text-muted-foreground/50">→</span>
					{/if}
				</span>
			{/each}
		</div>
	</div>

	<!-- Status Badge -->
	<div class="text-xs font-medium {isOverridden ? 'text-orange-600' : 'text-green-600'}">
		{isOverridden ? 'Custom' : 'Inherited'}
	</div>

	<!-- Actions -->
	<div class="flex gap-1">
		{#if isOverridden}
			<Button variant="ghost" size="icon" onclick={onReset} class="h-8 w-8">
				{#snippet children()}
					<RotateCcw class="h-4 w-4" />
				{/snippet}
			</Button>
		{/if}
		<Button variant="ghost" size="icon" onclick={onEdit} class="h-8 w-8">
			{#snippet children()}
				<SquarePen class="h-4 w-4" />
			{/snippet}
		</Button>
	</div>
</div>
