<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import { SquarePen, RotateCcw } from 'lucide-svelte';
	import Button from '../ui/Button.svelte';
	import type { FieldStatus } from './priority';
	import { formatScraperName } from './scraperNames';

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
		label: m.priority_status_inherited(),
			row: 'bg-background'
		},
		skipped: {
			dot: 'bg-slate-500',
			badge: 'text-slate-600',
		label: m.priority_status_skipped(),
			row: 'bg-red-50/50 border-red-200 dark:bg-red-950/20 dark:border-red-900'
		},
		custom: {
			dot: 'bg-orange-500',
			badge: 'text-orange-600',
		label: m.priority_status_custom(),
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
		aria-label={m.priority_aria_status({ label: a.label })}
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
				<span class="italic">{m.priority_suppressed()}</span>
			{:else if status === 'custom' && priority.length === 0}
				<!-- All scrapers in this custom override are disabled/unqueryable.
				     The field will be empty at runtime — don't show the global
				     chain as that would be misleading. Show a warning instead. -->
				<span class="italic text-destructive">{m.priority_all_disabled()}</span>
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
				aria-label={m.priority_reset_to_global_aria()}
				title={m.priority_reset_to_global_title()}
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
			aria-label={m.priority_edit_aria()}
			title={m.priority_edit_title()}
		>
			{#snippet children()}
				<SquarePen class="h-4 w-4" />
			{/snippet}
		</Button>
	</div>
</div>
