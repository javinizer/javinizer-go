<script lang="ts">
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import type {
		ActressMergeResolution,
		ActressMergePreviewResponse,
		ActressMergePolicy,
		ActressMergeField
	} from '$lib/api/types';
	import * as m from '$lib/paraglide/messages';

	let {
		showMergeModal = $bindable(),
		selectedIds,
		mergePrimaryId = $bindable(),
		mergeSourceQueue,
		mergeCurrentSourceId,
		mergeResolutions,
		mergePolicy,
		onSetMergePolicy,
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
		mergePolicy: ActressMergePolicy;
		onSetMergePolicy: (policy: ActressMergePolicy) => void;
		mergePreview: ActressMergePreviewResponse | null;
		mergePreviewFetching: boolean;
		mergeSummary: { success: number; failed: number; messages: string[] };
		mergePending: boolean;
		getActressLabelByID: (id: number) => string;
		onCloseMergeModal: () => void;
		onResetMergeQueueAndPreview: () => void;
		onApplyCurrentMerge: () => void;
		onSkipCurrentMerge: () => void;
		onSetResolution: (field: string, decision: ActressMergeResolution | undefined) => void;
		formatMergeValue: (value: unknown) => string;
	} = $props();

	function fieldLabel(field: ActressMergeField['field']): string {
		switch (field) {
			case 'dmm_id':
				return m.actresses_dmm_id();
			case 'first_name':
				return m.actresses_first_name();
			case 'last_name':
				return m.actresses_last_name();
			case 'japanese_name':
				return m.actresses_japanese_name();
			case 'thumb_url':
				return m.actresses_thumb_url();
		}
	}

	let discardsSourceDmm = $derived(
		mergePreview?.fields.some((field) =>
			field.field === 'dmm_id' && field.result_value === 0 && Number(field.source_value) > 0
		) ?? false
	);
</script>

{#if showMergeModal}
	<div class="fixed inset-0 z-50 bg-black/50 p-4 flex items-center justify-center">
		<Card class="w-full max-w-3xl max-h-[90vh] overflow-hidden">
			<div class="p-4 border-b flex items-center justify-between">
				<h2 class="text-lg font-semibold">{m.actresses_merge_selected()}</h2>
				<Button variant="outline" size="sm" onclick={onCloseMergeModal} disabled={mergePending}>{m.common_close()}</Button>
			</div>

			<div class="p-4 space-y-4 overflow-auto max-h-[70vh]">
				{#if selectedIds.length < 2}
					<p class="text-sm text-muted-foreground">{m.actresses_merge_select_at_least_two()}</p>
				{:else}
					<div class="space-y-2">
						<label class="text-sm font-medium" for="merge-primary">{m.actresses_primary_to_keep()}</label>
						<select
							id="merge-primary"
							disabled={mergePending}
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

					{#if mergeCurrentSourceId}
						<div class="space-y-2">
							<label class="block text-sm font-medium" for="merge-policy">{m.actresses_merge_policy()}</label>
							<select
								id="merge-policy"
								value={mergePolicy}
								disabled={mergePending}
								class="max-w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
								onchange={(event) => onSetMergePolicy(event.currentTarget.value as ActressMergePolicy)}
							>
								<option value="fill_empty">{m.actresses_merge_policy_fill_empty()}</option>
								<option value="preserve_target">{m.actresses_merge_policy_preserve_target()}</option>
							</select>
							<p class="text-xs text-muted-foreground">{m.actresses_merge_policy_help()}</p>
						</div>
					{/if}

					{#if mergeCurrentSourceId && (mergePreviewFetching || !mergePreview)}
						<p class="text-sm text-muted-foreground">{m.actresses_loading_merge_preview()}</p>
					{:else if mergePreview && mergeCurrentSourceId}
						<div class="space-y-3">
							<div class="text-sm">
								{m.actresses_merge_review_fields({ source: mergeCurrentSourceId, target: mergePrimaryId ?? 0 })}
							</div>

							<p class="rounded-md border border-amber-500/30 bg-amber-500/10 p-3 text-sm">
								{m.actresses_merge_source_deleted({ id: mergeCurrentSourceId })}
							</p>
							{#if discardsSourceDmm}
								<p class="rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm" role="alert">
									{m.actresses_merge_discards_dmm({ id: mergePreview.source.dmm_id ?? 0 })}
								</p>
							{/if}

							<div class="space-y-3">
								{#each mergePreview.fields as field (field.field)}
									<fieldset class="min-w-0 rounded-md border border-input p-3 space-y-2" disabled={mergePending}>
										<legend class="px-1 font-medium text-sm">{fieldLabel(field.field)}</legend>
										<label class="flex items-center gap-2 text-sm cursor-pointer">
											<input
												type="radio"
												name={`merge-${field.field}`}
												checked={!mergeResolutions[field.field]}
												onchange={() => onSetResolution(field.field, undefined)}
											/>
											{m.actresses_merge_use_policy({ decision: field.default_resolution === 'target' ? m.actresses_keep_target() : m.actresses_use_source() })}
										</label>
										<div class="grid grid-cols-1 md:grid-cols-2 gap-3 text-sm">
											<label class="min-w-0 rounded-md border border-input p-2 flex items-start gap-2 cursor-pointer">
												<input
													type="radio"
													name={`merge-${field.field}`}
													checked={mergeResolutions[field.field] === 'target'}
													onchange={() => onSetResolution(field.field, 'target')}
												/>
												<span class="min-w-0 break-words">
													<span class="font-medium">{m.actresses_keep_target()}</span><br />
													<span class="text-muted-foreground">{formatMergeValue(field.target_value)}</span>
												</span>
											</label>
											<label class="min-w-0 rounded-md border border-input p-2 flex items-start gap-2 cursor-pointer">
												<input
													type="radio"
													name={`merge-${field.field}`}
													checked={mergeResolutions[field.field] === 'source'}
													onchange={() => onSetResolution(field.field, 'source')}
												/>
												<span class="min-w-0 break-words">
													<span class="font-medium">{m.actresses_use_source()}</span><br />
													<span class="text-muted-foreground">{formatMergeValue(field.source_value)}</span>
												</span>
											</label>
										</div>
										<p class="text-sm break-words" data-merge-result={field.field}>
											{m.actresses_merge_result({ value: formatMergeValue(field.result_value) })}
										</p>
									</fieldset>
								{/each}
							</div>

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
