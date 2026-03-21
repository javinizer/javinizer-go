<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { cubicOut } from 'svelte/easing';
	import { fly, slide } from 'svelte/transition';
	import { apiClient } from '$lib/api/client';
	import type { BatchJobResponse } from '$lib/api/types';
	import { LoaderCircle, X, ChevronUp, ChevronDown } from 'lucide-svelte';

	interface Props {
		jobId: string;
		onReopen: () => void;
		onDismiss: () => void;
	}

	let { jobId, onReopen, onDismiss }: Props = $props();

	let job: BatchJobResponse | null = $state(null);
	let pollInterval: ReturnType<typeof setInterval> | null = null;
	let expanded = $state(false);

	async function fetchJob() {
		try {
			job = await apiClient.getBatchJob(jobId);

			// Stop polling if job is complete
			if (
				job &&
				(job.status === 'completed' || job.status === 'failed' || job.status === 'cancelled')
			) {
				if (pollInterval) {
					clearInterval(pollInterval);
					pollInterval = null;
				}
				// Auto-dismiss after 3 seconds when complete
				setTimeout(() => {
					onDismiss();
				}, 3000);
			}
		} catch (e) {
			console.error('Failed to fetch job status:', e);
		}
	}

	onMount(() => {
		fetchJob();
		// Poll for updates every 2 seconds
		pollInterval = setInterval(fetchJob, 2000);
	});

	onDestroy(() => {
		if (pollInterval) {
			clearInterval(pollInterval);
		}
	});
</script>

{#if job}
	<div
		class="fixed bottom-4 right-4 z-40 bg-primary text-primary-foreground rounded-lg shadow-lg hover:shadow-xl transition-all"
		in:fly|local={{ y: 24, duration: 240, easing: cubicOut }}
		out:fly|local={{ y: 24, duration: 180, easing: cubicOut }}
	>
		<button
			onclick={onReopen}
			class="flex items-center gap-3 px-4 py-3 w-full text-left hover:bg-white/5 rounded-t-lg transition-colors"
		>
			{#if job.status === 'running'}
				<LoaderCircle class="h-5 w-5 animate-spin shrink-0" />
			{:else if job.status === 'completed'}
				<div class="h-5 w-5 shrink-0 text-green-300">✓</div>
			{:else if job.status === 'failed'}
				<div class="h-5 w-5 shrink-0 text-red-300">✗</div>
			{:else}
				<LoaderCircle class="h-5 w-5 animate-spin shrink-0" />
			{/if}

			<div class="flex flex-col items-start min-w-0 flex-1">
				<div class="text-sm font-medium">
					{job.status === 'running' ? 'Scraping in progress' : job.status === 'completed' ? 'Scraping complete' : job.status === 'failed' ? 'Scraping failed' : 'Processing'}
				</div>
				<div class="text-xs opacity-90">
					{job.completed + job.failed} / {job.total_files} files • {job.progress.toFixed(0)}%
				</div>
			</div>
		</button>

		<div class="flex items-center gap-1 px-2 pb-2">
			<button
				onclick={(e) => {
					e.stopPropagation();
					expanded = !expanded;
				}}
				class="p-1 hover:bg-white/10 rounded transition-colors shrink-0"
				aria-label={expanded ? 'Collapse' : 'Expand'}
			>
				{#if expanded}
					<ChevronDown class="h-4 w-4" />
				{:else}
					<ChevronUp class="h-4 w-4" />
				{/if}
			</button>

			<button
				onclick={(e) => {
					e.stopPropagation();
					onDismiss();
				}}
				class="p-1 hover:bg-white/10 rounded transition-colors shrink-0"
				aria-label="Dismiss"
			>
				<X class="h-4 w-4" />
			</button>
		</div>

		{#if expanded}
			<div class="border-t border-white/20 px-4 py-3 text-left" transition:slide|local={{ duration: 180, easing: cubicOut }}>
				<div class="space-y-2">
					<div class="flex items-center justify-between text-xs">
						<span class="opacity-75">Progress</span>
						<span>{job.progress.toFixed(1)}%</span>
					</div>
					<div class="h-2 bg-white/20 rounded-full overflow-hidden">
						<div
							class="h-full bg-white transition-all duration-300"
							style="width: {job.progress}%"
						></div>
					</div>
					<div class="flex items-center justify-between text-xs opacity-75">
						<span>{job.completed} completed</span>
						<span>{job.failed} failed</span>
						<span>{job.total_files - job.completed - job.failed} remaining</span>
					</div>
				</div>
			</div>
		{/if}
	</div>
{/if}
