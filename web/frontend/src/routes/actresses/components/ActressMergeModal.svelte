<script lang="ts">
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import type { ActressMergeResolution, ActressMergePreviewResponse } from '$lib/api/types';

	let {
		showMergeModal = $bindable(),
		selectedIds,
		mergePrimaryId = $bindable(),
		mergeSourceQueue,
		mergeCurrentSourceId,
		mergeResolutions = $bindable(),
		mergePreview,
		mergePreviewFetching,
		mergeSummary,
		mergePending,
		getActressLabelByID,
		onCloseMergeModal,
		onResetMergeQueueAndPreview,
		onApplyCurrentMerge,
		onSkipCurrentMerge,
		onSetResolution,
		formatMergeValue
	}: {
		showMergeModal: boolean;
		selectedIds: number[];
		mergePrimaryId: number | null;
		mergeSourceQueue: number[];
		mergeCurrentSourceId: number | null;
		mergeResolutions: Record<string, ActressMergeResolution>;
		mergePreview: ActressMergePreviewResponse | null;
		mergePreviewFetching: boolean;
		mergeSummary: { success: number; failed: number; messages: string[] };
		mergePending: boolean;
		getActressLabelByID: (id: number) => string;
		onCloseMergeModal: () => void;
		onResetMergeQueueAndPreview: () => void;
		onApplyCurrentMerge: () => void;
		onSkipCurrentMerge: () => void;
		onSetResolution: (field: string, decision: ActressMergeResolution) => void;
		formatMergeValue: (value: unknown) => string;
	} = $props();
</script>

{#if showMergeModal}
	<div class="fixed inset-0 z-50 bg-black/50 p-4 flex items-center justify-center">
		<Card class="w-full max-w-3xl max-h-[90vh] overflow-hidden">
			<div class="p-4 border-b flex items-center justify-between">
				<h2 class="text-lg font-semibold">Merge Selected Actresses</h2>
				<Button variant="outline" size="sm" onclick={onCloseMergeModal}>Close</Button>
			</div>

			<div class="p-4 space-y-4 overflow-auto max-h-[70vh]">
				{#if selectedIds.length < 2}
					<p class="text-sm text-muted-foreground">Select at least two actresses from the current page to merge.</p>
				{:else}
					<div class="space-y-2">
						<label class="text-sm font-medium" for="merge-primary">Primary actress to keep</label>
						<select
							id="merge-primary"
							value={mergePrimaryId ?? ''}
							class="rounded-md border border-input bg-background px-3 py-2 text-sm"
							onchange={(event) => {
								const value = Number.parseInt((event.currentTarget as HTMLSelectElement).value, 10);
								mergePrimaryId = Number.isNaN(value) ? null : value;
								onResetMergeQueueAndPreview();
							}}
						>
							{#each selectedIds as selectedID}
								<option value={selectedID}>{getActressLabelByID(selectedID)}</option>
							{/each}
						</select>
					</div>

					<div class="rounded-md border border-input bg-muted/20 px-3 py-2 text-sm">
						Queue: {mergeSourceQueue.length} remaining
						{#if mergeCurrentSourceId}
							• processing source #{mergeCurrentSourceId}
						{/if}
					</div>

					{#if mergePreviewFetching && mergeCurrentSourceId}
						<p class="text-sm text-muted-foreground">Loading merge preview...</p>
					{:else if mergePreview && mergeCurrentSourceId}
						<div class="space-y-3">
							<div class="text-sm">
								Review conflicts for <span class="font-medium">#{mergeCurrentSourceId}</span> -> <span class="font-medium">#{mergePrimaryId}</span>
							</div>

							{#if mergePreview.conflicts.length === 0}
								<div class="rounded-md border border-input bg-muted/20 px-3 py-2 text-sm">
									No field conflicts. Safe to merge with defaults.
								</div>
							{:else}
								<div class="space-y-2">
									{#each mergePreview.conflicts as conflict}
										<div class="rounded-md border border-input p-3 space-y-2">
											<div class="font-medium text-sm">{conflict.field}</div>
											<div class="grid grid-cols-1 md:grid-cols-2 gap-3 text-sm">
												<label class="rounded-md border border-input p-2 flex items-start gap-2 cursor-pointer">
													<input
														type="radio"
														name={`conflict-${conflict.field}`}
														checked={(mergeResolutions[conflict.field] || conflict.default_resolution) === 'target'}
														onchange={() => onSetResolution(conflict.field, 'target')}
													/>
													<span>
														<span class="font-medium">Keep target</span><br />
														<span class="text-muted-foreground">{formatMergeValue(conflict.target_value)}</span>
													</span>
												</label>
												<label class="rounded-md border border-input p-2 flex items-start gap-2 cursor-pointer">
													<input
														type="radio"
														name={`conflict-${conflict.field}`}
														checked={(mergeResolutions[conflict.field] || conflict.default_resolution) === 'source'}
														onchange={() => onSetResolution(conflict.field, 'source')}
													/>
													<span>
														<span class="font-medium">Use source</span><br />
														<span class="text-muted-foreground">{formatMergeValue(conflict.source_value)}</span>
													</span>
												</label>
											</div>
										</div>
									{/each}
								</div>
							{/if}

							<div class="flex items-center gap-2">
								<Button variant="outline" onclick={onSkipCurrentMerge} disabled={mergePending}>
									Skip
								</Button>
								<Button onclick={onApplyCurrentMerge} disabled={mergePending}>
									{mergePending ? 'Merging...' : 'Apply Merge'}
								</Button>
							</div>
						</div>
					{:else}
						<div class="rounded-md border border-input bg-green-500/10 px-3 py-2 text-sm">
							Queue complete.
						</div>
					{/if}
				{/if}

				{#if mergeSummary.messages.length > 0}
					<div class="space-y-2">
						<div class="text-sm font-medium">
							Summary: {mergeSummary.success} succeeded, {mergeSummary.failed} failed
						</div>
						<div class="max-h-40 overflow-auto rounded-md border border-input p-2 text-xs space-y-1">
							{#each mergeSummary.messages as message, idx (`merge-log-${idx}`)}
								<div>{message}</div>
							{/each}
						</div>
					</div>
				{/if}
			</div>
		</Card>
	</div>
{/if}
