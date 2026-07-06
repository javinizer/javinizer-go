<script lang="ts">
	import { fade, scale } from 'svelte/transition';
	import { cubicOut } from 'svelte/easing';
	import { portalToBody } from '$lib/actions/portal';
	import PathInput from '$lib/components/PathInput.svelte';
	import FileBrowser from '$lib/components/FileBrowser.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { toastStore } from '$lib/stores/toast';
	import { FolderPlus, Trash2, Star, Folder, FolderOpen, X, Info } from 'lucide-svelte';

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
	let showBrowser = $state(false);
	let browserPath = $state('');
	let browserInitialPath = $state('');

	function addDir(e?: Event) {
		e?.preventDefault();
		const path = newDir.trim();
		if (!path) return;
		if (directories.includes(path)) {
			toastStore.error('Directory already in the allowed list', 3000);
			return;
		}
		directories = [...directories, path];
		newDir = '';
	}

	function displayPath(dir: string): string {
		if (dir === '.') return 'current directory (.)';
		return dir;
	}

	function pathTooltip(dir: string): string {
		if (dir === '.') return '"." means the current working directory (where the app was launched from). Add an absolute path like /Users/you/Videos for a stable, explicit location.';
		return dir;
	}

	function removeDir(index: number) {
		directories = directories.filter((_, i) => i !== index);
	}

	function openBrowser() {
		// Start at the typed path, else the first allowed dir, else the user's
		// home (~) — a useful default when the allowlist is empty (first-time
		// setup) rather than '/' which buries videos folders several levels deep.
		browserInitialPath = newDir.trim() || whitelistPaths[0] || '~';
		browserPath = browserInitialPath;
		showBrowser = true;
	}

	function handleBrowserPathChange(path: string) {
		browserPath = path;
	}

	function confirmBrowse() {
		showBrowser = false;
		newDir = browserPath;
		addDir();
	}

	function cancelBrowse() {
		showBrowser = false;
	}
</script>

<div class={className}>
	{#if directories.length === 0}
		<div class="mb-4 rounded-lg border border-dashed border-border bg-muted/20 px-4 py-6 text-center">
			<div class="mx-auto mb-3 flex h-11 w-11 items-center justify-center rounded-full bg-primary/10 text-primary">
				<FolderPlus class="h-5 w-5" />
			</div>
			<p class="text-sm text-muted-foreground">{emptyHint}</p>
		</div>
	{:else}
		<ul class="mb-4 space-y-2">
			{#each directories as dir, index (dir + '-' + index)}
				<li class="group flex items-center gap-3 rounded-lg border border-border bg-background px-3 py-2.5 transition-colors hover:border-primary/40">
					{#if showDefaultBadge && index === 0}
						<span
							class="inline-flex shrink-0 items-center gap-1 rounded-full bg-amber-500/15 px-2 py-0.5 text-xs font-medium text-amber-700 dark:text-amber-400"
							title="The first allowed directory is used as the default scan path"
						>
							<Star class="h-3 w-3" />
							Default
						</span>
					{:else}
						<Folder class="h-4 w-4 shrink-0 text-muted-foreground" />
					{/if}
					<span class="min-w-0 flex-1 truncate font-mono text-sm" title={pathTooltip(dir)}>{displayPath(dir)}</span>
					{#if dir === '.'}
						<span class="shrink-0 text-muted-foreground" title={pathTooltip(dir)} aria-label="What does dot mean?">
							<Info class="h-3.5 w-3.5" />
						</span>
					{/if}
					<button
						type="button"
						class="shrink-0 text-muted-foreground opacity-60 transition-colors hover:text-destructive group-hover:opacity-100"
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

	<form onsubmit={addDir} class="flex items-start gap-2">
		<PathInput
			bind:value={newDir}
			{placeholder}
			{whitelistPaths}
			class="flex-1"
			onnavigate={() => addDir()}
		/>
		<Button
			variant="outline"
			size="sm"
			type="button"
			onclick={openBrowser}
			title="Browse for a directory"
			aria-label="Browse for a directory"
		>
			{#snippet children()}
				<FolderOpen class="h-4 w-4" />
				<span class="hidden sm:inline">Browse</span>
			{/snippet}
		</Button>
		<Button
			variant="outline"
			size="sm"
			type="submit"
			onclick={addDir}
			disabled={!newDir.trim()}
			title="Add allowed directory"
		>
			{#snippet children()}
				<FolderPlus class="h-4 w-4" />
				<span class="hidden sm:inline">Add</span>
			{/snippet}
		</Button>
	</form>
	<p class="mt-2 text-xs text-muted-foreground">
		Press <kbd class="rounded border border-border bg-muted px-1 py-0.5 font-mono text-[10px]">Enter</kbd> to add the typed path. Use ↑↓ to pick a suggestion.
	</p>
</div>

{#if showBrowser}
	<!-- svelte-ignore a11y_click_events_have_key_events -->
	<div
		class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
		use:portalToBody
		in:fade|local={{ duration: 140 }}
		out:fade|local={{ duration: 120 }}
		role="presentation"
		onclick={cancelBrowse}
		onkeydown={(e) => { if (e.key === 'Escape') cancelBrowse(); }}
	>
		<div
			class="flex max-h-[80vh] w-full max-w-4xl flex-col rounded-lg bg-background shadow-xl"
			in:scale|local={{ start: 0.97, duration: 180, easing: cubicOut }}
			out:scale|local={{ start: 1, opacity: 0.7, duration: 140, easing: cubicOut }}
			role="dialog"
			aria-modal="true"
			aria-label="Browse for a directory"
			tabindex="-1"
			onclick={(e) => e.stopPropagation()}
			onkeydown={(e) => { if (e.key !== 'Escape') e.stopPropagation(); }}
		>
			<div class="flex items-center justify-between border-b p-4">
				<div>
					<h2 class="text-lg font-semibold">Select a directory</h2>
					<p class="mt-0.5 text-sm text-muted-foreground">Navigate to and choose a folder to allow</p>
				</div>
				<button
					type="button"
					class="text-muted-foreground transition-colors hover:text-foreground"
					aria-label="Close directory browser"
					onclick={cancelBrowse}
				>
					<X class="h-5 w-5" />
				</button>
			</div>

			<div class="flex-1 overflow-auto p-4">
				<FileBrowser
					initialPath={browserInitialPath}
					onPathChange={handleBrowserPathChange}
					multiSelect={false}
					folderOnly={true}
					{whitelistPaths}
				/>
			</div>

			<div class="space-y-3 border-t p-4">
				<div class="flex items-center gap-2">
					<span class="shrink-0 text-sm font-medium text-muted-foreground">Selected:</span>
					<code class="min-w-0 flex-1 overflow-x-auto rounded bg-accent px-3 py-1.5 font-mono text-sm text-foreground">
						{browserPath || browserInitialPath}
					</code>
				</div>
				<div class="flex items-center justify-end gap-2">
					<Button variant="outline" onclick={cancelBrowse}>
						{#snippet children()}
							Cancel
						{/snippet}
					</Button>
					<Button onclick={confirmBrowse} disabled={!browserPath.trim()}>
						{#snippet children()}
							<FolderPlus class="mr-1 h-4 w-4" />
							Add directory
						{/snippet}
					</Button>
				</div>
			</div>
		</div>
	</div>
{/if}
