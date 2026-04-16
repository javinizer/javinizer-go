<script lang="ts">
	import Button from '$lib/components/ui/Button.svelte';
	import { ChevronDown, ChevronUp, LoaderCircle, Play, RefreshCw, Settings2, X } from 'lucide-svelte';

	interface Props {
		isUpdateMode: boolean;
		canOrganize: boolean;
		organizing: boolean;
		movieResultsLength: number;
		destinationPath: string;
		forceOverwrite?: boolean;
		preserveNfo?: boolean;
		skipNfo?: boolean;
		skipDownload?: boolean;
		onClose: () => void;
		onUpdateAll: () => void;
		onOrganizeAll: () => void;
	}

		let {
		isUpdateMode,
		canOrganize,
		organizing,
		movieResultsLength,
		destinationPath,
		forceOverwrite = $bindable(false),
		preserveNfo = $bindable(false),
		skipNfo = $bindable(false),
		skipDownload = $bindable(false),
		onClose,
		onUpdateAll,
		onOrganizeAll
	}: Props = $props();

	$effect(() => {
		if (forceOverwrite) preserveNfo = false;
	});

	$effect(() => {
		if (preserveNfo) forceOverwrite = false;
	});

	let showOptions = $state(false);
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
			<Button onclick={onOrganizeAll} disabled={organizing || !canOrganize || !destinationPath.trim()}>
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

{#if isUpdateMode}
	<div class="mb-4">
		<button
			onclick={() => (showOptions = !showOptions)}
			class="flex items-center gap-2 text-sm font-medium text-muted-foreground hover:text-foreground transition-colors"
		>
			<Settings2 class="h-4 w-4" />
			Options
			{#if showOptions}
				<ChevronUp class="h-3 w-3" />
			{:else}
				<ChevronDown class="h-3 w-3" />
			{/if}
		</button>

		{#if showOptions}
			<div class="grid gap-3 md:grid-cols-4 mt-3">
				<label
					class="flex items-center gap-3 p-3 rounded-lg border border-border bg-background hover:bg-accent/50 cursor-pointer transition-colors"
				>
					<input
						type="checkbox"
						bind:checked={forceOverwrite}
						class="h-4 w-4 rounded border-gray-300 text-primary focus:ring-2 focus:ring-primary"
					/>
					<div class="flex-1">
						<span class="text-sm font-medium">Force Overwrite</span>
						<p class="text-xs text-muted-foreground">Ignore existing NFO, use only scraper data</p>
					</div>
				</label>

				<label
					class="flex items-center gap-3 p-3 rounded-lg border border-border bg-background hover:bg-accent/50 cursor-pointer transition-colors"
				>
					<input
						type="checkbox"
						bind:checked={preserveNfo}
						class="h-4 w-4 rounded border-gray-300 text-primary focus:ring-2 focus:ring-primary"
					/>
					<div class="flex-1">
						<span class="text-sm font-medium">Preserve NFO</span>
						<p class="text-xs text-muted-foreground">Never overwrite NFO fields, only add missing</p>
					</div>
				</label>

				<label
					class="flex items-center gap-3 p-3 rounded-lg border border-border bg-background hover:bg-accent/50 cursor-pointer transition-colors"
				>
					<input
						type="checkbox"
						bind:checked={skipNfo}
						class="h-4 w-4 rounded border-gray-300 text-primary focus:ring-2 focus:ring-primary"
					/>
					<div class="flex-1">
						<span class="text-sm font-medium">Skip NFO</span>
						<p class="text-xs text-muted-foreground">Don't generate NFO metadata files</p>
					</div>
				</label>

				<label
					class="flex items-center gap-3 p-3 rounded-lg border border-border bg-background hover:bg-accent/50 cursor-pointer transition-colors"
				>
					<input
						type="checkbox"
						bind:checked={skipDownload}
						class="h-4 w-4 rounded border-gray-300 text-primary focus:ring-2 focus:ring-primary"
					/>
					<div class="flex-1">
						<span class="text-sm font-medium">Skip Download</span>
						<p class="text-xs text-muted-foreground">Don't download cover, poster, and screenshots</p>
					</div>
				</label>
			</div>
		{/if}
	</div>
{/if}
