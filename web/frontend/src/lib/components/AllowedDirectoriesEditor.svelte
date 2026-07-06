<script lang="ts">
	import PathInput from '$lib/components/PathInput.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { toastStore } from '$lib/stores/toast';
	import { FolderPlus, Trash2, Star, Folder } from 'lucide-svelte';

	interface Props {
		directories: string[];
		placeholder?: string;
		emptyHint?: string;
		showDefaultBadge?: boolean;
		whitelistPaths?: string[];
		class?: string;
	}

	let {
		directories = $bindable(),
		placeholder = 'Add a directory (e.g. /mnt/videos)',
		emptyHint = 'No allowed directories configured. Add one below to enable scanning.',
		showDefaultBadge = true,
		whitelistPaths = [],
		class: className = '',
	}: Props = $props();

	let newDir = $state('');

	function addDir() {
		const path = newDir.trim();
		if (!path) return;
		if (directories.includes(path)) {
			toastStore.error('Directory already in the allowed list', 3000);
			return;
		}
		directories = [...directories, path];
		newDir = '';
	}

	function removeDir(index: number) {
		directories = directories.filter((_, i) => i !== index);
	}
</script>

<div class={className}>
	{#if directories.length === 0}
		<div class="rounded-lg border border-dashed border-border p-4 text-center text-sm text-muted-foreground">
			{emptyHint}
		</div>
	{:else}
		<ul class="space-y-2 mb-3">
			{#each directories as dir, index (dir + '-' + index)}
				<li class="flex items-center gap-2 rounded-md border border-border bg-background px-3 py-2">
					{#if showDefaultBadge && index === 0}
						<span
							class="inline-flex items-center gap-1 rounded bg-amber-500/15 text-amber-700 dark:text-amber-400 text-xs font-medium px-2 py-0.5 shrink-0"
							title="The first allowed directory is used as the default scan path"
						>
							<Star class="h-3 w-3" />
							Default
						</span>
					{:else}
						<Folder class="h-4 w-4 text-muted-foreground shrink-0" />
					{/if}
					<span class="flex-1 min-w-0 truncate font-mono text-sm">{dir}</span>
					<button
						type="button"
						class="text-muted-foreground hover:text-destructive transition-colors shrink-0"
						title="Remove directory"
						aria-label="Remove allowed directory {dir}"
						onclick={() => removeDir(index)}
					>
						<Trash2 class="h-4 w-4" />
					</button>
				</li>
			{/each}
		</ul>
	{/if}

	<div class="flex items-start gap-2">
		<PathInput
			bind:value={newDir}
			{placeholder}
			{whitelistPaths}
			class="flex-1"
			onnavigate={() => addDir()}
		/>
		<Button variant="outline" size="sm" onclick={addDir} disabled={!newDir.trim()} title="Add allowed directory">
			{#snippet children()}
				<FolderPlus class="h-4 w-4" />
			{/snippet}
		</Button>
	</div>
	<p class="text-xs text-muted-foreground mt-1">
		Autocomplete uses the current allowed directories. Type the first path manually if the list is empty.
	</p>
</div>
