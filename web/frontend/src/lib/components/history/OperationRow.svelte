<script lang="ts">
	import { CircleCheckBig, CircleX, Undo2, LoaderCircle } from 'lucide-svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import type { OperationItem } from '$lib/api/types';

	interface Props {
		operation: OperationItem;
		onRevert?: (movieId: string) => void;
		reverting?: boolean;
		revertible?: boolean;
	}

	let { operation, onRevert, reverting = false, revertible = true }: Props = $props();

	// Determine display state based on revert_status
	const state = $derived.by(() => {
		const rs = operation.revert_status.toLowerCase();
		if (rs === 'reverted') return 'reverted';
		if (rs === 'failed') return 'failed';
		return 'success'; // 'pending' treated as success (can be reverted)
	});

	const statusBadge = $derived.by<'success' | 'failed' | 'reverted'>(() => {
		if (state === 'reverted') return 'reverted';
		if (state === 'failed') return 'failed';
		return 'success';
	});

	// Path display helpers
	function getFilename(path: string): string {
		if (!path) return '';
		const parts = path.split('/');
		return parts[parts.length - 1] || path;
	}

	function getParentDir(path: string): string {
		if (!path) return '';
		const lastSlash = path.lastIndexOf('/');
		if (lastSlash === -1) return '';
		return path.slice(0, lastSlash);
	}

	function truncateDir(dir: string, maxLen: number = 50): string {
		if (!dir || dir.length <= maxLen) return dir;
		const half = Math.floor((maxLen - 3) / 2);
		return dir.slice(0, half) + '...' + dir.slice(-half);
	}
</script>

<div class="p-4 rounded-lg border bg-card">
	<!-- Header: icon + movie_id + status badge -->
	<div class="flex items-center justify-between mb-2">
		<div class="flex items-center gap-2">
			{#if state === 'success'}
				<CircleCheckBig class="h-4 w-4 text-green-500" />
			{:else if state === 'reverted'}
				<Undo2 class="h-4 w-4 text-yellow-500" />
			{:else}
				<CircleX class="h-4 w-4 text-red-500" />
			{/if}
			<span class="font-semibold text-sm">{operation.movie_id}</span>
			<StatusBadge status={statusBadge} size="sm" />
		</div>

		<!-- Right action -->
		{#if state === 'success' && onRevert && revertible}
			{#if reverting}
				<Button variant="outline" size="sm" disabled>
					<LoaderCircle class="h-4 w-4 animate-spin" />
					Reverting...
				</Button>
			{:else}
				<Button
					variant="outline"
					size="sm"
					class="text-destructive hover:bg-destructive/10"
					onclick={() => onRevert(operation.movie_id)}
				>
					<Undo2 class="h-4 w-4 mr-1" />
					Revert File
				</Button>
			{/if}
		{:else if state === 'reverted'}
			<Button variant="ghost" size="sm" disabled>
				<Undo2 class="h-4 w-4 mr-1" />
				Reverted ✓
			</Button>
		{/if}
	</div>

	<!-- Path display: From → To -->
	<div class="text-sm space-y-1 ml-6">
		{#if operation.original_path}
			<div class="flex items-baseline gap-1" title={operation.original_path}>
				<span class="text-muted-foreground shrink-0">From:</span>
				<span class="font-medium text-foreground">{getFilename(operation.original_path)}</span>
				{#if getParentDir(operation.original_path)}
					<span class="text-muted-foreground text-xs truncate max-w-xs">
						in {truncateDir(getParentDir(operation.original_path))}
					</span>
				{/if}
			</div>
		{/if}
		{#if operation.new_path}
			<div class="flex items-baseline gap-1" title={operation.new_path}>
				<span class="text-muted-foreground shrink-0">To:</span>
				<span class="font-medium text-foreground">{getFilename(operation.new_path)}</span>
				{#if getParentDir(operation.new_path)}
					<span class="text-muted-foreground text-xs truncate max-w-xs">
						in {truncateDir(getParentDir(operation.new_path))}
					</span>
				{/if}
			</div>
		{/if}
		{#if operation.in_place_renamed}
			<span class="text-xs text-muted-foreground">(in-place rename)</span>
		{/if}
	</div>
</div>
