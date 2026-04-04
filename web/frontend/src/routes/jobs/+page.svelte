<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { quintOut } from 'svelte/easing';
	import { fade, fly } from 'svelte/transition';
	import {
		Activity,
		ArrowRight,
		CircleX,
		Clock,
		RefreshCw,
		CheckCircle2,
		AlertTriangle
	} from 'lucide-svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { apiClient } from '$lib/api/client';
	import type { BatchJobResponse } from '$lib/api/types';

	let jobs = $state<BatchJobResponse[]>([]);
	let loading = $state(true);
	let hasLoadedOnce = $state(false);
	let isRefreshing = $state(false);
	let error = $state<string | null>(null);
	let listRenderVersion = $state(0);

	function itemDelay(index: number): number {
		return Math.min(index * 28, 220);
	}

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

	function formatDate(dateStr: string): string {
		const date = new Date(dateStr);
		return new Intl.DateTimeFormat('en-US', {
			dateStyle: 'medium',
			timeStyle: 'short'
		}).format(date);
	}

	function getStatusColor(status: string): string {
		switch (status.toLowerCase()) {
			case 'running':
				return 'bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300';
			case 'completed':
				return 'bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300';
			case 'failed':
				return 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300';
			case 'organized':
				return 'bg-purple-100 text-purple-700 dark:bg-purple-900 dark:text-purple-300';
			case 'cancelled':
				return 'bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300';
			default:
				return 'bg-muted text-muted-foreground';
		}
	}

	type ButtonVariant = 'default' | 'destructive' | 'outline' | 'secondary' | 'ghost' | 'link';

	function getActionButtons(job: BatchJobResponse): { label: string; action: () => void; variant?: ButtonVariant }[] {
		const status = job.status.toLowerCase();
		switch (status) {
			case 'running':
				return [
					{ label: 'Cancel', action: () => cancelJob(job.id), variant: 'outline' },
					{ label: 'View', action: () => goto(`/review/${job.id}`) }
				];
			case 'completed':
				return [
					{ label: 'Review', action: () => goto(`/review/${job.id}`) },
					{ label: 'Dismiss', action: () => dismissJob(job.id), variant: 'outline' }
				];
			case 'failed':
				return [
					{ label: 'Review', action: () => goto(`/review/${job.id}`) },
					{ label: 'Dismiss', action: () => dismissJob(job.id), variant: 'outline' }
				];
			case 'organized':
				return [{ label: 'Dismiss', action: () => dismissJob(job.id), variant: 'outline' }];
			case 'cancelled':
				return [{ label: 'Dismiss', action: () => dismissJob(job.id), variant: 'outline' }];
			default:
				return [];
		}
	}

	onMount(() => {
		loadJobs();
	});
</script>

<div class="container mx-auto px-4 py-8">
	<div class="max-w-6xl mx-auto space-y-6">
		<div class="flex items-center justify-between">
			<div>
				<h1 class="text-3xl font-bold">Batch Jobs</h1>
				<p class="text-muted-foreground mt-1">View and manage active and recent batch jobs</p>
			</div>
			<Button variant="outline" onclick={loadJobs}>
				<RefreshCw class="h-4 w-4 mr-2 {isRefreshing ? 'animate-spin' : ''}" />
				Refresh
			</Button>
		</div>

		{#if error}
			<div in:fade|local={{ duration: 150 }}>
				<Card class="p-4 bg-destructive/10 border-destructive text-destructive">
					<div class="flex items-center gap-2">
						<AlertTriangle class="h-5 w-5" />
						<span>{error}</span>
					</div>
				</Card>
			</div>
		{/if}

		{#if loading && !hasLoadedOnce}
			<Card class="p-8 text-center">
				<Clock class="h-8 w-8 animate-spin mx-auto mb-2" />
				<p class="text-muted-foreground">Loading jobs...</p>
			</Card>
		{:else if jobs.length === 0}
			<Card class="p-8 text-center">
				<Activity class="h-8 w-8 mx-auto mb-2 text-muted-foreground" />
				<p class="text-muted-foreground">No batch jobs found</p>
				<Button onclick={() => goto('/browse')} class="mt-4">
					<ArrowRight class="h-4 w-4 mr-2" />
					Start New Scrape
				</Button>
			</Card>
		{:else}
			{#key listRenderVersion}
				<div class="space-y-4" in:fade|local={{ duration: 170 }}>
					{#each jobs as job, index (`${job.id}-${listRenderVersion}`)}
						<div in:fly|local={{ y: 8, duration: 210, delay: itemDelay(index), easing: quintOut }}>
							<Card class="p-4 hover:shadow-md transition-shadow">
								<div class="flex items-start justify-between gap-4">
									<div class="flex-1 min-w-0">
										<div class="flex items-center gap-3 mb-3">
											{#if job.status.toLowerCase() === 'running'}
												<Clock class="h-5 w-5" />
											{:else if job.status.toLowerCase() === 'completed' || job.status.toLowerCase() === 'organized'}
												<CheckCircle2 class="h-5 w-5 text-green-600" />
											{:else if job.status.toLowerCase() === 'failed'}
												<AlertTriangle class="h-5 w-5 text-red-600" />
											{:else if job.status.toLowerCase() === 'cancelled'}
												<CircleX class="h-5 w-5 text-gray-500" />
											{:else}
												<Clock class="h-5 w-5" />
											{/if}
											<h3 class="font-semibold truncate">
												Job {job.id.slice(0, 8)}
											</h3>
											<span class="px-2 py-0.5 text-xs rounded {getStatusColor(job.status)}">
												{job.status}
											</span>
										</div>

										<div class="grid grid-cols-2 md:grid-cols-4 gap-3 text-sm mb-3">
											<div>
												<span class="text-muted-foreground">Files:</span>
												<span class="font-medium ml-1">{job.total_files}</span>
											</div>
											<div>
												<span class="text-muted-foreground">Completed:</span>
												<span class="font-medium ml-1 text-green-600">{job.completed}</span>
											</div>
											<div>
												<span class="text-muted-foreground">Failed:</span>
												<span class="font-medium ml-1 text-red-600">{job.failed}</span>
											</div>
											<div>
												<span class="text-muted-foreground">Started:</span>
												<span class="font-medium ml-1">{formatDate(job.started_at)}</span>
											</div>
										</div>

										{#if job.status.toLowerCase() === 'running' || job.status.toLowerCase() === 'pending'}
											<div class="space-y-1">
												<div class="h-2 rounded-full bg-muted overflow-hidden">
													<div
														class="h-full bg-primary transition-all duration-300"
														style="width: {Math.max(0, Math.min(100, job.progress))}%"
													></div>
												</div>
												<div class="text-xs text-muted-foreground">
													{job.progress.toFixed(0)}% complete
												</div>
											</div>
										{/if}
									</div>

									<div class="flex flex-wrap gap-2">
										{#each getActionButtons(job) as btn}
											<Button
												variant={btn.variant || 'default'}
												size="sm"
												onclick={btn.action}
											>
												{btn.label}
											</Button>
										{/each}
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