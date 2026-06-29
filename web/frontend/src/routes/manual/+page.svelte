<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { apiClient } from '$lib/api/client';
	import { startJob } from '$lib/stores/background-job.svelte';
	import { getPendingScrape, clearPendingScrape } from '$lib/stores/pending-scrape.svelte';
	import type { PendingScrape } from '$lib/stores/pending-scrape.svelte';
	import {
		buildManualScrapeRequest,
		classifyInput,
		mergeManualInputs
	} from './logic/build-manual-scrape-request';
	import type { ManualRow } from './logic/build-manual-scrape-request';
	import type { BatchScrapeResponse } from '$lib/api/types';
	import {
		loadManualInputs,
		persistManualInputs,
		clearManualInputs
	} from '$lib/stores/manual-inputs-session';

	let snapshot: PendingScrape | null = $state(null);
	let rows: ManualRow[] = $state([]);
	let submitting = $state(false);
	let errorMsg = $state<string | null>(null);
	const queryClient = useQueryClient();

	function badgeLabel(input: string): string {
		switch (classifyInput(input)) {
			case 'manual-id':
				return 'Manual · ID';
			case 'manual-url':
				return 'Manual · URL (scraper auto-filtered)';
			default:
				return 'Auto';
		}
	}

	onMount(() => {
		const snap = getPendingScrape();
		if (!snap) {
			goto('/browse', { replaceState: true });
			return;
		}
		snapshot = snap;
		const stored = loadManualInputs();
		const merged = mergeManualInputs(stored, snap.files);
		rows = snap.files.map((f) => ({ filePath: f, input: merged[f] ?? '' }));
	});

	// Persist per-row inputs on every change (typing / remove / clear) so a Back
	// round-trip to /browse preserves them (D4b: merge keyed by path, never
	// blind-overwrite — re-advance hydrates via mergeManualInputs above).
	$effect(() => {
		if (!snapshot) return;
		const map = mergeManualInputs(
			Object.fromEntries(rows.map((r) => [r.filePath, r.input])),
			rows.map((r) => r.filePath)
		);
		persistManualInputs(map);
	});

	function removeRow(idx: number) {
		rows = rows.filter((_, i) => i !== idx);
	}

	function clearAllOverrides() {
		rows = rows.map((r) => ({ ...r, input: '' }));
	}

	async function submit() {
		if (!snapshot || submitting) return;
		submitting = true;
		errorMsg = null;
		try {
			const req = buildManualScrapeRequest(rows, {
				destination: snapshot.destination.trim() || undefined,
				operation_mode: snapshot.effectiveOperationMode,
				selected_scrapers: snapshot.showScraperSelector ? snapshot.selectedScrapers : undefined,
				force: snapshot.force,
				preset: snapshot.update ? snapshot.preset : undefined,
				scalar_strategy: snapshot.update ? snapshot.scalarStrategy : undefined,
				array_strategy: snapshot.update ? snapshot.arrayStrategy : undefined,
				update: snapshot.update
			});
			const res: BatchScrapeResponse = await apiClient.batchScrape(req);
			startJob(res.job_id);
			await queryClient.invalidateQueries({ queryKey: ['batch-jobs'] });
			clearPendingScrape();
			rows = [];
			clearManualInputs();
		} catch (e) {
			errorMsg = e instanceof Error ? e.message : 'Failed to start manual scrape';
		} finally {
			submitting = false;
		}
	}
</script>

<svelte:head><title>Manual Scrape</title></svelte:head>

{#if snapshot}
	<div class="mx-auto max-w-4xl p-6 space-y-6">
		<div class="flex items-center justify-between">
			<h1 class="text-2xl font-semibold">Manual Scrape</h1>
			<button class="text-sm text-muted-foreground hover:text-foreground" onclick={() => goto('/browse')}>
				← Back to browse
			</button>
		</div>

		<!-- Read-only summary of inherited globals (D6) -->
		<dl class="rounded-lg border border-border p-4 text-sm space-y-1">
			<div class="flex justify-between"><dt class="text-muted-foreground">Mode</dt>
				<dd class="font-medium">{snapshot.update ? 'Update Metadata' : 'Scrape & Organize'}</dd></div>
			<div class="flex justify-between"><dt class="text-muted-foreground">Operation</dt>
				<dd class="font-medium">{snapshot.effectiveOperationMode}</dd></div>
			{#if !snapshot.update}
				<div class="flex justify-between"><dt class="text-muted-foreground">Destination</dt>
					<dd class="font-medium">{snapshot.destination || '(in place)'}</dd></div>
			{/if}
			<div class="flex justify-between"><dt class="text-muted-foreground">Scrapers</dt>
				<dd class="font-medium">
					{#if snapshot.showScraperSelector}{snapshot.selectedScrapers.join(', ')}
					{:else}All enabled{/if}
				</dd></div>
			{#if snapshot.update}
				<div class="flex justify-between"><dt class="text-muted-foreground">Preset</dt>
					<dd class="font-medium">{snapshot.preset ?? '—'}</dd></div>
				<div class="flex justify-between"><dt class="text-muted-foreground">Strategies</dt>
					<dd class="font-medium">{snapshot.scalarStrategy ?? '—'} / {snapshot.arrayStrategy ?? '—'}</dd></div>
			{/if}
			<div class="flex justify-between"><dt class="text-muted-foreground">Force refresh</dt>
				<dd class="font-medium">{snapshot.force ? 'on' : 'off'}</dd></div>
		</dl>

		{#if errorMsg}
			<p class="rounded-lg border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive">{errorMsg}</p>
		{/if}

		<div class="space-y-2">
			<div class="flex items-center justify-between">
				<h2 class="font-medium">Files ({rows.length})</h2>
				{#if rows.some((r) => r.input.trim() !== '')}
					<button class="text-sm text-muted-foreground hover:text-foreground" onclick={clearAllOverrides}>
						Clear all overrides
					</button>
				{/if}
			</div>
			{#each rows as row, i (row.filePath)}
				<div class="flex items-center gap-2 rounded-lg border border-border p-2">
					<span class="min-w-0 flex-1 truncate text-sm" title={row.filePath}>{row.filePath}</span>
					<input
						class="w-64 rounded border border-border px-2 py-1 text-sm"
						placeholder="Auto — type ID or URL to override"
						aria-label="Manual input for {row.filePath}"
						bind:value={row.input}
					/>
					<span class="w-44 text-xs text-muted-foreground" role="status" aria-live="polite">
						{badgeLabel(row.input)}
					</span>
					<button
						class="text-xs text-muted-foreground hover:text-destructive"
						aria-label="Remove {row.filePath} from batch"
						onclick={() => removeRow(i)}
					>Remove from batch</button>
				</div>
			{/each}
		</div>

		<button
			class="w-full rounded-lg bg-primary px-4 py-2 text-primary-foreground disabled:opacity-50"
			disabled={submitting || rows.length === 0}
			onclick={submit}
		>
			{submitting ? 'Starting…' : 'Start manual scrape'}
		</button>
	</div>
{/if}
