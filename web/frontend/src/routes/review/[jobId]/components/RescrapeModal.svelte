<script lang="ts">
	import { quintOut } from 'svelte/easing';
	import { fade, scale } from 'svelte/transition';
	import { LoaderCircle, RotateCcw, X } from 'lucide-svelte';
	import { portalToBody } from '$lib/actions/portal';
	import type { Scraper } from '$lib/api/types';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import ScraperSelector from '$lib/components/ScraperSelector.svelte';
	import * as m from '$lib/paraglide/messages';

	type ScalarStrategy = '' | 'prefer-nfo' | 'prefer-scraper' | 'preserve-existing' | 'fill-missing-only' | 'merge-arrays';

	interface Props {
		show: boolean;
		rescraping: boolean;
		rescrapeMovieId: string;
		rescrapeMovieName?: string;
		bulkMovieCount?: number;
		availableScrapers: Scraper[];
		selectedScrapers: string[];
		manualSearchMode: boolean;
		manualSearchInput: string;
		rescrapePreset?: string;
		rescrapeScalarStrategy: ScalarStrategy;
		onApplyPreset: (preset: 'conservative' | 'gap-fill' | 'aggressive') => void;
		onExecute: (mode: { manualSearchMode: boolean; manualSearchInput: string }) => void;
	}

	let {
		show = $bindable(false),
		rescraping,
		rescrapeMovieId,
		rescrapeMovieName,
		bulkMovieCount = undefined,
		availableScrapers,
		selectedScrapers = $bindable([]),
		manualSearchMode = $bindable(false),
		manualSearchInput = $bindable(''),
		rescrapePreset = $bindable(undefined),
		rescrapeScalarStrategy = $bindable('prefer-nfo'),
		onApplyPreset,
		onExecute
	}: Props = $props();

	function close() {
		if (rescraping) return;
		show = false;
	}
</script>

{#if show}
	<div
		class="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4"
		use:portalToBody
		in:fade|local={{ duration: 140 }}
		out:fade|local={{ duration: 120 }}
	>
		<div
			class="w-full max-w-lg"
			in:scale|local={{ start: 0.97, duration: 180, easing: quintOut }}
			out:scale|local={{ start: 1, opacity: 0.7, duration: 130, easing: quintOut }}
		>
			<Card class="w-full flex flex-col max-h-[90vh]">
				<div class="p-6 border-b flex items-center justify-between">
					<h2 class="text-xl font-bold">
					{#if bulkMovieCount}
						{m.review_rescrape_movies_title({ count: bulkMovieCount })}
					{:else}
						{manualSearchMode ? m.review_manual_search_title() : m.review_rescrape_movie_title({ name: rescrapeMovieName || rescrapeMovieId })}
					{/if}
				</h2>
					<Button variant="ghost" size="icon" onclick={close} disabled={rescraping}>
						{#snippet children()}
							<X class="h-4 w-4" />
						{/snippet}
					</Button>
				</div>

				<div class="flex-1 overflow-auto p-6">
					{#if !bulkMovieCount}
					<div class="flex gap-2 mb-6 p-1 bg-accent rounded-lg">
						<button
							onclick={() => (manualSearchMode = false)}
							class="flex-1 px-4 py-2 rounded transition-all {!manualSearchMode ? 'bg-card shadow-sm font-medium' : 'text-muted-foreground hover:text-foreground'}"
						>
							{m.review_rescrape_from_file()}
						</button>
						<button
							onclick={() => (manualSearchMode = true)}
							class="flex-1 px-4 py-2 rounded transition-all {manualSearchMode ? 'bg-card shadow-sm font-medium' : 'text-muted-foreground hover:text-foreground'}"
						>
							{m.review_manual_search_title()}
						</button>
					</div>
					{/if}

					{#if manualSearchMode}
						<div class="space-y-4">
							<div>
								<label for="manual-search-input" class="text-sm font-medium mb-2 block">
									{m.review_search_input_label()}
								</label>
								<input
									id="manual-search-input"
									type="text"
									bind:value={manualSearchInput}
									placeholder={m.review_search_input_placeholder()}
									class="w-full px-3 py-2 border rounded-md bg-background focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono text-sm"
								/>
								<p class="text-xs text-muted-foreground mt-2">
									{m.review_search_input_hint()}
								</p>
							</div>

							<div>
								<p class="text-sm text-muted-foreground mb-4">
									{m.review_select_scrapers_aggregate()}
								</p>

								<ScraperSelector
									scrapers={availableScrapers}
									bind:selected={selectedScrapers}
									disabled={false}
								/>
							</div>
						</div>
					{:else}
						<p class="text-sm text-muted-foreground mb-4">
							Select which scrapers to use for fetching fresh metadata. The results will be
							aggregated according to your configured priorities.
						</p>

						<ScraperSelector
							scrapers={availableScrapers}
							bind:selected={selectedScrapers}
							disabled={false}
						/>
					{/if}

					<div class="mt-6 space-y-4">
						<div>
							<h3 class="font-semibold mb-2">{m.browse_nfo_merge_strategy()}</h3>
							<p class="text-sm text-muted-foreground mb-3">
								{m.review_nfo_merge_strategy_desc()}
							</p>
						</div>

						<div class="space-y-2">
							<div class="flex items-center justify-between">
								<h4 class="text-sm font-medium">{m.browse_quick_presets()}</h4>
								{#if rescrapePreset}
									<button onclick={() => (rescrapePreset = undefined)} class="text-xs text-primary hover:underline">
										{m.browse_clear_preset()}
									</button>
								{/if}
							</div>
							<div class="grid grid-cols-3 gap-2">
								<button
									onclick={() => onApplyPreset('conservative')}
									class="p-3 rounded-lg border-2 text-sm transition-all {rescrapePreset === 'conservative' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
								>
									<div class="font-medium">{m.browse_preset_conservative()}</div>
									<div class="text-xs text-muted-foreground mt-1">{m.browse_preset_conservative_desc()}</div>
								</button>
								<button
									onclick={() => onApplyPreset('gap-fill')}
									class="p-3 rounded-lg border-2 text-sm transition-all {rescrapePreset === 'gap-fill' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
								>
									<div class="font-medium">{m.browse_preset_gap_fill()}</div>
									<div class="text-xs text-muted-foreground mt-1">{m.browse_preset_gap_fill_desc()}</div>
								</button>
								<button
									onclick={() => onApplyPreset('aggressive')}
									class="p-3 rounded-lg border-2 text-sm transition-all {rescrapePreset === 'aggressive' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
								>
									<div class="font-medium">{m.browse_preset_aggressive()}</div>
									<div class="text-xs text-muted-foreground mt-1">{m.browse_preset_aggressive_desc()}</div>
								</button>
							</div>
						</div>

						<div class="space-y-2">
							<h4 class="text-sm font-medium">{m.review_or_choose_individual()}</h4>
							<div class="grid grid-cols-2 gap-2">
								<button
									onclick={() => {
										rescrapeScalarStrategy = 'prefer-nfo';
										rescrapePreset = undefined;
									}}
									class="p-3 rounded-lg border-2 text-sm transition-all {rescrapeScalarStrategy === 'prefer-nfo' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
								>
									<div class="font-medium">{m.browse_prefer_nfo()}</div>
									<div class="text-xs text-muted-foreground mt-1">{m.review_prefer_nfo_desc()}</div>
								</button>
								<button
									onclick={() => {
										rescrapeScalarStrategy = 'prefer-scraper';
										rescrapePreset = undefined;
									}}
									class="p-3 rounded-lg border-2 text-sm transition-all {rescrapeScalarStrategy === 'prefer-scraper' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
								>
									<div class="font-medium">{m.browse_prefer_scraped()}</div>
									<div class="text-xs text-muted-foreground mt-1">{m.browse_prefer_scraped_desc()}</div>
								</button>
								<button
									onclick={() => {
										rescrapeScalarStrategy = 'preserve-existing';
										rescrapePreset = undefined;
									}}
									class="p-3 rounded-lg border-2 text-sm transition-all {rescrapeScalarStrategy === 'preserve-existing' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
								>
									<div class="font-medium">{m.browse_preserve_existing()}</div>
									<div class="text-xs text-muted-foreground mt-1">{m.browse_preserve_existing_desc()}</div>
								</button>
								<button
									onclick={() => {
										rescrapeScalarStrategy = 'fill-missing-only';
										rescrapePreset = undefined;
									}}
									class="p-3 rounded-lg border-2 text-sm transition-all {rescrapeScalarStrategy === 'fill-missing-only' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
								>
									<div class="font-medium">{m.browse_fill_missing_only()}</div>
									<div class="text-xs text-muted-foreground mt-1">{m.browse_fill_missing_only_desc()}</div>
								</button>
								<button
									onclick={() => {
										rescrapeScalarStrategy = '';
										rescrapePreset = undefined;
									}}
									class="p-3 rounded-lg border-2 text-sm transition-all col-span-2 {rescrapeScalarStrategy === '' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
								>
									<div class="font-medium">{m.review_replace_all()}</div>
									<div class="text-xs text-muted-foreground mt-1">{m.review_replace_all_desc()}</div>
								</button>
							</div>
						</div>
					</div>
				</div>

				<div class="p-6 border-t flex items-center justify-end gap-3">
					<Button variant="outline" onclick={close} disabled={rescraping}>
						{#snippet children()}{m.common_cancel()}{/snippet}
					</Button>
					<Button
						onclick={() => onExecute({ manualSearchMode, manualSearchInput })}
						disabled={rescraping}
					>
						{#snippet children()}
							{#if rescraping}
								<LoaderCircle class="h-4 w-4 mr-2 animate-spin" />
								{bulkMovieCount ? m.review_rescraping_count_movies({ count: bulkMovieCount }) : (manualSearchMode ? m.review_scraping() : m.review_rescraping())}
							{:else}
								<RotateCcw class="h-4 w-4 mr-2" />
								{bulkMovieCount ? m.review_rescrape_count_movies({ count: bulkMovieCount }) : (manualSearchMode ? m.review_search_button() : m.review_rescrape_button())}
							{/if}
						{/snippet}
					</Button>
				</div>
			</Card>
		</div>
	</div>
{/if}
