<script lang="ts">
	import { onMount } from 'svelte';
	import { flip } from 'svelte/animate';
	import { goto } from '$app/navigation';
	import { browser } from '$app/environment';
	import { cubicOut, quintOut } from 'svelte/easing';
	import { fade, fly, scale } from 'svelte/transition';
	import {
		Activity,
		ArrowRight,
		BookOpen,
		CheckCircle2,
		Clock3,
		FolderOpen,
		History,
		Link2,
		RefreshCw,
		Settings,
		TriangleAlert,
		Users,
		CircleX
	} from 'lucide-svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { apiClient } from '$lib/api/client';
	import { websocketStore } from '$lib/stores/websocket';
	import type { ActressListResponse, HealthResponse, HistoryRecord, HistoryStats } from '$lib/api/types';

	const STORAGE_KEY_INPUT = 'javinizer_input_path';
	const STORAGE_KEY_OUTPUT = 'javinizer_output_path';

	let loading = $state(true);
	let refreshing = $state(false);
	let initialized = $state(false);
	let dashboardError = $state<string | null>(null);

	let health = $state<HealthResponse | null>(null);
	let stats = $state<HistoryStats | null>(null);
	let recentRuns = $state<HistoryRecord[]>([]);
	let historyWindow = $state<HistoryRecord[]>([]);
	let actressTotal = $state<number | null>(null);
	let currentWorkingDirectory = $state('');
	let inputPath = $state('');
	let outputPath = $state('');
	let recentRenderVersion = $state(0);

	const wsState = $derived($websocketStore);
	const recentRunCount = $derived(recentRuns.length);
	const releaseVersion = $derived(health?.version ?? 'unknown');

	const sevenDayMetrics = $derived.by(() => {
		const now = Date.now();
		const sevenDaysAgo = now - 7 * 24 * 60 * 60 * 1000;
		const records = historyWindow.filter((record) => {
			const timestamp = Date.parse(record.created_at);
			return !Number.isNaN(timestamp) && timestamp >= sevenDaysAgo;
		});

		const total = records.length;
		const failed = records.filter((record) => record.status === 'failed').length;
		const success = records.filter((record) => record.status === 'success').length;
		const successRate = total > 0 ? Math.round((success / total) * 100) : 0;

		return { total, failed, successRate };
	});

	const latestActivity = $derived.by(() => {
		if (wsState.messages.length === 0) return null;
		return wsState.messages[wsState.messages.length - 1];
	});

	const activeJobCount = $derived.by(() => {
		const terminal = new Set(['completed', 'failed', 'cancelled', 'skipped', 'done']);
		let active = 0;

		for (const [, files] of Object.entries(wsState.messagesByFile)) {
			const statuses = Object.values(files).map((msg) => msg.status.toLowerCase());
			if (statuses.length === 0) continue;
			const hasActive = statuses.some((status) => !terminal.has(status));
			if (hasActive) active += 1;
		}

		return active;
	});

	function itemDelay(index: number): number {
		return Math.min(index * 30, 240);
	}

	function formatDate(dateStr: string): string {
		const date = new Date(dateStr);
		return new Intl.DateTimeFormat('en-US', {
			dateStyle: 'medium',
			timeStyle: 'short'
		}).format(date);
	}

	function operationLabel(operation: string): string {
		const labels: Record<string, string> = {
			scrape: 'Scrape',
			organize: 'Organize',
			download: 'Download',
			nfo: 'NFO'
		};
		return labels[operation] || operation;
	}

	function statusBadgeClass(status: string): string {
		switch (status) {
			case 'success':
				return 'bg-green-100 text-green-700';
			case 'failed':
				return 'bg-red-100 text-red-700';
			case 'reverted':
				return 'bg-yellow-100 text-yellow-700';
			default:
				return 'bg-muted text-muted-foreground';
		}
	}

	function truncateMiddle(value: string, maxLength = 52): string {
		if (!value || value.length <= maxLength) return value;
		const half = Math.floor((maxLength - 3) / 2);
		return `${value.slice(0, half)}...${value.slice(-half)}`;
	}

	function getHealthChecks() {
		return [
			{
				label: 'API Connectivity',
				ok: health?.status === 'ok',
				hint: health?.status === 'ok' ? 'Backend reachable' : 'Cannot reach API',
				actionLabel: 'Retry',
				action: refreshDashboard
			},
			{
				label: 'WebSocket Stream',
				ok: wsState.connected,
				hint: wsState.connected ? 'Real-time updates enabled' : 'No live progress feed',
				actionLabel: 'Browse Jobs',
				action: () => goto('/browse')
			},
			{
				label: 'Scrapers Configured',
				ok: (health?.scrapers?.length ?? 0) > 0,
				hint:
					(health?.scrapers?.length ?? 0) > 0
						? `${health?.scrapers.length ?? 0} scraper(s) available`
						: 'No scraper reported by API',
				actionLabel: 'Open Settings',
				action: () => goto('/settings')
			},
			{
				label: 'Output Path',
				ok: outputPath.trim().length > 0,
				hint: outputPath.trim().length > 0 ? 'Destination path is saved' : 'Set an output path',
				actionLabel: 'Set in Browse',
				action: () => goto('/browse')
			}
		];
	}

	async function openDocs() {
		if (browser) {
			window.open(`${location.protocol}//${location.hostname}:8080/docs`, '_blank', 'noopener,noreferrer');
			return;
		}
		await goto('/docs');
	}

	async function loadDashboard() {
		if (!initialized) {
			loading = true;
		} else {
			refreshing = true;
		}
		dashboardError = null;

		const [
			healthResult,
			statsResult,
			recentResult,
			windowResult,
			actressResult,
			cwdResult
		] = await Promise.allSettled([
			apiClient.health(),
			apiClient.getHistoryStats(),
			apiClient.getHistory({ limit: 8, offset: 0 }),
			apiClient.getHistory({ limit: 200, offset: 0 }),
			apiClient.listActresses({ limit: 1, offset: 0 }),
			apiClient.getCurrentWorkingDirectory()
		]);

		let failedCalls = 0;

		if (healthResult.status === 'fulfilled') {
			health = healthResult.value;
		} else {
			failedCalls += 1;
		}

		if (statsResult.status === 'fulfilled') {
			stats = statsResult.value;
		} else {
			failedCalls += 1;
		}

		if (recentResult.status === 'fulfilled') {
			recentRuns = recentResult.value.records;
			recentRenderVersion += 1;
		} else {
			failedCalls += 1;
			if (!initialized) recentRuns = [];
		}

		if (windowResult.status === 'fulfilled') {
			historyWindow = windowResult.value.records;
		} else {
			failedCalls += 1;
			if (!initialized) historyWindow = [];
		}

		if (actressResult.status === 'fulfilled') {
			const actressData: ActressListResponse = actressResult.value;
			actressTotal = actressData.total;
		} else {
			failedCalls += 1;
			if (!initialized) actressTotal = null;
		}

		if (cwdResult.status === 'fulfilled') {
			currentWorkingDirectory = cwdResult.value.path;
		} else {
			failedCalls += 1;
		}

		if (browser) {
			const savedInput = localStorage.getItem(STORAGE_KEY_INPUT);
			const savedOutput = localStorage.getItem(STORAGE_KEY_OUTPUT);
			inputPath = savedInput || currentWorkingDirectory;
			outputPath = savedOutput || currentWorkingDirectory;
		} else {
			inputPath = currentWorkingDirectory;
			outputPath = currentWorkingDirectory;
		}

		if (failedCalls > 0) {
			dashboardError =
				failedCalls === 6
					? 'Unable to load dashboard data.'
					: `Loaded with ${failedCalls} partial error${failedCalls > 1 ? 's' : ''}.`;
		}

		loading = false;
		refreshing = false;
		initialized = true;
	}

	function refreshDashboard() {
		return loadDashboard();
	}

	onMount(() => {
		loadDashboard();
	});
</script>

<div class="container mx-auto px-4 py-8">
	<div class="max-w-7xl mx-auto space-y-6">
		<!-- Top Row -->
		<div class="grid grid-cols-1 lg:grid-cols-3 gap-4">
			<div class="lg:col-span-2" in:fly|local={{ y: -12, duration: 240, easing: cubicOut }}>
				<Card class="relative overflow-hidden p-6 border-primary/30 bg-linear-to-br from-primary/10 via-card to-card">
					<div class="absolute -right-10 -top-10 h-40 w-40 rounded-full bg-primary/10 blur-2xl"></div>
					<div class="relative space-y-4">
						<div>
							<p class="text-sm font-medium text-primary">Dashboard</p>
							<h1 class="text-4xl font-bold tracking-tight">Javinizer Control Center</h1>
							<p class="text-muted-foreground mt-2 max-w-2xl">
								Run scraping workflows, monitor system health, and jump back into recent jobs.
							</p>
						</div>
						<div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-2">
							<Button onclick={() => goto('/browse')}>
								<FolderOpen class="h-4 w-4" />
								Start Scrape
							</Button>
							<Button variant="outline" onclick={() => goto('/history')}>
								<History class="h-4 w-4" />
								Recent History
							</Button>
							<Button variant="outline" onclick={() => goto('/actresses')}>
								<Users class="h-4 w-4" />
								Manage Actresses
							</Button>
							<Button variant="outline" onclick={() => goto('/settings')}>
								<Settings class="h-4 w-4" />
								Settings
							</Button>
						</div>
					</div>
				</Card>
			</div>

			<div in:fly|local={{ y: -8, duration: 260, easing: quintOut }}>
				<Card class="p-5 h-full">
					<div class="flex items-center justify-between mb-3">
						<h2 class="text-lg font-semibold">Current Activity</h2>
						<Button variant="ghost" size="sm" onclick={refreshDashboard} title="Refresh dashboard">
							<RefreshCw class="h-4 w-4 {refreshing ? 'animate-spin' : ''}" />
						</Button>
					</div>

					{#if latestActivity}
						<div class="space-y-2">
							<div class="flex items-center gap-2 text-sm">
								<Activity class="h-4 w-4 text-primary" />
								<span class="font-medium">Job {latestActivity.job_id.slice(0, 8)}</span>
							</div>
							<p class="text-sm text-muted-foreground line-clamp-2">{latestActivity.message}</p>
							<div class="h-2 rounded-full bg-muted overflow-hidden">
								<div class="h-full bg-primary transition-all duration-300" style="width: {Math.max(0, Math.min(100, latestActivity.progress))}%"></div>
							</div>
							<div class="flex items-center justify-between text-xs text-muted-foreground">
								<span>Status: {latestActivity.status}</span>
								<span>{latestActivity.progress.toFixed(0)}%</span>
							</div>
							<div class="flex gap-2 pt-1">
								<Button size="sm" variant="outline" onclick={() => goto('/browse')}>
									Open Jobs
								</Button>
								<Button size="sm" variant="outline" onclick={() => goto('/history')}>
									View History
								</Button>
							</div>
						</div>
					{:else}
						<div class="text-sm text-muted-foreground space-y-2">
							<p>No recent job activity yet.</p>
							<Button size="sm" onclick={() => goto('/browse')}>
								<ArrowRight class="h-4 w-4" />
								Go to Browse
							</Button>
						</div>
					{/if}

					<div class="mt-4 pt-4 border-t text-xs text-muted-foreground flex items-center justify-between">
						<span>Active jobs: {activeJobCount}</span>
						<span>{wsState.connected ? 'WS connected' : 'WS disconnected'}</span>
					</div>
				</Card>
			</div>
		</div>

		{#if dashboardError}
			<div in:fade|local={{ duration: 150 }}>
				<Card class="p-4 bg-destructive/10 border-destructive text-destructive">
					<div class="flex items-center gap-2">
						<TriangleAlert class="h-5 w-5" />
						<span>{dashboardError}</span>
					</div>
				</Card>
			</div>
		{/if}

		<!-- Metrics -->
		<div class="grid grid-cols-2 lg:grid-cols-4 gap-4">
			<div in:fly|local={{ y: 8, duration: 200, easing: quintOut }}>
				<Card class="p-4">
					<p class="text-sm text-muted-foreground">Total Operations</p>
					<p class="text-3xl font-bold mt-1">{stats?.total ?? '-'}</p>
				</Card>
			</div>
			<div in:fly|local={{ y: 8, duration: 220, easing: quintOut }}>
				<Card class="p-4">
					<p class="text-sm text-muted-foreground">Success Rate (7d)</p>
					<p class="text-3xl font-bold mt-1 text-green-600">{sevenDayMetrics.successRate}%</p>
				</Card>
			</div>
			<div in:fly|local={{ y: 8, duration: 240, easing: quintOut }}>
				<Card class="p-4">
					<p class="text-sm text-muted-foreground">Failures (7d)</p>
					<p class="text-3xl font-bold mt-1 text-red-600">{sevenDayMetrics.failed}</p>
				</Card>
			</div>
			<div in:fly|local={{ y: 8, duration: 260, easing: quintOut }}>
				<Card class="p-4">
					<p class="text-sm text-muted-foreground">Actresses in DB</p>
					<p class="text-3xl font-bold mt-1">{actressTotal ?? '-'}</p>
				</Card>
			</div>
		</div>

		<!-- Main Grid -->
		<div class="grid grid-cols-1 lg:grid-cols-3 gap-4">
			<div class="lg:col-span-2" in:scale|local={{ start: 0.98, duration: 200, easing: quintOut }}>
				<Card class="p-5">
					<div class="flex items-center justify-between mb-3">
						<h2 class="text-xl font-semibold">Recent Runs</h2>
						<div class="text-sm text-muted-foreground">{recentRunCount} recent record(s)</div>
					</div>

					{#if loading && !initialized}
						<div class="text-sm text-muted-foreground py-10 text-center">Loading recent runs...</div>
					{:else if recentRuns.length === 0}
						<div class="text-sm text-muted-foreground py-10 text-center">No operations recorded yet.</div>
					{:else}
						{#key recentRenderVersion}
							<div class="space-y-2" in:fade|local={{ duration: 150 }}>
								{#each recentRuns as run, index (`${run.id}-${recentRenderVersion}`)}
									<div animate:flip={{ duration: 220, easing: quintOut }} in:fly|local={{ y: 8, duration: 180, delay: itemDelay(index), easing: quintOut }}>
										<div class="rounded-md border p-3 hover:bg-accent/40 transition-colors">
											<div class="flex flex-wrap items-center justify-between gap-2">
												<div class="flex items-center gap-2 min-w-0">
													<span class="font-medium">{operationLabel(run.operation)}</span>
													<span class="px-2 py-0.5 text-xs rounded {statusBadgeClass(run.status)}">{run.status}</span>
													{#if run.movie_id}
														<span class="text-xs text-muted-foreground truncate max-w-44">{run.movie_id}</span>
													{/if}
												</div>
												<div class="text-xs text-muted-foreground">{formatDate(run.created_at)}</div>
											</div>
											{#if run.error_message}
												<p class="text-xs text-red-600 mt-1 line-clamp-1">{run.error_message}</p>
											{/if}
										</div>
									</div>
								{/each}
							</div>
						{/key}
					{/if}

					<div class="pt-3 mt-3 border-t flex justify-end">
						<Button variant="outline" onclick={() => goto('/history')}>
							<History class="h-4 w-4" />
							Open Full History
						</Button>
					</div>
				</Card>
			</div>

			<div class="space-y-4" in:fade|local={{ duration: 200 }}>
				<Card class="p-5">
					<h2 class="text-lg font-semibold mb-3">Setup Health</h2>
					<div class="space-y-3">
						{#each getHealthChecks() as check}
							<div class="rounded-md border p-3">
								<div class="flex items-start justify-between gap-2">
									<div class="min-w-0">
										<div class="flex items-center gap-2">
											{#if check.ok}
												<CheckCircle2 class="h-4 w-4 text-green-600" />
											{:else}
												<CircleX class="h-4 w-4 text-red-600" />
											{/if}
											<p class="font-medium text-sm">{check.label}</p>
										</div>
										<p class="text-xs text-muted-foreground mt-1">{check.hint}</p>
									</div>
									{#if !check.ok}
										<Button size="sm" variant="outline" onclick={check.action}>{check.actionLabel}</Button>
									{/if}
								</div>
							</div>
						{/each}
					</div>
				</Card>

				<Card class="p-5">
					<h2 class="text-lg font-semibold mb-3">Path Shortcuts</h2>
					<div class="space-y-2 text-sm">
						<div class="rounded-md border p-2">
							<p class="text-xs text-muted-foreground">Working Directory</p>
							<p class="font-medium" title={currentWorkingDirectory}>{truncateMiddle(currentWorkingDirectory)}</p>
						</div>
						<div class="rounded-md border p-2">
							<p class="text-xs text-muted-foreground">Input Path</p>
							<p class="font-medium" title={inputPath}>{truncateMiddle(inputPath)}</p>
						</div>
						<div class="rounded-md border p-2">
							<p class="text-xs text-muted-foreground">Output Path</p>
							<p class="font-medium" title={outputPath}>{truncateMiddle(outputPath)}</p>
						</div>
					</div>
					<div class="grid grid-cols-2 gap-2 mt-3">
						<Button size="sm" variant="outline" onclick={() => goto('/browse')}>
							<Link2 class="h-4 w-4" />
							Open Browse
						</Button>
						<Button size="sm" variant="outline" onclick={openDocs}>
							<BookOpen class="h-4 w-4" />
							API Docs
						</Button>
					</div>
				</Card>
			</div>
		</div>

		<div class="flex items-center justify-between text-xs text-muted-foreground px-1" in:fade|local={{ duration: 180 }}>
			<div class="flex items-center gap-2">
				<Clock3 class="h-3.5 w-3.5" />
				<span>WebSocket messages: {wsState.messages.length}</span>
				<span>•</span>
				<span>Version: {releaseVersion}</span>
			</div>
			<div>
				{refreshing ? 'Refreshing dashboard...' : 'Dashboard ready'}
			</div>
		</div>
	</div>
</div>
