<script lang="ts">
	import { onDestroy } from 'svelte';
	import { flip } from 'svelte/animate';
	import { cubicOut } from 'svelte/easing';
	import { fade, scale, slide } from 'svelte/transition';
	import { goto } from '$app/navigation';
	import { portalToBody } from '$lib/actions/portal';
	import { apiClient } from '$lib/api/client';
	import { websocketStore } from '$lib/stores/websocket';
	import { createBatchJobPollingQuery, createConfigQuery } from '$lib/query/queries';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import type { BatchJobResponse, ProgressMessage, FileResult } from '$lib/api/types';
	import { X, CircleCheckBig, CircleX, ChevronDown, ChevronRight } from 'lucide-svelte';
	import Button from './ui/Button.svelte';
	import Card from './ui/Card.svelte';

	interface Props {
		jobId: string;
		destination: string;
		updateMode?: boolean;
		onClose: () => void;
	}

	let { jobId, destination, updateMode = false, onClose }: Props = $props();

	const queryClient = useQueryClient();
	const jobQuery = createBatchJobPollingQuery(jobId);
	const configQuery = createConfigQuery();
	let job = $derived(jobQuery.data ?? null);
	let loading = $derived(jobQuery.isPending);
	const cancelMutation = createMutation(() => ({
		mutationFn: () => apiClient.cancelBatchJob(jobId),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ['batch-job-slim', jobId] });
		},
	}));

	let error = $derived(jobQuery.error?.message ?? cancelMutation.error?.message ?? null);
	let maxWorkers = $derived(configQuery.data?.performance?.max_workers || 5);
	let countdown = $state(3);
	let countdownInterval: ReturnType<typeof setInterval> | null = null;
	let cancelRedirect = $state(false);

	let showCompleted = $state(false);
	let showFailed = $state(false);

	const wsState = $derived($websocketStore);
	const progressMessages = $derived(
		wsState.messages.filter((m: ProgressMessage) => m.job_id === jobId)
	);
	const messagesByFile = $derived(wsState.messagesByFile[jobId] || {});
	const latestMessage = $derived(progressMessages[progressMessages.length - 1]);

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

	$effect(() => {
		const status = jobQuery.data?.status;
		if (status === 'completed' && !countdownInterval && !cancelRedirect) {
			countdownInterval = setInterval(() => {
				countdown -= 1;
				if (countdown <= 0 && !cancelRedirect) {
					if (countdownInterval) {
						clearInterval(countdownInterval);
						countdownInterval = null;
					}
					const params = new URLSearchParams({ destination });
					if (updateMode) params.set('update', 'true');
					goto(`/review/${jobId}?${params.toString()}`);
				}
			}, 1000);
		}
	});

	async function handleCancel() {
		cancelMutation.mutate();
	}

	onDestroy(() => {
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
					<div class="flex items-center gap-2 px-3 py-1 bg-blue-100 dark:bg-blue-900/30 rounded-full text-sm">
						<div class="h-2 w-2 rounded-full bg-blue-600 dark:bg-blue-400 animate-pulse"></div>
						<span class="font-medium text-blue-700 dark:text-blue-300">
							{activeFiles.length} / {maxWorkers} workers
						</span>
					</div>
				{/if}
			</div>
			<Button variant="ghost" size="icon" onclick={onClose}>
				<X class="h-4 w-4" />
			</Button>
		</div>

		<!-- Content -->
		<div class="flex-1 overflow-y-auto p-6 space-y-5">
			{#if loading}
				<p class="text-sm text-muted-foreground text-center py-6">Loading job status...</p>
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
					<div class="h-3 bg-secondary rounded-full overflow-hidden">
						<div
							class="h-full bg-primary rounded-full transition-all duration-300"
							style="width: {job.progress}%"
						></div>
					</div>
					<div class="flex items-center justify-between text-xs text-muted-foreground">
						<span>{job.progress.toFixed(1)}%</span>
						<span>
							{#if completedFiles.length > 0}<span class="text-green-600 dark:text-green-400">{completedFiles.length} done</span> • {/if}{#if failedFiles.length > 0}<span class="text-red-600 dark:text-red-400">{failedFiles.length} failed</span> • {/if}{#if queuedFiles.length > 0}{queuedFiles.length} queued{/if}
						</span>
					</div>
				</div>

				<!-- Unified File Feed -->
				<div class="space-y-1.5">
					<!-- Active Files -->
					{#each activeFiles as result (result.file_path)}
						<div animate:flip={{ duration: 220, easing: cubicOut }} class="active-file flex items-center gap-3 rounded-lg border border-blue-200 dark:border-blue-800 bg-blue-50/50 dark:bg-blue-900/20 px-3 py-2.5">
							<div class="h-2 w-2 rounded-full bg-blue-600 dark:bg-blue-400 shrink-0 animate-pulse"></div>
							<div class="flex-1 min-w-0">
								<div class="flex items-center gap-2">
									<span class="font-medium text-sm text-blue-900 dark:text-blue-100 truncate">
										{result.movie_id || 'Processing...'}
									</span>
									<span class="text-xs text-blue-600/60 dark:text-blue-400/60 truncate">
										{getFileDisplayName(result.file_path)}
									</span>
								</div>
								{#if messagesByFile[result.file_path]}
									<p class="text-xs text-blue-600 dark:text-blue-400 mt-0.5 truncate">
										{messagesByFile[result.file_path].message}
									</p>
								{/if}
							</div>
						</div>
					{/each}

					<!-- Queued Files (show first 3) -->
					{#each queuedFiles.slice(0, 3) as filePath (filePath)}
						<div animate:flip={{ duration: 180, easing: cubicOut }} class="flex items-center gap-3 rounded-lg px-3 py-2 opacity-40">
							<div class="h-2 w-2 rounded-full bg-muted-foreground/40 shrink-0"></div>
							<span class="text-sm text-muted-foreground truncate">
								{getFileDisplayName(filePath)}
							</span>
						</div>
					{/each}
					{#if queuedFiles.length > 3}
						<p class="text-xs text-muted-foreground pl-5">+{queuedFiles.length - 3} more queued</p>
					{/if}

					<!-- Completed Summary (collapsible) -->
					{#if completedFiles.length > 0}
						<button
							onclick={() => showCompleted = !showCompleted}
							class="w-full flex items-center gap-3 rounded-lg px-3 py-2 text-left hover:bg-accent/50 transition-colors"
						>
							<CircleCheckBig class="h-4 w-4 text-green-600 dark:text-green-400 shrink-0" />
							<span class="text-sm font-medium text-green-700 dark:text-green-300">
								{completedFiles.length} completed
							</span>
							{#if showCompleted}
								<ChevronDown class="h-4 w-4 text-green-600 dark:text-green-400 ml-auto" />
							{:else}
								<ChevronRight class="h-4 w-4 text-green-600 dark:text-green-400 ml-auto" />
							{/if}
						</button>
						{#if showCompleted}
							<div class="space-y-1 pl-5" transition:slide|local={{ duration: 180, easing: cubicOut }}>
								{#each completedFiles as result (result.file_path)}
									<div animate:flip={{ duration: 180, easing: cubicOut }} class="flex items-center gap-3 px-3 py-1.5 text-sm">
										<CircleCheckBig class="h-3.5 w-3.5 text-green-600 dark:text-green-400 shrink-0" />
										<span class="truncate text-green-900 dark:text-green-200">{result.movie_id || 'Unknown'}</span>
										<span class="text-xs text-green-700/50 dark:text-green-300/50 truncate">{getFileDisplayName(result.file_path)}</span>
									</div>
								{/each}
							</div>
						{/if}
					{/if}

					<!-- Failed Summary (collapsible) -->
					{#if failedFiles.length > 0}
						<button
							onclick={() => showFailed = !showFailed}
							class="w-full flex items-center gap-3 rounded-lg px-3 py-2 text-left hover:bg-accent/50 transition-colors"
						>
							<CircleX class="h-4 w-4 text-red-600 dark:text-red-400 shrink-0" />
							<span class="text-sm font-medium text-red-700 dark:text-red-300">
								{failedFiles.length} failed
							</span>
							{#if showFailed}
								<ChevronDown class="h-4 w-4 text-red-600 dark:text-red-400 ml-auto" />
							{:else}
								<ChevronRight class="h-4 w-4 text-red-600 dark:text-red-400 ml-auto" />
							{/if}
						</button>
						{#if showFailed}
							<div class="space-y-1 pl-5" transition:slide|local={{ duration: 180, easing: cubicOut }}>
								{#each failedFiles as result (result.file_path)}
									<div animate:flip={{ duration: 180, easing: cubicOut }} class="flex items-start gap-3 px-3 py-1.5 text-sm">
										<CircleX class="h-3.5 w-3.5 text-red-600 dark:text-red-400 mt-0.5 shrink-0" />
										<div class="flex-1 min-w-0">
											<div class="flex items-center gap-2">
												<span class="truncate text-red-900 dark:text-red-200">{result.movie_id || 'Unknown'}</span>
												<span class="text-xs text-red-700/50 dark:text-red-300/50 truncate">{getFileDisplayName(result.file_path)}</span>
											</div>
											{#if result.error}
												<p class="text-xs text-red-600 dark:text-red-400 mt-0.5 break-words">{result.error}</p>
											{/if}
										</div>
									</div>
								{/each}
							</div>
						{/if}
					{/if}
				</div>

				<!-- Latest Progress Message (fallback) -->
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
						<CircleCheckBig class="h-5 w-5 text-green-500 dark:text-green-400" />
						<p class="text-sm font-medium text-green-700 dark:text-green-300">
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
						<CircleCheckBig class="h-5 w-5 text-green-500 dark:text-green-400" />
						<p class="text-sm font-medium text-green-700 dark:text-green-300">
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
			border-color: hsl(var(--primary));
			box-shadow: 0 0 0 0 hsl(var(--primary) / 0.4);
		}
		50% {
			border-color: hsl(var(--primary) / 0.8);
			box-shadow: 0 0 0 4px hsl(var(--primary) / 0.1);
		}
	}

	.active-file {
		animation: pulse-border 2s ease-in-out infinite;
	}
</style>
