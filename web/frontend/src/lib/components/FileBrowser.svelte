<script lang="ts">
	import { flip } from 'svelte/animate';
	import { cubicOut } from 'svelte/easing';
	import { apiClient } from '$lib/api/client';
	import { formatBytes } from '$lib/utils';
	import type { FileInfo, BrowseResponse, PathAutocompleteSuggestion } from '$lib/api/types';
	import {
		Folder,
		File,
		ChevronRight,
		House,
		RefreshCw,
		CheckSquare,
		Square,
		CheckCheck,
		Check,
		Search,
		X,
		LoaderCircle,
		Scan,
		ArrowUp,
		ArrowDown,
		Calendar,
		HardDrive,
		ArrowUpDown,
		ArrowRight
	} from 'lucide-svelte';
	import Button from './ui/Button.svelte';
	import Card from './ui/Card.svelte';

	interface Props {
		initialPath?: string;
		selectedFiles?: string[];  // Bindable selected files array
		onFileSelect?: (files: string[]) => void;
		onPathChange?: (path: string) => void;
		multiSelect?: boolean;
		folderOnly?: boolean;  // When true, only allows folder navigation (no file selection)
		onScan?: (path: string, recursive: boolean, visibleFiles: FileInfo[], filter: string) => void;  // Unified scan callback with filter
		scanLoading?: boolean;  // External loading state
	}

	let {
		initialPath = '',
		selectedFiles: externalSelectedFiles = $bindable([]),
		onFileSelect,
		onPathChange,
		multiSelect = true,
		folderOnly = false,
		onScan,
		scanLoading = false
	}: Props = $props();

	let currentPath = $state('');
	let items: FileInfo[] = $state([]);

	// Internal Set derived from external array for efficient lookups
	let selectedFilesSet = $derived(new Set(externalSelectedFiles));
	let loading = $state(false);
	let error = $state<string | null>(null);
	let filterText = $state('');
	let pathParts = $derived(currentPath.split('/').filter((p) => p));

	// Editable path input state
	let pathInputValue = $state('');
	let isPathEditing = $state(false);
	let pathSuggestions = $state<PathAutocompleteSuggestion[]>([]);
	let showPathSuggestions = $state(false);
	let activeSuggestionIndex = $state(-1);
	let autocompleteLoading = $state(false);
	let autocompleteDebounceId: ReturnType<typeof setTimeout> | null = null;
	let autocompleteRequestToken = 0;

	const pathAutocompleteLimit = 8;

	// Sync path state when initialPath changes
	$effect(() => {
		if (initialPath && !currentPath) {
			currentPath = initialPath;
			pathInputValue = initialPath;
		}
	});

	// Scan options
	let recursiveScan = $state(false);

	// Sort state
	type SortField = 'name' | 'mod_time' | 'size';
	type SortDirection = 'asc' | 'desc';
	let sortField = $state<SortField>('name');
	let sortDirection = $state<SortDirection>('asc');

	// Filter and sort items
	const sortedAndFilteredItems = $derived(() => {
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

	// Toggle sort - click same field toggles direction, different field switches to it
	function toggleSort(field: SortField) {
		if (sortField === field) {
			sortDirection = sortDirection === 'asc' ? 'desc' : 'asc';
		} else {
			sortField = field;
			// Default direction: name=asc, date/size=desc (most recent/largest first)
			sortDirection = field === 'name' ? 'asc' : 'desc';
		}
	}

	// Handle scan button click
	function handleScan() {
		const visibleFiles = sortedAndFilteredItems().filter((f) => !f.is_dir);
		onScan?.(currentPath, recursiveScan, visibleFiles, filterText);
	}

	function clearPathSuggestions() {
		autocompleteRequestToken += 1;
		pathSuggestions = [];
		showPathSuggestions = false;
		activeSuggestionIndex = -1;
		autocompleteLoading = false;
	}

	// Navigate to path from input
	function navigateToInputPath() {
		if (pathInputValue.trim()) {
			clearPathSuggestions();
			browse(pathInputValue.trim());
		}
	}

	function selectPathSuggestion(suggestion: PathAutocompleteSuggestion) {
		pathInputValue = suggestion.path;
		clearPathSuggestions();
		browse(suggestion.path);
	}

	async function fetchPathSuggestions(inputPath: string) {
		const requestToken = ++autocompleteRequestToken;
		autocompleteLoading = true;

		try {
			const response = await apiClient.autocompletePath({
				path: inputPath,
				limit: pathAutocompleteLimit
			});

			if (requestToken !== autocompleteRequestToken || !isPathEditing) return;

			pathSuggestions = response.suggestions;
			showPathSuggestions = response.suggestions.length > 0;
			activeSuggestionIndex = response.suggestions.length > 0 ? 0 : -1;
		} catch {
			if (requestToken !== autocompleteRequestToken) return;
			pathSuggestions = [];
			showPathSuggestions = false;
			activeSuggestionIndex = -1;
		} finally {
			if (requestToken === autocompleteRequestToken) {
				autocompleteLoading = false;
			}
		}
	}

	// Handle path input keydown
	function handlePathKeydown(e: KeyboardEvent) {
		if (e.key === 'ArrowDown' && pathSuggestions.length > 0) {
			e.preventDefault();
			showPathSuggestions = true;
			activeSuggestionIndex =
				activeSuggestionIndex >= pathSuggestions.length - 1 ? 0 : activeSuggestionIndex + 1;
		} else if (e.key === 'ArrowUp' && pathSuggestions.length > 0) {
			e.preventDefault();
			showPathSuggestions = true;
			activeSuggestionIndex =
				activeSuggestionIndex <= 0 ? pathSuggestions.length - 1 : activeSuggestionIndex - 1;
		} else if (e.key === 'Enter') {
			if (showPathSuggestions && activeSuggestionIndex >= 0 && pathSuggestions[activeSuggestionIndex]) {
				e.preventDefault();
				selectPathSuggestion(pathSuggestions[activeSuggestionIndex]);
				return;
			}
			navigateToInputPath();
		} else if (e.key === 'Escape') {
			pathInputValue = currentPath;
			isPathEditing = false;
			clearPathSuggestions();
		}
	}

	// Watch for changes to initialPath and browse when it's set
	$effect(() => {
		if (initialPath) {
			browse(initialPath);
		}
	});

	$effect(() => {
		const inputPath = pathInputValue.trim();

		if (autocompleteDebounceId) {
			clearTimeout(autocompleteDebounceId);
			autocompleteDebounceId = null;
		}

		if (!isPathEditing || !inputPath) {
			clearPathSuggestions();
			return;
		}

		autocompleteDebounceId = setTimeout(() => {
			void fetchPathSuggestions(inputPath);
		}, 160);

		return () => {
			if (autocompleteDebounceId) {
				clearTimeout(autocompleteDebounceId);
				autocompleteDebounceId = null;
			}
		};
	});

	async function browse(path: string) {
		loading = true;
		error = null;
		clearPathSuggestions();
		filterText = ''; // Clear filter when navigating
		try {
			const response: BrowseResponse = await apiClient.browse({ path: path || '/' });
			currentPath = response.current_path;
			pathInputValue = response.current_path; // Sync path input
			isPathEditing = false; // Reset editing state
			onPathChange?.(currentPath);
			// Items will be sorted by sortedAndFilteredItems derived
			items = response.items;
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
			// Don't clear selections when navigating folders
		} else if (!folderOnly) {
			// Only allow file selection if not in folderOnly mode
			toggleFileSelection(item.path);
		}
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
		const parts = currentPath.split('/').filter((p) => p);
		parts.pop();
		const newPath = parts.length > 0 ? '/' + parts.join('/') : '/';
		browse(newPath);
	}

	function selectAll() {
		// Select all visible (filtered) files, merging with existing
		const allFiles = sortedAndFilteredItems().filter((item) => !item.is_dir).map((item) => item.path);
		externalSelectedFiles = [...new Set([...externalSelectedFiles, ...allFiles])];
		onFileSelect?.(externalSelectedFiles);
	}

	function selectNone() {
		// Clear only files visible in current directory
		const visiblePaths = new Set(sortedAndFilteredItems().filter((item) => !item.is_dir).map((item) => item.path));
		externalSelectedFiles = externalSelectedFiles.filter(f => !visiblePaths.has(f));
		onFileSelect?.(externalSelectedFiles);
	}

	function selectMatched() {
		// Select all visible (filtered) matched files, merging with existing
		const matchedFiles = sortedAndFilteredItems()
			.filter((item) => !item.is_dir && item.matched)
			.map((item) => item.path);
		externalSelectedFiles = [...new Set([...externalSelectedFiles, ...matchedFiles])];
		onFileSelect?.(externalSelectedFiles);
	}

	// Derived state for file counts (based on filtered items)
	const fileCount = $derived(sortedAndFilteredItems().filter((item) => !item.is_dir).length);
	const matchedCount = $derived(sortedAndFilteredItems().filter((item) => !item.is_dir && item.matched).length);
	const folderCount = $derived(sortedAndFilteredItems().filter((item) => item.is_dir).length);

	// Clear filter when navigating to a new directory
	function clearFilter() {
		filterText = '';
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

		<Button variant="ghost" size="icon" onclick={goUp} disabled={currentPath === '/'} title="Go up">
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
			<div class="relative flex-1">
				<input
					type="text"
					bind:value={pathInputValue}
					onkeydown={handlePathKeydown}
					onfocus={() => isPathEditing = true}
					onblur={() => {
						isPathEditing = false;
						setTimeout(() => {
							showPathSuggestions = false;
						}, 120);
					}}
					placeholder="Enter path (e.g., /path/to/videos)"
					class="w-full px-3 py-1.5 pr-9 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono text-sm"
				/>
				{#if autocompleteLoading}
					<div class="absolute inset-y-0 right-3 flex items-center text-muted-foreground">
						<LoaderCircle class="h-3.5 w-3.5 animate-spin" />
					</div>
				{/if}

				{#if showPathSuggestions && pathSuggestions.length > 0}
					<div class="absolute z-20 mt-2 w-full rounded-lg border bg-background shadow-lg overflow-hidden">
						<div class="max-h-64 overflow-y-auto py-1">
							{#each pathSuggestions as suggestion, index (suggestion.path)}
								<button
									type="button"
									onmousedown={(event) => {
										event.preventDefault();
										selectPathSuggestion(suggestion);
									}}
									class="w-full flex items-center gap-2 px-3 py-2 text-left text-sm transition-colors
										{index === activeSuggestionIndex ? 'bg-accent text-accent-foreground' : 'hover:bg-accent/60'}"
								>
									<Folder class="h-4 w-4 text-blue-500 shrink-0" />
									<div class="min-w-0 flex-1">
										<div class="truncate font-medium">{suggestion.name}</div>
										<div class="truncate text-xs text-muted-foreground font-mono">{suggestion.path}</div>
									</div>
								</button>
							{/each}
						</div>
					</div>
				{/if}
			</div>
			<Button variant="outline" size="sm" onclick={navigateToInputPath} disabled={!pathInputValue.trim() || loading} title="Navigate to path">
				{#snippet children()}
					<ArrowRight class="h-4 w-4" />
				{/snippet}
			</Button>
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
			<!-- Scan controls (only if onScan is provided) -->
			{#if onScan}
				<div class="flex items-center gap-3">
					<label class="flex items-center gap-1.5 text-xs cursor-pointer">
						<input
							type="checkbox"
							bind:checked={recursiveScan}
							class="h-3.5 w-3.5 rounded border-gray-300 text-primary focus:ring-1 focus:ring-primary"
						/>
						<span class="text-muted-foreground">Recursive</span>
					</label>
					<Button
						variant="default"
						size="sm"
						onclick={handleScan}
						disabled={scanLoading}
						title={recursiveScan ? "Scan all subfolders" : "Scan current folder only"}
					>
						{#snippet children()}
							{#if scanLoading}
								<LoaderCircle class="h-3.5 w-3.5 mr-1.5 animate-spin" />
							{:else}
								<Scan class="h-3.5 w-3.5 mr-1.5" />
							{/if}
							{scanLoading ? 'Scanning...' : 'Scan'}
						{/snippet}
					</Button>
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
					disabled={externalSelectedFiles.length === 0}
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
		{:else if sortedAndFilteredItems().length === 0}
			<div class="text-center py-8 text-muted-foreground">
				<p>No files or folders match "{filterText}"</p>
				<button onclick={clearFilter} class="text-primary hover:underline text-sm mt-2">
					Clear filter
				</button>
			</div>
		{:else}
			{#each sortedAndFilteredItems() as item (item.path)}
				<div animate:flip={{ duration: 220, easing: cubicOut }}>
					{#if item.is_dir}
					<!-- Directories are always clickable -->
					<button
						onclick={() => handleItemClick(item)}
						class="group w-full flex items-center gap-3 p-3 rounded-lg transition-all duration-200 cursor-pointer
							border-2 border-transparent hover:border-accent hover:bg-accent/50 hover:shadow-md"
					>
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
					<!-- Files in normal mode: selectable -->
					<button
						onclick={() => handleItemClick(item)}
						class="group w-full flex items-center gap-3 p-3 rounded-lg transition-all duration-200 cursor-pointer
							{selectedFilesSet.has(item.path)
								? 'bg-primary/10 border-2 border-primary shadow-sm'
								: 'border-2 border-transparent hover:border-accent hover:bg-accent/50 hover:shadow-md'}"
					>
						<!-- Checkbox for files -->
						{#if selectedFilesSet.has(item.path)}
							<CheckSquare class="h-5 w-5 text-primary shrink-0" />
						{:else}
							<Square class="h-5 w-5 text-muted-foreground shrink-0" />
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
								{formatBytes(item.size)} • {formatDate(item.mod_time)}
								{#if item.movie_id}
									<span class="ml-2 text-green-600 font-medium">• {item.movie_id}</span>
								{/if}
							</div>
						</div>
					</button>
					{/if}
				</div>
			{/each}
		{/if}
	</div>
</Card>
