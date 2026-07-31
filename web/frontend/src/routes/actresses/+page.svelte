<script lang="ts">
	import { cubicOut, quintOut } from 'svelte/easing';
	import { fade, fly, scale } from 'svelte/transition';
	import { onMount } from 'svelte';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { Plus, RefreshCw, Download, Upload, Loader2, WandSparkles } from 'lucide-svelte';
	import { apiClient } from '$lib/api/client';
	import type { Actress, ActressUpsertRequest, ImportResponse, ActressSyncJob, ActressSyncTask } from '$lib/api/types';
	import { toastStore } from '$lib/stores/toast';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { createActressStore } from './stores/actress-store.svelte';
	import ActressForm from './components/ActressForm.svelte';
	import ActressToolbar from './components/ActressToolbar.svelte';
	import ActressCardsView from './components/ActressCardsView.svelte';
	import ActressCompactView from './components/ActressCompactView.svelte';
	import ActressTableView from './components/ActressTableView.svelte';
	import ActressMergeModal from './components/ActressMergeModal.svelte';
	import ActressPagination from './components/ActressPagination.svelte';
	import ActressSyncModal from './components/ActressSyncModal.svelte';
	import { isActressSyncTerminal, loadActressSyncSnapshot, loadActiveActressSyncJobs, mergeActiveActressSyncJobs, orderActiveActressSyncJobs } from './sync-runner';
	import * as m from '$lib/paraglide/messages';
	import { createConfigQuery } from '$lib/query/queries';

	const store = createActressStore();
	const queryClient = useQueryClient();
	const configQuery = createConfigQuery();
	let firstNameOrder = $derived(configQuery.data?.output?.first_name_order ?? false);
	let japaneseNames = $derived(
		(configQuery.data?.output?.actress_language_ja ?? false) ||
		(configQuery.data?.metadata?.nfo?.actress_language_ja ?? false)
	);
	let importFile = $state<HTMLInputElement | null>(null);
	let syncJob = $state<ActressSyncJob | null>(null);
	let syncTasks = $state<ActressSyncTask[]>([]);
	let syncQueue = $state<ActressSyncJob[]>([]);
	let showSyncModal = $state(false);
	let syncStarting = $state(false);
	let syncCancelling = $state(false);
	let syncPollInFlight = $state(false);
	let syncRestoring = $state(true);
	let advanceAfterReconcile = false;
	let syncDestroyed = false;
	let syncAdvancePromise: Promise<boolean> | undefined;
	let syncPollFailureShown = false;
	let syncTimer: ReturnType<typeof setInterval> | undefined;

	function stopPolling() {
		if (syncTimer) clearInterval(syncTimer);
		syncTimer = undefined;
	}

	function mergeActiveSyncJobs(jobs: ActressSyncJob[]) {
		syncQueue = mergeActiveActressSyncJobs(syncJob, syncQueue, jobs);
	}

	async function refreshActiveSyncQueue() {
		const jobs = await loadActiveActressSyncJobs(apiClient);
		if (syncDestroyed) return;
		mergeActiveSyncJobs(jobs);
	}

	async function advanceSyncJob() {
		if (syncDestroyed) return false;
		if (syncQueue.length === 0) await refreshActiveSyncQueue();
		if (syncDestroyed) return false;
		const [next, ...remaining] = syncQueue;
		if (!next) return false;
		syncQueue = remaining;
		syncJob = next;
		syncTasks = [];
		showSyncModal = true;
		startPolling();
		return true;
	}

	function showNextSyncJob() {
		if (syncAdvancePromise) return syncAdvancePromise;
		syncAdvancePromise = advanceSyncJob().finally(() => { syncAdvancePromise = undefined; });
		return syncAdvancePromise;
	}

	async function pollSyncJob() {
		const currentJob = syncJob;
		if (!currentJob || syncPollInFlight || syncDestroyed) return;
		syncPollInFlight = true;
		try {
			const snapshot = await loadActressSyncSnapshot(apiClient, currentJob.id);
			if (syncDestroyed || !syncJob || syncJob.id !== currentJob.id) return;
			syncPollFailureShown = false;
			syncJob = snapshot.job;
			syncTasks = snapshot.tasks;
			if (isActressSyncTerminal(syncJob)) {
				stopPolling();
				await queryClient.invalidateQueries({ queryKey: ['actresses'] });
				if (syncDestroyed) return;
				syncRestoring = true;
				try {
					await refreshActiveSyncQueue();
				} finally {
					if (!syncDestroyed) syncRestoring = false;
				}
				if (advanceAfterReconcile) {
					advanceAfterReconcile = false;
					void showNextSyncJob();
				}
			}
		} catch (error) {
			if (syncDestroyed || !syncJob || syncJob.id !== currentJob.id) return;
			if (!syncPollFailureShown) {
				syncPollFailureShown = true;
				toastStore.error(error instanceof Error ? error.message : m.actresses_sync_load_failed());
			}
		} finally {
			if (!syncDestroyed) syncPollInFlight = false;
		}
	}

	function startPolling() {
		if (syncDestroyed) return;
		stopPolling();
		void pollSyncJob();
		syncTimer = setInterval(pollSyncJob, 1500);
	}

	async function startSync(scope: 'missing' | 'selected') {
		if (syncDestroyed || syncRestoring || syncStarting) return;
		if (syncJob && !isActressSyncTerminal(syncJob)) {
			showSyncModal = true;
			if (!syncTimer) startPolling();
			return;
		}
		syncStarting = true;
		try {
			if (await showNextSyncJob()) return;
			if (syncDestroyed) return;
			if (syncJob && !isActressSyncTerminal(syncJob)) {
				showSyncModal = true;
				if (!syncTimer) startPolling();
				return;
			}
			const response = await apiClient.createActressSyncJob({ scope, actress_ids: scope === 'selected' ? store.selectedIds : undefined });
			if (syncDestroyed) return;
			syncJob = response.job;
			syncTasks = [];
			showSyncModal = true;
			startPolling();
		} catch (error) {
			if (!syncDestroyed) toastStore.error(error instanceof Error ? error.message : m.actresses_sync_start_failed());
		} finally {
			if (!syncDestroyed) syncStarting = false;
		}
	}

	async function cancelSync() {
		const currentJob = syncJob;
		if (!currentJob || currentJob.cancel_requested || syncCancelling || syncDestroyed) return;
		syncCancelling = true;
		try {
			const response = await apiClient.cancelActressSyncJob(currentJob.id);
			if (!syncDestroyed && syncJob?.id === currentJob.id) {
				syncJob = response.job;
				void pollSyncJob();
			}
		} catch (error) {
			if (!syncDestroyed) toastStore.error(error instanceof Error ? error.message : m.actresses_sync_load_failed());
		} finally {
			if (!syncDestroyed) syncCancelling = false;
		}
	}

	function closeSyncModal() {
		showSyncModal = false;
		if (!syncJob || !isActressSyncTerminal(syncJob)) return;
		if (syncRestoring) {
			advanceAfterReconcile = true;
			return;
		}
		void showNextSyncJob();
	}

	onMount(() => {
		store.hydrateSortPreferences();
		void loadActiveActressSyncJobs(apiClient).then((jobs) => {
			if (syncDestroyed) return;
			const ordered = orderActiveActressSyncJobs(jobs);
			syncJob = ordered.current;
			syncQueue = ordered.queued;
			if (syncJob) {
				showSyncModal = true;
				startPolling();
			}
		}).catch((error) => {
			if (!syncDestroyed) toastStore.error(error instanceof Error ? error.message : m.actresses_sync_load_failed());
		}).finally(() => {
			if (!syncDestroyed) syncRestoring = false;
		});
		return () => {
			syncDestroyed = true;
			stopPolling();
		};
	});

	const exportMutation = createMutation(() => ({
		mutationFn: () => apiClient.exportActresses(),
		onSuccess: async (data: Actress[]) => {
			const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
			const url = URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			a.download = 'actresses.json';
			document.body.appendChild(a);
			a.click();
			document.body.removeChild(a);
			URL.revokeObjectURL(url);
			toastStore.success(m.actresses_exported({ count: data.length }), 3000);
		},
		onError: (err: Error) => {
			toastStore.error(err.message || m.actresses_export_failed(), 4000);
		}
	}));

	const importMutation = createMutation(() => ({
		mutationFn: (payload: { actresses: ActressUpsertRequest[] }) =>
			apiClient.importActresses(payload),
		onSuccess: (res: ImportResponse) => {
			toastStore.success(m.actresses_import_complete({ imported: res.imported, skipped: res.skipped, errors: res.errors }), 5000);
			void queryClient.invalidateQueries({ queryKey: ['actresses'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message || m.actresses_import_failed(), 4000);
		}
	}));

	function handleExport() {
		exportMutation.mutate();
	}

	function handleImportClick() {
		importFile?.click();
	}

	async function handleImportChange(e: Event) {
		const target = e.target as HTMLInputElement;
		const file = target.files?.[0];
		if (!file) return;

		try {
			const text = await file.text();
			const parsed: ActressUpsertRequest[] = JSON.parse(text);
			if (!Array.isArray(parsed)) throw new Error(m.actresses_expected_json_array());

			const actresses = parsed.filter(a => a.first_name || a.japanese_name);

			if (actresses.length === 0) {
				toastStore.error(m.actresses_no_valid_in_file(), 4000);
				return;
			}

			if (!confirm(m.actresses_import_confirm({ count: actresses.length }))) return;

			importMutation.mutate({ actresses });
		} catch (err) {
			toastStore.error(m.actresses_invalid_json({ error: err instanceof Error ? err.message : String(err) }), 4000);
		}

		target.value = '';
	}
</script>

<div class="w-full px-4 py-8 lg:px-6">
	<div class="space-y-6">
		<div
			class="flex flex-wrap items-center justify-between gap-3"
			in:fly|local={{ y: -10, duration: 240, easing: cubicOut }}
		>
			<div>
				<h1 class="text-3xl font-bold">{m.actresses_title()}</h1>
				<p class="text-muted-foreground mt-1">{m.actresses_subtitle()}</p>
			</div>
			<div class="flex items-center gap-2">
				<Button variant="outline" size="sm" onclick={() => startSync('missing')} disabled={syncStarting || syncRestoring}><WandSparkles class="h-4 w-4" />{m.actresses_sync_missing()}</Button>
				<Button variant="outline" size="sm" onclick={() => startSync('selected')} disabled={syncStarting || syncRestoring || store.selectedIds.length === 0}><WandSparkles class="h-4 w-4" />{m.actresses_sync_selected()}</Button>
				<input
					type="file"
					accept=".json"
					bind:this={importFile}
					onchange={handleImportChange}
					class="hidden"
				/>
				<Button
					variant="outline"
					size="sm"
					onclick={handleExport}
					disabled={exportMutation.isPending}
				>
					{#if exportMutation.isPending}
						<Loader2 class="h-4 w-4 animate-spin mr-1" />
					{:else}
						<Download class="h-4 w-4 mr-1" />
					{/if}
					{m.actresses_export()}
				</Button>
				<Button
					variant="outline"
					size="sm"
					onclick={handleImportClick}
					disabled={importMutation.isPending}
				>
					{#if importMutation.isPending}
						<Loader2 class="h-4 w-4 animate-spin mr-1" />
					{:else}
						<Upload class="h-4 w-4 mr-1" />
					{/if}
					{m.actresses_import()}
				</Button>
				<Button variant="outline" onclick={store.refresh}>
					<RefreshCw class="h-4 w-4 {store.isRefreshing ? 'animate-spin' : ''}" />
					{m.common_refresh()}
				</Button>
				<Button onclick={store.resetForm}>
					<Plus class="h-4 w-4" />
					{m.actresses_new_actress()}
				</Button>
			</div>
		</div>

		<div class="grid grid-cols-1 xl:grid-cols-5 gap-6" in:fade|local={{ duration: 240 }}>
			<div class="xl:col-span-2 xl:self-start xl:sticky xl:top-20">
				<ActressForm
					editingId={store.editingId}
					bind:form={store.form}
					formError={store.formError}
					isPending={store.saveActressMutation.isPending}
					onSave={store.saveActress}
					onReset={store.resetForm}
				/>
			</div>

			<div class="xl:col-span-3 space-y-4">
				<ActressToolbar
					bind:queryInput={store.queryInput}
					activeQuery={store.activeQuery}
					bind:viewMode={store.viewMode}
					bind:sortBy={store.sortBy}
					sortOrder={store.sortOrder}
				bind:filter={store.filter}
					selectedIds={store.selectedIds}
					total={store.total}
					actressesCount={store.actresses.length}
					isRefreshing={store.isRefreshing}
					onApplySearch={store.applySearch}
					onClearSearch={store.clearSearch}
					onToggleSortOrder={store.toggleSortOrder}
					onSelectCurrentPage={store.selectCurrentPage}
					onClearSelection={store.clearSelection}
					onStartMergeSelected={store.startMergeSelected}
				/>

				{#if store.error}
					<div in:fly|local={{ y: 8, duration: 180 }}>
						<Card class="p-4 border-destructive bg-destructive/10 text-destructive">
							{store.error}
						</Card>
					</div>
				{/if}

				{#if store.loading}
					<div in:fade|local={{ duration: 180 }}>
						<Card class="p-8 text-center text-muted-foreground">{m.actresses_loading()}</Card>
					</div>
				{:else if store.actresses.length === 0}
					<div in:fade|local={{ duration: 180 }}>
						<Card class="p-8 text-center">
							<p class="text-muted-foreground">{m.actresses_none_found()}</p>
						</Card>
					</div>
				{:else}
					{#key store.viewMode}
						<div in:scale|local={{ start: 0.98, duration: 180, easing: quintOut }} out:fade|local={{ duration: 120 }}>
							{#if store.viewMode === 'cards'}
								<ActressCardsView
									actresses={store.actresses}
									selectedIds={store.selectedIds}
									itemDelay={store.itemDelay}
									getDisplayName={(actress) => store.getDisplayName(actress, firstNameOrder, japaneseNames)}
									isSelected={store.isSelected}
									onToggleSelection={store.toggleSelection}
									onStartEdit={store.startEdit}
									onRemoveActress={(actress) => store.removeActress(actress, firstNameOrder, japaneseNames)}
									deletePending={store.deleteActressMutation.isPending}
								/>
							{:else if store.viewMode === 'compact'}
								<ActressCompactView
									actresses={store.actresses}
									itemDelay={store.itemDelay}
									getDisplayName={(actress) => store.getDisplayName(actress, firstNameOrder, japaneseNames)}
									isSelected={store.isSelected}
									onToggleSelection={store.toggleSelection}
									onStartEdit={store.startEdit}
									onRemoveActress={(actress) => store.removeActress(actress, firstNameOrder, japaneseNames)}
									deletePending={store.deleteActressMutation.isPending}
								/>
							{:else}
								<ActressTableView
									actresses={store.actresses}
									itemDelay={store.itemDelay}
									getDisplayName={(actress) => store.getDisplayName(actress, firstNameOrder, japaneseNames)}
									isSelected={store.isSelected}
									onToggleSelection={store.toggleSelection}
									onStartEdit={store.startEdit}
									onRemoveActress={(actress) => store.removeActress(actress, firstNameOrder, japaneseNames)}
									deletePending={store.deleteActressMutation.isPending}
								/>
							{/if}
						</div>
					{/key}
				{/if}

				<ActressPagination
					currentPage={store.currentPage}
					totalPages={store.totalPages}
					canGoPrev={store.canGoPrev}
					canGoNext={store.canGoNext}
					onPrevPage={store.prevPage}
					onNextPage={store.nextPage}
				/>
			</div>
		</div>
	</div>
</div>

{#if showSyncModal && syncJob}
	<ActressSyncModal job={syncJob} tasks={syncTasks} onCancel={cancelSync} onClose={closeSyncModal} />
{/if}

<ActressMergeModal
	bind:showMergeModal={store.showMergeModal}
	selectedIds={store.selectedIds}
	bind:mergePrimaryId={store.mergePrimaryId}
	mergeSourceQueue={store.mergeSourceQueue}
	mergeCurrentSourceId={store.mergeCurrentSourceId}
	bind:mergeResolutions={store.mergeResolutions}
	mergePreview={store.mergePreview}
	mergePreviewFetching={store.mergePreviewQuery.isFetching}
	mergeSummary={store.mergeSummary}
	mergePending={store.mergeActressMutation.isPending}
	getActressLabelByID={(id) => store.getActressLabelByID(id, firstNameOrder, japaneseNames)}
	onCloseMergeModal={store.closeMergeModal}
	onResetMergeQueueAndPreview={store.resetMergeQueueAndPreview}
	onApplyCurrentMerge={store.applyCurrentMerge}
	onSkipCurrentMerge={store.skipCurrentMerge}
	onSetResolution={store.setResolution}
	formatMergeValue={store.formatMergeValue}
/>