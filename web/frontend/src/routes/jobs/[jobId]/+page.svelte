<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import { formatDateTime } from '$lib/i18n/format';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { fade, fly } from 'svelte/transition';
	import { quintOut } from 'svelte/easing';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
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
	import { createJobDetailQuery, createJobOperationsQuery, createConfigQuery } from '$lib/query/queries';
	import type { OperationItem } from '$lib/api/types';

	let jobId = $derived($page.params.jobId ?? '');

	const queryClient = useQueryClient();

	let jobQuery = $derived(createJobDetailQuery(jobId));
	let operationsQuery = $derived(createJobOperationsQuery(jobId));
	let job = $derived(jobQuery.data ?? null);
	let operations = $derived(operationsQuery.data?.operations ?? []);
	let jobStatus = $derived(operationsQuery.data?.job_status ?? jobQuery.data?.status ?? '');
	let loading = $derived(jobQuery.isPending || operationsQuery.isPending);
	let error = $derived(jobQuery.error?.message ?? operationsQuery.error?.message ?? null);

	const configQuery = createConfigQuery();
	let config = $derived(configQuery.data ?? null);

	let revertModalOpen = $state(false);
	let revertModalMode = $state<'batch' | 'operation'>('batch');
	let revertTargetMovieId = $state('');
	let revertTargetFileName = $state('');
	let revertFileCount = $state(0);
	let revertingMovieIds = $state<Set<string>>(new Set());

	const revertEligibleCount = $derived(operations.filter((o) => o.revert_status === 'applied' || o.revert_status === 'failed').length);

	const revertBatchMutation = createMutation(() => ({
		mutationFn: () => apiClient.revertBatchJob(jobId),
		onSuccess: (result) => {
			revertModalOpen = false;
			if (result.failed === 0) {
				toastStore.success(m.jobs_revert_success({ count: result.succeeded }));
			} else {
				toastStore.warning(m.jobs_revert_partial({ succeeded: result.succeeded, total: result.total, failed: result.failed }));
			}
			void queryClient.invalidateQueries({ queryKey: ['job', jobId] });
			void queryClient.invalidateQueries({ queryKey: ['job', jobId, 'operations'] });
			void queryClient.invalidateQueries({ queryKey: ['batch-jobs'] });
		},
		onError: (err) => {
			toastStore.error(m.jobs_revert_failed({ error: err.message }));
			revertModalOpen = false;
		},
		onSettled: () => { revertingMovieIds = new Set(); }
	}));

	const revertOperationMutation = createMutation(() => ({
		mutationFn: (movieId: string) => apiClient.revertJobOperation(jobId, movieId),
		onSuccess: (_, movieId) => {
			revertModalOpen = false;
			toastStore.success(m.jobs_operation_reverted({ movieId }));
			void queryClient.invalidateQueries({ queryKey: ['job', jobId] });
			void queryClient.invalidateQueries({ queryKey: ['job', jobId, 'operations'] });
			void queryClient.invalidateQueries({ queryKey: ['batch-jobs'] });
		},
		onError: (err) => {
			toastStore.error(m.jobs_revert_failed({ error: err.message }));
			revertModalOpen = false;
		},
		onSettled: () => { revertingMovieIds = new Set(); }
	}));

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

	function openBatchRevertModal() {
		revertModalMode = 'batch';
		revertFileCount = revertEligibleCount;
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

	function handleRevertConfirm(): Promise<void> {
		if (revertModalMode === 'batch') {
			revertBatchMutation.mutate();
		} else {
			revertOperationMutation.mutate(revertTargetMovieId);
		}
		return Promise.resolve();
	}

	function formatDate(dateStr: string) {
		return formatDateTime(dateStr);
	}
</script>

<div class="container mx-auto px-4 py-8">
	<div class="max-w-7xl mx-auto space-y-6">
		<!-- Back link -->
		<button
			onclick={() => goto('/jobs')}
			class="flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
			aria-label={m.jobs_back_to_jobs_aria()}
		>
			<ArrowLeft class="h-4 w-4" />
			{m.jobs_back_to_jobs()}
		</button>

		{#if loading}
			<Card class="p-8 text-center">
				<Clock class="h-8 w-8 animate-spin mx-auto mb-2" />
				<p class="text-muted-foreground">{m.jobs_loading_details()}</p>
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
				<h1 class="text-2xl font-bold tracking-tight">{m.jobs_detail_title({ id: job.id.slice(0, 8) })}</h1>
				<div class="flex items-center gap-3 mt-2 text-sm text-muted-foreground">
					<StatusBadge status={getStatusFromJobStatus(jobStatus)} />
					<span>{m.jobs_file_count_short({ count: operations.length })}</span>
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
					<h2 class="text-lg font-semibold mb-4">{m.jobs_batch_summary()}</h2>
					<div class="grid grid-cols-1 sm:grid-cols-2 gap-4 text-sm">
						<div>
							<span class="text-muted-foreground">{m.jobs_summary_status()}</span>
							<span class="ml-2"><StatusBadge status={getStatusFromJobStatus(jobStatus)} size="sm" /></span>
						</div>
						<div>
							<span class="text-muted-foreground">{m.jobs_summary_total_files()}</span>
							<span class="ml-2 font-medium">{operations.length}</span>
						</div>
						{#if job.destination}
							<div>
								<span class="text-muted-foreground">{m.jobs_summary_destination()}</span>
								<span class="ml-2 font-mono text-xs">{job.destination}</span>
							</div>
						{/if}
						<div>
							<span class="text-muted-foreground">{m.jobs_summary_started()}</span>
							<span class="ml-2">{formatDate(job.started_at)}</span>
						</div>
						{#if job.completed_at}
							<div>
								<span class="text-muted-foreground">{m.jobs_summary_completed()}</span>
								<span class="ml-2">{formatDate(job.completed_at)}</span>
							</div>
						{/if}
						{#if job.organized_at}
							<div>
								<span class="text-muted-foreground">{m.jobs_summary_organized()}</span>
								<span class="ml-2">{formatDate(job.organized_at)}</span>
							</div>
						{/if}
						{#if job.reverted_at}
							<div>
								<span class="text-muted-foreground">{m.jobs_summary_reverted()}</span>
								<span class="ml-2">{formatDate(job.reverted_at)}</span>
							</div>
						{/if}
					</div>

					<!-- Batch revert button -->
					{#if revertEligibleCount > 0 && jobStatus.toLowerCase() === 'organized' && config?.output?.allow_revert}
						<div class="mt-4 pt-4 border-t flex justify-end">
							<Button
								variant="destructive"
								size="sm"
								onclick={openBatchRevertModal}
							>
								<Undo2 class="h-4 w-4 mr-1.5" />
								{m.jobs_revert_batch({ count: revertEligibleCount })}
							</Button>
						</div>
					{/if}
				</Card>
			</div>

			<!-- File List -->
			<div class="space-y-3">
				<h2 class="text-lg font-semibold">{m.jobs_files()}</h2>

				{#if operations.length === 0}
					<Card class="p-8 text-center">
						<p class="text-muted-foreground">{m.jobs_no_operations()}</p>
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
