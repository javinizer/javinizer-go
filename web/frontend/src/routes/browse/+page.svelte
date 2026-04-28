<script lang="ts">
	import { untrack } from 'svelte';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { flip } from 'svelte/animate';
	import { quintOut } from 'svelte/easing';
	import { fade, scale, slide } from 'svelte/transition';
	import { portalToBody } from '$lib/actions/portal';
	import FileBrowser from '$lib/components/FileBrowser.svelte';
	import ProgressModal from '$lib/components/ProgressModal.svelte';
	import BackgroundJobIndicator from '$lib/components/BackgroundJobIndicator.svelte';
	import ScraperSelector from '$lib/components/ScraperSelector.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import { apiClient } from '$lib/api/client';
	import { toastStore } from '$lib/stores/toast';
	import { createConfigQuery, createScrapersQuery } from '$lib/query/queries';
	import { Play, FolderOutput, FolderOpen, FileEdit, FileText, RotateCcw, LoaderCircle, RefreshCw, Settings, ChevronUp, ChevronDown, X, Scan } from 'lucide-svelte';
	import type { Scraper, FileInfo, Config } from '$lib/api/types';
	import type { OperationMode } from '$lib/api/types';

	type BrowseMode = 'scrape' | 'update';
	let selectedFiles: string[] = $state([]);
	let currentJobId: string | null = $state(null);
	let showProgress = $state(false);
	let scraping = $state(false);
	let forceRefresh = $state(false);
	let operationMode: BrowseMode = $state('scrape');
	let scanning = $state(false);
	let recursiveScan = $state(false);
	let selectedFolders: string[] = $state([]);
	let triggerScan = $state(0);
	let initialPath = $state('');
	let destinationPath = $state('');
	let showDestinationBrowser = $state(false);
	let tempDestinationPath = $state('');
	let currentBrowserPath = $state('');
	const configQuery = createConfigQuery();
	const scrapersQuery = createScrapersQuery();
	const queryClient = useQueryClient();
	const cwdQuery = createQuery(() => ({
		queryKey: ['cwd'],
		queryFn: () => apiClient.getCurrentWorkingDirectory(),
	}));

	let config = $derived(configQuery.data ?? null);
	let availableScrapers = $derived(scrapersQuery.data ?? []);
	let selectedScrapers: string[] = $state([]);
	let showScraperSelector = $state(false);
	let scrapersInitialized = $state(false);

	$effect(() => {
		const scrapers = scrapersQuery.data;
		if (scrapers && scrapers.length > 0) {
			untrack(() => {
				if (!scrapersInitialized) {
					scrapersInitialized = true;
					selectedScrapers = scrapers.filter((s) => s.enabled).map((s) => s.name);
				}
			});
		}
	});

	$effect(() => {
		const cwd = cwdQuery.data?.path;
		if (!cwd) return;
		const savedInputPath = localStorage.getItem(STORAGE_KEY_INPUT);
		if (!initialPath) {
			initialPath = savedInputPath || cwd;
		}
		const savedOutputPath = localStorage.getItem(STORAGE_KEY_OUTPUT);
		if (!destinationPath) {
			destinationPath = savedOutputPath || initialPath;
		}
	});
	type ScalarStrategy = 'prefer-nfo' | 'prefer-scraper' | 'preserve-existing' | 'fill-missing-only';
	type ArrayStrategy = 'merge' | 'replace';

	let selectedPreset: string | undefined = $state(undefined);  // Merge strategy preset: conservative, gap-fill, aggressive
	let scalarStrategy: ScalarStrategy = $state('prefer-nfo');  // For scalar fields
	let arrayStrategy: ArrayStrategy = $state('merge');        // For array fields
	let showOptionsPanel = $state(false);  // Expandable options panel in sticky bar
	let operationModeOverride: OperationMode = $state('organize');
	let operationModeOverrideTouched: boolean = $state(false);

	function getSettingsOperationMode(): OperationMode {
		if (config) {
			const mode = config.output?.operation_mode;
			if (mode && typeof mode === 'string') {
				return mode as OperationMode;
			}
		}
		return 'organize';
	}

	let isInPlaceImplied: boolean = $derived.by(() => {
		if (destinationPath.trim() === '' || destinationPath.trim() !== initialPath.trim()) return false;
		const output = config?.output as Record<string, any> | undefined;
		if (output?.folder_format) return false;
		if (output?.subfolder_format && output.subfolder_format.length > 0) return false;
		return true;
	});

	let effectiveOperationMode: OperationMode = $derived(
		isInPlaceImplied && (operationModeOverride === 'organize' || operationModeOverride === 'in-place')
			? 'in-place-norenamefolder'
			: (operationModeOverrideTouched ? operationModeOverride : getSettingsOperationMode())
	);

	// localStorage keys
	const STORAGE_KEY_INPUT = 'javinizer_input_path';
	const STORAGE_KEY_OUTPUT = 'javinizer_output_path';
	const STORAGE_KEY_RECURSIVE = 'javinizer_filebrowser_recursive';

	// Load recursive scan from sessionStorage
	try {
		if (sessionStorage.getItem(STORAGE_KEY_RECURSIVE) === 'true') {
			recursiveScan = true;
		}
	} catch {}

	$effect(() => {
		recursiveScan;
		try {
			sessionStorage.setItem(STORAGE_KEY_RECURSIVE, String(recursiveScan));
		} catch {}
	});




	function handleFileSelect(files: string[]) {
		selectedFiles = files;
	}

	function handleBrowserPathChange(path: string) {
		currentBrowserPath = path;
		// Save to localStorage for persistence
		localStorage.setItem(STORAGE_KEY_INPUT, path);
	}

	// Unified scan handler - handles both recursive and non-recursive scans
	// filter: when provided with recursive scan, only scans directories/files matching the filter (case-insensitive)
	async function handleScan(path: string, recursive: boolean, visibleFiles: FileInfo[], filter: string = '', selectedFolders: string[] = []) {
		if (!path.trim()) return;

		scanning = true;
		try {
			if (recursive && selectedFolders.length > 0) {
				const scanPromises = selectedFolders.map(folderPath =>
					apiClient.scan({ path: folderPath, recursive: true, filter: filter || undefined })
				);
				const settled = await Promise.allSettled(scanPromises);
				const seenPaths = new Set<string>();
				const allMatched: string[] = [];
				const failedFolders: string[] = [];
				let fulfilledCount = 0;
				for (let i = 0; i < settled.length; i++) {
					const result = settled[i];
					if (result.status === 'fulfilled') {
						fulfilledCount++;
						for (const f of result.value.files) {
							if (f.matched && !f.is_dir && !seenPaths.has(f.path)) {
								seenPaths.add(f.path);
								allMatched.push(f.path);
							}
						}
					} else {
						failedFolders.push(selectedFolders[i]);
					}
				}
				if (allMatched.length > 0) {
					selectedFiles = [...new Set([...selectedFiles, ...allMatched])];
					const failedInfo = failedFolders.length > 0 ? ` (${failedFolders.length} folder${failedFolders.length !== 1 ? 's' : ''} failed)` : '';
					toastStore.success(
						`Added ${allMatched.length} JAV file${allMatched.length !== 1 ? 's' : ''} from ${fulfilledCount} folder${fulfilledCount !== 1 ? 's' : ''}${failedInfo}`,
						3000
					);
				} else if (failedFolders.length === selectedFolders.length) {
					toastStore.error(`All ${failedFolders.length} folder scan${failedFolders.length !== 1 ? 's' : ''} failed`, 5000);
				} else if (failedFolders.length > 0) {
					toastStore.warning(`No JAV files found in ${fulfilledCount} folder${fulfilledCount !== 1 ? 's' : ''}; ${failedFolders.length} folder${failedFolders.length !== 1 ? 's' : ''} failed`, 5000);
				} else {
					toastStore.warning(`No JAV files found in selected folders`, 5000);
				}
			} else {
				const response = await apiClient.scan({
					path: path,
					recursive: recursive,
					filter: recursive ? filter : undefined
				});

				let matchedFiles: string[];

				if (recursive) {
					matchedFiles = response.files
						.filter((f) => f.matched && !f.is_dir)
						.map((f) => f.path);
				} else {
					const visibleFilePaths = new Set(visibleFiles.map((f) => f.path));
					matchedFiles = response.files
						.filter((f) => f.matched && !f.is_dir && visibleFilePaths.has(f.path))
						.map((f) => f.path);
				}

				if (matchedFiles.length > 0) {
					selectedFiles = [...new Set([...selectedFiles, ...matchedFiles])];
					const scanType = recursive ? 'recursive' : 'current folder';
					const filterInfo = recursive && filter ? ` matching "${filter}"` : '';
					toastStore.success(
						`Added ${matchedFiles.length} JAV file${matchedFiles.length !== 1 ? 's' : ''}${filterInfo} (${scanType})`,
						3000
					);
				} else {
					if (!recursive) {
						const totalMatched = response.files.filter((f) => f.matched && !f.is_dir).length;
						if (totalMatched > 0) {
							toastStore.warning(`No JAV files match current filter (${totalMatched} found in folder)`, 5000);
							return;
						}
					}
					const filterInfo = recursive && filter ? ` matching "${filter}"` : '';
					toastStore.warning(`No JAV files found${filterInfo}${recursive ? ' in any subfolder' : ''}`, 5000);
				}
			}
		} catch (error) {
			toastStore.error(error instanceof Error ? error.message : 'Failed to scan directory', 5000);
		} finally {
			scanning = false;
		}
	}

	// Apply preset to scalar and array strategies
	function applyPreset(preset: string) {
		selectedPreset = preset;
		switch (preset) {
			case 'conservative':
				scalarStrategy = 'preserve-existing';
				arrayStrategy = 'merge';
				break;
			case 'gap-fill':
				scalarStrategy = 'fill-missing-only';
				arrayStrategy = 'merge';
				break;
			case 'aggressive':
				scalarStrategy = 'prefer-scraper';
				arrayStrategy = 'replace';
				break;
		}
	}

	async function startBatchScrape() {
		if (selectedFiles.length === 0) return;

		const isUpdateMode = operationMode === 'update';
		scraping = true;
		try {
			const response = await apiClient.batchScrape({
				files: selectedFiles,
				strict: false,
				force: forceRefresh,
				destination: isUpdateMode ? undefined : (destinationPath.trim() || undefined),
				update: isUpdateMode,
				selected_scrapers: showScraperSelector ? selectedScrapers : undefined,
				preset: isUpdateMode ? (selectedPreset as 'conservative' | 'gap-fill' | 'aggressive' | undefined) : undefined,
				scalar_strategy: isUpdateMode ? scalarStrategy : undefined,
				array_strategy: isUpdateMode ? arrayStrategy : undefined,
				operation_mode: effectiveOperationMode,
			});
			currentJobId = response.job_id;
			void queryClient.invalidateQueries({ queryKey: ['batch-jobs'] });

			const modeText = isUpdateMode ? 'Updating metadata' : 'Batch scraping';
			toastStore.success(
				`${modeText} started for ${selectedFiles.length} file${selectedFiles.length !== 1 ? 's' : ''}`,
				5000
			);

			showProgress = true;
		} catch (error) {
			// Show error toast
			const errorMessage = error instanceof Error ? error.message : 'Failed to start batch operation';
			toastStore.error(errorMessage, 7000);
		} finally {
			scraping = false;
		}
	}

	function closeProgress() {
		showProgress = false;
		// Keep the job ID so user can reopen if needed
	}

	function reopenProgress() {
		showProgress = true;
	}

	function dismissBackgroundIndicator() {
		currentJobId = null;
	}

	function openDestinationBrowser() {
		tempDestinationPath = destinationPath;
		showDestinationBrowser = true;
	}

	function handleDestinationSelect(files: string[]) {
		// This is called when navigating - we'll ignore file selections
		// and just track the current path from the browser
	}

	function handleDestinationPathChange(path: string) {
		tempDestinationPath = path;
	}

	function confirmDestination() {
		destinationPath = tempDestinationPath;
		// Save to localStorage for persistence
		localStorage.setItem(STORAGE_KEY_OUTPUT, tempDestinationPath);
		showDestinationBrowser = false;
	}

	function cancelDestination() {
		showDestinationBrowser = false;
	}

	async function resetDirectories() {
		// Clear localStorage
		localStorage.removeItem(STORAGE_KEY_INPUT);
		localStorage.removeItem(STORAGE_KEY_OUTPUT);
		// Reset to working directory
		try {
			const response = await apiClient.getCurrentWorkingDirectory();
			initialPath = response.path;
			destinationPath = response.path;
		} catch (error) {
			console.error('Failed to get current working directory:', error);
		}
	}
</script>

<div class="container mx-auto px-4 py-8 pb-32">
	<div class="max-w-7xl mx-auto space-y-6">
		<!-- Header -->
		<div class="flex items-center justify-between">
			<div>
				<h1 class="text-3xl font-bold">Browse & Scrape</h1>
				<p class="text-muted-foreground mt-1">
					Select video files and scrape metadata from configured sources
				</p>
			</div>
			<div class="flex gap-2">
				<Button variant="outline" onclick={resetDirectories}>
					{#snippet children()}
						<RotateCcw class="h-4 w-4 mr-2" />
						Reset Paths
					{/snippet}
				</Button>
			</div>
		</div>

		<!-- Operation Mode Selection -->
		<Card class="p-4">
			<div class="space-y-3">
				<h3 class="font-semibold">Operation Mode</h3>
				<div class="grid grid-cols-2 gap-3">
					<button
						onclick={() => operationMode = 'scrape'}
						class="flex flex-col items-start gap-2 p-4 rounded-lg border-2 transition-all {operationMode === 'scrape' ? 'border-primary bg-primary/5' : 'border-border hover:border-primary/50'}"
					>
						<div class="flex items-center gap-2">
							<Play class="h-5 w-5 {operationMode === 'scrape' ? 'text-primary' : 'text-muted-foreground'}" />
							<span class="font-medium {operationMode === 'scrape' ? 'text-primary' : ''}">Scrape & Organize</span>
						</div>
						<p class="text-xs text-muted-foreground text-left">
							Scrape metadata and organize files into destination folder with artwork and NFO
						</p>
					</button>

					<button
						onclick={() => operationMode = 'update'}
						class="flex flex-col items-start gap-2 p-4 rounded-lg border-2 transition-all {operationMode === 'update' ? 'border-primary bg-primary/5' : 'border-border hover:border-primary/50'}"
					>
						<div class="flex items-center gap-2">
							<RefreshCw class="h-5 w-5 {operationMode === 'update' ? 'text-primary' : 'text-muted-foreground'}" />
							<span class="font-medium {operationMode === 'update' ? 'text-primary' : ''}">Update Metadata</span>
						</div>
						<p class="text-xs text-muted-foreground text-left">
							Update metadata and media files in place, video files remain where they are
						</p>
					</button>
				</div>
			</div>
		</Card>


	<!-- Merge Strategy Selection (only shown in update mode) -->
	{#if operationMode === 'update'}
		<div transition:slide|local={{ duration: 220, easing: quintOut }}>
		<Card class="p-4">
			<div class="space-y-4">
				<div>
					<h3 class="font-semibold">NFO Merge Strategy</h3>
					<p class="text-sm text-muted-foreground">Choose how to merge existing NFO data with freshly scraped data</p>
				</div>

				<!-- Preset Selection -->
				<div class="space-y-2">
					<div class="flex items-center justify-between">
						<h4 class="text-sm font-medium">Quick Presets</h4>
						{#if selectedPreset}
							<button
								onclick={() => { selectedPreset = undefined; }}
								class="text-xs text-primary hover:underline"
							>
								Clear preset
							</button>
						{/if}
					</div>
					<div class="grid grid-cols-3 gap-2">
						<button
							onclick={() => applyPreset('conservative')}
							class="p-3 rounded-lg border-2 text-sm transition-all {selectedPreset === 'conservative' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
						>
							<div class="font-medium">🛡️ Conservative</div>
							<div class="text-xs text-muted-foreground mt-1">Never overwrite existing</div>
						</button>
						<button
							onclick={() => applyPreset('gap-fill')}
							class="p-3 rounded-lg border-2 text-sm transition-all {selectedPreset === 'gap-fill' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
						>
							<div class="font-medium">📝 Gap Fill</div>
							<div class="text-xs text-muted-foreground mt-1">Fill missing fields only</div>
						</button>
						<button
							onclick={() => applyPreset('aggressive')}
							class="p-3 rounded-lg border-2 text-sm transition-all {selectedPreset === 'aggressive' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
						>
							<div class="font-medium">⚡ Aggressive</div>
							<div class="text-xs text-muted-foreground mt-1">Trust scrapers completely</div>
						</button>
					</div>
				</div>

				<!-- Scalar Fields Strategy -->
				<div class="space-y-2">
					<h4 class="text-sm font-medium">Scalar Fields (Title, Studio, Label, etc.)</h4>
					<div class="grid grid-cols-2 gap-2">
						<button
							onclick={() => { scalarStrategy = 'prefer-nfo'; selectedPreset = undefined; }}
							class="p-3 rounded-lg border-2 text-sm transition-all {scalarStrategy === 'prefer-nfo' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
						>
							<div class="font-medium">Prefer NFO</div>
							<div class="text-xs text-muted-foreground mt-1">Keep existing values</div>
						</button>
						<button
							onclick={() => { scalarStrategy = 'prefer-scraper'; selectedPreset = undefined; }}
							class="p-3 rounded-lg border-2 text-sm transition-all {scalarStrategy === 'prefer-scraper' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
						>
							<div class="font-medium">Prefer Scraped</div>
							<div class="text-xs text-muted-foreground mt-1">Update with fresh data</div>
						</button>
						<button
							onclick={() => { scalarStrategy = 'preserve-existing'; selectedPreset = undefined; }}
							class="p-3 rounded-lg border-2 text-sm transition-all {scalarStrategy === 'preserve-existing' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
						>
							<div class="font-medium">Preserve Existing</div>
							<div class="text-xs text-muted-foreground mt-1">Never overwrite</div>
						</button>
						<button
							onclick={() => { scalarStrategy = 'fill-missing-only'; selectedPreset = undefined; }}
							class="p-3 rounded-lg border-2 text-sm transition-all {scalarStrategy === 'fill-missing-only' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
						>
							<div class="font-medium">Fill Missing Only</div>
							<div class="text-xs text-muted-foreground mt-1">Safe gap filling</div>
						</button>
					</div>
				</div>

				<!-- Array Fields Strategy -->
				<div class="space-y-2">
					<h4 class="text-sm font-medium">Array Fields (Actresses, Genres, Screenshots)</h4>
					<div class="grid grid-cols-2 gap-2">
						<button
							onclick={() => { arrayStrategy = 'merge'; selectedPreset = undefined; }}
							class="p-3 rounded-lg border-2 text-sm transition-all {arrayStrategy === 'merge' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
						>
							<div class="font-medium">Merge</div>
							<div class="text-xs text-muted-foreground mt-1">Combine arrays</div>
						</button>
						<button
							onclick={() => { arrayStrategy = 'replace'; selectedPreset = undefined; }}
							class="p-3 rounded-lg border-2 text-sm transition-all {arrayStrategy === 'replace' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
						>
							<div class="font-medium">Replace</div>
							<div class="text-xs text-muted-foreground mt-1">Use scraped arrays only</div>
						</button>
					</div>
				</div>
			</div>
		</Card>
		</div>
	{/if}
	<!-- File Operations Selection (only shown in scrape mode) -->
	{#if operationMode === 'scrape'}
		<div transition:slide|local={{ duration: 220, easing: quintOut }}>
		<Card class="p-4">
			<div class="space-y-3">
				<div>
					<h3 class="font-semibold">File Operations</h3>
					<p class="text-sm text-muted-foreground">Choose how files are organized during scraping</p>
				</div>
				<div class="grid grid-cols-2 gap-2 md:grid-cols-3 lg:grid-cols-4">
					{#each [
						{ value: 'organize' as OperationMode, label: 'Organize', desc: 'Move to folder', icon: FolderOutput },
						{ value: 'in-place' as OperationMode, label: 'Reorganize in place', desc: 'Keep location, rename folder and file', icon: FolderOpen },
						{ value: 'in-place-norenamefolder' as OperationMode, label: 'Rename file only', desc: 'Rename video file, keep folder', icon: FileEdit },
						{ value: 'metadata-only' as OperationMode, label: 'Metadata only', desc: 'No file changes', icon: FileText },
					] as mode}
						{@const disabled = isInPlaceImplied && (mode.value === 'organize' || mode.value === 'in-place')}
						<button
							onclick={() => { if (!disabled) { operationModeOverride = mode.value; operationModeOverrideTouched = true; } }}
							disabled={disabled}
							class="relative flex flex-col items-start gap-1 p-3 rounded-lg border-2 text-sm transition-all {disabled ? 'border-border opacity-40 cursor-not-allowed' : effectiveOperationMode === mode.value ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
						>
							{#if !operationModeOverrideTouched && getSettingsOperationMode() === mode.value}
								<span class="absolute top-1 right-1 text-[10px] text-primary bg-primary/10 px-1.5 py-0.5 rounded">Default</span>
							{/if}
							<div class="font-medium">{mode.label}</div>
							<div class="text-xs text-muted-foreground">{mode.desc}</div>
						</button>
					{/each}
				</div>
				{#if isInPlaceImplied}
					<p class="text-xs text-muted-foreground">
						Output destination matches source path with no folder/subfolder format — Organize and Reorganize in place are unavailable. <button class="underline text-primary" onclick={() => { destinationPath = ''; localStorage.removeItem(STORAGE_KEY_OUTPUT); }}>Change destination</button>
					</p>
				{:else if operationModeOverrideTouched && effectiveOperationMode !== getSettingsOperationMode()}
					<p class="text-xs text-primary">
						Overriding settings for this batch only. <button class="underline" onclick={() => operationModeOverrideTouched = false}>Reset to default</button>
					</p>
				{/if}
			</div>
		</Card>
		</div>
	{/if}

	<!-- Destination Folder (only shown in scrape mode) -->
	{#if operationMode === 'scrape'}
		<div transition:slide|local={{ duration: 220, easing: quintOut }}>
		<Card class="p-4">
				<div class="space-y-3">
					<div class="flex items-center gap-2">
						<FolderOutput class="h-5 w-5 text-primary" />
						<h3 class="font-semibold">Output Destination</h3>
					</div>
					<div class="flex gap-2">
						<input
							type="text"
							bind:value={destinationPath}
							oninput={() => {
								// Save to localStorage for persistence
								localStorage.setItem(STORAGE_KEY_OUTPUT, destinationPath);
							}}
							placeholder="Enter destination path (e.g., /path/to/output)"
							class="flex-1 px-3 py-2 border rounded-md bg-background focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono text-sm"
						/>
						<Button onclick={openDestinationBrowser}>
							{#snippet children()}
								<FolderOpen class="h-4 w-4 mr-2" />
								Browse
							{/snippet}
						</Button>
					</div>
					<p class="text-xs text-muted-foreground">
					{#if isInPlaceImplied}
						Destination matches source path with no folder format — files will be renamed in place only.
					{:else}
						Scraped files will be organized with metadata, artwork, and NFO files in this directory
					{/if}
				</p>
				</div>
			</Card>
		</div>
		{/if}

	<!-- Selected Files List -->
	{#if selectedFiles.length > 0}
		<div transition:fade|local={{ duration: 180 }}>
		<Card class="p-4">
				<div class="space-y-3">
					<div class="flex items-center justify-between">
						<div class="flex items-center gap-2">
							<div class="w-2 h-2 rounded-full bg-primary animate-pulse"></div>
							<h3 class="font-semibold">
								{selectedFiles.length} File{selectedFiles.length !== 1 ? 's' : ''} Selected for
								Scraping
							</h3>
						</div>
						<Button
							variant="ghost"
							size="sm"
							onclick={() => {
								selectedFiles = [];
							}}
						>
							{#snippet children()}
								Clear All
							{/snippet}
						</Button>
					</div>

					<!-- Files List -->
					<div class="max-h-60 overflow-y-auto space-y-1 border rounded-md p-2 bg-accent/20">
						{#each selectedFiles as filePath (filePath)}
							{@const fileName = filePath.split('/').pop()}
							{@const dirPath = filePath.substring(0, filePath.lastIndexOf('/'))}
							<div animate:flip={{ duration: 220, easing: quintOut }}>
								<div
									class="flex items-center justify-between bg-background px-3 py-2 rounded border hover:border-primary transition-colors group"
								>
									<div class="flex-1 min-w-0">
										<div class="font-medium text-sm truncate" title={fileName}>{fileName}</div>
										<div class="text-xs text-muted-foreground truncate" title={dirPath}>
											{dirPath}
										</div>
									</div>
									<button
										onclick={(e) => {
											e.stopPropagation();
											selectedFiles = selectedFiles.filter((f) => f !== filePath);
										}}
										class="ml-2 px-2 py-1 text-destructive hover:bg-destructive/10 rounded transition-colors opacity-0 group-hover:opacity-100"
										title="Remove"
									>
										×
									</button>
								</div>
							</div>
						{/each}
					</div>
				</div>
			</Card>
		</div>
		{/if}

		<!-- File Browser -->
		<FileBrowser
			{initialPath}
			bind:selectedFiles={selectedFiles}
			onFileSelect={handleFileSelect}
			onPathChange={handleBrowserPathChange}
			multiSelect={true}
			onScan={handleScan}
			bind:recursiveScan={recursiveScan}
			bind:selectedFolders={selectedFolders}
			triggerScan={triggerScan}
		/>

		<!-- Help Text -->
		<Card class="p-4 bg-accent/30">
			<h3 class="font-semibold mb-2">How to use:</h3>
			<ul class="text-sm text-muted-foreground space-y-1">
				<li>1. Select operation mode: <strong>Scrape & Organize</strong> (choose move/copy/hard link/soft link during review) or <strong>Update Metadata</strong> (files stay in place)</li>
				<li>2. Navigate to your video files using the file browser (type a path or click folders)</li>
				<li>3. Click <strong>Scan</strong> to find JAV files (enable <strong>Recursive</strong> to include subfolders)</li>
				<li>4. Configure options (force refresh, scraper selection) in the bottom bar as needed</li>
				<li>5. Click the action button to start the operation</li>
			</ul>
			<p class="text-xs text-muted-foreground mt-3 pt-3 border-t border-border/50">
				<strong>Tip:</strong> Use the filter box to narrow down files, then scan to select only matching results.
			</p>
		</Card>
	</div>
</div>

<!-- Sticky Bottom Action Bar -->
<div class="sticky bottom-0 left-0 right-0 bg-background border-t shadow-lg z-40">
	<!-- Expandable Options Panel -->
	{#if showOptionsPanel}
		<div class="border-b bg-accent/20" transition:slide|local={{ duration: 180, easing: quintOut }}>
			<div class="container mx-auto px-4 py-4 max-w-7xl">
				<div class="flex items-center justify-between mb-3">
					<h3 class="text-sm font-semibold">Options</h3>
					<button
						onclick={() => showOptionsPanel = false}
						class="text-muted-foreground hover:text-foreground transition-colors"
					>
						<X class="h-4 w-4" />
					</button>
				</div>
				<div class="grid gap-3 md:grid-cols-2">
					<label
						class="flex items-center gap-3 p-3 rounded-lg border border-border bg-background hover:bg-accent/50 cursor-pointer transition-colors"
					>
						<input
							type="checkbox"
							bind:checked={forceRefresh}
							class="h-4 w-4 rounded border-gray-300 text-primary focus:ring-2 focus:ring-primary"
						/>
						<div class="flex-1">
							<span class="text-sm font-medium">Force Refresh</span>
							<p class="text-xs text-muted-foreground">Clear cache and fetch fresh metadata</p>
						</div>
					</label>

					<label
						class="flex items-center gap-3 p-3 rounded-lg border border-border bg-background hover:bg-accent/50 cursor-pointer transition-colors"
					>
						<input
							type="checkbox"
							bind:checked={showScraperSelector}
							class="h-4 w-4 rounded border-gray-300 text-primary focus:ring-2 focus:ring-primary"
						/>
						<div class="flex-1">
							<span class="text-sm font-medium">Manual Scraper Selection</span>
							<p class="text-xs text-muted-foreground">Choose specific scrapers</p>
						</div>
					</label>

				</div>

				<!-- Scraper Selector (if enabled) -->
				{#if showScraperSelector}
					<div class="mt-4 pt-4 border-t" transition:fade|local={{ duration: 160 }}>
						<ScraperSelector scrapers={availableScrapers} bind:selected={selectedScrapers} />
					</div>
				{/if}
			</div>
		</div>
	{/if}

	<!-- Main Action Bar -->
	<div class="container mx-auto px-4 py-3 max-w-7xl">
		<div class="flex items-center justify-between gap-4">
			<!-- Left: Selection info and options toggle -->
			<div class="flex items-center gap-3">
				{#if selectedFiles.length > 0}
					<div class="flex items-center gap-2">
						<div class="w-2 h-2 rounded-full bg-primary animate-pulse"></div>
						<span class="text-sm font-medium">
							{selectedFiles.length} file{selectedFiles.length !== 1 ? 's' : ''} selected
						</span>
						<button
							onclick={() => selectedFiles = []}
							class="text-xs text-muted-foreground hover:text-destructive transition-colors"
						>
							(clear)
						</button>
					</div>
				{:else}
					<span class="text-sm text-muted-foreground">No files selected</span>
				{/if}
			</div>

			<!-- Right: Scan, options toggle and action button -->
			<div class="flex items-center gap-3">
				<!-- Recursive toggle + Scan -->
				<div class="flex items-center gap-2">
					<label class="flex items-center gap-1.5 text-xs cursor-pointer">
						<input
							type="checkbox"
							bind:checked={recursiveScan}
							class="h-3.5 w-3.5 rounded border-gray-300 text-primary focus:ring-1 focus:ring-primary"
						/>
						<span class="text-muted-foreground hidden sm:inline">Recursive</span>
					</label>
					<Button
						variant="outline"
						size="sm"
						onclick={() => triggerScan++}
						disabled={scanning}
						title={recursiveScan ? "Scan all subfolders" : "Scan current folder only"}
					>
						{#snippet children()}
							{#if scanning}
								<LoaderCircle class="h-3.5 w-3.5 mr-1.5 animate-spin" />
							{:else}
								<Scan class="h-3.5 w-3.5 mr-1.5" />
							{/if}
							{scanning ? 'Scanning...' : 'Scan'}
						{/snippet}
					</Button>
				</div>

				<!-- Separator -->
				<div class="h-6 w-px bg-border"></div>

				<!-- Options toggle -->
				<Button
					variant="outline"
					size="sm"
					onclick={() => showOptionsPanel = !showOptionsPanel}
				>
					{#snippet children()}
						<Settings class="h-4 w-4 mr-2" />
						Options
						{#if showOptionsPanel}
							<ChevronDown class="h-4 w-4 ml-1" />
						{:else}
							<ChevronUp class="h-4 w-4 ml-1" />
						{/if}
					{/snippet}
				</Button>

				<!-- Active options indicators -->
				{#if forceRefresh || showScraperSelector}
					<div class="hidden sm:flex items-center gap-1 text-xs">
						{#if forceRefresh}
							<span class="px-2 py-0.5 bg-primary/10 text-primary rounded">Force</span>
						{/if}
						{#if showScraperSelector}
							<span class="px-2 py-0.5 bg-primary/10 text-primary rounded">{selectedScrapers.length} scrapers</span>
						{/if}
					</div>
				{/if}

				<!-- Action button -->
				<Button onclick={startBatchScrape} disabled={selectedFiles.length === 0 || scraping}>
					{#snippet children()}
						{#if scraping}
							<LoaderCircle class="h-4 w-4 mr-2 animate-spin" />
						{:else if operationMode === 'update'}
							<RefreshCw class="h-4 w-4 mr-2" />
						{:else}
							<Play class="h-4 w-4 mr-2" />
						{/if}
						{#if scraping}
							Starting...
						{:else if operationMode === 'update'}
							Update {selectedFiles.length} File{selectedFiles.length !== 1 ? 's' : ''}
						{:else}
							Scrape {selectedFiles.length} File{selectedFiles.length !== 1 ? 's' : ''}
						{/if}
					{/snippet}
				</Button>
			</div>
		</div>
	</div>
</div>

<!-- Progress Modal -->
{#if showProgress && currentJobId}
	<ProgressModal
		jobId={currentJobId}
		destination={destinationPath}
		updateMode={operationMode === 'update'}
		onClose={closeProgress}
	/>
{/if}

<!-- Destination Browser Modal -->
{#if showDestinationBrowser}
	<div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4" use:portalToBody in:fade|local={{ duration: 140 }} out:fade|local={{ duration: 120 }}>
		<div class="bg-background rounded-lg shadow-xl max-w-4xl w-full max-h-[80vh] flex flex-col" in:scale|local={{ start: 0.97, duration: 180, easing: quintOut }} out:scale|local={{ start: 1, opacity: 0.7, duration: 140, easing: quintOut }}>
			<!-- Modal Header -->
			<div class="p-6 border-b flex items-center justify-between">
				<div>
					<h2 class="text-xl font-bold">Select Destination Folder</h2>
					<p class="text-sm text-muted-foreground mt-1">
						Navigate to and select the folder where files will be organized
					</p>
				</div>
				<button
					onclick={cancelDestination}
					class="text-muted-foreground hover:text-foreground transition-colors"
				>
					✕
				</button>
			</div>

			<!-- Modal Body -->
			<div class="flex-1 overflow-auto p-6">
				<FileBrowser
					{initialPath}
					onFileSelect={handleDestinationSelect}
					onPathChange={handleDestinationPathChange}
					multiSelect={false}
					folderOnly={true}
				/>
			</div>

			<!-- Modal Footer -->
			<div class="p-6 border-t space-y-3">
				<div class="flex items-center gap-2">
					<span class="text-sm font-medium text-muted-foreground">Selected Path:</span>
					<code
						class="flex-1 px-3 py-1.5 bg-accent rounded text-sm font-mono text-foreground overflow-x-auto"
					>
						{tempDestinationPath || initialPath}
					</code>
				</div>
				<div class="flex items-center justify-end gap-2">
					<Button variant="outline" onclick={cancelDestination}>
						{#snippet children()}
							Cancel
						{/snippet}
					</Button>
					<Button onclick={confirmDestination}>
						{#snippet children()}
							Use This Folder
						{/snippet}
					</Button>
				</div>
			</div>
		</div>
	</div>
{/if}

<!-- Background Job Indicator -->
{#if currentJobId && !showProgress}
	<BackgroundJobIndicator
		jobId={currentJobId}
		onReopen={reopenProgress}
		onDismiss={dismissBackgroundIndicator}
	/>
{/if}
