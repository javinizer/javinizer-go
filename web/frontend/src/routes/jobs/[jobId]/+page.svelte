<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { fade, fly } from 'svelte/transition';
	import { quintOut } from 'svelte/easing';
	import {
		ArrowLeft,
		AlertTriangle,
		Clock,
		Undo2
	} from 'lucide-svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import RevertConfirmationModal from '$lib/components/RevertConfirmationModal.svelte';
	import OperationRow from '$lib/components/history/OperationRow.svelte';
	import { apiClient } from '$lib/api/client';
	import { toastStore } from '$lib/stores/toast';
	import type { JobListItem, OperationItem } from '$lib/api/types';

	// Route param
	let jobId = $derived($page.params.jobId ?? '');

	// Data state
	let job = $state<JobListItem | null>(null);
	let operations = $state<OperationItem[]>([]);
	let jobStatus = $state<string>('');
	let loading = $state(true);
	let error = $state<string | null>(null);

	// Revert modal
	let revertModalOpen = $state(false);
	let revertModalMode = $state<'batch' | 'operation'>('batch');
	let revertTargetMovieId = $state('');
	let revertTargetFileName = $state('');
	let revertFileCount = $state(0);

	// Per-operation reverting state
	let revertingMovieIds = $state<Set<string>>(new Set());

	// Config state (for allow_revert check)
	let config: any = $state<any>(null);

	// Computed counts
	const pendingCount = $derived(operations.filter((o) => o.revert_status === 'pending' || o.revert_status === 'failed').length);

	// Convert job status to StatusBadge status
	function getStatusFromJobStatus(status: string): 'success' | 'failed' | 'reverted' | 'running' | 'organized' | 'cancelled' | 'partially-reverted' {
		const s = status.toLowerCase();
		if (s === 'organized') return 'organized';
		if (s === 'reverted') return 'reverted';
		if (s === 'completed') return 'success';
		if (s === 'failed') return 'failed';
		if (s === 'running') return 'running';
		if (s === 'cancelled') return 'cancelled';
		if (s === 'partially-reverted') return 'partially-reverted';
		return 'success';
	}

	// Data loading
	async function loadJob() {
		if (!jobId) return;
		try {
			job = await apiClient.getJob(jobId);
			jobStatus = job.status;
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load job';
		}
	}

	async function loadOperations() {
		if (!jobId) return;
		try {
			const response = await apiClient.getJobOperations(jobId);
			operations = response.operations || [];
			jobStatus = response.job_status || jobStatus;
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load operations';
			operations = [];
		}
	}

	async function loadData() {
		loading = true;
		error = null;
		await Promise.all([loadJob(), loadOperations()]);
		loading = false;
	}

	// Revert flows
	function openBatchRevertModal() {
		revertModalMode = 'batch';
		revertFileCount = pendingCount;
		revertTargetMovieId = '';
		revertTargetFileName = '';
		revertModalOpen = true;
	}

	function openOperationRevertModal(movieId: string) {
		revertModalMode = 'operation';
		revertTargetMovieId = movieId;
		revertTargetFileName = movieId;
		revertFileCount = 1;
		revertModalOpen = true;
		revertingMovieIds = new Set([...revertingMovieIds, movieId]);
	}

	async function handleRevertConfirm(): Promise<void> {
		if (!jobId) return;
		try {
			if (revertModalMode === 'batch') {
				const result = await apiClient.revertBatchJob(jobId);
				revertModalOpen = false;

				if (result.failed === 0) {
					toastStore.success(`Successfully reverted ${result.succeeded} file${result.succeeded !== 1 ? 's' : ''}`);
				} else {
					toastStore.warning(
						`Reverted ${result.succeeded} of ${result.total} file${result.total !== 1 ? 's' : ''}. ${result.failed} failed.`
					);
				}
			} else {
				const result = await apiClient.revertJobOperation(jobId, revertTargetMovieId);
				revertModalOpen = false;
				toastStore.success(`Reverted ${revertTargetMovieId}`);
			}

			// Reload data
			await loadData();
		} catch (e) {
			const msg = e instanceof Error ? e.message : 'Revert failed';
			toastStore.error(`Revert failed: ${msg}`);
			revertModalOpen = false;
		} finally {
			revertingMovieIds = new Set();
		}
	}

	// Utility functions
	function formatDate(dateStr: string) {
		const date = new Date(dateStr);
		return new Intl.DateTimeFormat('en-US', {
			dateStyle: 'medium',
			timeStyle: 'short'
		}).format(date);
	}

	onMount(() => {
		loadData();
		// Load config to check allow_revert setting
		apiClient.getConfig().then((c) => { config = c; }).catch(() => {});
	});
</script>

<div class="container mx-auto px-4 py-8">
	<div class="max-w-7xl mx-auto space-y-6">
		<!-- Back link -->
		<button
			onclick={() => goto('/jobs')}
			class="flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
			aria-label="Back to jobs"
		>
			<ArrowLeft class="h-4 w-4" />
			Back to Jobs
		</button>

		{#if loading}
			<Card class="p-8 text-center">
				<Clock class="h-8 w-8 animate-spin mx-auto mb-2" />
				<p class="text-muted-foreground">Loading job details...</p>
			</Card>
		{:else if error}
			<Card class="p-4 bg-destructive/10 border-destructive">
				<div class="flex items-center gap-2 text-destructive">
					<AlertTriangle class="h-5 w-5" />
					<span>{error}</span>
				</div>
			</Card>
		{:else if job}
			<!-- Header -->
			<div in:fly|local={{ y: 12, duration: 220, easing: quintOut }}>
				<h1 class="text-2xl font-bold tracking-tight">Job {job.id.slice(0, 8)}</h1>
				<div class="flex items-center gap-3 mt-2 text-sm text-muted-foreground">
					<StatusBadge status={getStatusFromJobStatus(jobStatus)} />
					<span>{operations.length} file{operations.length !== 1 ? 's' : ''}</span>
					{#if job.organized_at}
						<span>{formatDate(job.organized_at)}</span>
					{:else if job.started_at}
						<span>{formatDate(job.started_at)}</span>
					{/if}
				</div>
			</div>

			<!-- Batch Summary Card -->
			<div in:fly|local={{ y: 10, duration: 240, delay: 50, easing: quintOut }}>
				<Card class="p-6">
					<h2 class="text-lg font-semibold mb-4">Batch Summary</h2>
					<div class="grid grid-cols-1 sm:grid-cols-2 gap-4 text-sm">
						<div>
							<span class="text-muted-foreground">Status:</span>
							<span class="ml-2"><StatusBadge status={getStatusFromJobStatus(jobStatus)} size="sm" /></span>
						</div>
						<div>
							<span class="text-muted-foreground">Total files:</span>
							<span class="ml-2 font-medium">{operations.length}</span>
						</div>
						{#if job.destination}
							<div>
								<span class="text-muted-foreground">Destination:</span>
								<span class="ml-2 font-mono text-xs">{job.destination}</span>
							</div>
						{/if}
						<div>
							<span class="text-muted-foreground">Started:</span>
							<span class="ml-2">{formatDate(job.started_at)}</span>
						</div>
						{#if job.completed_at}
							<div>
								<span class="text-muted-foreground">Completed:</span>
								<span class="ml-2">{formatDate(job.completed_at)}</span>
							</div>
						{/if}
						{#if job.organized_at}
							<div>
								<span class="text-muted-foreground">Organized:</span>
								<span class="ml-2">{formatDate(job.organized_at)}</span>
							</div>
						{/if}
						{#if job.reverted_at}
							<div>
								<span class="text-muted-foreground">Reverted:</span>
								<span class="ml-2">{formatDate(job.reverted_at)}</span>
							</div>
						{/if}
					</div>

					<!-- Batch revert button -->
					{#if pendingCount > 0 && jobStatus.toLowerCase() === 'organized' && config?.output?.allow_revert}
						<div class="mt-4 pt-4 border-t flex justify-end">
							<Button
								variant="destructive"
								size="sm"
								onclick={openBatchRevertModal}
							>
								<Undo2 class="h-4 w-4 mr-1.5" />
								Revert Batch ({pendingCount} file{pendingCount !== 1 ? 's' : ''})
							</Button>
						</div>
					{/if}
				</Card>
			</div>

			<!-- File List -->
			<div class="space-y-3">
				<h2 class="text-lg font-semibold">Files</h2>

				{#if operations.length === 0}
					<Card class="p-8 text-center">
						<p class="text-muted-foreground">No operations recorded for this job</p>
					</Card>
				{:else}
					{#each operations as op, index (`${op.id}-${op.revert_status}`)}
						<div
							in:fly|local={{ y: 10, duration: 200, delay: Math.min(index * 30, 150), easing: quintOut }}
						>
							<OperationRow
								operation={op}
								onRevert={openOperationRevertModal}
								reverting={revertingMovieIds.has(op.movie_id)}
								revertible={jobStatus.toLowerCase() === 'organized' && config?.output?.allow_revert === true}
							/>
						</div>
					{/each}
				{/if}
			</div>
		{/if}
	</div>
</div>

<!-- Revert Confirmation Modal -->
<RevertConfirmationModal
	bind:open={revertModalOpen}
	mode={revertModalMode}
	targetId={jobId}
	fileCount={revertFileCount}
	fileName={revertTargetFileName}
	onConfirm={handleRevertConfirm}
	onCancel={() => {
		revertModalOpen = false;
		revertingMovieIds = new Set();
	}}
/>
