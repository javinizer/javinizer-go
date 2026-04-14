<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { fade, fly } from 'svelte/transition';
	import { page } from '$app/stores';
	import {
		Activity,
		ArrowRight,
		CircleX,
		Clock,
		RefreshCw,
		CheckCircle2,
		AlertTriangle,
		FolderOpen,
		Trash2,
		Eye,
		Undo2
	} from 'lucide-svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import RevertConfirmationModal from '$lib/components/RevertConfirmationModal.svelte';
	import { apiClient } from '$lib/api/client';
	import { toastStore } from '$lib/stores/toast';
	import type { BatchJobResponse, FileResult } from '$lib/api/types';

	let jobs = $state<BatchJobResponse[]>([]);
	let loading = $state(true);
	let hasLoadedOnce = $state(false);
	let isRefreshing = $state(false);
	let isClearing = $state(false);
	let error = $state<string | null>(null);
	let listRenderVersion = $state(0);
	let activeFilter = $state<string>('all');

	// Revert modal state
	let revertModalOpen = $state(false);
	let revertTargetId = $state('');
	let revertFileCount = $state(0);

	// Config state (for allow_revert check)
	let config: any = $state<any>(null);

	async function loadJobs() {
		if (!hasLoadedOnce) {
			loading = true;
		} else {
			isRefreshing = true;
		}
		error = null;

		try {
			const response = await apiClient.listBatchJobs();
			jobs = response.jobs;
			listRenderVersion += 1;
			hasLoadedOnce = true;
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load jobs';
			if (!hasLoadedOnce) {
				jobs = [];
			}
		} finally {
			loading = false;
			isRefreshing = false;
		}
	}

	function setFilter(filter: string) {
		activeFilter = filter;
		const url = new URL(window.location.href);
		if (filter === 'all') {
			url.searchParams.delete('status');
		} else {
			url.searchParams.set('status', filter);
		}
		window.history.replaceState({}, '', url.toString());
	}

	function getJobsByStatus(status: string): BatchJobResponse[] {
		return jobs.filter(job => job.status.toLowerCase() === status.toLowerCase());
	}

	function getStatusCount(status: string): number {
		return getJobsByStatus(status).length;
	}

	let filteredJobs = $derived(() => {
		if (activeFilter === 'all') {
			return jobs;
		}
		return getJobsByStatus(activeFilter);
	});

	async function cancelJob(jobId: string) {
		try {
			await apiClient.cancelBatchJob(jobId);
			await loadJobs();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to cancel job';
		}
	}

	async function dismissJob(jobId: string) {
		try {
			await apiClient.deleteBatchJob(jobId);
			await loadJobs();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to dismiss job';
		}
	}

	async function clearAllJobs() {
		const clearableJobs = jobs.filter(job => job.status.toLowerCase() !== 'running');
		if (clearableJobs.length === 0) return;

		const confirmed = confirm(`Clear all non-running jobs? This will remove ${clearableJobs.length} job(s).`);
		if (!confirmed) return;

		isClearing = true;
		error = null;
		let failedCount = 0;

		try {
			for (const job of clearableJobs) {
				try {
					await apiClient.deleteBatchJob(job.id);
				} catch (e) {
					failedCount++;
				}
			}

			if (failedCount > 0) {
				error = `Failed to clear ${failedCount} job(s). Some jobs may still be removed.`;
			}

			await loadJobs();
		} finally {
			isClearing = false;
		}
	}

	function openRevertModal(jobId: string, fileCount: number) {
		revertTargetId = jobId;
		revertFileCount = fileCount;
		revertModalOpen = true;
	}

	function getRevertableCount(job: BatchJobResponse): number {
		return job.operation_count - (job.reverted_count ?? 0);
	}

	async function handleRevertConfirm(): Promise<void> {
		try {
			const result = await apiClient.revertBatchJob(revertTargetId);
			revertModalOpen = false;
			if (result.failed === 0) {
				toastStore.success(`Successfully reverted ${result.succeeded} file${result.succeeded !== 1 ? 's' : ''}`);
			} else {
				toastStore.warning(`Reverted ${result.succeeded} of ${result.total}. ${result.failed} failed.`);
			}
			await loadJobs();
		} catch (e) {
			toastStore.error(`Revert failed: ${e instanceof Error ? e.message : 'Unknown error'}`);
			revertModalOpen = false;
		}
	}

	function formatDate(dateStr: string): string {
		const date = new Date(dateStr);
		const now = new Date();
		const diffMs = now.getTime() - date.getTime();
		const diffMins = Math.floor(diffMs / 60000);
		const diffHours = Math.floor(diffMs / 3600000);
		const diffDays = Math.floor(diffMs / 86400000);

		if (diffMins < 1) return 'Just now';
		if (diffMins < 60) return `${diffMins}m ago`;
		if (diffHours < 24) return `${diffHours}h ago`;
		if (diffDays < 7) return `${diffDays}d ago`;
		return new Intl.DateTimeFormat('en-US', { dateStyle: 'medium' }).format(date);
	}

	function getStatusConfig(status: string): { icon: typeof Clock; color: string; bg: string; label: string } {
		const s = status.toLowerCase();
		switch (s) {
			case 'running':
				return { icon: Clock, color: 'text-blue-500', bg: 'bg-blue-500/10', label: 'Running' };
			case 'completed':
				return { icon: CheckCircle2, color: 'text-green-500', bg: 'bg-green-500/10', label: 'Ready' };
			case 'failed':
				return { icon: AlertTriangle, color: 'text-red-500', bg: 'bg-red-500/10', label: 'Failed' };
			case 'organized':
				return { icon: CheckCircle2, color: 'text-purple-500', bg: 'bg-purple-500/10', label: 'Organized' };
			case 'cancelled':
				return { icon: CircleX, color: 'text-gray-400', bg: 'bg-gray-500/10', label: 'Cancelled' };
			case 'reverted':
				return { icon: Undo2, color: 'text-yellow-500', bg: 'bg-yellow-500/10', label: 'Reverted' };
			default:
				return { icon: Clock, color: 'text-gray-400', bg: 'bg-gray-500/10', label: status };
		}
	}

	function getFirstPoster(job: BatchJobResponse): string | null {
		const results = Object.values(job.results || {});
		for (const r of results) {
			if (r.data?.cropped_poster_url) {
				return r.data.cropped_poster_url;
			}
			if (r.data?.poster_url) {
				return apiClient.getPreviewImageURL(r.data.poster_url);
			}
		}
		return null;
	}

	function getFileNames(job: BatchJobResponse): string {
		const files = job.files || Object.keys(job.results || {});
		if (files.length === 0) return 'No files';
		if (files.length === 1) {
			const name = files[0].split('/').pop() || files[0];
			return name.length > 40 ? name.slice(0, 37) + '...' : name;
		}
		const first = files[0].split('/').pop() || files[0];
		return `${first} +${files.length - 1} more`;
	}

	onMount(() => {
		const statusParam = $page.url.searchParams.get('status');
		if (statusParam && ['running', 'completed', 'failed', 'cancelled', 'organized', 'reverted'].includes(statusParam)) {
			activeFilter = statusParam;
		} else {
			activeFilter = 'all';
		}
		loadJobs();
		// Load config to check allow_revert setting
		apiClient.getConfig().then((c) => { config = c; }).catch(() => {});
	});
</script>

<div class="min-h-screen bg-background">
	<div class="container mx-auto px-4 py-8 max-w-7xl">
		<div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 mb-6">
			<div>
				<h1 class="text-2xl font-bold tracking-tight">Jobs</h1>
				<p class="text-muted-foreground text-sm mt-1">Manage your batch jobs and organize history</p>
			</div>
			<div class="flex items-center gap-2">
				<Button variant="outline" size="sm" onclick={loadJobs} disabled={isRefreshing || isClearing}>
					<RefreshCw class="h-4 w-4 mr-1.5 {isRefreshing ? 'animate-spin' : ''}" />
					Refresh
				</Button>
				<Button
					variant="outline"
					size="sm"
					onclick={clearAllJobs}
					disabled={isClearing || jobs.length === 0 || jobs.every(j => j.status.toLowerCase() === 'running')}
					title="Clear all completed, failed, cancelled jobs"
				>
					<Trash2 class="h-4 w-4 mr-1.5 {isClearing ? 'animate-pulse' : ''}" />
					{isClearing ? 'Clearing...' : 'Clear All'}
				</Button>
				<Button size="sm" onclick={() => goto('/browse')}>
					<FolderOpen class="h-4 w-4 mr-1.5" />
					New Scrape
				</Button>
			</div>
		</div>

		{#if error}
			<div in:fade={{ duration: 150 }} class="mb-4">
				<Card class="p-3 bg-destructive/5 border-destructive/20">
					<div class="flex items-center gap-2 text-destructive text-sm">
						<AlertTriangle class="h-4 w-4" />
						<span>{error}</span>
					</div>
				</Card>
			</div>
		{/if}

		{#if !loading || hasLoadedOnce}
			<div class="flex flex-wrap gap-1.5 mb-6" in:fade={{ duration: 150 }}>
				{#each ['all', 'running', 'failed', 'completed', 'cancelled', 'organized', 'reverted'] as filter}
					{@const count = filter === 'all' ? jobs.length : getStatusCount(filter)}
					<Button
						variant={activeFilter === filter ? 'default' : 'ghost'}
						size="sm"
						onclick={() => setFilter(filter)}
					>
						{filter === 'all' ? 'All' : getStatusConfig(filter).label}
						<span class="ml-1.5 text-xs opacity-70">({count})</span>
					</Button>
				{/each}
			</div>
		{/if}

		{#if loading && !hasLoadedOnce}
			<div class="flex items-center justify-center py-20">
				<div class="text-center">
					<Clock class="h-8 w-8 animate-spin mx-auto mb-3 text-muted-foreground" />
					<p class="text-muted-foreground text-sm">Loading jobs...</p>
				</div>
			</div>
		{:else if jobs.length === 0}
			<Card class="p-12 text-center">
				<Activity class="h-12 w-12 mx-auto mb-4 text-muted-foreground/50" />
				<p class="text-muted-foreground mb-4">No batch jobs yet</p>
				<Button onclick={() => goto('/browse')}>
					<ArrowRight class="h-4 w-4 mr-1.5" />
					Start Your First Scrape
				</Button>
			</Card>
		{:else if filteredJobs().length === 0}
			<Card class="p-8 text-center">
				<p class="text-muted-foreground">No jobs match this filter</p>
			</Card>
		{:else}
			{#key listRenderVersion}
				<div class="space-y-3" in:fade={{ duration: 150 }}>
					{#each filteredJobs() as job, index (`${job.id}-${listRenderVersion}`)}
						{@const statusConfig = getStatusConfig(job.status)}
						{@const poster = getFirstPoster(job)}
						<div
							in:fly={{ y: 10, duration: 200, delay: Math.min(index * 30, 150) }}
							class="group"
						>
							<Card class="overflow-hidden hover:border-border/80 transition-colors shadow-sm">
								<div class="flex items-center p-3 gap-4">
									{#if poster}
										<div class="w-20 h-20 flex-shrink-0 bg-muted rounded-md overflow-hidden flex items-center justify-center">
											<img
												src={poster}
												alt=""
												class="w-full h-full object-cover object-center"
												onerror={(e) => {
													(e.target as HTMLImageElement).style.display = 'none';
												}}
											/>
										</div>
									{:else}
										<div class="w-20 h-20 flex-shrink-0 bg-muted rounded-md flex items-center justify-center">
											<FolderOpen class="h-8 w-8 text-muted-foreground/30" />
										</div>
									{/if}

									<div class="flex-1 min-w-0">
										<div class="flex items-center gap-2 mb-1">
											<span class="font-mono text-xs text-muted-foreground">
												{job.id.slice(0, 8)}
											</span>
											<span class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-xs font-medium {statusConfig.bg} {statusConfig.color}">
												<statusConfig.icon class="h-3 w-3" />
												{statusConfig.label}
											</span>
										</div>
										<p class="text-sm truncate mb-1.5" title={job.files?.[0]}>
											{getFileNames(job)}
										</p>
										<div class="flex items-center gap-4 text-xs text-muted-foreground">
											<span>{job.total_files} file{job.total_files !== 1 ? 's' : ''}</span>
											{#if job.completed > 0}
												<span class="text-green-600">{job.completed} done</span>
											{/if}
											{#if job.failed > 0}
												<span class="text-red-500">{job.failed} failed</span>
											{/if}
											<span>{formatDate(job.started_at)}</span>
										</div>

										{#if job.status.toLowerCase() === 'running'}
											<div class="mt-2">
												<div class="h-1.5 rounded-full bg-muted overflow-hidden">
													<div
														class="h-full bg-primary transition-all duration-300"
														style="width: {Math.max(0, Math.min(100, job.progress))}%"
													></div>
												</div>
											</div>
										{/if}
									</div>

									<div class="flex items-center gap-1.5 flex-shrink-0">
										{#if job.status.toLowerCase() === 'running'}
											<Button variant="outline" size="sm" onclick={() => cancelJob(job.id)}>
												Cancel
											</Button>
											<Button variant="default" size="sm" onclick={() => goto(`/review/${job.id}`)}>
												<Eye class="h-4 w-4 mr-1" />
												View
											</Button>
										{:else if job.status.toLowerCase() === 'completed'}
											<Button variant="default" size="sm" onclick={() => goto(`/review/${job.id}`)}>
												Review & Organize
											</Button>
											<Button variant="ghost" size="sm" onclick={() => dismissJob(job.id)} title="Dismiss">
												<Trash2 class="h-4 w-4 text-muted-foreground" />
											</Button>
										{:else if job.status.toLowerCase() === 'organized'}
											<Button variant="default" size="sm" onclick={() => goto(`/jobs/${job.id}`)}>
												<ArrowRight class="h-4 w-4 mr-1" />
												View Details
											</Button>
											{#if config?.output?.allow_revert}
											<Button
												variant="outline"
												size="sm"
												class="text-destructive hover:bg-destructive/10"
												onclick={() => openRevertModal(job.id, getRevertableCount(job))}
											>
												<Undo2 class="h-4 w-4 mr-1" />
												Revert
											</Button>
											{/if}
										{:else if job.status.toLowerCase() === 'failed'}
											<Button variant="outline" size="sm" onclick={() => goto(`/review/${job.id}`)}>
												<Eye class="h-4 w-4 mr-1" />
												Review
											</Button>
											<Button variant="ghost" size="sm" onclick={() => dismissJob(job.id)} title="Dismiss">
												<Trash2 class="h-4 w-4 text-muted-foreground" />
											</Button>
										{:else}
											<Button variant="ghost" size="sm" onclick={() => dismissJob(job.id)} title="Dismiss">
												<Trash2 class="h-4 w-4 text-muted-foreground" />
											</Button>
										{/if}
									</div>
								</div>
							</Card>
						</div>
					{/each}
				</div>
			{/key}
		{/if}
	</div>
</div>

<!-- Revert Confirmation Modal -->
<RevertConfirmationModal
	bind:open={revertModalOpen}
	mode="batch"
	targetId={revertTargetId}
	fileCount={revertFileCount}
	onConfirm={handleRevertConfirm}
	onCancel={() => (revertModalOpen = false)}
/>
