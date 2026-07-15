<script lang="ts">
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import type { ActressMergeResolution, ActressMergePreviewResponse } from '$lib/api/types';
	import * as m from '$lib/paraglide/messages';

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
				<h2 class="text-lg font-semibold">{m.actresses_merge_selected()}</h2>
				<Button variant="outline" size="sm" onclick={onCloseMergeModal}>{m.common_close()}</Button>
			</div>

			<div class="p-4 space-y-4 overflow-auto max-h-[70vh]">
				{#if selectedIds.length < 2}
					<p class="text-sm text-muted-foreground">{m.actresses_merge_select_at_least_two()}</p>
				{:else}
					<div class="space-y-2">
						<label class="text-sm font-medium" for="merge-primary">{m.actresses_primary_to_keep()}</label>
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
						{m.actresses_queue_remaining({ count: mergeSourceQueue.length })}
						{#if mergeCurrentSourceId}
							{m.actresses_processing_source({ id: mergeCurrentSourceId })}
						{/if}
					</div>

					{#if mergePreviewFetching && mergeCurrentSourceId}
						<p class="text-sm text-muted-foreground">{m.actresses_loading_merge_preview()}</p>
					{:else if mergePreview && mergeCurrentSourceId}
						<div class="space-y-3">
							<div class="text-sm">
								{m.actresses_review_conflicts({ source: mergeCurrentSourceId, target: mergePrimaryId ?? 0 })}
							</div>

							{#if mergePreview.conflicts.length === 0}
								<div class="rounded-md border border-input bg-muted/20 px-3 py-2 text-sm">
									{m.actresses_no_conflicts()}
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
														<span class="font-medium">{m.actresses_keep_target()}</span><br />
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
														<span class="font-medium">{m.actresses_use_source()}</span><br />
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
									{m.actresses_skip()}
								</Button>
								<Button onclick={onApplyCurrentMerge} disabled={mergePending}>
									{mergePending ? m.actresses_merging() : m.actresses_apply_merge()}
								</Button>
							</div>
						</div>
					{:else}
						<div class="rounded-md border border-input bg-green-500/10 px-3 py-2 text-sm">
							{m.actresses_queue_complete()}
						</div>
					{/if}
				{/if}

				{#if mergeSummary.messages.length > 0}
					<div class="space-y-2">
						<div class="text-sm font-medium">
							{m.actresses_merge_summary({ success: mergeSummary.success, failed: mergeSummary.failed })}
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
