<script lang="ts">
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import { apiClient as api } from '$lib/api/client';
	import type { DumpStatus } from '$lib/api/types';
	import { Download, RefreshCw, Search, CheckCircle, AlertCircle, Database } from 'lucide-svelte';

	let status: DumpStatus | null = $state(null);
	let loading = $state(false);
	let downloading = $state(false);
	let searchQuery = $state('');
	let searchResult: { content_id: string | null; dvd_id: string | null } | null = $state(null);
	let searchError = $state('');
	let error = $state('');

	async function fetchStatus() {
		loading = true;
		error = '';
		try {
			status = await api.r18dev.getDumpStatus();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to fetch dump status';
		} finally {
			loading = false;
		}
	}

	async function download(updateOnly = false) {
		downloading = true;
		error = '';
		try {
			await api.r18dev.downloadDump(updateOnly);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Download failed';
		} finally {
			downloading = false;
		}
	}

	async function search() {
		if (!searchQuery.trim()) return;
		searchError = '';
		searchResult = null;
		try {
			searchResult = await api.r18dev.searchDump(searchQuery.trim());
		} catch (e) {
			searchError = e instanceof Error ? e.message : 'Search failed';
		}
	}

	function formatBytes(bytes: number): string {
		if (bytes < 1024) return `${bytes} B`;
		if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
		return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
	}

	function formatNumber(n: number): string {
		return n.toLocaleString();
	}

	$effect(() => {
		fetchStatus();
	});
</script>

<SettingsSection title="r18.dev Dump" description="Manage the local r18.dev database dump for offline content_id resolution" defaultExpanded={false}>
	{#if loading}
		<div class="flex items-center gap-2 text-sm text-muted-foreground py-4">
			<RefreshCw class="h-4 w-4 animate-spin" />
			Loading dump status...
		</div>
	{:else if error}
		<div class="flex items-center gap-2 text-sm text-destructive py-4">
			<AlertCircle class="h-4 w-4" />
			{error}
		</div>
	{:else if status}
		{#if status.present}
			<div class="space-y-4">
				<div class="flex items-center gap-2 text-sm text-green-600 dark:text-green-400">
					<CheckCircle class="h-4 w-4" />
					Dump is present and ready
				</div>

				{#if !status.enabled}
					<div class="flex items-center gap-2 text-sm text-amber-600 dark:text-amber-400">
						<AlertCircle class="h-4 w-4" />
						Dump is downloaded but disabled in config — the scraper won't use it.
					</div>
				{/if}

				<dl class="grid grid-cols-2 gap-2 text-sm">
					<div>
						<dt class="text-muted-foreground">Rows</dt>
						<dd class="font-medium">{#if status.row_count}{formatNumber(status.row_count)}{:else}—{/if}</dd>
					</div>
					<div>
						<dt class="text-muted-foreground">Size</dt>
						<dd class="font-medium">{#if status.size_bytes}{formatBytes(status.size_bytes)}{:else}—{/if}</dd>
					</div>
					<div>
						<dt class="text-muted-foreground">Source date</dt>
						<dd class="font-medium">{status.source_date || '—'}</dd>
					</div>
					<div>
						<dt class="text-muted-foreground">Imported at</dt>
						<dd class="font-medium">{status.imported_at ? new Date(status.imported_at).toLocaleString() : '—'}</dd>
					</div>
				</dl>

				<div class="flex gap-2 pt-2">
					<button
						type="button"
						class="inline-flex items-center gap-2 px-3 py-2 text-sm font-medium rounded-md bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed"
						onclick={() => download(true)}
						disabled={downloading}
					>
						<RefreshCw class="h-4 w-4 {downloading ? 'animate-spin' : ''}" />
						{downloading ? 'Updating...' : 'Check for Updates'}
					</button>
				</div>
			</div>
		{:else}
			<div class="space-y-4">
				<div class="flex items-center gap-2 text-sm text-muted-foreground">
					<Database class="h-4 w-4" />
					No dump downloaded. The r18.dev scraper uses HTTP fallback (slower).
				</div>

				<button
					type="button"
					class="inline-flex items-center gap-2 px-4 py-2 text-sm font-medium rounded-md bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed"
					onclick={() => download(false)}
					disabled={downloading}
				>
					<Download class="h-4 w-4" />
					{downloading ? 'Downloading...' : 'Download Dump (~250MB)'}
				</button>
			</div>
		{/if}

		<!-- Search box for ad-hoc lookups -->
		<div class="border-t border-border mt-4 pt-4">
			<h4 class="text-sm font-medium mb-2">Search Dump</h4>
			<div class="flex gap-2">
				<input
					type="text"
					bind:value={searchQuery}
					placeholder="dvd_id or content_id (e.g. ABF-030)"
					class="flex-1 px-3 py-2 text-sm rounded-md border border-input bg-background"
					onkeydown={(e) => e.key === 'Enter' && search()}
				/>
				<button
					type="button"
					class="inline-flex items-center gap-1 px-3 py-2 text-sm font-medium rounded-md border border-input bg-background hover:bg-accent"
					onclick={search}
				>
					<Search class="h-4 w-4" />
					Search
				</button>
			</div>
			{#if searchResult}
				<div class="mt-2 p-2 rounded-md bg-muted text-sm">
					{#if searchResult.content_id}
						<span class="text-muted-foreground">content_id:</span>
						<span class="font-mono">{searchResult.content_id}</span>
					{:else if searchResult.dvd_id}
						<span class="text-muted-foreground">dvd_id:</span>
						<span class="font-mono">{searchResult.dvd_id}</span>
					{:else}
						<span class="text-muted-foreground">No match found</span>
					{/if}
				</div>
			{/if}
			{#if searchError}
				<div class="mt-2 text-sm text-destructive">{searchError}</div>
			{/if}
		</div>
	{:else}
		<p class="text-sm text-muted-foreground">Unable to load dump status.</p>
	{/if}
</SettingsSection>
