<script lang="ts">
	import { quintOut } from 'svelte/easing';
	import { fade, scale } from 'svelte/transition';
	import { LoaderCircle, RotateCcw, X } from 'lucide-svelte';
	import { portalToBody } from '$lib/actions/portal';
	import type { Scraper } from '$lib/api/types';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import ScraperSelector from '$lib/components/ScraperSelector.svelte';

	type ScalarStrategy = '' | 'prefer-nfo' | 'prefer-scraper' | 'preserve-existing' | 'fill-missing-only';

	interface Props {
		show: boolean;
		rescraping: boolean;
		rescrapeMovieId: string;
		availableScrapers: Scraper[];
		selectedScrapers: string[];
		manualSearchMode: boolean;
		manualSearchInput: string;
		rescrapePreset?: string;
		rescrapeScalarStrategy: ScalarStrategy;
		onApplyPreset: (preset: 'conservative' | 'gap-fill' | 'aggressive') => void;
		onExecute: () => void;
	}

	let {
		show = $bindable(false),
		rescraping,
		rescrapeMovieId,
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
					<h2 class="text-xl font-bold">{manualSearchMode ? 'Manual Search' : `Rescrape ${rescrapeMovieId}`}</h2>
					<Button variant="ghost" size="icon" onclick={close} disabled={rescraping}>
						{#snippet children()}
							<X class="h-4 w-4" />
						{/snippet}
					</Button>
				</div>

				<div class="flex-1 overflow-auto p-6">
					{#if rescraping}
						<div class="flex flex-col items-center justify-center py-8 space-y-4">
							<LoaderCircle class="h-12 w-12 animate-spin text-primary" />
							<div class="text-center space-y-2">
								<p class="text-sm font-medium">{manualSearchMode ? 'Scraping metadata...' : 'Rescraping metadata...'}</p>
								<p class="text-xs text-muted-foreground">
									Fetching data from {selectedScrapers.join(', ')}
								</p>
							</div>
						</div>
					{:else}
						<div class="flex gap-2 mb-6 p-1 bg-accent rounded-lg">
							<button
								onclick={() => (manualSearchMode = false)}
								class="flex-1 px-4 py-2 rounded transition-all {!manualSearchMode ? 'bg-white shadow-sm font-medium' : 'text-muted-foreground hover:text-foreground'}"
							>
								Rescrape from File
							</button>
							<button
								onclick={() => (manualSearchMode = true)}
								class="flex-1 px-4 py-2 rounded transition-all {manualSearchMode ? 'bg-white shadow-sm font-medium' : 'text-muted-foreground hover:text-foreground'}"
							>
								Manual Search
							</button>
						</div>

						{#if manualSearchMode}
							<div class="space-y-4">
								<div>
									<label for="manual-search-input" class="text-sm font-medium mb-2 block">
										DVD ID, Content ID, or Direct URL
									</label>
									<input
										id="manual-search-input"
										type="text"
										bind:value={manualSearchInput}
										placeholder="e.g., IPX-123 or https://www.dmm.co.jp/..."
										class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono text-sm"
									/>
									<p class="text-xs text-muted-foreground mt-2">
										Enter a DVD ID (e.g., IPX-123), content ID (e.g., ipx00535), or a direct URL from DMM or R18.dev
									</p>
								</div>

								<div>
									<p class="text-sm text-muted-foreground mb-4">
										Select which scrapers to use. The results will be aggregated according to your configured priorities.
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
								<h3 class="font-semibold mb-2">NFO Merge Strategy</h3>
								<p class="text-sm text-muted-foreground mb-3">
									Choose how to merge existing NFO data with freshly scraped data. Leave empty to replace all data.
								</p>
							</div>

							<div class="space-y-2">
								<div class="flex items-center justify-between">
									<h4 class="text-sm font-medium">Quick Presets</h4>
									{#if rescrapePreset}
										<button onclick={() => (rescrapePreset = undefined)} class="text-xs text-primary hover:underline">
											Clear preset
										</button>
									{/if}
								</div>
								<div class="grid grid-cols-3 gap-2">
									<button
										onclick={() => onApplyPreset('conservative')}
										class="p-3 rounded-lg border-2 text-sm transition-all {rescrapePreset === 'conservative' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
									>
										<div class="font-medium">🛡️ Conservative</div>
										<div class="text-xs text-muted-foreground mt-1">Never overwrite existing</div>
									</button>
									<button
										onclick={() => onApplyPreset('gap-fill')}
										class="p-3 rounded-lg border-2 text-sm transition-all {rescrapePreset === 'gap-fill' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
									>
										<div class="font-medium">📝 Gap Fill</div>
										<div class="text-xs text-muted-foreground mt-1">Fill missing fields only</div>
									</button>
									<button
										onclick={() => onApplyPreset('aggressive')}
										class="p-3 rounded-lg border-2 text-sm transition-all {rescrapePreset === 'aggressive' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
									>
										<div class="font-medium">⚡ Aggressive</div>
										<div class="text-xs text-muted-foreground mt-1">Trust scrapers completely</div>
									</button>
								</div>
							</div>

							<div class="space-y-2">
								<h4 class="text-sm font-medium">Or Choose Individual Strategies</h4>
								<div class="grid grid-cols-2 gap-2">
									<button
										onclick={() => {
											rescrapeScalarStrategy = 'prefer-nfo';
											rescrapePreset = undefined;
										}}
										class="p-3 rounded-lg border-2 text-sm transition-all {rescrapeScalarStrategy === 'prefer-nfo' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
									>
										<div class="font-medium">Prefer NFO</div>
										<div class="text-xs text-muted-foreground mt-1">Keep existing data</div>
									</button>
									<button
										onclick={() => {
											rescrapeScalarStrategy = 'prefer-scraper';
											rescrapePreset = undefined;
										}}
										class="p-3 rounded-lg border-2 text-sm transition-all {rescrapeScalarStrategy === 'prefer-scraper' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
									>
										<div class="font-medium">Prefer Scraped</div>
										<div class="text-xs text-muted-foreground mt-1">Update with fresh data</div>
									</button>
									<button
										onclick={() => {
											rescrapeScalarStrategy = 'preserve-existing';
											rescrapePreset = undefined;
										}}
										class="p-3 rounded-lg border-2 text-sm transition-all {rescrapeScalarStrategy === 'preserve-existing' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
									>
										<div class="font-medium">Preserve Existing</div>
										<div class="text-xs text-muted-foreground mt-1">Never overwrite</div>
									</button>
									<button
										onclick={() => {
											rescrapeScalarStrategy = 'fill-missing-only';
											rescrapePreset = undefined;
										}}
										class="p-3 rounded-lg border-2 text-sm transition-all {rescrapeScalarStrategy === 'fill-missing-only' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
									>
										<div class="font-medium">Fill Missing Only</div>
										<div class="text-xs text-muted-foreground mt-1">Safe gap filling</div>
									</button>
									<button
										onclick={() => {
											rescrapeScalarStrategy = '';
											rescrapePreset = undefined;
										}}
										class="p-3 rounded-lg border-2 text-sm transition-all col-span-2 {rescrapeScalarStrategy === '' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
									>
										<div class="font-medium">Replace All</div>
										<div class="text-xs text-muted-foreground mt-1">Fresh scrape only (ignore existing NFO)</div>
									</button>
								</div>
							</div>
						</div>
					{/if}
				</div>

				<div class="p-6 border-t flex items-center justify-end gap-3">
					<Button variant="outline" onclick={close} disabled={rescraping}>
						{#snippet children()}Cancel{/snippet}
					</Button>
					<Button onclick={onExecute} disabled={rescraping}>
						{#snippet children()}
							{#if rescraping}
								<LoaderCircle class="h-4 w-4 mr-2 animate-spin" />
								{manualSearchMode ? 'Scraping...' : 'Rescraping...'}
							{:else}
								<RotateCcw class="h-4 w-4 mr-2" />
								{manualSearchMode ? 'Search' : 'Rescrape'}
							{/if}
						{/snippet}
					</Button>
				</div>
			</Card>
		</div>
	</div>
{/if}
