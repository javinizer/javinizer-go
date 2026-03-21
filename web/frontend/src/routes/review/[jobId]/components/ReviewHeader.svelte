<script lang="ts">
	import Button from '$lib/components/ui/Button.svelte';
	import { LoaderCircle, Play, RefreshCw, X } from 'lucide-svelte';

	interface Props {
		isUpdateMode: boolean;
		organizing: boolean;
		movieResultsLength: number;
		destinationPath: string;
		onClose: () => void;
		onUpdateAll: () => void;
		onOrganizeAll: () => void;
	}

	let {
		isUpdateMode,
		organizing,
		movieResultsLength,
		destinationPath,
		onClose,
		onUpdateAll,
		onOrganizeAll
	}: Props = $props();
</script>

<div class="flex items-center justify-between mb-6">
	<div>
		<h1 class="text-3xl font-bold">Review & Edit Metadata</h1>
		<p class="text-muted-foreground mt-1">
			{#if isUpdateMode}
				Metadata and media files have been updated in place. Review and edit as needed.
			{:else}
				Review and edit scraped metadata before organizing files
			{/if}
		</p>
	</div>
	<div class="flex items-center gap-3">
		<Button variant="outline" onclick={onClose} disabled={organizing}>
			{#snippet children()}
				<X class="h-4 w-4 mr-2" />
				{isUpdateMode ? 'Close' : 'Cancel'}
			{/snippet}
		</Button>
		{#if isUpdateMode}
			<Button onclick={onUpdateAll} disabled={organizing}>
				{#snippet children()}
					{#if organizing}
						<LoaderCircle class="h-4 w-4 mr-2 animate-spin" />
					{:else}
						<RefreshCw class="h-4 w-4 mr-2" />
					{/if}
					{organizing ? 'Updating...' : `Update ${movieResultsLength} File${movieResultsLength !== 1 ? 's' : ''}`}
				{/snippet}
			</Button>
		{:else}
			<Button onclick={onOrganizeAll} disabled={organizing || !destinationPath.trim()}>
				{#snippet children()}
					{#if organizing}
						<LoaderCircle class="h-4 w-4 mr-2" />
					{:else}
						<Play class="h-4 w-4 mr-2" />
					{/if}
					{organizing ? 'Organizing...' : `Organize ${movieResultsLength} File${movieResultsLength !== 1 ? 's' : ''}`}
				{/snippet}
			</Button>
		{/if}
	</div>
</div>
