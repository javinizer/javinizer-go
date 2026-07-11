<script lang="ts">
	import { quintOut } from 'svelte/easing';
	import { fade, scale, slide } from 'svelte/transition';
	import { Check, ChevronRight, Columns3, LoaderCircle, Minus, X } from 'lucide-svelte';
	import { portalToBody } from '$lib/actions/portal';
	import type { ScraperResult } from '$lib/api/types';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import { formatActressName } from '$lib/utils/actress';
	import { createConfigQuery } from '$lib/query/queries';

	interface FieldDef {
		key: string;
		label: string;
		kind: 'text' | 'id' | 'date' | 'number' | 'list' | 'url' | 'count';
		get: (r: ScraperResult) => string;
	}

	const FIELDS: FieldDef[] = [
		{ key: 'content_id', label: 'Content ID', kind: 'id', get: (r) => r.content_id ?? '' },
		{ key: 'title', label: 'Title', kind: 'text', get: (r) => r.title ?? '' },
		{ key: 'original_title', label: 'Original Title', kind: 'text', get: (r) => r.original_title ?? '' },
		{ key: 'description', label: 'Description', kind: 'text', get: (r) => r.description ?? '' },
		{ key: 'maker', label: 'Maker', kind: 'text', get: (r) => r.maker ?? '' },
		{ key: 'label', label: 'Label', kind: 'text', get: (r) => r.label ?? '' },
		{ key: 'series', label: 'Series', kind: 'text', get: (r) => r.series ?? '' },
		{ key: 'director', label: 'Director', kind: 'text', get: (r) => r.director ?? '' },
		{ key: 'release_date', label: 'Release Date', kind: 'date', get: (r) => r.release_date ?? '' },
		{ key: 'runtime', label: 'Runtime', kind: 'number', get: (r) => (r.runtime ? `${r.runtime} min` : '') },
		{ key: 'rating_score', label: 'Rating', kind: 'number', get: (r) => r.rating?.score?.toString() ?? '' },
		{
			key: 'actresses',
			label: 'Actresses',
			kind: 'list',
			get: (r) =>
				r.actresses
					?.filter((a) => (a.first_name ?? '') !== '' || (a.last_name ?? '') !== '' || (a.japanese_name ?? '') !== '')
					.map((a) => formatActressName(a, firstNameOrder))
					.join(', ') ?? ''
		},
		{ key: 'genres', label: 'Genres', kind: 'list', get: (r) => r.genres?.filter(Boolean).join(', ') ?? '' },
		{ key: 'poster_url', label: 'Poster URL', kind: 'url', get: (r) => r.poster_url ?? '' },
		{ key: 'cover_url', label: 'Cover URL', kind: 'url', get: (r) => r.cover_url ?? '' },
		{ key: 'trailer_url', label: 'Trailer URL', kind: 'url', get: (r) => r.trailer_url ?? '' },
		{
			key: 'screenshot_urls',
			label: 'Screenshots',
			kind: 'count',
			get: (r) =>
				r.screenshot_urls?.length
					? `${r.screenshot_urls.length} image${r.screenshot_urls.length === 1 ? '' : 's'}`
					: ''
		}
	];

	const configQuery = createConfigQuery();
	let firstNameOrder = $derived(configQuery.data?.output?.first_name_order ?? false);

	interface Props {
		show: boolean;
		loading: boolean;
		results: ScraperResult[];
		fieldSources: Record<string, string> | undefined;
		pendingField: string | null;
		onLoad: () => void;
		onApply: (field: string, source: string) => void;
	}

	let {
		show = $bindable(false),
		loading,
		results,
		fieldSources,
		pendingField,
		onLoad,
		onApply
	}: Props = $props();

	let focusedKey = $state<string>(FIELDS[0].key);
	let applyingSource = $state<string | null>(null);

	$effect(() => {
		if (!pendingField) applyingSource = null;
	});

	function close() {
		show = false;
		applyingSource = null;
		expandedSources = new Set();
	}

	function activeSourceFor(field: string): string | undefined {
		return fieldSources?.[field];
	}

	function valueOf(field: FieldDef, r: ScraperResult): string {
		try {
			return field.get(r) ?? '';
		} catch {
			return '';
		}
	}

	interface FieldStatus {
		hasValue: boolean;
		activeSource?: string;
		alternatives: number;
		conflict: boolean;
	}

	function statusFor(field: FieldDef): FieldStatus {
		const active = activeSourceFor(field.key);
		const values = results.map((r) => valueOf(field, r)).filter((v) => v);
		const distinct = new Set(values.map((v) => v.trim().toLowerCase()));
		return {
			hasValue: values.length > 0,
			activeSource: active,
			alternatives: values.length,
			conflict: distinct.size > 1
		};
	}

	const focusedField = $derived(FIELDS.find((f) => f.key === focusedKey) ?? FIELDS[0]);

	let expandedSources = $state<Set<string>>(new Set());
	$effect(() => {
		focusedKey;
		expandedSources = new Set();
	});

	function isLongText(value: string): boolean {
		return value.length > 240;
	}

	function toggleExpand(source: string) {
		const next = new Set(expandedSources);
		if (next.has(source)) next.delete(source);
		else next.add(source);
		expandedSources = next;
	}

	const conflictCount = $derived(FIELDS.filter((f) => statusFor(f).conflict).length);

	function sourceCandidates(field: FieldDef) {
		const seen = new Set<string>();
		return results
			.map((r) => ({ source: r.source ?? 'unknown', value: valueOf(field, r) }))
			.filter((c) => {
				if (!c.value) return false;
				if (seen.has(c.source)) return false;
				seen.add(c.source);
				return true;
			});
	}

	function handleKey(e: KeyboardEvent) {
		if (!show) return;
		if (e.key === 'Escape') {
			close();
			return;
		}
		if (e.target instanceof HTMLElement && e.target.closest('[role="radiogroup"]')) return;
		if (e.key === 'ArrowDown' || e.key === 'j') {
			e.preventDefault();
			const i = FIELDS.findIndex((f) => f.key === focusedKey);
			focusedKey = FIELDS[(i + 1) % FIELDS.length].key;
		} else if (e.key === 'ArrowUp' || e.key === 'k') {
			e.preventDefault();
			const i = FIELDS.findIndex((f) => f.key === focusedKey);
			focusedKey = FIELDS[(i - 1 + FIELDS.length) % FIELDS.length].key;
		}
	}
</script>

<svelte:window onkeydown={handleKey} />

{#if show}
	<div
		class="fixed inset-0 bg-black/60 backdrop-blur-sm z-50 flex items-center justify-center p-4"
		use:portalToBody
		in:fade|local={{ duration: 140 }}
		out:fade|local={{ duration: 120 }}
		role="presentation"
	>
		<div
			class="w-full max-w-5xl"
			role="dialog"
			aria-modal="true"
			aria-labelledby="source-modal-title"
			in:scale|local={{ start: 0.97, duration: 180, easing: quintOut }}
			out:scale|local={{ start: 1, opacity: 0.7, duration: 130, easing: quintOut }}
		>
			<Card class="w-full flex flex-col max-h-[90vh] overflow-hidden">
				<!-- Header -->
				<div class="px-6 py-4 border-b flex items-center justify-between gap-4">
					<div class="flex items-center gap-3 min-w-0">
						<div
							class="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary"
						>
							<Columns3 class="h-4 w-4" />
						</div>
						<div class="min-w-0">
							<h2 id="source-modal-title" class="text-lg font-semibold tracking-tight truncate">
								Source Merge Resolver
							</h2>
							<p class="text-xs text-muted-foreground truncate">
								Pick which scraper's value wins for each field
								{#if conflictCount > 0}
									<span class="text-amber-600 dark:text-amber-400 font-medium"
										>· {conflictCount} with conflicts</span
									>
								{/if}
							</p>
						</div>
					</div>
					<Button variant="ghost" size="icon" onclick={close} aria-label="Close">
						{#snippet children()}
							<X class="h-4 w-4" />
						{/snippet}
					</Button>
				</div>

				{#if loading}
					<div class="flex items-center justify-center py-20 text-muted-foreground">
						<LoaderCircle class="h-5 w-5 mr-2 animate-spin" />
						Loading scraper results…
					</div>
				{:else if results.length === 0}
					<div class="text-center py-20 text-muted-foreground">
						<p class="font-medium">No scraper results in memory</p>
						<p class="text-xs mt-2 max-w-sm mx-auto">
							Raw results are retained for the review window. Re-scrape to repopulate, or they'll
							synthesize from the cached movie on next view.
						</p>
						<Button variant="outline" size="sm" class="mt-4" onclick={onLoad}>
							{#snippet children()}Retry load{/snippet}
						</Button>
					</div>
				{:else}
					<div class="flex-1 flex min-h-0" role="group" aria-label="Field comparison">
						<!-- Left rail: field list -->
						<nav class="w-56 shrink-0 border-r overflow-y-auto py-2" aria-label="Fields">
							{#each FIELDS as field (field.key)}
								{@const st = statusFor(field)}
								{@const isFocused = focusedKey === field.key}
								<button
									type="button"
									onclick={() => (focusedKey = field.key)}
									class="w-full text-left px-3 py-2 flex items-center gap-2.5 transition-colors
										focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2
										{isFocused ? 'bg-accent' : 'hover:bg-accent/50'}"
								>
									<span
										class="h-1.5 w-1.5 shrink-0 rounded-full
											{st.conflict
											? 'bg-amber-500'
											: st.activeSource
												? 'bg-green-500'
												: st.hasValue
													? 'bg-zinc-400 dark:bg-zinc-600'
													: 'bg-transparent'}"
										aria-hidden="true"
									></span>
									<span
										class="flex-1 text-sm truncate {isFocused
											? 'font-medium'
											: 'text-muted-foreground'}"
									>
										{field.label}
									</span>
									{#if st.alternatives > 1}
										<span class="text-[10px] tabular-nums text-muted-foreground/70"
											>{st.alternatives}</span
										>
									{/if}
									{#if isFocused}
										<ChevronRight class="h-3 w-3 text-muted-foreground shrink-0" />
									{/if}
								</button>
							{/each}
						</nav>

						<!-- Main panel: focused field comparison -->
						<div class="flex-1 min-w-0 overflow-y-auto p-6">
							{#key focusedKey}
								<div in:slide|local={{ duration: 150 }} class="space-y-4">
									<div class="flex items-baseline justify-between gap-3 border-b pb-3">
										<div>
											<h3 class="text-base font-semibold tracking-tight">{focusedField.label}</h3>
											<p class="text-xs text-muted-foreground mt-0.5">
												{sourceCandidates(focusedField).length} source{sourceCandidates(
													focusedField
												).length === 1
													? ''
													: 's'} provided a value
											</p>
										</div>
										{#if activeSourceFor(focusedField.key)}
											<span class="text-xs text-muted-foreground">
												Active:
												<span
													class="text-green-600 dark:text-green-400 font-medium font-mono"
													>{activeSourceFor(focusedField.key)}</span
												>
											</span>
										{/if}
									</div>

									{#if sourceCandidates(focusedField).length === 0}
										<div class="flex items-center gap-2 py-8 text-sm text-muted-foreground">
											<Minus class="h-4 w-4" />
											No source provided a value for this field.
										</div>
									{:else}
										{@const candidates = sourceCandidates(focusedField)}
										<ul
											class="space-y-2"
											role="radiogroup"
											aria-label="{focusedField.label} sources"
											aria-busy={pendingField === focusedField.key}
										>
											{#each candidates as c (c.source)}
												{@const active = activeSourceFor(focusedField.key) === c.source}
												{@const isPending = pendingField === focusedField.key}
												{@const isMonospace =
													focusedField.kind === 'url' ||
													focusedField.kind === 'id' ||
													focusedField.kind === 'date'}
												{@const isClamped = isLongText(c.value) && !expandedSources.has(c.source)}
												<li>
													<div
														class="group relative rounded-lg border transition-all cursor-pointer
															focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2
															{active
															? 'border-green-500/50 bg-green-500/[0.04] dark:bg-green-500/[0.06]'
															: 'border-border hover:border-primary/40 hover:bg-accent/40'}"
														role="radio"
														aria-checked={active}
														aria-disabled={isPending}
														tabindex="0"
														onclick={() => {
															if (window.getSelection()?.toString().trim()) return;
															if (!active && !isPending) {
																applyingSource = c.source;
																onApply(focusedField.key, c.source);
															}
														}}
														onkeydown={(e) => {
															if (e.key === ' ') e.preventDefault();
															if (
																(e.key === 'Enter' || e.key === ' ') &&
																!active &&
																!isPending
															) {
																applyingSource = c.source;
																onApply(focusedField.key, c.source);
															}
														}}
													>
														{#if active}
															<div
																class="absolute left-0 top-0 bottom-0 w-1 rounded-l-lg bg-green-500"
															></div>
														{/if}
														<div class="p-3 pl-4">
															<div class="flex items-center justify-between gap-2 mb-1">
																<div class="flex items-center gap-2 min-w-0">
																	<span
																		class="text-xs font-medium font-mono text-muted-foreground"
																		>{c.source}</span
																	>
																	{#if active}
																		<span
																			class="inline-flex items-center gap-1 text-[10px] uppercase tracking-wide font-semibold text-green-600 dark:text-green-400"
																		>
																			<Check class="h-3 w-3" /> Active
																		</span>
																	{/if}
																</div>
																{#if isPending && applyingSource === c.source}
																	<LoaderCircle
																		class="h-4 w-4 shrink-0 animate-spin text-muted-foreground"
																	/>
																{:else}
																	<span
																		class="flex h-4 w-4 shrink-0 items-center justify-center rounded-full border-2 transition-colors
																			{active
																			? 'border-green-500'
																			: 'border-muted-foreground/40 group-hover:border-primary/60'}"
																		aria-hidden="true"
																	>
																		{#if active}
																			<span class="h-2 w-2 rounded-full bg-green-500"></span>
																		{/if}
																	</span>
																{/if}
															</div>
															<p
																class="text-sm break-words whitespace-pre-wrap
																{isMonospace ? 'font-mono text-xs' : ''}
																{isClamped ? 'line-clamp-4' : ''}"
															>
																{c.value}
															</p>
															{#if isLongText(c.value)}
																<button
																	type="button"
																	class="mt-1 text-xs text-muted-foreground hover:text-foreground transition-colors cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
																	onclick={(e) => {
																		e.stopPropagation();
																		toggleExpand(c.source);
																	}}
																onkeydown={(e) => {
																	if (e.key === 'Enter' || e.key === ' ') e.stopPropagation();
																}}
																>
																	{expandedSources.has(c.source) ? 'Show less' : 'Show more'}
																</button>
															{/if}
														</div>
													</div>
												</li>
											{/each}
										</ul>
									{/if}
								</div>
							{/key}
						</div>
					</div>
				{/if}

				<!-- Footer -->
				<div class="px-6 py-3 border-t flex items-center justify-between gap-3">
					<p class="text-xs text-muted-foreground hidden sm:block">
						<kbd class="px-1.5 py-0.5 rounded border bg-muted font-mono text-[10px]">↑</kbd>
						<kbd class="px-1.5 py-0.5 rounded border bg-muted font-mono text-[10px] ml-1">↓</kbd>
						to navigate fields
					</p>
					<Button variant="outline" onclick={close} class="ml-auto">
						{#snippet children()}Close{/snippet}
					</Button>
				</div>
			</Card>
		</div>
	</div>
{/if}
