<script lang="ts">
	import { cubicOut } from 'svelte/easing';
	import { fly, slide } from 'svelte/transition';
	import { createBatchJobPollingQuery } from '$lib/query/queries';
	import { getBackgroundJobState } from '$lib/stores/background-job.svelte';
	import { isTerminalStatus } from '$lib/utils/job-progress';
	import { LoaderCircle, X, ChevronUp, ChevronDown, Check, XCircle, Ban, FolderInput } from 'lucide-svelte';

	const iconMap = {
		spinner: LoaderCircle,
		check: Check,
		xcircle: XCircle,
		ban: Ban,
		folder: FolderInput,
		revert: Ban,
	};

	interface Props {
		jobId: string;
		onReopen: () => void;
		onDismiss: () => void;
	}

	let { jobId, onReopen, onDismiss }: Props = $props();

	let jobQuery = $derived(createBatchJobPollingQuery(jobId));
	let job = $derived(jobQuery.data ?? null);
	let expanded = $state(false);

	$effect(() => {
		const status = jobQuery.data?.status;
		const showModal = getBackgroundJobState().showModal;
		if (isTerminalStatus(status) && !showModal) {
			const timer = setTimeout(() => {
				const current = getBackgroundJobState();
				if (!current.showModal && current.jobId === jobId) {
					onDismiss();
				}
			}, 3000);
			return () => clearTimeout(timer);
		}
	});

	const statusConfig = $derived.by(() => {
		if (!job) return { label: 'Processing', ring: 'ring-muted-foreground/20', bar: 'bg-muted-foreground', icon: 'spinner' as const, iconClass: 'h-4 w-4 animate-spin shrink-0 text-muted-foreground' };
		switch (job.status) {
			case 'completed':
				return { label: 'Scraping complete', ring: 'ring-green-500/40 dark:ring-green-400/30', bar: 'bg-green-500 dark:bg-green-400', icon: 'check' as const, iconClass: 'h-4 w-4 shrink-0 text-green-500 dark:text-green-400' };
			case 'failed':
				return { label: 'Scraping failed', ring: 'ring-red-500/40 dark:ring-red-400/30', bar: 'bg-red-500 dark:bg-red-400', icon: 'xcircle' as const, iconClass: 'h-4 w-4 shrink-0 text-red-500 dark:text-red-400' };
			case 'cancelled':
				return { label: 'Scraping cancelled', ring: 'ring-amber-500/40 dark:ring-amber-400/30', bar: 'bg-amber-500 dark:bg-amber-400', icon: 'ban' as const, iconClass: 'h-4 w-4 shrink-0 text-amber-500 dark:text-amber-400' };
			case 'organized':
				return { label: 'Files organized', ring: 'ring-green-500/40 dark:ring-green-400/30', bar: 'bg-green-500 dark:bg-green-400', icon: 'folder' as const, iconClass: 'h-4 w-4 shrink-0 text-green-500 dark:text-green-400' };
			case 'reverted':
				return { label: 'Revert complete', ring: 'ring-amber-500/40 dark:ring-amber-400/30', bar: 'bg-amber-500 dark:bg-amber-400', icon: 'revert' as const, iconClass: 'h-4 w-4 shrink-0 text-amber-500 dark:text-amber-400' };
			case 'pending':
				return { label: 'Queued', ring: 'ring-muted-foreground/20', bar: 'bg-muted-foreground', icon: 'spinner' as const, iconClass: 'h-4 w-4 shrink-0 text-muted-foreground' };
			case 'running':
				return { label: 'Scraping in progress', ring: 'ring-primary/30', bar: 'bg-primary', icon: 'spinner' as const, iconClass: 'h-4 w-4 animate-spin shrink-0 text-primary' };
			default:
				return { label: 'Processing', ring: 'ring-muted-foreground/20', bar: 'bg-muted-foreground', icon: 'spinner' as const, iconClass: 'h-4 w-4 animate-spin shrink-0 text-muted-foreground' };
		}
	});

	const Icon = $derived(iconMap[statusConfig.icon]);
</script>

{#if job}
	<div
		class="fixed bottom-4 right-4 z-40 rounded-xl border shadow-lg bg-card text-card-foreground ring-1 {statusConfig.ring} transition-shadow hover:shadow-xl"
		in:fly|local={{ y: 24, duration: 240, easing: cubicOut }}
		out:fly|local={{ y: 24, duration: 180, easing: cubicOut }}
	>
		<button
			onclick={onReopen}
			class="flex items-center gap-3 px-4 py-3 w-full text-left hover:bg-accent/50 rounded-t-xl transition-colors"
		>
			<Icon class={statusConfig.iconClass} />

			<div class="flex flex-col items-start min-w-0 flex-1">
				<div class="text-sm font-medium leading-tight">
					{statusConfig.label}
				</div>
				<div class="text-xs text-muted-foreground mt-0.5">
					{job.completed + job.failed} / {job.total_files} files &middot; {job.progress.toFixed(0)}%
				</div>
			</div>
		</button>

		<div class="flex items-center gap-1 px-2 pb-2">
			<button
				onclick={(e) => {
					e.stopPropagation();
					expanded = !expanded;
				}}
				class="p-1.5 hover:bg-accent/60 rounded-md transition-colors shrink-0 text-muted-foreground hover:text-foreground"
				aria-label={expanded ? 'Collapse' : 'Expand'}
			>
				{#if expanded}
					<ChevronDown class="h-3.5 w-3.5" />
				{:else}
					<ChevronUp class="h-3.5 w-3.5" />
				{/if}
			</button>

			<button
				onclick={(e) => {
					e.stopPropagation();
					onDismiss();
				}}
				class="p-1.5 hover:bg-accent/60 rounded-md transition-colors shrink-0 text-muted-foreground hover:text-foreground"
				aria-label="Dismiss"
			>
				<X class="h-3.5 w-3.5" />
			</button>
		</div>

		{#if expanded}
			<div class="border-t border-border px-4 py-3 text-left" transition:slide|local={{ duration: 180, easing: cubicOut }}>
				<div class="space-y-2.5">
					<div class="flex items-center justify-between text-xs">
						<span class="text-muted-foreground">Progress</span>
						<span class="font-medium tabular-nums">{job.progress.toFixed(1)}%</span>
					</div>
					<div class="h-1.5 bg-muted rounded-full overflow-hidden">
						<div
							class="h-full {statusConfig.bar} rounded-full transition-all duration-300"
							style="width: {job.progress}%"
						></div>
					</div>
					<div class="grid grid-cols-3 gap-2 text-xs text-muted-foreground">
						<span>{job.completed} completed</span>
						<span class="text-center">{job.failed} failed</span>
						<span class="text-right">{Math.max(0, job.total_files - job.completed - job.failed)} remaining</span>
					</div>
				</div>
			</div>
		{/if}
	</div>
{/if}
