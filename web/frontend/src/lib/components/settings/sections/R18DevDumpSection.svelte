<script lang="ts">
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import { apiClient as api } from '$lib/api/client';
	import { websocketStore } from '$lib/stores/websocket';
	import type { DumpStatus } from '$lib/api/types';
	import { Download, RefreshCw, Search, CheckCircle, AlertCircle, Database, Trash2 } from 'lucide-svelte';

	interface Props {
		onConfigChange?: (enabled: boolean) => void;
	}
	let { onConfigChange }: Props = $props();

	let status: DumpStatus | null = $state(null);
	let loading = $state(false);
	let downloading = $state(false);
	let downloadError = $state('');
	let searchQuery = $state('');
	let searchResult: { content_id: string | null; dvd_id: string | null } | null = $state(null);
	let searchError = $state('');
	let error = $state('');
	let dumpEnabled = $state(true);

	// Subscribe to WebSocket messages for dump download progress.
	let wsState = $state<{ messages: { job_id: string; progress: number; message: string; status: string }[] }>({
		messages: [],
	});
	$effect(() => {
		const unsub = websocketStore.subscribe((s) => {
			wsState.messages = s.messages.filter((m) => m.job_id === 'r18dev-dump-download');
		});
		return unsub;
	});

	let downloadProgress = $derived(
		wsState.messages.length > 0 ? wsState.messages[wsState.messages.length - 1] : null,
	);

	async function fetchStatus() {
		loading = true;
		error = '';
		try {
			status = await api.r18dev.getDumpStatus();
			dumpEnabled = status.enabled;
			// Resume tracking an in-flight job: if the settings page was opened
			// or refreshed while a dump download/update is already running on the
			// backend, enter the progress/polling state so the UI follows the job
			// instead of showing stale Download/Clear controls.
			if (status.running && !downloading && !polling) {
				downloading = true;
				pollDownloadProgress();
			}
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to fetch dump status';
		} finally {
			loading = false;
		}
	}

	async function download(updateOnly = false) {
		downloading = true;
		downloadError = '';
		websocketStore.clearMessages('r18dev-dump-download');
		try {
			await api.r18dev.downloadDump(updateOnly);
			// The download runs in a background goroutine after 202 is returned.
			// Poll status to show progress until the dump appears or an error occurs.
			await pollDownloadProgress();
		} catch (e) {
			downloadError = e instanceof Error ? e.message : 'Download failed';
			downloading = false;
		}
	}

	let polling = $state(false);

	async function pollDownloadProgress() {
		polling = true;
		// Record the pre-download state to detect completion as a fallback
		// for when the WebSocket terminal frame is missed.
		const wasPresent = status?.present ?? false;
		const prevImportedAt = status?.imported_at ?? '';
		const prevSourceDate = status?.source_date ?? '';
		// The backend download context may run for up to 30 minutes on slow
		// connections or slow imports. Poll for the full server-side window
		// instead of stopping after a fixed iteration count, and rely on the
		// `running` flag (and WS terminal frames) to determine completion.
		const pollIntervalMs = 3000;
		const deadline = Date.now() + 30 * 60 * 1000;
		while (Date.now() < deadline) {
			if (!polling) return; // stopped by component unmount
			await new Promise((r) => setTimeout(r, pollIntervalMs));
			if (!polling) return;
			// Check WebSocket terminal state first.
			if (downloadProgress) {
				const wsStatus = downloadProgress.message || downloadProgress.status;
				if (wsStatus === 'done' || wsStatus === 'error') {
					await fetchStatus();
					downloading = false;
					polling = false;
					if (wsStatus === 'error') {
						downloadError = 'Download failed. Check logs for details.';
					}
					return;
				}
			}
			// Also refresh status for UI updates (progress bar, etc).
			try {
				const s = await api.r18dev.getDumpStatus();
				status = s;
				// Fallback: if the dump was absent before and is now present,
				// the download completed (WS frame may have been missed).
				if (!wasPresent && s.present) {
					downloading = false;
					polling = false;
					return;
				}
				// Fallback for unchanged updates and missed WS 'done' frames:
				// the backend exposes a `running` flag and a `last_error` field.
				// Once the job is no longer running, distinguish success from
				// failure: a non-empty last_error means the download failed;
				// an empty/absent last_error means it succeeded (the goroutine
				// always sets last_error on completion). This covers the
				// unchanged case where neither imported_at nor source_date
				// changes and the WebSocket terminal frame was missed.
				if (!s.running) {
					if (s.last_error) {
						downloadError = s.last_error;
					}
					downloading = false;
					polling = false;
					return;
				}
			// Fallback for updates: if the dump was already present, detect
				// completion by a changed imported_at timestamp OR a changed
				// source_date (the backend updates it on successful import).
				// If neither changed (unchanged update), check if the WS
				// terminal frame was received — now the backend sends 'done'
				// AFTER clearing running, so /status returns present:true,
				// running:false. We can detect this by checking if status
				// shows present AND the WS says done.
				if (wasPresent && s.imported_at && s.imported_at !== prevImportedAt) {
					downloading = false;
					polling = false;
					return;
				}
				if (wasPresent && s.source_date && s.source_date !== prevSourceDate) {
					downloading = false;
					polling = false;
					return;
				}
				// Last-resort fallback for unchanged updates: if the WS
				// terminal frame says 'done', the update completed.
				if (wasPresent && downloadProgress) {
					const wsStatus = downloadProgress.message || downloadProgress.status;
					if (wsStatus === 'done') {
						downloading = false;
						polling = false;
						return;
					}
				}
			} catch {
				// Auth error or network error — keep polling.
			}
		}
		downloading = false;
		polling = false;
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

	let clearing = $state(false);
	let showClearConfirm = $state(false);

	async function clearDump() {
		clearing = true;
		downloadError = '';
		try {
			await api.r18dev.clearDump();
			await fetchStatus();
		} catch (e) {
			downloadError = e instanceof Error ? e.message : 'Failed to clear dump';
		} finally {
			clearing = false;
			showClearConfirm = false;
		}
	}

	async function toggleDumpEnabled() {
		dumpEnabled = !dumpEnabled;
		onConfigChange?.(dumpEnabled);
		try {
			const cfg = await api.config.getConfig();
			const meta = (cfg as any).metadata || {};
			meta.r18dev_dump = { ...(meta.r18dev_dump || {}), enabled: dumpEnabled };
			(cfg as any).metadata = meta;
			await api.request('/api/v1/config', {
				method: 'PUT',
				body: JSON.stringify(cfg),
			});
			await fetchStatus();
		} catch (e) {
			// Revert on error
			dumpEnabled = !dumpEnabled;
			onConfigChange?.(dumpEnabled);
			downloadError = e instanceof Error ? e.message : 'Failed to update config';
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

	function progressPhase(): string {
		if (!downloadProgress) return downloading ? 'Starting...' : '';
		const msg = downloadProgress.message || downloadProgress.status;
		if (msg === 'downloading') return 'Downloading...';
		if (msg === 'importing') return 'Importing into database...';
		if (msg === 'done') return 'Complete!';
		if (msg === 'error') return 'Download failed';
		return msg;
	}

	$effect(() => {
		fetchStatus();
		return () => { polling = false; };
	});
</script>

<SettingsSection title="r18.dev Dump" description="Manage the local r18.dev database dump for offline content_id resolution" defaultExpanded={false}>
	<!-- Enable/Disable toggle -->
	<div class="flex items-center justify-between mb-4">
		<div>
			<label class="text-sm font-medium" for="dump-enabled">Use r18.dev Dump</label>
			<p class="text-xs text-muted-foreground mt-1">When enabled, the scraper consults the local dump before falling back to HTTP</p>
		</div>
		<button
			id="dump-enabled"
			type="button"
			role="switch"
			aria-checked={dumpEnabled}
			class="relative inline-flex h-6 w-11 items-center rounded-full transition-colors {dumpEnabled ? 'bg-primary' : 'bg-muted'}"
			onclick={toggleDumpEnabled}
		>
			<span class="inline-block h-4 w-4 transform rounded-full bg-white transition-transform {dumpEnabled ? 'translate-x-6' : 'translate-x-1'}"></span>
		</button>
	</div>

	{#if loading}
		<div class="flex items-center gap-2 text-sm text-muted-foreground py-4">
			<RefreshCw class="h-4 w-4 animate-spin"></RefreshCw>
			Loading dump status...
		</div>
	{:else if error}
		<div class="flex items-center gap-2 text-sm text-destructive py-4">
			<AlertCircle class="h-4 w-4"></AlertCircle>
			{error}
		</div>
	{:else if status}
		{#if status.present}
			<div class="space-y-4">
				<div class="flex items-center gap-2 text-sm text-green-600 dark:text-green-400">
					<CheckCircle class="h-4 w-4"></CheckCircle>
					Dump is present and ready
				</div>

				{#if !status.enabled}
					<div class="flex items-center gap-2 text-sm text-amber-600 dark:text-amber-400">
						<AlertCircle class="h-4 w-4"></AlertCircle>
						Dump is downloaded but disabled — the scraper won't use it.
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

				{#if downloadError}
					<div class="flex items-center gap-2 text-sm text-destructive">
						<AlertCircle class="h-4 w-4"></AlertCircle>
						{downloadError}
					</div>
				{/if}

				{#if downloading}
					<div class="space-y-2">
						<div class="flex items-center gap-2 text-sm text-muted-foreground">
							<RefreshCw class="h-4 w-4 animate-spin"></RefreshCw>
							{progressPhase()}
						</div>
						{#if downloadProgress && downloadProgress.progress > 0}
							<div class="w-full bg-muted rounded-full h-2 overflow-hidden">
								<div class="bg-primary h-full transition-all duration-300" style="width: {downloadProgress.progress}%"></div>
							</div>
							<div class="text-xs text-muted-foreground text-right">{Math.round(downloadProgress.progress)}%</div>
						{:else}
							<div class="w-full bg-muted rounded-full h-2 overflow-hidden">
								<div class="bg-muted-foreground/30 h-full animate-pulse" style="width: 30%"></div>
							</div>
						{/if}
					</div>
				{:else}
					<div class="flex gap-2 pt-2">
						<button
							type="button"
							class="inline-flex items-center gap-2 px-3 py-2 text-sm font-medium rounded-md bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed"
							onclick={() => download(true)}
							disabled={downloading}
						>
							<RefreshCw class="h-4 w-4"></RefreshCw>
							Check for Updates
						</button>
						<button
							type="button"
							class="inline-flex items-center gap-2 px-3 py-2 text-sm font-medium rounded-md border border-input bg-background hover:bg-accent"
							onclick={fetchStatus}
						>
							<RefreshCw class="h-4 w-4"></RefreshCw>
							Refresh
						</button>
						<button
							type="button"
							class="inline-flex items-center gap-2 px-3 py-2 text-sm font-medium rounded-md border border-destructive/50 text-destructive hover:bg-destructive/10 disabled:opacity-50 disabled:cursor-not-allowed"
							onclick={() => showClearConfirm = true}
							disabled={downloading || clearing}
						>
							<Trash2 class="h-4 w-4 {clearing ? 'animate-spin' : ''}"></Trash2>
							{clearing ? 'Clearing...' : 'Clear Dump'}
						</button>
					</div>
				{/if}
			</div>
		{:else}
			<div class="space-y-4">
				<div class="flex items-center gap-2 text-sm text-muted-foreground">
					<Database class="h-4 w-4"></Database>
					No dump downloaded. The r18.dev scraper uses HTTP fallback (slower).
				</div>

				{#if downloadError}
					<div class="flex items-center gap-2 text-sm text-destructive">
						<AlertCircle class="h-4 w-4"></AlertCircle>
						{downloadError}
					</div>
				{/if}

				{#if downloading}
					<div class="space-y-2">
						<div class="flex items-center gap-2 text-sm text-muted-foreground">
							<RefreshCw class="h-4 w-4 animate-spin"></RefreshCw>
							{progressPhase()}
						</div>
						{#if downloadProgress && downloadProgress.progress > 0}
							<div class="w-full bg-muted rounded-full h-2 overflow-hidden">
								<div class="bg-primary h-full transition-all duration-300" style="width: {downloadProgress.progress}%"></div>
							</div>
							<div class="text-xs text-muted-foreground text-right">{Math.round(downloadProgress.progress)}%</div>
						{:else}
							<div class="w-full bg-muted rounded-full h-2 overflow-hidden">
								<div class="bg-muted-foreground/30 h-full animate-pulse" style="width: 30%"></div>
							</div>
						{/if}
					</div>
				{:else}
					<button
						type="button"
						class="inline-flex items-center gap-2 px-4 py-2 text-sm font-medium rounded-md bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed"
						onclick={() => download(false)}
						disabled={downloading}
					>
						<Download class="h-4 w-4"></Download>
						Download Dump (~250MB)
					</button>
				{/if}
			</div>
		{/if}

		{#if status?.present}
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
					<Search class="h-4 w-4"></Search>
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
		{/if}
	{:else}
		<p class="text-sm text-muted-foreground">Unable to load dump status.</p>
	{/if}

	{#if showClearConfirm}
		<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/50" role="dialog" aria-modal="true">
			<div class="bg-card border border-border rounded-lg p-6 max-w-sm mx-4">
				<h3 class="text-lg font-semibold mb-2">Clear Dump?</h3>
				<p class="text-sm text-muted-foreground mb-4">
					This will delete the local r18.dev dump database. The scraper will fall back to HTTP content_id resolution until you re-download it.
				</p>
				<div class="flex justify-end gap-2">
					<button
						type="button"
						class="inline-flex items-center px-3 py-2 text-sm font-medium rounded-md border border-input bg-background hover:bg-accent"
						onclick={() => showClearConfirm = false}
						disabled={clearing}
					>Cancel</button>
					<button
						type="button"
						class="inline-flex items-center gap-2 px-3 py-2 text-sm font-medium rounded-md bg-destructive text-destructive-foreground hover:bg-destructive/90 disabled:opacity-50 disabled:cursor-not-allowed"
						onclick={clearDump}
						disabled={clearing}
					>
						{#if clearing}<RefreshCw class="h-4 w-4 animate-spin"></RefreshCw>{:else}<Trash2 class="h-4 w-4"></Trash2>{/if}
						{clearing ? 'Clearing...' : 'Clear Dump'}
					</button>
				</div>
			</div>
		</div>
	{/if}
</SettingsSection>
