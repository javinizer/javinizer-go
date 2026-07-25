<script lang="ts">
	import type { OrganizePreviewResponse } from '$lib/api/types';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import { FolderOpen } from 'lucide-svelte';
	import * as m from '$lib/paraglide/messages';

	type OrganizeOperation = 'move' | 'copy' | 'hardlink' | 'softlink';

	interface Props {
		destinationPath: string;
		organizeOperation: OrganizeOperation;
		previewNeedsDestination: boolean;
		effectiveOperationMode?: string;
		skipNfo?: boolean;
		skipDownload?: boolean;
		onOpenDestinationBrowser: () => void;
	}

	let {
		destinationPath = $bindable(''),
		organizeOperation = $bindable<OrganizeOperation>('move'),
		previewNeedsDestination = false,
		effectiveOperationMode,
		skipNfo = $bindable(false),
		skipDownload = $bindable(false),
		onOpenDestinationBrowser
	}: Props = $props();

	let opMode = $derived(effectiveOperationMode || 'organize');
	let needsDestination = $derived(opMode === 'organize');
	let isInPlaceImplied = $derived(
		effectiveOperationMode === 'organize' && opMode === 'in-place-norenamefolder'
	);

	function getOperationLabel(mode?: string): string {
		switch (mode) {
			case 'in-place': return m.browse_op_reorganize();
			case 'in-place-norenamefolder': return m.browse_op_rename_only();
			case 'metadata-artwork': return m.browse_op_metadata_artwork();
			case 'organize': return m.browse_op_organize();
			default: return m.browse_op_organize();
		}
	}
</script>

<Card class="p-4">
	<div class="space-y-3 min-w-0">
		<div class="flex items-center gap-2">
			<FolderOpen class="h-5 w-5 text-primary" />
			<h3 class="font-semibold">
				{needsDestination ? m.browse_output_destination() : m.browse_file_operations()}
			</h3>
		</div>

		{#if needsDestination}
			<div class="flex gap-2 min-w-0">
				<input
					type="text"
					bind:value={destinationPath}
					placeholder={m.review_destination_placeholder()}
					class="flex-1 min-w-0 px-3 py-2 border rounded-md bg-background focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono text-sm"
					title={destinationPath}
				/>
				<Button onclick={onOpenDestinationBrowser} variant="outline">
					{#snippet children()}
						<FolderOpen class="h-4 w-4 mr-2" />
						{m.browse_browse_button()}
					{/snippet}
				</Button>
			</div>

			{#if previewNeedsDestination && !destinationPath.trim()}
				<p class="text-xs text-muted-foreground">
					{m.review_set_destination_for_preview()}
				</p>
			{/if}

			<div class="space-y-2">
				<label for="organizeOperation" class="text-sm font-medium">{m.review_file_operation_label()}</label>
				<select
					id="organizeOperation"
					bind:value={organizeOperation}
					class="w-full px-3 py-2 border rounded-md bg-background focus:ring-2 focus:ring-primary focus:border-primary transition-all text-sm"
				>
					<option value="move">{m.review_op_move()}</option>
					<option value="copy">{m.review_op_copy()}</option>
					<option value="hardlink">{m.review_op_hardlink()}</option>
					<option value="softlink">{m.review_op_softlink()}</option>
				</select>
				<p class="text-xs text-muted-foreground">
					{#if organizeOperation === 'hardlink'}
						Hard links require source and destination on the same filesystem.
					{:else if organizeOperation === 'softlink'}
						{m.review_op_softlink_desc()}
					{:else if organizeOperation === 'copy'}
						{m.review_op_copy_desc()}
					{:else}
						{m.review_op_move_desc()}
					{/if}
				</p>
			</div>
		{:else}
			<p class="text-xs text-muted-foreground">
				{m.review_files_stay_in_place({ label: getOperationLabel(opMode) })}
			</p>
			{#if isInPlaceImplied}
				<p class="text-xs text-primary mt-1">
					{m.review_auto_switched_organize()}
				</p>
			{/if}
		{/if}

		<div class="grid gap-3 md:grid-cols-2">
			<label
				class="flex items-center gap-3 p-3 rounded-lg border border-border bg-background hover:bg-accent/50 cursor-pointer transition-colors"
			>
				<input
					type="checkbox"
					bind:checked={skipNfo}
					class="h-4 w-4 rounded border-input text-primary focus:ring-2 focus:ring-primary"
				/>
				<div class="flex-1">
					<span class="text-sm font-medium">{m.review_skip_nfo_generation()}</span>
					<p class="text-xs text-muted-foreground">{m.review_skip_nfo_desc()}</p>
				</div>
			</label>

			<label
				class="flex items-center gap-3 p-3 rounded-lg border border-border bg-background hover:bg-accent/50 cursor-pointer transition-colors"
			>
				<input
					type="checkbox"
					bind:checked={skipDownload}
					class="h-4 w-4 rounded border-input text-primary focus:ring-2 focus:ring-primary"
				/>
				<div class="flex-1">
					<span class="text-sm font-medium">{m.review_skip_media_download()}</span>
					<p class="text-xs text-muted-foreground">{m.review_skip_media_download_desc()}</p>
					</div>
			</label>
		</div>

	</div>
</Card>