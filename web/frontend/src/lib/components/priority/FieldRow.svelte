<script lang="ts">
	import { SquarePen, RotateCcw } from 'lucide-svelte';
	import Button from '../ui/Button.svelte';
	import type { FieldStatus } from './priority';

	interface Props {
		fieldName: string;
		fieldLabel: string;
		priority: string[];
		globalPriority: string[];
		status: FieldStatus;
		onEdit: () => void;
		onReset: () => void;
	}

	let { fieldName, fieldLabel, priority, globalPriority, status, onEdit, onReset }: Props =
		$props();

	// Helper to format scraper names
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
		return name.charAt(0).toUpperCase() + name.slice(1);
	}

	// Per-status visual language.
	// inherited (green): no override, uses the global priority list.
	// skipped   (red/slate): suppressed via ["__skip__"] — field left empty.
	// custom   (orange): an exclusive override listing scrapers (possibly fewer
	//             than the global list — the user removed some for this field).
	const appearance: Record<
		FieldStatus,
		{ dot: string; badge: string; label: string; row: string }
	> = {
		inherited: {
			dot: 'bg-green-500',
			badge: 'text-green-600',
			label: 'Inherited',
			row: 'bg-background'
		},
		skipped: {
			dot: 'bg-slate-500',
			badge: 'text-slate-600',
			label: 'Skipped',
			row: 'bg-red-50/50 border-red-200 dark:bg-red-950/20 dark:border-red-900'
		},
		custom: {
			dot: 'bg-orange-500',
			badge: 'text-orange-600',
			label: 'Custom',
			row: 'bg-orange-50/50 border-orange-200 dark:bg-orange-950/20 dark:border-orange-900'
		}
	};

	const a = $derived(appearance[status]);
	const isOverridden = $derived(status !== 'inherited');
</script>

<div class="flex items-center gap-3 p-3 rounded-lg border {a.row}">
	<!-- Status Indicator -->
	<div
		class="w-2 h-2 rounded-full shrink-0 {a.dot}"
		role="img"
		aria-label="{a.label} priority"
	></div>

	<!-- Field Name -->
	<div class="flex-1 min-w-0">
		<div class="font-medium text-sm">
			{fieldLabel}
		</div>
		<div class="text-xs text-muted-foreground truncate">
			{#if status === 'skipped'}
				<!-- Suppressed via ["__skip__"]: the field is deliberately left empty
				     (no scrapers consulted). Distinguished from custom/orange so the
				     user sees their suppression intent reflected. -->
				<span class="italic">Suppressed (no scrapers consulted)</span>
			{:else if status === 'custom' && priority.length === 0}
				<!-- Defensive fallback: a present-empty [] override is LEGACY and folds
				     to inherit on read (matched in getGlobalPriority), so this branch
				     should rarely fire. Treat as inherited: show the global chain. -->
				{#each globalPriority as scraper, index}
					<span class="inline-flex items-center">
						{formatScraperName(scraper)}
						{#if index < globalPriority.length - 1}
							<span class="mx-1 text-muted-foreground/50">→</span>
						{/if}
					</span>
				{/each}
			{:else}
				{#each priority as scraper, index}
					<span class="inline-flex items-center">
						{formatScraperName(scraper)}
						{#if index < priority.length - 1}
							<span class="mx-1 text-muted-foreground/50">→</span>
						{/if}
					</span>
				{/each}
			{/if}
		</div>
	</div>

	<!-- Status Badge -->
	<div class="text-xs font-medium {a.badge}">
		{a.label}
	</div>

	<!-- Actions -->
	<div class="flex gap-1">
		{#if isOverridden}
			<Button
				variant="ghost"
				size="icon"
				onclick={onReset}
				class="h-8 w-8"
				aria-label="Reset to global priority"
				title="Reset to global"
			>
				{#snippet children()}
					<RotateCcw class="h-4 w-4" />
				{/snippet}
			</Button>
		{/if}
		<Button
			variant="ghost"
			size="icon"
			onclick={onEdit}
			class="h-8 w-8"
			aria-label="Edit priority"
			title="Edit priority"
		>
			{#snippet children()}
				<SquarePen class="h-4 w-4" />
			{/snippet}
		</Button>
	</div>
</div>
