<script lang="ts">
	import { apiClient } from '$lib/api/client';
	import { formatBytes } from '$lib/utils';
	import type { FileInfo, BrowseResponse } from '$lib/api/types';
	import {
		Folder,
		File,
		ChevronRight,
		Home,
		RefreshCw,
		CheckSquare,
		Square,
		CheckCheck,
		Check,
		Search,
		X
	} from 'lucide-svelte';
	import Button from './ui/Button.svelte';
	import Card from './ui/Card.svelte';

	interface Props {
		initialPath?: string;
		onFileSelect?: (files: string[]) => void;
		onPathChange?: (path: string) => void;
		multiSelect?: boolean;
		folderOnly?: boolean;  // When true, only allows folder navigation (no file selection)
	}

	let { initialPath = '', onFileSelect, onPathChange, multiSelect = true, folderOnly = false }: Props = $props();

	let currentPath = $state(initialPath);
	let items: FileInfo[] = $state([]);
	let selectedFiles = $state<Set<string>>(new Set());
	let loading = $state(false);
	let error = $state<string | null>(null);
	let filterText = $state('');
	let pathParts = $derived(currentPath.split('/').filter((p) => p));

	// Filter items based on filter text (case-insensitive)
	const filteredItems = $derived(() => {
		if (!filterText.trim()) return items;
		const search = filterText.toLowerCase();
		return items.filter((item) => item.name.toLowerCase().includes(search));
	});

	// Watch for changes to initialPath and browse when it's set
	$effect(() => {
		if (initialPath) {
			browse(initialPath);
		}
	});

	async function browse(path: string) {
		loading = true;
		error = null;
		filterText = ''; // Clear filter when navigating
		try {
			const response: BrowseResponse = await apiClient.browse({ path: path || '/' });
			currentPath = response.current_path;
			onPathChange?.(currentPath);
			items = response.items.sort((a, b) => {
				// Directories first, then files
				if (a.is_dir && !b.is_dir) return -1;
				if (!a.is_dir && b.is_dir) return 1;
				return a.name.localeCompare(b.name);
			});
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to browse directory';
		} finally {
			loading = false;
		}
	}

	function navigateToPath(index: number) {
		const parts = pathParts.slice(0, index + 1);
		const newPath = '/' + parts.join('/');
		browse(newPath);
	}

	function handleItemClick(item: FileInfo) {
		if (item.is_dir) {
			browse(item.path);
			selectedFiles.clear();
		} else if (!folderOnly) {
			// Only allow file selection if not in folderOnly mode
			toggleFileSelection(item.path);
		}
	}

	function toggleFileSelection(path: string) {
		if (multiSelect) {
			const newSet = new Set(selectedFiles);
			if (newSet.has(path)) {
				newSet.delete(path);
			} else {
				newSet.add(path);
			}
			selectedFiles = newSet;
		} else {
			selectedFiles = new Set([path]);
		}
		onFileSelect?.(Array.from(selectedFiles));
	}

	function goUp() {
		const parts = currentPath.split('/').filter((p) => p);
		parts.pop();
		const newPath = parts.length > 0 ? '/' + parts.join('/') : '/';
		browse(newPath);
	}

	function selectAll() {
		// Select all visible (filtered) files
		const allFiles = filteredItems().filter((item) => !item.is_dir).map((item) => item.path);
		selectedFiles = new Set(allFiles);
		onFileSelect?.(Array.from(selectedFiles));
	}

	function selectNone() {
		selectedFiles = new Set();
		onFileSelect?.([]);
	}

	function selectMatched() {
		// Select all visible (filtered) matched files
		const matchedFiles = filteredItems()
			.filter((item) => !item.is_dir && item.matched)
			.map((item) => item.path);
		selectedFiles = new Set(matchedFiles);
		onFileSelect?.(Array.from(selectedFiles));
	}

	// Derived state for file counts (based on filtered items)
	const fileCount = $derived(filteredItems().filter((item) => !item.is_dir).length);
	const matchedCount = $derived(filteredItems().filter((item) => !item.is_dir && item.matched).length);
	const folderCount = $derived(filteredItems().filter((item) => item.is_dir).length);

	// Clear filter when navigating to a new directory
	function clearFilter() {
		filterText = '';
	}
</script>

<Card class="p-4">
	<!-- Breadcrumb Navigation -->
	<div class="flex items-center gap-2 mb-4 pb-4 border-b">
		<Button variant="ghost" size="icon" onclick={() => browse('/')}>
			{#snippet children()}
				<Home class="h-4 w-4" />
			{/snippet}
		</Button>

		<Button variant="ghost" size="icon" onclick={goUp} disabled={currentPath === '/'}>
			{#snippet children()}
				<span class="text-lg">↑</span>
			{/snippet}
		</Button>

		<Button variant="ghost" size="icon" onclick={() => browse(currentPath)}>
			{#snippet children()}
				<RefreshCw class="h-4 w-4" />
			{/snippet}
		</Button>

		<div class="flex items-center gap-1 flex-1 overflow-x-auto">
			<button
				onclick={() => browse('/')}
				class="px-2 py-1 rounded hover:bg-accent hover:text-primary transition-colors cursor-pointer font-medium"
			>
				/
			</button>
			{#each pathParts as part, index}
				<ChevronRight class="h-4 w-4 text-muted-foreground" />
				<button
					onclick={() => navigateToPath(index)}
					class="px-2 py-1 rounded hover:bg-accent hover:text-primary transition-colors cursor-pointer whitespace-nowrap"
				>
					{part}
				</button>
			{/each}
		</div>
	</div>

	<!-- Filter Input -->
	{#if items.length > 0}
		<div class="mb-4 pb-4 border-b">
			<div class="relative">
				<Search class="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
				<input
					type="text"
					bind:value={filterText}
					placeholder="Filter files and folders..."
					class="w-full pl-10 pr-10 py-2 border rounded-md text-sm focus:ring-2 focus:ring-primary focus:border-primary transition-all"
				/>
				{#if filterText}
					<button
						onclick={clearFilter}
						class="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors"
						title="Clear filter"
					>
						<X class="h-4 w-4" />
					</button>
				{/if}
			</div>
			{#if filterText}
				<div class="mt-2 text-xs text-muted-foreground">
					Showing {folderCount} folder{folderCount !== 1 ? 's' : ''} and {fileCount} file{fileCount !== 1 ? 's' : ''} matching "{filterText}"
				</div>
			{/if}
		</div>
	{/if}

	<!-- Selection Controls (hidden in folderOnly mode) -->
	{#if items.length > 0 && fileCount > 0 && !folderOnly}
		<div class="mb-4 pb-4 border-b flex items-center justify-between gap-4">
			<div class="flex items-center gap-2 text-sm text-muted-foreground">
				<span class="font-medium">{fileCount} files</span>
				{#if matchedCount > 0}
					<span class="text-green-600">• {matchedCount} matched</span>
				{/if}
			</div>
			<div class="flex items-center gap-2">
				<Button variant="outline" size="sm" onclick={selectAll} disabled={fileCount === 0}>
					{#snippet children()}
						<CheckSquare class="h-3.5 w-3.5 mr-1.5" />
						Select All
					{/snippet}
				</Button>
				{#if matchedCount > 0}
					<Button
						variant="outline"
						size="sm"
						onclick={selectMatched}
						disabled={matchedCount === 0}
					>
						{#snippet children()}
							<CheckCheck class="h-3.5 w-3.5 mr-1.5" />
							Select Matched
						{/snippet}
					</Button>
				{/if}
				<Button
					variant="outline"
					size="sm"
					onclick={selectNone}
					disabled={selectedFiles.size === 0}
				>
					{#snippet children()}
						<Square class="h-3.5 w-3.5 mr-1.5" />
						Clear
					{/snippet}
				</Button>
			</div>
		</div>
	{/if}

	<!-- File List -->
	<div class="space-y-1">
		{#if loading}
			<div class="text-center py-8 text-muted-foreground">
				<RefreshCw class="h-8 w-8 animate-spin mx-auto mb-2" />
				<p>Loading...</p>
			</div>
		{:else if error}
			<div class="text-center py-8 text-destructive">
				<p>{error}</p>
			</div>
		{:else if items.length === 0}
			<div class="text-center py-8 text-muted-foreground">
				<p>Empty directory</p>
			</div>
		{:else if filteredItems().length === 0}
			<div class="text-center py-8 text-muted-foreground">
				<p>No files or folders match "{filterText}"</p>
				<button onclick={clearFilter} class="text-primary hover:underline text-sm mt-2">
					Clear filter
				</button>
			</div>
		{:else}
			{#each filteredItems() as item}
				{#if item.is_dir}
					<!-- Directories are always clickable -->
					<button
						onclick={() => handleItemClick(item)}
						class="w-full flex items-center gap-3 p-3 rounded-lg transition-all duration-200 cursor-pointer
							border-2 border-transparent hover:border-accent hover:bg-accent/50 hover:shadow-md"
					>
						<Folder class="h-5 w-5 text-blue-500 transition-transform group-hover:scale-110" />
						<div class="flex-1 text-left">
							<div class="font-medium text-blue-600 dark:text-blue-400">
								{item.name}
							</div>
						</div>
					</button>
				{:else if folderOnly}
					<!-- Files in folderOnly mode: non-interactive display -->
					<div
						class="w-full flex items-center gap-3 p-3 rounded-lg border-2 border-transparent opacity-50"
					>
						<File class="h-5 w-5 text-muted-foreground flex-shrink-0" />
						<div class="flex-1 text-left">
							<div class="font-medium text-muted-foreground">
								{item.name}
							</div>
							<div class="text-xs text-muted-foreground mt-0.5">
								{formatBytes(item.size)}
								{#if item.movie_id}
									<span class="ml-2 text-green-600/50 font-medium">• {item.movie_id}</span>
								{/if}
							</div>
						</div>
					</div>
				{:else}
					<!-- Files in normal mode: selectable -->
					<button
						onclick={() => handleItemClick(item)}
						class="w-full flex items-center gap-3 p-3 rounded-lg transition-all duration-200 cursor-pointer
							{selectedFiles.has(item.path)
								? 'bg-primary/10 border-2 border-primary shadow-sm'
								: 'border-2 border-transparent hover:border-accent hover:bg-accent/50 hover:shadow-md'}"
					>
						<!-- Checkbox for files -->
						{#if selectedFiles.has(item.path)}
							<CheckSquare class="h-5 w-5 text-primary flex-shrink-0" />
						{:else}
							<Square class="h-5 w-5 text-muted-foreground flex-shrink-0" />
						{/if}
						<!-- File icon -->
						<File
							class="h-5 w-5 transition-transform group-hover:scale-110 {item.matched ? 'text-green-500' : 'text-muted-foreground'}"
						/>
						<div class="flex-1 text-left">
							<div class="font-medium">
								{item.name}
							</div>
							<div class="text-xs text-muted-foreground mt-0.5">
								{formatBytes(item.size)}
								{#if item.movie_id}
									<span class="ml-2 text-green-600 font-medium">• {item.movie_id}</span>
								{/if}
							</div>
						</div>
					</button>
				{/if}
			{/each}
		{/if}
	</div>

	<!-- Selection Summary (hidden in folderOnly mode) -->
	{#if selectedFiles.size > 0 && !folderOnly}
		<div class="mt-4 pt-4 border-t bg-accent/30 -mx-4 -mb-4 px-4 py-3 rounded-b-lg">
			<div class="flex items-center justify-between mb-2">
				<span class="text-sm font-medium flex items-center gap-2">
					<div class="w-2 h-2 rounded-full bg-primary animate-pulse"></div>
					{selectedFiles.size} file{selectedFiles.size !== 1 ? 's' : ''} selected
				</span>
				<Button variant="ghost" size="sm" onclick={selectNone}>
					{#snippet children()}
						Clear Selection
					{/snippet}
				</Button>
			</div>
			<!-- Selected Files List (collapsible) -->
			<details class="text-xs">
				<summary class="cursor-pointer text-muted-foreground hover:text-foreground transition-colors py-1">
					Show selected files
				</summary>
				<div class="mt-2 space-y-1 max-h-40 overflow-y-auto">
					{#each Array.from(selectedFiles) as filePath}
						{@const fileName = filePath.split('/').pop()}
						<div class="flex items-center justify-between bg-background/50 px-2 py-1 rounded">
							<span class="truncate" title={filePath}>{fileName}</span>
							<button
								onclick={(e) => {
									e.stopPropagation();
									toggleFileSelection(filePath);
								}}
								class="ml-2 text-destructive hover:text-destructive/80 transition-colors"
								title="Remove"
							>
								×
							</button>
						</div>
					{/each}
				</div>
			</details>
		</div>
	{/if}
</Card>
