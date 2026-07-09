<script lang="ts">
	import { Sparkles, Loader2 } from 'lucide-svelte';
	import ScraperSelector from '$lib/components/ScraperSelector.svelte';
	import type { Scraper } from '$lib/api/types';

	let {
		selected = $bindable(),
		scrapers = [] as Scraper[],
		loading = false,
		error = null as string | null,
		submitting = false,
	}: {
		selected: string[];
		scrapers: Scraper[];
		loading?: boolean;
		error?: string | null;
		submitting?: boolean;
	} = $props();

	let hasScrapers = $derived(scrapers.length > 0);
	let selectedCount = $derived(selected.length);
</script>

<div class="step-head">
	<div class="step-badge"><Sparkles class="h-5 w-5" /></div>
	<h1 class="step-title">Choose your metadata sources</h1>
	<p class="step-sub">
		Pick the scrapers Javinizer will query for metadata and arrange them by priority — higher means a
		source wins when fields conflict. You can fine-tune each scraper's options later in Settings.
	</p>
</div>

{#if error}
	<div class="alert" role="alert">{error}</div>
{/if}

{#if loading}
	<div class="state">
		<Loader2 class="h-5 w-5 animate-spin" />
		<span>Loading scrapers…</span>
	</div>
{:else if !hasScrapers}
	<div class="state">
		<p>No scrapers reported by the API.</p>
		<p class="state-hint">You can configure scrapers later in Settings → Scrapers.</p>
	</div>
{:else}
	<div class="selector-wrap" class:disabled={submitting}>
		<ScraperSelector scrapers={scrapers} bind:selected={selected} disabled={submitting} showAll={true} />
	</div>

	<div class="summary">
		<span class="summary-dot" data-on={selectedCount > 0}></span>
		{#if selectedCount === 0}
			<span>No scrapers selected — scraping will be unavailable until you enable some.</span>
		{:else}
			<span><strong>{selectedCount}</strong> scraper{selectedCount > 1 ? 's' : ''} selected, ordered by priority.</span>
		{/if}
	</div>
{/if}

<style>
	.step-head {
		margin-bottom: 1.5rem;
	}

	.step-badge {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 44px;
		height: 44px;
		border-radius: 12px;
		background: hsl(var(--primary) / 0.12);
		color: hsl(var(--primary));
		margin-bottom: 0.85rem;
	}

	.step-title {
		font-size: 1.6rem;
		font-weight: 700;
		letter-spacing: -0.02em;
		line-height: 1.15;
	}

	.step-sub {
		margin-top: 0.4rem;
		color: hsl(var(--muted-foreground));
		font-size: 0.92rem;
		line-height: 1.5;
	}

	.alert {
		border: 1px solid hsl(var(--destructive) / 0.4);
		background: hsl(var(--destructive) / 0.1);
		color: hsl(var(--destructive));
		padding: 0.55rem 0.75rem;
		border-radius: 0.5rem;
		font-size: 0.85rem;
		margin-bottom: 1rem;
	}

	.state {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 0.5rem;
		padding: 2.5rem 1rem;
		text-align: center;
		color: hsl(var(--muted-foreground));
		font-size: 0.9rem;
	}

	.state-hint {
		font-size: 0.8rem;
	}

	.selector-wrap.disabled {
		opacity: 0.6;
		pointer-events: none;
	}

	.summary {
		margin-top: 1rem;
		display: flex;
		align-items: center;
		gap: 0.55rem;
		padding: 0.6rem 0.8rem;
		border-radius: 0.5rem;
		background: hsl(var(--muted) / 0.5);
		font-size: 0.82rem;
		color: hsl(var(--muted-foreground));
	}

	.summary strong {
		color: hsl(var(--foreground));
	}

	.summary-dot {
		width: 8px;
		height: 8px;
		border-radius: 9999px;
		background: hsl(var(--muted-foreground) / 0.5);
	}

	.summary-dot[data-on='true'] {
		background: hsl(142 71% 45%);
		box-shadow: 0 0 8px hsl(142 71% 45% / 0.6);
	}
</style>
