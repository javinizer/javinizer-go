<script lang="ts">
	import type { VideoCollision, VideoCollisionDecision, VideoCollisionReport } from '$lib/api/types';
	import * as m from '$lib/paraglide/messages';
	interface Props {
		report: VideoCollisionReport | null;
		pending: boolean;
		error: string | null;
		blocked: boolean;
		decisions: Record<string, VideoCollisionDecision>;
		onReview: () => Promise<void>;
		onDecide: (item: VideoCollision, policy: 'fail' | 'skip' | 'replace') => Promise<void>;
	}
	let { report, pending, error, blocked, decisions, onReview, onDecide }: Props = $props();
	const actionableItems = $derived(report?.items.filter((item) =>
		decisions[item.selected_source ?? item.source]?.policy === 'skip' ||
		(item.kind !== 'new' && item.kind !== 'same-file')) ?? []);
	function size(bytes: number) {
		if (bytes < 1024) return `${bytes} B`;
		const unit = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), 3);
		return new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 }).format(bytes / 1024 ** unit) + ' ' + ['B', 'KiB', 'MiB', 'GiB'][unit];
	}
</script>

<section class="rounded-lg border border-border border-l-4 border-l-amber-500 bg-card p-4 space-y-3" aria-label={m.review_video_title()} data-testid="video-collision-review">
	<div class="flex flex-wrap items-center justify-between gap-3">
		<div>
			<h3 class="font-semibold">{m.review_video_title()}</h3>
			<p class="text-xs text-muted-foreground max-w-3xl">{m.review_video_help()}</p>
		</div>
		<button type="button" class="shrink-0 rounded border border-border px-3 py-2 text-sm font-medium hover:bg-muted disabled:opacity-50" disabled={pending} onclick={onReview}>{pending ? m.common_loading() : m.review_video_refresh()}</button>
	</div>
	{#if error}<p role="alert" class="text-sm text-destructive">{error}</p>{/if}
	{#if report}
		<p class="text-sm font-mono" aria-live="polite">{m.review_video_counts({new:report.new, replaced:report.replaced, skipped:report.skipped, unresolved:report.unresolved, same:report.same_file})}</p>
		{#if actionableItems.length > 0}
			<div class="divide-y divide-border max-h-96 overflow-auto">
				{#each actionableItems as item (item.source + item.destination)}
					<div class="py-3 flex flex-col gap-2 lg:flex-row lg:items-center">
						<div class="min-w-0 flex-1 text-xs font-mono space-y-1">
							<div class="break-all">{item.source} <span class="text-muted-foreground">({size(item.source_size)})</span></div>
							<div class="break-all">→ {item.destination} <span class="text-muted-foreground">({size(item.destination_size)})</span></div>
							<div class="text-amber-700 dark:text-amber-400">{item.kind}</div>
						</div>
						<select class="rounded border border-border bg-background px-2 py-2 text-sm" aria-label={m.review_video_decision({path:item.source})} disabled={pending} value={decisions[item.selected_source ?? item.source]?.policy ?? 'fail'} onchange={(event) => onDecide(item, event.currentTarget.value as 'fail' | 'skip' | 'replace')}>
							<option value="fail">{m.review_video_unresolved()}</option>
							<option value="skip">{m.review_video_skip()}</option>
							{#if item.review_token && !item.selected_source && (item.kind === 'file' || item.kind === 'stale')}<option value="replace">{m.review_video_replace()}</option>{/if}
						</select>
					</div>
				{/each}
			</div>
		{/if}
	{/if}
	{#if blocked}<p class="text-xs text-amber-700 dark:text-amber-400">{m.review_video_blocked()}</p>{/if}
</section>
