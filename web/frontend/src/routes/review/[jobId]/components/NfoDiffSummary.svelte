<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import { ChevronDown, ChevronRight, ClipboardList } from 'lucide-svelte';
	import type { FieldDifference } from '$lib/api/types';

	interface Props {
		nfoDifferences: FieldDifference[];
	}

	let { nfoDifferences }: Props = $props();

	let expanded = $state(false);
	let showUnchanged = $state(false);

	const comparableFields: { field: string; label: () => string }[] = [
		{ field: 'title', label: () => m.movie_field_title() },
		{ field: 'description', label: () => m.movie_field_description() },
		{ field: 'director', label: () => m.movie_field_director() },
		{ field: 'maker', label: () => m.movie_field_maker() },
		{ field: 'label', label: () => m.movie_field_label() },
		{ field: 'series', label: () => m.movie_field_series() },
		{ field: 'content_id', label: () => m.movie_field_content_id() },
		{ field: 'runtime', label: () => m.movie_field_runtime() },
		{ field: 'rating', label: () => m.movie_field_rating() },
		{ field: 'release_date', label: () => m.movie_field_release_date() },
		{ field: 'cover_url', label: () => m.movie_field_cover() },
		{ field: 'poster_url', label: () => m.movie_field_poster() },
		{ field: 'trailer_url', label: () => m.movie_field_trailer() },
		{ field: 'actresses', label: () => m.movie_field_actresses() },
		{ field: 'genres', label: () => m.movie_field_genres() },
	];

	const diffByField = $derived.by(() => {
		const map = new Map<string, FieldDifference>();
		for (const d of nfoDifferences) map.set(d.field, d);
		return map;
	});

	const unchangedFields = $derived(
		comparableFields.filter((f) => !diffByField.has(f.field)),
	);

	const changeCount = $derived(nfoDifferences.length);

	type ChangeKind = 'changed' | 'new' | 'removed';

	function changeKind(d: FieldDifference): ChangeKind {
		const nfoEmpty = d.nfo_value === null || d.nfo_value === undefined || d.nfo_value === '';
		const scrapedEmpty =
			d.scraped_value === null || d.scraped_value === undefined || d.scraped_value === '';
		if (nfoEmpty && !scrapedEmpty) return 'new';
		if (!nfoEmpty && scrapedEmpty) return 'removed';
		return 'changed';
	}

	function formatValue(v: string | number | boolean | null | undefined): string {
		if (v === null || v === undefined || v === '') return '—';
		if (typeof v === 'boolean') return v ? 'true' : 'false';
		return String(v);
	}

	function fieldLabel(field: string): string {
		const entry = comparableFields.find((f) => f.field === field);
		return entry ? entry.label() : field;
	}

	const kindStyles: Record<ChangeKind, string> = {
		changed: 'text-amber-600 dark:text-amber-400',
		new: 'text-emerald-600 dark:text-emerald-400',
		removed: 'text-red-600 dark:text-red-400',
	};

	const kindDot: Record<ChangeKind, string> = {
		changed: 'bg-amber-500',
		new: 'bg-emerald-500',
		removed: 'bg-red-500',
	};
</script>

{#if changeCount > 0}
	<div class="rounded-lg border border-input bg-background/60 p-3 space-y-2">
		<button
			type="button"
			class="flex w-full items-center gap-2 text-sm font-medium text-foreground hover:text-primary transition-colors"
			aria-expanded={expanded}
			onclick={() => (expanded = !expanded)}
		>
			<ClipboardList class="h-4 w-4 text-muted-foreground" />
			<span>
				{changeCount === 1
					? m.review_fields_will_change_one({ count: changeCount })
					: m.review_fields_will_change({ count: changeCount })}
			</span>
			<span class="ml-auto text-muted-foreground">
				{#if expanded}
					<ChevronDown class="h-4 w-4" />
				{:else}
					<ChevronRight class="h-4 w-4" />
				{/if}
			</span>
		</button>

		{#if expanded}
			<div class="overflow-x-auto">
				<table class="w-full text-xs">
					<thead>
						<tr class="text-left text-muted-foreground border-b border-input">
							<th class="py-1.5 pr-3 font-medium">{m.review_nfo_value_label().replace('Current ', '')}</th>
							<th class="py-1.5 pr-3 font-medium">{m.review_nfo_value_label()}</th>
							<th class="py-1.5 font-medium">{m.review_scraped_value_label()}</th>
						</tr>
					</thead>
					<tbody>
						{#each nfoDifferences as diff (diff.field)}
							{@const kind = changeKind(diff)}
							<tr class="border-b border-input/50 last:border-0">
								<td class="py-1.5 pr-3 align-top">
									<span class="inline-flex items-center gap-1.5 {kindStyles[kind]}">
										<span class="h-2 w-2 rounded-full {kindDot[kind]} shrink-0"></span>
										{fieldLabel(diff.field)}
									</span>
								</td>
								<td class="py-1.5 pr-3 align-top text-muted-foreground break-all">
									{formatValue(diff.nfo_value)}
								</td>
								<td class="py-1.5 align-top text-foreground break-all">
									{formatValue(diff.scraped_value)}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>

			{#if unchangedFields.length > 0}
				<button
					type="button"
					class="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
					onclick={() => (showUnchanged = !showUnchanged)}
				>
					{#if showUnchanged}
						<ChevronDown class="h-3.5 w-3.5" />
						{m.review_hide_unchanged_fields()}
					{:else}
						<ChevronRight class="h-3.5 w-3.5" />
						{m.review_show_unchanged_fields({ count: unchangedFields.length })}
					{/if}
				</button>
				{#if showUnchanged}
					<div class="flex flex-wrap gap-1.5">
						{#each unchangedFields as f (f.field)}
							<span class="inline-flex items-center text-muted-foreground/80 px-2 py-0.5 rounded bg-muted text-xs">
								{f.label()}
							</span>
						{/each}
					</div>
				{/if}
			{/if}
		{/if}
	</div>
{/if}
