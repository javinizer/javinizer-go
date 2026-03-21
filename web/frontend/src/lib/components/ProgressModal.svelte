<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { flip } from 'svelte/animate';
	import { cubicOut } from 'svelte/easing';
	import { fade, scale, slide } from 'svelte/transition';
	import { goto } from '$app/navigation';
	import { portalToBody } from '$lib/actions/portal';
	import { apiClient } from '$lib/api/client';
	import { websocketStore } from '$lib/stores/websocket';
	import type { BatchJobResponse, ProgressMessage, FileResult } from '$lib/api/types';
	import { X, CircleCheckBig, CircleX, LoaderCircle, ChevronDown, ChevronRight } from 'lucide-svelte';
	import Button from './ui/Button.svelte';
	import Card from './ui/Card.svelte';

	interface Props {
		jobId: string;
		destination: string;
		updateMode?: boolean;
		onClose: () => void;
	}

	let { jobId, destination, updateMode = false, onClose }: Props = $props();

	let job: BatchJobResponse | null = $state(null);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let pollInterval: ReturnType<typeof setInterval> | null = null;
	let countdown = $state(3);
	let countdownInterval: ReturnType<typeof setInterval> | null = null;
	let cancelRedirect = $state(false);

	// UI state for collapsible sections
	let showCompleted = $state(false);
	let showFailed = $state(false);

	// WebSocket state
	const wsState = $derived($websocketStore);
	const progressMessages = $derived(
		wsState.messages.filter((m: ProgressMessage) => m.job_id === jobId)
	);
	const messagesByFile = $derived(wsState.messagesByFile[jobId] || {});
	const latestMessage = $derived(progressMessages[progressMessages.length - 1]);

	// Categorize files by status
	const activeFiles = $derived.by<FileResult[]>(() => {
		if (!job?.results) return [];
		return (Object.values(job.results) as FileResult[]).filter(r => r.status === 'running');
	});

	const queuedFiles = $derived.by<string[]>(() => {
		if (!job || !job.files) return [];
		const processedPaths = new Set(Object.keys(job.results));
		return job.files.filter(f => !processedPaths.has(f));
	});

	const completedFiles = $derived.by<FileResult[]>(() => {
		if (!job?.results) return [];
		return (Object.values(job.results) as FileResult[]).filter(r => r.status === 'completed');
	});

	const failedFiles = $derived.by<FileResult[]>(() => {
		if (!job?.results) return [];
		return (Object.values(job.results) as FileResult[]).filter(r => r.status === 'failed');
	});

	// Worker pool status - fetched from server config
	let maxWorkers = $state(5); // Default to 5 until config is loaded
	const activeWorkerCount = $derived(activeFiles.length);

	// Fetch server config to get actual max_workers
	async function fetchConfig() {
		try {
			const config = await apiClient.getConfig();
			maxWorkers = config.performance?.max_workers || 5;
		} catch (err) {
			console.error('Failed to fetch config:', err);
			// Keep default value of 5
		}
	}

	async function fetchJob() {
		try {
			job = await apiClient.getBatchJob(jobId);
			loading = false;

			// Stop polling if job is complete (scraping finished)
			if (
				job &&
				(job.status === 'completed' || job.status === 'failed' || job.status === 'cancelled')
			) {
				if (pollInterval) {
					clearInterval(pollInterval);
					pollInterval = null;
				}
				// Redirect to review page if completed successfully
				if (job.status === 'completed' && !countdownInterval && !cancelRedirect) {
					// Start countdown timer
					countdownInterval = setInterval(() => {
						countdown -= 1;
						if (countdown <= 0 && !cancelRedirect) {
							if (countdownInterval) clearInterval(countdownInterval);
							const params = new URLSearchParams({ destination });
							if (updateMode) params.set('update', 'true');
							goto(`/review/${jobId}?${params.toString()}`);
						}
					}, 1000);
				}
			}
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to fetch job status';
			loading = false;
		}
	}

	async function handleCancel() {
		if (!job) return;
		try {
			await apiClient.cancelBatchJob(jobId);
			await fetchJob();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to cancel job';
		}
	}

	onMount(() => {
		fetchConfig(); // Fetch server config to get max_workers
		fetchJob();
		// Poll for updates every 2 seconds
		pollInterval = setInterval(fetchJob, 2000);
	});

	onDestroy(() => {
		if (pollInterval) {
			clearInterval(pollInterval);
		}
		if (countdownInterval) {
			clearInterval(countdownInterval);
		}
	});

	function handleStayHere() {
		cancelRedirect = true;
		if (countdownInterval) {
			clearInterval(countdownInterval);
			countdownInterval = null;
		}
	}

	function handleViewResults() {
		const params = new URLSearchParams({ destination });
		if (updateMode) params.set('update', 'true');
		goto(`/review/${jobId}?${params.toString()}`);
	}

	function getFileDisplayName(path: string): string {
		// Handle both Unix (/) and Windows (\) path separators
		const parts = path.split(/[\\/]/);
		return parts[parts.length - 1] || path;
	}
</script>

<!-- Modal Overlay -->
<div class="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4" use:portalToBody in:fade|local={{ duration: 150 }} out:fade|local={{ duration: 120 }}>
	<div in:scale|local={{ start: 0.97, duration: 190, easing: cubicOut }} out:scale|local={{ start: 1, opacity: 0.75, duration: 140, easing: cubicOut }} class="w-full max-w-3xl">
	<Card class="w-full max-h-[85vh] overflow-hidden flex flex-col">
		<!-- Header -->
		<div class="flex items-center justify-between p-6 border-b">
			<div class="flex items-center gap-3">
				<h2 class="text-2xl font-semibold">Batch Scraping Progress</h2>
				{#if job && job.status === 'running'}
					<div class="flex items-center gap-2 px-3 py-1 bg-blue-50 rounded-full text-sm">
						<LoaderCircle class="h-4 w-4 animate-spin text-blue-600" />
						<span class="font-medium text-blue-700">
							{activeWorkerCount} / {maxWorkers} workers active
						</span>
					</div>
				{/if}
			</div>
			<Button variant="ghost" size="icon" onclick={onClose}>
				<X class="h-4 w-4" />
			</Button>
		</div>

		<!-- Content -->
		<div class="flex-1 overflow-y-auto p-6 space-y-6">
			{#if loading}
				<div class="text-center py-8">
					<LoaderCircle class="h-8 w-8 animate-spin mx-auto mb-2" />
					<p class="text-muted-foreground">Loading job status...</p>
				</div>
			{:else if error}
				<div class="bg-destructive/10 border border-destructive text-destructive px-4 py-3 rounded">
					<p>{error}</p>
				</div>
			{:else if job}
				<!-- Progress Bar -->
				<div class="space-y-2">
					<div class="flex items-center justify-between text-sm">
						<span class="font-medium">Overall Progress</span>
						<span class="text-muted-foreground">
							{job.completed + job.failed} / {job.total_files} files
						</span>
					</div>
					<div class="h-4 bg-secondary rounded-full overflow-hidden">
						<div
							class="h-full bg-primary transition-all duration-300"
							style="width: {job.progress}%"
						></div>
					</div>
					<div class="flex items-center justify-between text-sm text-muted-foreground">
						<span>{job.progress.toFixed(1)}%</span>
						<span>
							{job.completed} completed • {job.failed} failed
						</span>
					</div>
				</div>

				<!-- Status Badge -->
				<div class="flex items-center gap-2">
					<span class="text-sm font-medium">Status:</span>
					{#if job.status === 'completed'}
						<span
							class="inline-flex items-center gap-1 px-2 py-1 bg-green-100 text-green-700 rounded text-sm"
						>
							<CircleCheckBig class="h-4 w-4" />
							Completed
						</span>
					{:else if job.status === 'failed'}
						<span
							class="inline-flex items-center gap-1 px-2 py-1 bg-red-100 text-red-700 rounded text-sm"
						>
							<CircleX class="h-4 w-4" />
							Failed
						</span>
					{:else if job.status === 'cancelled'}
						<span class="inline-flex items-center gap-1 px-2 py-1 bg-gray-100 text-gray-700 rounded text-sm">
							<CircleX class="h-4 w-4" />
							Cancelled
						</span>
					{:else}
						<span class="inline-flex items-center gap-1 px-2 py-1 bg-blue-100 text-blue-700 rounded text-sm">
							<LoaderCircle class="h-4 w-4 animate-spin" />
							{job.status}
						</span>
					{/if}
				</div>

				<!-- Active Files (Processing NOW) -->
				{#if activeFiles.length > 0}
					<div class="space-y-3">
						<h3 class="font-semibold text-blue-600 flex items-center gap-2 text-lg">
							<LoaderCircle class="h-5 w-5 animate-spin" />
							Processing ({activeFiles.length})
						</h3>
						<div class="space-y-2">
							{#each activeFiles as result, index (result.file_path)}
								<div animate:flip={{ duration: 220, easing: cubicOut }} class="active-file border-2 rounded-lg p-3 bg-blue-50/50" style="animation-delay: {index * 0.2}s">
									<div class="flex items-start gap-3">
										<LoaderCircle class="h-5 w-5 text-blue-600 animate-spin mt-0.5 shrink-0" />
										<div class="flex-1 min-w-0">
											<div class="font-medium text-blue-900 truncate">
												{result.movie_id || 'Processing...'}
											</div>
											<div class="text-sm text-blue-700/70 truncate">
												{getFileDisplayName(result.file_path)}
											</div>
											{#if messagesByFile[result.file_path]}
												<div class="text-xs text-blue-600 mt-1">
													{messagesByFile[result.file_path].message}
												</div>
											{/if}
										</div>
									</div>
								</div>
							{/each}
						</div>
					</div>
				{/if}

				<!-- Queued Files (Waiting) -->
				{#if queuedFiles.length > 0}
					<div class="space-y-3">
						<h3 class="font-semibold text-gray-600 flex items-center gap-2">
							<div class="h-5 w-5 rounded-full border-2 border-gray-400 flex items-center justify-center">
								<div class="h-2 w-2 rounded-full bg-gray-400"></div>
							</div>
							Queued ({queuedFiles.length})
						</h3>
						<div class="space-y-1">
							{#each queuedFiles.slice(0, 5) as filePath (filePath)}
								<div animate:flip={{ duration: 180, easing: cubicOut }} class="flex items-center gap-2 p-2 rounded bg-gray-50 text-sm">
									<div class="h-2 w-2 rounded-full bg-gray-400 shrink-0"></div>
									<div class="text-gray-700 truncate">
										{getFileDisplayName(filePath)}
									</div>
								</div>
							{/each}
							{#if queuedFiles.length > 5}
								<div class="text-xs text-gray-500 pl-4 pt-1">
									and {queuedFiles.length - 5} more...
								</div>
							{/if}
						</div>
					</div>
				{/if}

				<!-- Completed Files (Collapsible) -->
				{#if completedFiles.length > 0}
					<div class="space-y-2">
						<button
							onclick={() => showCompleted = !showCompleted}
							class="w-full flex items-center justify-between p-3 rounded-lg bg-green-50 hover:bg-green-100 transition-colors"
						>
							<div class="flex items-center gap-2">
								<CircleCheckBig class="h-5 w-5 text-green-600" />
								<h3 class="font-semibold text-green-700">
									Completed ({completedFiles.length})
								</h3>
							</div>
							{#if showCompleted}
								<ChevronDown class="h-5 w-5 text-green-600" />
							{:else}
								<ChevronRight class="h-5 w-5 text-green-600" />
							{/if}
						</button>
						{#if showCompleted}
							<div class="space-y-1 pl-4 max-h-60 overflow-y-auto" transition:slide|local={{ duration: 180, easing: cubicOut }}>
								{#each completedFiles as result (result.file_path)}
									<div animate:flip={{ duration: 180, easing: cubicOut }} class="flex items-start gap-2 p-2 rounded bg-green-50/50 text-sm">
										<CircleCheckBig class="h-4 w-4 text-green-600 mt-0.5 shrink-0" />
										<div class="flex-1 min-w-0">
											<div class="font-medium text-green-900 truncate">
												{result.movie_id || 'Unknown'}
											</div>
											<div class="text-xs text-green-700/70 truncate">
												{getFileDisplayName(result.file_path)}
											</div>
										</div>
									</div>
								{/each}
							</div>
						{/if}
					</div>
				{/if}

				<!-- Failed Files (Collapsible) -->
				{#if failedFiles.length > 0}
					<div class="space-y-2">
						<button
							onclick={() => showFailed = !showFailed}
							class="w-full flex items-center justify-between p-3 rounded-lg bg-red-50 hover:bg-red-100 transition-colors"
						>
							<div class="flex items-center gap-2">
								<CircleX class="h-5 w-5 text-red-600" />
								<h3 class="font-semibold text-red-700">
									Failed ({failedFiles.length})
								</h3>
							</div>
							{#if showFailed}
								<ChevronDown class="h-5 w-5 text-red-600" />
							{:else}
								<ChevronRight class="h-5 w-5 text-red-600" />
							{/if}
						</button>
						{#if showFailed}
							<div class="space-y-1 pl-4 max-h-60 overflow-y-auto" transition:slide|local={{ duration: 180, easing: cubicOut }}>
								{#each failedFiles as result (result.file_path)}
									<div animate:flip={{ duration: 180, easing: cubicOut }} class="flex items-start gap-2 p-2 rounded bg-red-50/50 text-sm">
										<CircleX class="h-4 w-4 text-red-600 mt-0.5 shrink-0" />
										<div class="flex-1 min-w-0">
											<div class="font-medium text-red-900 truncate">
												{result.movie_id || 'Unknown'}
											</div>
											<div class="text-xs text-red-700/70 truncate">
												{getFileDisplayName(result.file_path)}
											</div>
											{#if result.error}
												<div class="text-xs text-red-600 mt-1 wrap-break-word">
													{result.error}
												</div>
											{/if}
										</div>
									</div>
								{/each}
							</div>
						{/if}
					</div>
				{/if}

				<!-- Latest Progress Message (fallback for jobs without file tracking) -->
				{#if latestMessage && activeFiles.length === 0 && completedFiles.length === 0}
					<div class="bg-accent/50 rounded-lg p-4">
						<p class="text-sm font-medium mb-1">Latest Update:</p>
						<p class="text-sm text-muted-foreground">{latestMessage.message}</p>
						{#if latestMessage.file_path}
							<p class="text-xs text-muted-foreground mt-1">
								{latestMessage.file_path}
							</p>
						{/if}
					</div>
				{/if}
			{/if}
		</div>

		<!-- Footer -->
		<div class="flex items-center justify-between gap-3 p-6 border-t">
			{#if job && job.status === 'running'}
				<div></div>
				<div class="flex items-center gap-3">
					<Button variant="destructive" onclick={handleCancel}>Cancel Job</Button>
					<Button variant="outline" onclick={onClose}>Close & Run in Background</Button>
				</div>
			{:else if job && job.status === 'completed'}
				{#if !cancelRedirect && countdown > 0}
					<div class="flex items-center gap-2">
						<CircleCheckBig class="h-5 w-5 text-green-500" />
						<p class="text-sm font-medium text-green-700">
							Scraping completed! {job.completed} file{job.completed !== 1 ? 's' : ''} processed successfully.
						</p>
					</div>
					<div class="flex items-center gap-3">
						<p class="text-sm text-muted-foreground">Redirecting in {countdown}s...</p>
						<Button variant="outline" onclick={handleStayHere}>Stay Here</Button>
						<Button onclick={handleViewResults}>View Results Now</Button>
					</div>
				{:else}
					<div class="flex items-center gap-2">
						<CircleCheckBig class="h-5 w-5 text-green-500" />
						<p class="text-sm font-medium text-green-700">
							Scraping completed! {job.completed} file{job.completed !== 1 ? 's' : ''} processed successfully.
						</p>
					</div>
					<div class="flex items-center gap-3">
						<Button variant="outline" onclick={onClose}>Close</Button>
						<Button onclick={handleViewResults}>View Results</Button>
					</div>
				{/if}
			{:else}
				<div></div>
				<Button variant="outline" onclick={onClose}>
					{job && (job.status === 'failed' || job.status === 'cancelled') ? 'Close' : 'Close & Run in Background'}
				</Button>
			{/if}
		</div>
	</Card>
	</div>
</div>

<style>
	@keyframes pulse-border {
		0%, 100% {
			border-color: rgb(37 99 235);
			box-shadow: 0 0 0 0 rgba(37, 99, 235, 0.4);
		}
		50% {
			border-color: rgb(96 165 250);
			box-shadow: 0 0 0 4px rgba(37, 99, 235, 0.1);
		}
	}

	.active-file {
		animation: pulse-border 2s ease-in-out infinite;
	}

	/* Stagger animations for multiple active files */
	.active-file:nth-child(1) { animation-delay: 0s; }
	.active-file:nth-child(2) { animation-delay: 0.2s; }
	.active-file:nth-child(3) { animation-delay: 0.4s; }
	.active-file:nth-child(4) { animation-delay: 0.6s; }
	.active-file:nth-child(5) { animation-delay: 0.8s; }
</style>
