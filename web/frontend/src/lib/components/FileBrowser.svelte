<script lang="ts">
	import { untrack } from 'svelte';
	import { apiClient } from '$lib/api/client';
	import { formatBytes } from '$lib/utils';
	import { splitPath, buildPathUp, buildBreadcrumbPath, isRootPath } from '$lib/utils/path';
	import type { FileInfo, BrowseResponse } from '$lib/api/types';
	import {
		Folder,
		File,
		House,
		RefreshCw,
		CheckSquare,
		Square,
		CheckCheck,
		Check,
		Search,
		X,
		ArrowUp,
		ArrowDown,
		Calendar,
		HardDrive,
		ArrowUpDown
	} from 'lucide-svelte';
	import Button from './ui/Button.svelte';
	import Card from './ui/Card.svelte';
	import PathInput from './PathInput.svelte';

	interface Props {
		initialPath?: string;
		selectedFiles?: string[];
		onFileSelect?: (files: string[]) => void;
		onPathChange?: (path: string) => void;
		multiSelect?: boolean;
		folderOnly?: boolean;
		onScan?: (path: string, recursive: boolean, visibleFiles: FileInfo[], filter: string, selectedFolders: string[]) => void;
		recursiveScan?: boolean;
		selectedFolders?: string[];
		triggerScan?: number;
		whitelistPaths?: string[];
	}

	let {
		initialPath = '',
		selectedFiles: externalSelectedFiles = $bindable([]),
		onFileSelect,
		onPathChange,
		multiSelect = true,
		folderOnly = false,
		onScan,
		recursiveScan = $bindable(false),
		selectedFolders: selectedFolders = $bindable([]),
		triggerScan = 0,
		whitelistPaths = []
	}: Props = $props();

	let currentPath = $state('');
	let parentPath = $state('');
	let items: FileInfo[] = $state([]);

	let selectedFilesSet = $derived(new Set(externalSelectedFiles));
	let loading = $state(false);
	let error = $state<string | null>(null);
	let filterText = $state('');
	let pathParts = $derived(splitPath(currentPath));

	// Editable path input state
	let pathInputValue = $state('');

	// Sync path state when initialPath changes
	$effect(() => {
		if (initialPath && !currentPath) {
			currentPath = initialPath;
			pathInputValue = initialPath;
		}
	});

	// Scan options
	let anchorFolderPath: string | null = $state(null);
	let anchorFilePath: string | null = $state(null);
	let selectedFoldersSet = $derived(new Set(selectedFolders));

	// Sort state
	type SortField = 'name' | 'mod_time' | 'size';
	type SortDirection = 'asc' | 'desc';
	const SORT_STORAGE_KEY = 'javinizer_filebrowser_sort';
	let sortField = $state<SortField>('name');
	let sortDirection = $state<SortDirection>('asc');

	function loadSortFromStorage() {
		try {
			const stored = sessionStorage.getItem(SORT_STORAGE_KEY);
			if (stored) {
				const parsed = JSON.parse(stored);
				if (parsed.field === 'name' || parsed.field === 'mod_time' || parsed.field === 'size') {
					sortField = parsed.field;
				}
				if (parsed.direction === 'asc' || parsed.direction === 'desc') {
					sortDirection = parsed.direction;
				}
			}
		} catch {}
	}

	function saveSortToStorage() {
		try {
			sessionStorage.setItem(SORT_STORAGE_KEY, JSON.stringify({ field: sortField, direction: sortDirection }));
		} catch {}
	}

	loadSortFromStorage();

	let currentPage = $state(1);
	const PAGE_SIZE = 100;

	// Filter and sort items
	const sortedAndFilteredItems = $derived.by(() => {
		// First filter
		let result = items;
		if (filterText.trim()) {
			const search = filterText.toLowerCase();
			result = items.filter((item) => item.name.toLowerCase().includes(search));
		}

		// Then sort: directories first, then by selected field/direction
		return [...result].sort((a, b) => {
			// Directories always first
			if (a.is_dir && !b.is_dir) return -1;
			if (!a.is_dir && b.is_dir) return 1;

			// Sort by selected field
			let comparison = 0;
			switch (sortField) {
				case 'name':
					comparison = a.name.localeCompare(b.name);
					break;
				case 'mod_time':
					comparison = new Date(a.mod_time).getTime() - new Date(b.mod_time).getTime();
					break;
				case 'size':
					comparison = a.size - b.size;
					break;
			}

			return sortDirection === 'asc' ? comparison : -comparison;
		});
	});

	const pagedItems = $derived.by(() => {
		const start = (currentPage - 1) * PAGE_SIZE;
		return sortedAndFilteredItems.slice(start, start + PAGE_SIZE);
	});

	const totalPages = $derived(Math.max(1, Math.ceil(sortedAndFilteredItems.length / PAGE_SIZE)));

	$effect(() => {
		filterText;
		currentPage = 1;
	});

	// Toggle sort - click same field toggles direction, different field switches to it
	function toggleSort(field: SortField) {
		if (sortField === field) {
			sortDirection = sortDirection === 'asc' ? 'desc' : 'asc';
		} else {
			sortField = field;
			sortDirection = field === 'name' ? 'asc' : 'desc';
		}
		saveSortToStorage();
	}

	function handleScan() {
		const visibleFiles = sortedAndFilteredItems.filter((f) => !f.is_dir);
		onScan?.(currentPath, recursiveScan, visibleFiles, filterText, selectedFolders);
	}

	$effect(() => {
		if (triggerScan > 0) {
			untrack(() => handleScan());
		}
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
		filterText = '';
		selectedFolders = [];
		anchorFolderPath = null;
		try {
			const response: BrowseResponse = await apiClient.browse({ path: path || '/' });
			currentPath = response.current_path;
			parentPath = response.parent_path || '';
			pathInputValue = response.current_path; // Sync path input
			onPathChange?.(currentPath);
			// Items will be sorted by sortedAndFilteredItems derived
			items = response.items;
			currentPage = 1;
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to browse directory';
		} finally {
			loading = false;
		}
	}

	function navigateToPath(index: number) {
		browse(buildBreadcrumbPath(currentPath, index));
	}

	function getFolderPathsSorted(): string[] {
		return sortedAndFilteredItems.filter((i) => i.is_dir).map((i) => i.path);
	}

	function getFilePathsSorted(): string[] {
		return sortedAndFilteredItems.filter((i) => !i.is_dir).map((i) => i.path);
	}

	function handleItemClick(item: FileInfo, event: MouseEvent) {
		if (item.is_dir) {
			if (event.shiftKey && anchorFolderPath !== null) {
				const folderPaths = getFolderPathsSorted();
				const anchorIdx = folderPaths.indexOf(anchorFolderPath);
				const clickedIdx = folderPaths.indexOf(item.path);
				if (anchorIdx >= 0 && clickedIdx >= 0) {
					const start = Math.min(anchorIdx, clickedIdx);
					const end = Math.max(anchorIdx, clickedIdx);
					selectedFolders = folderPaths.slice(start, end + 1);
				} else {
					selectedFolders = [item.path];
					anchorFolderPath = item.path;
				}
			} else if (event.shiftKey) {
				selectedFolders = [item.path];
				anchorFolderPath = item.path;
			} else if (event.ctrlKey || event.metaKey) {
				if (selectedFoldersSet.has(item.path)) {
					selectedFolders = selectedFolders.filter((p) => p !== item.path);
				} else {
					selectedFolders = [...selectedFolders, item.path];
				}
				anchorFolderPath = item.path;
			} else {
				selectedFolders = [];
				anchorFolderPath = null;
				browse(item.path);
			}
		} else if (!folderOnly) {
			if (event.shiftKey && anchorFilePath !== null) {
				const filePaths = getFilePathsSorted();
				const anchorIdx = filePaths.indexOf(anchorFilePath);
				const clickedIdx = filePaths.indexOf(item.path);
				if (anchorIdx >= 0 && clickedIdx >= 0) {
					const start = Math.min(anchorIdx, clickedIdx);
					const end = Math.max(anchorIdx, clickedIdx);
					const rangePaths = filePaths.slice(start, end + 1);
					externalSelectedFiles = [...new Set([...externalSelectedFiles, ...rangePaths])];
				} else {
					toggleFileSelection(item.path);
				}
				anchorFilePath = item.path;
			} else if (event.ctrlKey || event.metaKey || event.altKey) {
				toggleFileSelection(item.path);
				anchorFilePath = item.path;
			} else {
				toggleFileSelection(item.path);
				anchorFilePath = item.path;
			}
		}
	}

	function clearFolderSelection() {
		selectedFolders = [];
		anchorFolderPath = null;
	}

	function toggleFileSelection(path: string) {
		if (multiSelect) {
			if (selectedFilesSet.has(path)) {
				externalSelectedFiles = externalSelectedFiles.filter(f => f !== path);
			} else {
				externalSelectedFiles = [...externalSelectedFiles, path];
			}
		} else {
			externalSelectedFiles = [path];
		}
		onFileSelect?.(externalSelectedFiles);
	}

	function goUp() {
		browse(buildPathUp(currentPath, parentPath));
	}

	function selectAll() {
		const allFiles = sortedAndFilteredItems.filter((item) => !item.is_dir && isVideoFile(item.name)).map((item) => item.path);
		const allFolders = sortedAndFilteredItems.filter((item) => item.is_dir).map((item) => item.path);
		externalSelectedFiles = [...new Set([...externalSelectedFiles, ...allFiles])];
		selectedFolders = [...new Set([...selectedFolders, ...allFolders])];
		if (allFiles.length > 0) {
			anchorFilePath = allFiles[0];
		}
		if (allFolders.length > 0) {
			anchorFolderPath = allFolders[0];
		}
		onFileSelect?.(externalSelectedFiles);
	}

	function selectNone() {
		const visiblePaths = new Set(sortedAndFilteredItems.filter((item) => !item.is_dir && isVideoFile(item.name)).map((item) => item.path));
		externalSelectedFiles = externalSelectedFiles.filter(f => !visiblePaths.has(f));
		selectedFolders = [];
		anchorFolderPath = null;
		anchorFilePath = null;
		onFileSelect?.(externalSelectedFiles);
	}

	function selectMatched() {
		const matchedFiles = sortedAndFilteredItems
			.filter((item) => !item.is_dir && item.matched)
			.map((item) => item.path);
		externalSelectedFiles = [...new Set([...externalSelectedFiles, ...matchedFiles])];
		if (matchedFiles.length > 0) {
			anchorFilePath = matchedFiles[0];
		}
		onFileSelect?.(externalSelectedFiles);
	}

	// Derived state for file counts (based on filtered items)
	const fileCount = $derived(sortedAndFilteredItems.filter((item) => !item.is_dir).length);
	const matchedCount = $derived(sortedAndFilteredItems.filter((item) => !item.is_dir && item.matched).length);
	const folderCount = $derived(sortedAndFilteredItems.filter((item) => item.is_dir).length);
	const VIDEO_EXTENSIONS = new Set([
		'.mp4', '.mkv', '.avi', '.wmv', '.flv', '.mov',
		'.m4v', '.webm', '.mpg', '.mpeg', '.m2ts', '.ts'
	]);

	function isVideoFile(name: string): boolean {
		return VIDEO_EXTENSIONS.has(name.slice(name.lastIndexOf('.')).toLowerCase());
	}

	const selectableFileCount = $derived(sortedAndFilteredItems.filter((i) => !i.is_dir && isVideoFile(i.name)).length);

	const visibleSelectedCount = $derived(
		(() => {
			const visible = sortedAndFilteredItems;
			const filePaths = new Set(externalSelectedFiles);
			const visibleFileSelected = visible.filter((i) => !i.is_dir && isVideoFile(i.name) && filePaths.has(i.path)).length;
			return selectedFolders.length + visibleFileSelected;
		})()
	);

	// Clear filter when navigating to a new directory
	function clearFilter() {
		filterText = '';
		currentPage = 1;
	}

	// Format date for display
	function formatDate(dateStr: string): string {
		const date = new Date(dateStr);
		const now = new Date();
		const diff = now.getTime() - date.getTime();
		const days = Math.floor(diff / (1000 * 60 * 60 * 24));

		if (days === 0) {
			return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
		} else if (days === 1) {
			return 'Yesterday';
		} else if (days < 7) {
			return date.toLocaleDateString([], { weekday: 'short' });
		} else {
			return date.toLocaleDateString([], { month: 'short', day: 'numeric', year: date.getFullYear() !== now.getFullYear() ? 'numeric' : undefined });
		}
	}
</script>

<Card class="p-4">
	<!-- Path Navigation Bar -->
	<div class="flex items-center gap-2 mb-4 pb-4 border-b">
		<Button variant="ghost" size="icon" onclick={() => browse('/')} title="Go to root">
			{#snippet children()}
				<House class="h-4 w-4" />
			{/snippet}
		</Button>

		<Button variant="ghost" size="icon" onclick={goUp} disabled={!currentPath || isRootPath(currentPath)} title="Go up">
			{#snippet children()}
				<span class="text-lg">↑</span>
			{/snippet}
		</Button>

		<Button variant="ghost" size="icon" onclick={() => browse(currentPath)} title="Refresh">
			{#snippet children()}
				<RefreshCw class="h-4 w-4" />
			{/snippet}
		</Button>

		<!-- Editable Path Input -->
		<div class="flex-1 flex items-center gap-2">
			<PathInput
				bind:value={pathInputValue}
				onchange={(v) => { pathInputValue = v; }}
				escapeValue={currentPath}
				onnavigate={(path) => { if (path) browse(path); }}
				showNavigateButton={true}
				navigateDisabled={!pathInputValue.trim() || loading}
				{loading}
				whitelistPaths={whitelistPaths}
			/>
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

	<!-- Sort Controls (hidden in folderOnly mode) -->
	{#if items.length > 0 && !folderOnly}
		<div class="mb-4 pb-4 border-b flex items-center justify-between gap-4">
			<div class="flex items-center gap-2">
				<span class="text-xs text-muted-foreground font-medium">Sort by:</span>
				<div class="flex items-center gap-1">
					<button
						onclick={() => toggleSort('name')}
						class="px-2 py-1 text-xs rounded transition-colors flex items-center gap-1
							{sortField === 'name' ? 'bg-primary text-primary-foreground' : 'bg-accent hover:bg-accent/80'}"
					>
						<ArrowUpDown class="h-3 w-3" />
						Name
						{#if sortField === 'name'}
							{#if sortDirection === 'asc'}
								<ArrowUp class="h-3 w-3" />
							{:else}
								<ArrowDown class="h-3 w-3" />
							{/if}
						{/if}
					</button>
					<button
						onclick={() => toggleSort('mod_time')}
						class="px-2 py-1 text-xs rounded transition-colors flex items-center gap-1
							{sortField === 'mod_time' ? 'bg-primary text-primary-foreground' : 'bg-accent hover:bg-accent/80'}"
					>
						<Calendar class="h-3 w-3" />
						Modified
						{#if sortField === 'mod_time'}
							{#if sortDirection === 'asc'}
								<ArrowUp class="h-3 w-3" />
							{:else}
								<ArrowDown class="h-3 w-3" />
							{/if}
						{/if}
					</button>
					<button
						onclick={() => toggleSort('size')}
						class="px-2 py-1 text-xs rounded transition-colors flex items-center gap-1
							{sortField === 'size' ? 'bg-primary text-primary-foreground' : 'bg-accent hover:bg-accent/80'}"
					>
						<HardDrive class="h-3 w-3" />
						Size
						{#if sortField === 'size'}
							{#if sortDirection === 'asc'}
								<ArrowUp class="h-3 w-3" />
							{:else}
								<ArrowDown class="h-3 w-3" />
							{/if}
						{/if}
					</button>
				</div>
			</div>
			<!-- Selection controls -->
			{#if !folderOnly && (selectableFileCount > 0 || folderCount > 0)}
				<div class="flex items-center justify-between gap-3">
					<div class="flex items-center gap-3">
						{#if visibleSelectedCount > 0}
							<span class="text-xs text-primary font-medium">{visibleSelectedCount} selected</span>
						{/if}
					</div>
					<div class="flex items-center gap-2">
						<Button variant="outline" size="sm" onclick={selectAll} disabled={selectableFileCount === 0 && folderCount === 0}>
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
							disabled={visibleSelectedCount === 0}
						>
							{#snippet children()}
								<Square class="h-3.5 w-3.5 mr-1.5" />
								Clear
							{/snippet}
						</Button>
					</div>
				</div>
			{/if}
		</div>
	{/if}

	<!-- Pagination Controls (top) -->
	{#if items.length > 0 && totalPages > 1}
		<div class="mb-4 pb-4 border-b flex items-center justify-between gap-4">
			<span class="text-xs text-muted-foreground">
				Page {currentPage} of {totalPages} ({sortedAndFilteredItems.length} items)
			</span>
			<div class="flex items-center gap-2">
				<Button variant="outline" size="sm" onclick={() => currentPage = 1} disabled={currentPage === 1}>
					{#snippet children()}&laquo;{/snippet}
				</Button>
				<Button variant="outline" size="sm" onclick={() => currentPage--} disabled={currentPage === 1}>
					{#snippet children()}&lsaquo;{/snippet}
				</Button>
				<Button variant="outline" size="sm" onclick={() => currentPage++} disabled={currentPage === totalPages}>
					{#snippet children()}&rsaquo;{/snippet}
				</Button>
				<Button variant="outline" size="sm" onclick={() => currentPage = totalPages} disabled={currentPage === totalPages}>
					{#snippet children()}&raquo;{/snippet}
				</Button>
			</div>
		</div>
	{/if}

	<!-- File List -->
	<!-- max-h + overflow-y-auto: cap the list height so a long browse page (PAGE_SIZE=100)
	     can't stretch the persistent layout wrapper ~6000px and bleed that height into other
	     routes (e.g. /jobs) after browse -> scrape navigations. -->
	<div class="space-y-1 max-h-[70vh] overflow-y-auto pr-1">
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
		{:else if sortedAndFilteredItems.length === 0}
			<div class="text-center py-8 text-muted-foreground">
				<p>No files or folders match "{filterText}"</p>
				<button onclick={clearFilter} class="text-primary hover:underline text-sm mt-2">
					Clear filter
				</button>
			</div>
		{:else}
			{#each pagedItems as item (item.path)}
				<div>
					{#if item.is_dir}
					<button
						onclick={(e) => handleItemClick(item, e)}
						class="group w-full flex items-center gap-3 p-3 rounded-lg transition-all duration-200 cursor-pointer
							{selectedFoldersSet.has(item.path)
								? 'bg-primary/10 border-2 border-primary shadow-sm'
								: 'border-2 border-transparent hover:border-accent hover:bg-accent/50 hover:shadow-md'}"
					>
						{#if selectedFolders.length > 0}
							{#if selectedFoldersSet.has(item.path)}
								<CheckSquare class="h-5 w-5 text-primary shrink-0" />
							{:else}
								<Square class="h-5 w-5 text-muted-foreground shrink-0" />
							{/if}
						{/if}
						<Folder class="h-5 w-5 text-blue-500 transition-transform group-hover:scale-110" />
						<div class="flex-1 text-left">
							<div class="font-medium text-blue-600 dark:text-blue-400">
								{item.name}
							</div>
							<div class="text-xs text-muted-foreground mt-0.5">
								{formatDate(item.mod_time)}
							</div>
						</div>
					</button>
				{:else if folderOnly}
					<!-- Files in folderOnly mode: non-interactive display -->
					<div
						class="w-full flex items-center gap-3 p-3 rounded-lg border-2 border-transparent opacity-50"
					>
						<File class="h-5 w-5 text-muted-foreground shrink-0" />
						<div class="flex-1 text-left">
							<div class="font-medium text-muted-foreground">
								{item.name}
							</div>
							<div class="text-xs text-muted-foreground mt-0.5">
								{formatBytes(item.size)} • {formatDate(item.mod_time)}
								{#if item.movie_id}
									<span class="ml-2 text-green-600/50 font-medium">• {item.movie_id}</span>
								{/if}
							</div>
						</div>
					</div>
					{:else}
					{#if isVideoFile(item.name)}
					<button
						onclick={(e) => handleItemClick(item, e)}
						class="group w-full flex items-center gap-3 p-3 rounded-lg transition-all duration-200 cursor-pointer
							{selectedFilesSet.has(item.path)
								? 'bg-primary/10 border-2 border-primary shadow-sm'
								: 'border-2 border-transparent hover:border-accent hover:bg-accent/50 hover:shadow-md'}"
					>
						{#if selectedFilesSet.has(item.path)}
							<CheckSquare class="h-5 w-5 text-primary shrink-0" />
						{:else}
							<Square class="h-5 w-5 text-muted-foreground shrink-0" />
						{/if}
						<File
							class="h-5 w-5 transition-transform group-hover:scale-110 text-green-500"
						/>
						<div class="flex-1 text-left">
							<div class="font-medium">
								{item.name}
							</div>
							<div class="text-xs text-muted-foreground mt-0.5">
								{formatBytes(item.size)} • {formatDate(item.mod_time)}
								{#if item.movie_id}
									<span class="ml-2 text-green-600 font-medium">• {item.movie_id}</span>
								{/if}
							</div>
						</div>
					</button>
					{:else}
					<div
						class="w-full flex items-center gap-3 p-3 rounded-lg border-2 border-transparent opacity-50"
					>
						<File class="h-5 w-5 text-muted-foreground shrink-0" />
						<div class="flex-1 text-left">
							<div class="font-medium text-muted-foreground">
								{item.name}
							</div>
							<div class="text-xs text-muted-foreground mt-0.5">
								{formatBytes(item.size)} • {formatDate(item.mod_time)}
							</div>
						</div>
					</div>
					{/if}
					{/if}
				</div>
			{/each}
			{#if totalPages > 1}
				<div class="flex items-center justify-between pt-4 border-t mt-4">
					<span class="text-xs text-muted-foreground">
						Page {currentPage} of {totalPages} ({sortedAndFilteredItems.length} items)
					</span>
					<div class="flex items-center gap-2">
						<Button variant="outline" size="sm" onclick={() => currentPage = 1} disabled={currentPage === 1}>
							{#snippet children()}&laquo;{/snippet}
						</Button>
						<Button variant="outline" size="sm" onclick={() => currentPage--} disabled={currentPage === 1}>
							{#snippet children()}&lsaquo;{/snippet}
						</Button>
						<Button variant="outline" size="sm" onclick={() => currentPage++} disabled={currentPage === totalPages}>
							{#snippet children()}&rsaquo;{/snippet}
						</Button>
						<Button variant="outline" size="sm" onclick={() => currentPage = totalPages} disabled={currentPage === totalPages}>
							{#snippet children()}&raquo;{/snippet}
						</Button>
					</div>
				</div>
			{/if}
		{/if}
	</div>
</Card>
