<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import { CircleAlert } from 'lucide-svelte';
	import type { FieldDifference } from '$lib/api/types';

	interface Props {
		diff: FieldDifference;
	}

	let { diff }: Props = $props();

	let expanded = $state(false);

	const nfoDisplay = $derived(formatValue(diff.nfo_value));
	const scrapedDisplay = $derived(formatValue(diff.scraped_value));

	function formatValue(v: string | number | boolean | null | undefined): string {
		if (v === null || v === undefined || v === '') return '—';
		if (typeof v === 'boolean') return v ? 'true' : 'false';
		return String(v);
	}
</script>

<button
	type="button"
	class="inline-flex items-center text-amber-600 dark:text-amber-400 hover:text-amber-700 dark:hover:text-amber-300 transition-colors align-middle"
	title={m.review_nfo_diff_tooltip()}
	aria-label={m.review_nfo_diff_tooltip()}
	aria-expanded={expanded}
	onclick={(e) => {
		e.preventDefault();
		e.stopPropagation();
		expanded = !expanded;
	}}
>
	<CircleAlert class="h-3.5 w-3.5" />
</button>

{#if expanded}
	<div class="mt-1.5 w-full rounded-md border border-amber-300/60 dark:border-amber-500/40 bg-amber-50/50 dark:bg-amber-950/20 p-2 text-xs space-y-1">
		<div class="flex gap-2">
			<span class="font-medium text-muted-foreground min-w-24 shrink-0">{m.review_nfo_value_label()}</span>
			<span class="break-all text-foreground">{nfoDisplay}</span>
		</div>
		<div class="flex gap-2">
			<span class="font-medium text-muted-foreground min-w-24 shrink-0">{m.review_scraped_value_label()}</span>
			<span class="break-all text-foreground">{scrapedDisplay}</span>
		</div>
	</div>
{/if}
