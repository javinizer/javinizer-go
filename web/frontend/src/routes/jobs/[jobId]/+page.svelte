<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import { formatDateTime } from '$lib/i18n/format';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { untrack } from 'svelte';
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
	import type { RevertResultResponse } from '$lib/api/types';
	import { mergeRevertFeedback, reconcileRevertFeedback, type RevertFeedback } from './revert-feedback';

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
	let incompleteRevert = $state<RevertFeedback | null>(null);
	let viewEpoch = 0;
	type Reconciliation = { result: RevertResultResponse; operationIds: number[]; afterUpdate: number };
	let reconciliations = $state<Reconciliation[]>([]);

	$effect(() => {
		jobId;
		untrack(() => {
			viewEpoch += 1;
			incompleteRevert = null;
			reconciliations = [];
			revertModalOpen = false;
			revertingMovieIds = new Set();
		});
		return () => { viewEpoch += 1; };
	});

	$effect(() => {
		const rows = operationsQuery.data;
		const ready = operationsQuery.isSuccess && operationsQuery.fetchStatus === 'idle';
		operationsQuery.dataUpdatedAt;
		reconciliations;
		untrack(() => {
			if (!ready || rows?.job_id !== jobId || !incompleteRevert) return;
			const update = queryClient.getQueryState(['job', jobId, 'operations'])?.dataUpdateCount ?? 0;
			if (!reconciliations.some((entry) => update > entry.afterUpdate)) return;
			for (const entry of reconciliations) {
				if (update > entry.afterUpdate) {
					incompleteRevert = reconcileRevertFeedback(incompleteRevert, entry.result, entry.operationIds, rows);
				}
			}
			reconciliations = reconciliations.map((entry) => ({ ...entry, afterUpdate: update }));
		});
	});

	type RevertAttempt = { jobId: string; epoch: number; operationIds: number[]; movieId?: string };
	function newRevertAttempt(movieId?: string): RevertAttempt {
		return {
			jobId, epoch: viewEpoch, movieId,
			operationIds: operations.filter((operation) =>
				(operation.revert_status === 'applied' || operation.revert_status === 'failed') &&
				(movieId === undefined || operation.movie_id === movieId)
			).map((operation) => operation.id)
		};
	}
	function isCurrentAttempt(attempt: RevertAttempt) {
		return attempt.jobId === jobId && attempt.epoch === viewEpoch;
	}

	async function reportRevertResult(result: RevertResultResponse, attempt: RevertAttempt) {
		if (isCurrentAttempt(attempt) && result.job_id === attempt.jobId) {
			revertModalOpen = false;
			incompleteRevert = mergeRevertFeedback(incompleteRevert, result, attempt.operationIds);
			const scope = new Set([...attempt.operationIds, ...(result.errors ?? []).map((entry) => entry.operation_id)]);
			// A newer response supersedes older deferred reconciliation for its
			// own IDs only. A failed refresh can then recover on focus/reconnect.
			reconciliations = reconciliations.map((entry) => ({
				...entry, operationIds: entry.operationIds.filter((id) => !scope.has(id))
			})).filter((entry) => entry.operationIds.length);
			reconciliations.push({ result, operationIds: [...scope],
				afterUpdate: queryClient.getQueryState(['job', attempt.jobId, 'operations'])?.dataUpdateCount ?? 0 });
			if (result.failed > 0 || result.skipped > 0) {
				toastStore.warning(m.jobs_revert_incomplete(result));
			} else {
				toastStore.success(attempt.movieId ? m.jobs_operation_reverted({ movieId: attempt.movieId }) : m.jobs_revert_success({ count: result.succeeded }));
			}
		}
		// Invalidate the request's original job even if navigation happened.
		// The reactive reconciliation also handles a later successful refresh.
		void queryClient.invalidateQueries({ queryKey: ['batch-jobs'] });
		await queryClient.invalidateQueries({ queryKey: ['job', attempt.jobId] });
	}

	const revertEligibleCount = $derived(operations.filter((o) => o.revert_status === 'applied' || o.revert_status === 'failed').length);

	function reportRevertError(err: Error, attempt: RevertAttempt) {
		if (!isCurrentAttempt(attempt)) return;
		toastStore.error(m.jobs_revert_failed({ error: err.message }));
		revertModalOpen = false;
	}
	function settleRevertAttempt(attempt: RevertAttempt) {
		if (isCurrentAttempt(attempt)) revertingMovieIds = new Set();
	}

	const revertBatchMutation = createMutation(() => ({
		mutationFn: (attempt: RevertAttempt) => apiClient.revertBatchJob(attempt.jobId),
		onSuccess: reportRevertResult,
		onError: reportRevertError,
		onSettled: (_result, _error, attempt) => settleRevertAttempt(attempt)
	}));

	const revertOperationMutation = createMutation(() => ({
		mutationFn: (attempt: RevertAttempt & { movieId: string }) => apiClient.revertJobOperation(attempt.jobId, attempt.movieId),
		onSuccess: reportRevertResult,
		onError: reportRevertError,
		onSettled: (_result, _error, attempt) => settleRevertAttempt(attempt)
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

	async function handleRevertConfirm(): Promise<void> {
		if (revertModalMode === 'batch') {
			await revertBatchMutation.mutateAsync(newRevertAttempt());
		} else {
			await revertOperationMutation.mutateAsync({ ...newRevertAttempt(revertTargetMovieId), movieId: revertTargetMovieId });
		}
	}

	function formatDate(dateStr: string) {
		return formatDateTime(dateStr);
	}
</script>

<div class="w-full px-4 py-8 lg:px-6">
	<div class="space-y-6">
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

			{#if incompleteRevert?.jobId === jobId && (incompleteRevert.errors.length || incompleteRevert.unconfirmedScopes.length)}
				<div role="alert" class="rounded-lg border border-amber-500/40 bg-amber-500/10 p-4 space-y-2">
					<p class="font-medium">{m.jobs_revert_unresolved()}</p>
					{#if incompleteRevert.errors?.length}
						<ul class="space-y-2 text-sm">
							{#each incompleteRevert.errors as failure}
								<li class="break-words">
									<p><span class="font-semibold">{failure.movie_id}</span>: {failure.error || failure.reason || failure.outcome}</p>
									{#if failure.original_path}<p>{m.history_from_label()} {failure.original_path}</p>{/if}
									{#if failure.new_path}<p>{m.history_to_label()} {failure.new_path}</p>{/if}
								</li>
							{/each}
						</ul>
					{/if}
				</div>
			{/if}

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
{#key jobId}
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
{/key}
