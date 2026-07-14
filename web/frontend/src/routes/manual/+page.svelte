<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { useQueryClient } from '@tanstack/svelte-query';
	import {
		ArrowLeft,
		FileText,
		Eraser,
		Trash2,
		Scan,
		LoaderCircle,
		AlertTriangle,
		Hash,
		Globe,
		Sparkles,
		X
	} from 'lucide-svelte';
	import { apiClient } from '$lib/api/client';
	import { startJob } from '$lib/stores/background-job.svelte';
	import { getPendingScrape, setPendingScrape, clearPendingScrape } from '$lib/stores/pending-scrape.svelte';
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
		clearManualInputs,
		batchKeyFromFiles
	} from '$lib/stores/manual-inputs-session';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import ScraperSelector from '$lib/components/ScraperSelector.svelte';
	import { createScrapersQuery } from '$lib/query/queries';
	import { portalToBody } from '$lib/actions/portal';
	import { fade, scale } from 'svelte/transition';
	import { quintOut } from 'svelte/easing';

	let snapshot: PendingScrape | null = $state(null);
	let rows: ManualRow[] = $state([]);
	// Fixed at mount from the initial file set so edits/removals don't rekey
	// the persisted manual inputs mid-session.
	let batchKey = '';
	let submitting = $state(false);
	let errorMsg = $state<string | null>(null);
	let showScraperModal = $state(false);
	let modalSelectedScrapers = $state<string[]>([]);
	const queryClient = useQueryClient();
	const scrapersQuery = createScrapersQuery();
	const enabledScrapers = $derived(
		(scrapersQuery.data ?? []).filter((s) => s.enabled).map((s) => s.display_title || s.name)
	);

	const overridesCount = $derived(rows.filter((r) => r.input.trim() !== '').length);

	function classifyKind(input: string): 'auto' | 'id' | 'url' {
		const k = classifyInput(input);
		if (k === 'manual-id') return 'id';
		if (k === 'manual-url') return 'url';
		return 'auto';
	}

	function badgeClass(input: string): string {
		switch (classifyInput(input)) {
			case 'manual-id':
				return 'bg-primary/10 text-primary ring-1 ring-primary/20';
			case 'manual-url':
				return 'bg-violet-500/10 text-violet-600 dark:text-violet-400 ring-1 ring-violet-500/25';
			default:
				return 'bg-muted text-muted-foreground';
		}
	}

	function badgeShort(input: string): string {
		switch (classifyInput(input)) {
			case 'manual-id':
				return m.manual_badge_id();
			case 'manual-url':
				return m.manual_badge_url();
			default:
				return m.manual_badge_auto();
		}
	}

	function badgeTitle(input: string): string {
		switch (classifyInput(input)) {
			case 'manual-id':
				return m.manual_badge_id_title();
			case 'manual-url':
				return m.manual_badge_url_title();
			default:
				return m.manual_badge_auto_title();
		}
	}

	function fileParts(path: string): { basename: string; dir: string } {
		const idx = path.lastIndexOf('/');
		if (idx < 0) return { basename: path, dir: '' };
		return { basename: path.slice(idx + 1), dir: path.slice(0, idx) };
	}

	function openScraperModal() {
		if (!snapshot) return;
		modalSelectedScrapers = snapshot.showScraperSelector
			? [...snapshot.selectedScrapers]
			: (scrapersQuery.data ?? []).filter((s) => s.enabled).map((s) => s.name);
		showScraperModal = true;
	}

	function applyScraperSelection() {
		if (!snapshot) return;
		snapshot = {
			...snapshot,
			showScraperSelector: true,
			selectedScrapers: [...modalSelectedScrapers]
		};
		showScraperModal = false;
	}

	function resetScraperSelection() {
		if (!snapshot) return;
		snapshot = { ...snapshot, showScraperSelector: false, selectedScrapers: [] };
		showScraperModal = false;
	}

	onMount(() => {
		const snap = getPendingScrape();
		if (!snap) {
			void goto('/browse', { replaceState: true });
			return;
		}
		snapshot = snap;
		batchKey = batchKeyFromFiles(snap.files);
		const stored = loadManualInputs(batchKey);
		const merged = mergeManualInputs(stored, snap.files);
		rows = snap.files.map((f) => ({ filePath: f, input: merged[f] ?? '' }));
	});

	$effect(() => {
		if (!snapshot || !batchKey) return;
		const map = mergeManualInputs(
			Object.fromEntries(rows.map((r) => [r.filePath, r.input])),
			rows.map((r) => r.filePath)
		);
		persistManualInputs(batchKey, map);
	});

	// Persist inherited-setting edits (operation mode, destination, scrapers,
	// force, update/strategy) back to the pending-scrape store so a refresh on
	// /manual re-hydrates the user's edits, not the original /browse snapshot.
	// Reads `snapshot` (local $state) and writes to the store (sessionStorage +
	// module state) — the store's state is not read here, so no loop.
	$effect(() => {
		if (!snapshot) return;
		setPendingScrape(snapshot);
	});

	function removeRow(idx: number) {
		rows = rows.filter((_, i) => i !== idx);
		if (snapshot) {
			// Keep snapshot.files in sync with the visible rows so refresh
			// recovery (which rebuilds from snapshot.files) doesn't restore a
			// removed file, and the persisted pending-scrape stays consistent.
			snapshot = { ...snapshot, files: rows.map((row) => row.filePath) };
		}
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
				preset: snapshot.update ? (snapshot.preset || undefined) : undefined,
				scalar_strategy: snapshot.update ? snapshot.scalarStrategy : undefined,
				array_strategy: snapshot.update ? snapshot.arrayStrategy : undefined,
				update: snapshot.update
			});
			const res: BatchScrapeResponse = await apiClient.batchScrape(req);
			startJob(res.job_id);
			await queryClient.invalidateQueries({ queryKey: ['batch-jobs'] });
			clearPendingScrape();
			rows = [];
			clearManualInputs(batchKey);
		} catch (e) {
			errorMsg = e instanceof Error ? e.message : m.manual_scrape_failed();
		} finally {
			submitting = false;
		}
	}
</script>

<svelte:head><title>{m.manual_title()}</title></svelte:head>

{#if snapshot}
	<div class="container mx-auto px-4 py-8 pb-32">
		<div class="max-w-7xl mx-auto space-y-8">
			<!-- Header -->
			<div class="flex items-start justify-between gap-4">
				<div>
					<h1 class="text-3xl font-bold tracking-tight">{m.manual_title()}</h1>
					<p class="text-muted-foreground mt-1.5">
						{m.manual_desc()}
					</p>
				</div>
				<Button variant="ghost" size="sm" onclick={() => void goto('/browse')}>
					{#snippet children()}
						<ArrowLeft class="h-4 w-4" aria-hidden="true" />
						{m.manual_back_to_browse()}
					{/snippet}
				</Button>
			</div>

			<!-- Inherited settings (read-only) -->
			<Card class="p-5">
				<div class="flex items-center gap-2 mb-4">
					<span class="h-2 w-2 rounded-full bg-primary/60"></span>
					<h2 class="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
						{m.manual_selected_settings()}
					</h2>
				</div>
				<dl class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-x-8 gap-y-4">
					<div>
						<dt class="text-xs text-muted-foreground">{m.manual_mode()}</dt>
						<dd class="mt-0.5">
							<div class="inline-flex rounded-md border bg-background p-0.5 text-xs">
								<button type="button" onclick={() => { if (snapshot) snapshot.update = false; }} class="rounded px-2.5 py-1 transition-colors {!snapshot.update ? 'bg-primary text-primary-foreground font-medium' : 'text-muted-foreground hover:text-foreground'}">{m.manual_mode_scrape_organize()}</button>
								<button type="button" onclick={() => { if (snapshot) snapshot.update = true; }} class="rounded px-2.5 py-1 transition-colors {snapshot.update ? 'bg-primary text-primary-foreground font-medium' : 'text-muted-foreground hover:text-foreground'}">{m.manual_mode_update_metadata()}</button>
							</div>
						</dd>
					</div>
					<div>
						<dt class="text-xs text-muted-foreground">{m.manual_operation()}</dt>
						<dd class="mt-0.5">
							<select
								class="w-full h-8 px-2 text-sm border rounded-md bg-background focus:ring-2 focus:ring-primary focus:border-primary transition-all"
								bind:value={snapshot.effectiveOperationMode}
								aria-label={m.manual_operation_aria()}
							>
								<option value="organize">{m.manual_op_organize()}</option>
								<option value="in-place">{m.manual_op_in_place()}</option>
								<option value="in-place-norenamefolder">{m.manual_op_in_place_keep()}</option>
								<option value="metadata-artwork">{m.manual_op_metadata_artwork()}</option>
								<option value="preview">{m.manual_op_preview()}</option>
							</select>
						</dd>
					</div>
					{#if !snapshot.update}
						<div>
							<dt class="text-xs text-muted-foreground">{m.manual_destination()}</dt>
							<dd class="mt-0.5">
								<input
									type="text"
									bind:value={snapshot.destination}
									placeholder={m.manual_destination_placeholder()}
									class="w-full h-8 px-2 text-sm border rounded-md bg-background focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono"
									aria-label={m.manual_destination_aria()}
								/>
							</dd>
						</div>
					{/if}
					<div class="sm:col-span-2 lg:col-span-3">
						<dt class="text-xs text-muted-foreground">{m.manual_scrapers()}</dt>
						<dd class="mt-0.5">
							<button
								type="button"
								class="group -m-1 cursor-pointer rounded-md p-1 text-left transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
								onclick={openScraperModal}
								aria-label={m.manual_scrapers_edit_aria()}
								title={m.manual_scrapers_edit_title()}
							>
								<div class="flex flex-wrap gap-1.5">
									{#each (snapshot.showScraperSelector ? snapshot.selectedScrapers : enabledScrapers) as scraper}
										<span class="rounded-md bg-muted px-2 py-0.5 text-xs font-medium text-foreground transition-colors group-hover:bg-primary/10 group-hover:text-primary">{scraper}</span>
									{/each}
									{#if (snapshot.showScraperSelector ? snapshot.selectedScrapers : enabledScrapers).length === 0}
										<span class="text-sm font-medium">{m.manual_scrapers_all_enabled()}</span>
									{/if}
								</div>
							</button>
						</dd>
					</div>
					{#if snapshot.update}
						<div>
							<dt class="text-xs text-muted-foreground">{m.manual_preset()}</dt>
							<dd class="mt-0.5">
								<select
									class="w-full h-8 px-2 text-sm border rounded-md bg-background focus:ring-2 focus:ring-primary focus:border-primary transition-all"
									bind:value={snapshot.preset}
									aria-label={m.manual_preset_aria()}
								>
									<option value="">{m.manual_preset_none()}</option>
									<option value="conservative">{m.manual_preset_conservative()}</option>
									<option value="gap-fill">{m.manual_preset_gap_fill()}</option>
									<option value="aggressive">{m.manual_preset_aggressive()}</option>
								</select>
							</dd>
						</div>
						<div>
							<dt class="text-xs text-muted-foreground">{m.manual_strategies()}</dt>
							<dd class="mt-0.5 space-y-1">
								<select
									class="w-full h-8 px-2 text-sm border rounded-md bg-background focus:ring-2 focus:ring-primary focus:border-primary transition-all"
									bind:value={snapshot.scalarStrategy}
									aria-label={m.manual_scalar_strategy_aria()}
								>
									<option value="prefer-nfo">{m.manual_scalar_prefer_nfo()}</option>
									<option value="prefer-scraper">{m.manual_scalar_prefer_scraper()}</option>
									<option value="preserve-existing">{m.manual_scalar_preserve_existing()}</option>
									<option value="fill-missing-only">{m.manual_scalar_fill_missing()}</option>
								</select>
								<select
									class="w-full h-8 px-2 text-sm border rounded-md bg-background focus:ring-2 focus:ring-primary focus:border-primary transition-all"
									bind:value={snapshot.arrayStrategy}
									aria-label={m.manual_array_strategy_aria()}
								>
									<option value="merge">{m.manual_array_merge()}</option>
									<option value="replace">{m.manual_array_replace()}</option>
								</select>
							</dd>
						</div>
					{/if}
					<div>
						<dt class="text-xs text-muted-foreground">{m.manual_force_refresh()}</dt>
						<dd class="mt-0.5">
							<button
								type="button"
								role="switch"
								aria-checked={snapshot.force}
								onclick={() => { if (snapshot) snapshot.force = !snapshot.force; }}
								class="relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring {snapshot.force ? 'bg-primary' : 'bg-muted'}"
								aria-label={m.manual_force_refresh_aria()}
							>
								<span class="inline-block h-4 w-4 transform rounded-full bg-background shadow transition-transform {snapshot.force ? 'translate-x-4' : 'translate-x-0.5'}"></span>
							</button>
						</dd>
					</div>
				</dl>
			</Card>

			{#if errorMsg}
				<Card class="p-4 border-destructive/40 bg-destructive/5">
					<div class="flex items-start gap-3 text-sm text-destructive">
						<AlertTriangle class="h-4 w-4 mt-0.5 shrink-0" aria-hidden="true" />
						<span>{errorMsg}</span>
					</div>
				</Card>
			{/if}

			<!-- File rows -->
			<section class="space-y-3">
				<div class="flex items-center justify-between">
					<h2 class="flex items-center gap-2 font-semibold">
						<FileText class="h-4 w-4 text-muted-foreground" aria-hidden="true" />
						<span>{m.manual_files({ count: rows.length })}</span>
					</h2>
					{#if overridesCount > 0}
						<Button variant="ghost" size="sm" onclick={clearAllOverrides}>
							{#snippet children()}
								<Eraser class="h-3.5 w-3.5" aria-hidden="true" />
								{m.manual_clear_overrides()}
							{/snippet}
						</Button>
					{/if}
				</div>

				{#if rows.length === 0}
					<Card class="p-8 text-center">
						<p class="text-sm text-muted-foreground">
							{m.manual_no_files()}
						</p>
					</Card>
				{:else}
					<ul class="space-y-2.5">
						{#each rows as row, i (row.filePath)}
							{@const parts = fileParts(row.filePath)}
							{@const overridden = row.input.trim() !== ''}
							<li
								class="row-in flex flex-col gap-3 rounded-lg border border-border bg-card p-3 transition-colors hover:border-primary/40 lg:flex-row lg:items-center"
								style="--i: {i}"
							>
								<div class="flex items-center gap-3 min-w-0 flex-1">
									<span
										class="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-muted text-muted-foreground"
									>
										<FileText class="h-4 w-4" aria-hidden="true" />
									</span>
									<span class="min-w-0 flex flex-col">
										<span class="truncate font-mono text-sm font-medium" title={row.filePath}
											>{parts.basename}</span
										>
										{#if parts.dir}
											<span class="truncate font-mono text-xs text-muted-foreground" title={parts.dir}
												>{parts.dir}</span
											>
										{/if}
									</span>
								</div>

								<div class="flex flex-wrap items-center gap-2 lg:ml-auto">
									<input
										class="w-full min-w-[12rem] flex-1 rounded-md border border-input bg-background px-3 py-1 text-sm outline-none transition-colors focus:border-primary focus:ring-2 focus:ring-ring/30 lg:w-64 lg:flex-none {overridden
											? 'border-primary/50'
											: ''}"
										placeholder={m.manual_input_placeholder()}
										aria-label={m.manual_input_aria({ filePath: row.filePath })}
										bind:value={row.input}
									/>
									<span
										class="shrink-0 self-stretch rounded-full px-2.5 py-1 text-xs font-medium leading-none flex items-center gap-1 {badgeClass(row.input)}"
										role="status"
										aria-live="polite"
										title={badgeTitle(row.input)}
									>
										{#if classifyKind(row.input) === 'id'}
											<Hash class="h-3 w-3" aria-hidden="true" />
										{:else if classifyKind(row.input) === 'url'}
											<Globe class="h-3 w-3" aria-hidden="true" />
										{:else}
											<Sparkles class="h-3 w-3" aria-hidden="true" />
										{/if}
										{badgeShort(row.input)}
									</span>
									<button
										type="button"
										class="grid h-8 w-8 shrink-0 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
										aria-label={m.manual_remove_aria({ filePath: row.filePath })}
										title={m.manual_remove_title()}
										onclick={() => removeRow(i)}
									>
										<Trash2 class="h-4 w-4" aria-hidden="true" />
									</button>
								</div>
							</li>
						{/each}
					</ul>
				{/if}
			</section>

			<!-- Sticky commit bar -->
			<div class="sticky bottom-0 z-20">
				<Card class="flex items-center justify-between gap-4 p-3 shadow-lg">
					<p class="text-sm text-muted-foreground">
						{m.manual_count_files({ count: rows.length })}
						{#if overridesCount > 0}
							· {m.manual_count_overrides({ count: overridesCount })}
						{/if}
					</p>
					<Button variant="default" disabled={submitting || rows.length === 0} onclick={submit}>
						{#snippet children()}
							{#if submitting}
								<LoaderCircle class="h-4 w-4 animate-spin" aria-hidden="true" />
								{m.manual_starting()}
							{:else}
								<Scan class="h-4 w-4" aria-hidden="true" />
								{m.manual_start_scrape()}
							{/if}
						{/snippet}
					</Button>
				</Card>
			</div>
		</div>
	</div>

	<!-- Scraper selection + order modal -->
	{#if showScraperModal}
		<div
			class="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4"
			role="presentation"
			use:portalToBody
			in:fade|local={{ duration: 140 }}
			out:fade|local={{ duration: 120 }}
			onclick={(e) => { if (e.target === e.currentTarget) showScraperModal = false; }}
			onkeydown={(e) => { if (e.key === 'Escape') showScraperModal = false; }}
		>
			<div class="w-full max-w-lg" in:scale|local={{ start: 0.97, duration: 180, easing: quintOut }} out:scale|local={{ start: 1, opacity: 0.7, duration: 130, easing: quintOut }}>
				<Card class="w-full flex flex-col max-h-[90vh]">
					<div class="p-6 border-b flex items-center justify-between">
						<h2 class="text-xl font-bold">{m.manual_scrapers_modal_title()}</h2>
						<Button variant="ghost" size="icon" onclick={() => (showScraperModal = false)}>
							{#snippet children()}
								<X class="h-4 w-4" />
							{/snippet}
						</Button>
					</div>
					<div class="flex-1 overflow-auto p-6">
						<p class="text-sm text-muted-foreground mb-4">
							{m.manual_scraper_order_desc()}
						</p>
						<ScraperSelector
							scrapers={scrapersQuery.data ?? []}
							bind:selected={modalSelectedScrapers}
							disabled={false}
						/>
					</div>
					<div class="p-6 border-t flex items-center justify-between gap-3">
						<Button variant="ghost" onclick={resetScraperSelection}>
							{#snippet children()}{m.manual_reset_all_enabled()}{/snippet}
						</Button>
						<div class="flex gap-3">
							<Button variant="outline" onclick={() => (showScraperModal = false)}>
								{#snippet children()}{m.common_cancel()}{/snippet}
							</Button>
							<Button onclick={applyScraperSelection}>
								{#snippet children()}{m.common_apply()}{/snippet}
							</Button>
						</div>
					</div>
				</Card>
			</div>
		</div>
	{/if}
{/if}

<style>
	.row-in {
		animation: row-in 0.4s cubic-bezier(0.16, 1, 0.3, 1) both;
		animation-delay: calc(var(--i, 0) * 35ms);
	}
	@keyframes row-in {
		from {
			opacity: 0;
			transform: translateY(6px);
		}
		to {
			opacity: 1;
			transform: none;
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.row-in {
			animation: none;
		}
	}
</style>
