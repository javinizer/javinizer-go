<script lang="ts">
	import { onMount } from 'svelte';
	import FileBrowser from '$lib/components/FileBrowser.svelte';
	import ProgressModal from '$lib/components/ProgressModal.svelte';
	import BackgroundJobIndicator from '$lib/components/BackgroundJobIndicator.svelte';
	import ScraperSelector from '$lib/components/ScraperSelector.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import { apiClient } from '$lib/api/client';
	import { toastStore } from '$lib/stores/toast';
	import { Play, FolderInput, Scan, FolderOutput, FolderOpen, RotateCcw, Loader2, RefreshCw } from 'lucide-svelte';
	import type { Scraper } from '$lib/api/types';

	type OperationMode = 'scrape' | 'update';

	let selectedFiles: string[] = $state([]);
	let currentJobId: string | null = $state(null);
	let showProgress = $state(false);
	let scraping = $state(false);
	let forceRefresh = $state(false);
	let operationMode: OperationMode = $state('scrape');
	let customPath = $state('');
	let scanning = $state(false);
	let scanError = $state<string | null>(null);
	let initialPath = $state('');
	let destinationPath = $state('');
	let showDestinationBrowser = $state(false);
	let tempDestinationPath = $state('');
	let showInputBrowser = $state(false);
	let tempInputPath = $state('');
	let currentBrowserPath = $state('');
	let availableScrapers: Scraper[] = $state([]);
	let selectedScrapers: string[] = $state([]);
	let showScraperSelector = $state(false);
	let selectedPreset: string | undefined = $state(undefined);  // Merge strategy preset: conservative, gap-fill, aggressive
	let scalarStrategy: string = $state('prefer-nfo');  // For scalar fields: prefer-nfo, prefer-scraper, preserve-existing, fill-missing-only
	let arrayStrategy: string = $state('merge');        // For array fields: merge, replace

	// localStorage keys
	const STORAGE_KEY_INPUT = 'javinizer_input_path';
	const STORAGE_KEY_OUTPUT = 'javinizer_output_path';

	// Load current working directory and config on mount
	onMount(async () => {
		try {
			const response = await apiClient.getCurrentWorkingDirectory();
			initialPath = response.path;

			// Load input path from localStorage, or fall back to working directory
			const savedInputPath = localStorage.getItem(STORAGE_KEY_INPUT);
			customPath = savedInputPath || response.path;
		} catch (error) {
			console.error('Failed to get current working directory:', error);
		}

		// Load output path from localStorage, or fall back to initialPath
		const savedOutputPath = localStorage.getItem(STORAGE_KEY_OUTPUT);
		destinationPath = savedOutputPath || initialPath;

		// Fetch available scrapers
		try {
			availableScrapers = await apiClient.getScrapers();
			// Initialize with all enabled scrapers
			selectedScrapers = availableScrapers
				.filter((s) => s.enabled)
				.map((s) => s.name);
		} catch (error) {
			console.error('Failed to fetch scrapers:', error);
		}
	});

	function handleFileSelect(files: string[]) {
		selectedFiles = files;
	}

	function handleBrowserPathChange(path: string) {
		currentBrowserPath = path;
	}

	async function scanPath(path: string, updateBrowser: boolean = false) {
		if (!path.trim()) return;

		scanning = true;
		scanError = null;
		try {
			const response = await apiClient.scan({
				path: path,
				recursive: true
			});

			// Add all matched files to selection
			const matchedFiles = response.files
				.filter((f) => f.matched && !f.is_dir)
				.map((f) => f.path);

			if (matchedFiles.length > 0) {
				// Merge with existing selections
				selectedFiles = [...new Set([...selectedFiles, ...matchedFiles])];

				// Update the file browser if requested
				if (updateBrowser) {
					initialPath = path;
					currentBrowserPath = path;
				}
			} else {
				scanError = `No JAV files found in ${path}`;
			}
		} catch (error) {
			scanError = error instanceof Error ? error.message : 'Failed to scan directory';
		} finally {
			scanning = false;
		}
	}

	async function scanCurrentBrowserPath() {
		await scanPath(currentBrowserPath, false);
	}

	async function scanCustomPath() {
		await scanPath(customPath, true);
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
				preset: isUpdateMode ? (selectedPreset as any) : undefined,
				scalar_strategy: isUpdateMode ? scalarStrategy : undefined,
				array_strategy: isUpdateMode ? arrayStrategy : undefined
			});
			currentJobId = response.job_id;

			// Show success toast
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

	function openInputBrowser() {
		tempInputPath = customPath || initialPath;
		showInputBrowser = true;
	}

	function handleInputSelect(files: string[]) {
		// Ignore file selections, just track path changes
	}

	function handleInputPathChange(path: string) {
		tempInputPath = path;
	}

	function confirmInputPath() {
		customPath = tempInputPath;
		initialPath = tempInputPath;
		// Save to localStorage for persistence
		localStorage.setItem(STORAGE_KEY_INPUT, tempInputPath);
		showInputBrowser = false;
	}

	function cancelInputBrowser() {
		showInputBrowser = false;
	}

	function resetDirectories() {
		// Clear localStorage
		localStorage.removeItem(STORAGE_KEY_INPUT);
		localStorage.removeItem(STORAGE_KEY_OUTPUT);
		// Reset to initial/default paths
		customPath = initialPath;
		destinationPath = initialPath;
	}
</script>

<div class="container mx-auto px-4 py-8">
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

		<!-- Input Directory -->
		<Card class="p-4">
			<div class="space-y-3">
				<div class="flex items-center gap-2">
					<FolderInput class="h-5 w-5 text-primary" />
					<h3 class="font-semibold">Input Directory</h3>
				</div>
				<div class="flex gap-2">
					<input
						type="text"
						bind:value={customPath}
						oninput={() => {
							initialPath = customPath;
							// Save to localStorage for persistence
							localStorage.setItem(STORAGE_KEY_INPUT, customPath);
						}}
						onkeydown={(e) => {
							if (e.key === 'Enter') scanCustomPath();
						}}
						placeholder="Enter full path (e.g., /path/to/videos)"
						class="flex-1 px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono text-sm"
					/>
					<Button onclick={openInputBrowser}>
						{#snippet children()}
							<FolderOpen class="h-4 w-4 mr-2" />
							Browse
						{/snippet}
					</Button>
					<Button onclick={scanCustomPath} disabled={!customPath.trim() || scanning}>
						{#snippet children()}
							<Scan class="h-4 w-4 mr-2" />
							{scanning ? 'Scanning...' : 'Scan'}
						{/snippet}
					</Button>
				</div>
				<p class="text-xs text-muted-foreground">
					Directory path to scan for JAV video files
				</p>
				{#if scanError}
					<div class="text-sm text-destructive bg-destructive/10 px-3 py-2 rounded-md border border-destructive/20">
						{scanError}
					</div>
				{/if}
			</div>
		</Card>

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
	{/if}
		<!-- Destination Folder (only shown in scrape mode) -->
		{#if operationMode === 'scrape'}
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
							class="flex-1 px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono text-sm"
						/>
						<Button onclick={openDestinationBrowser}>
							{#snippet children()}
								<FolderOpen class="h-4 w-4 mr-2" />
								Browse
							{/snippet}
						</Button>
					</div>
					<p class="text-xs text-muted-foreground">
						Scraped files will be organized with metadata, artwork, and NFO files in this directory
					</p>
				</div>
			</Card>
		{/if}

		<!-- Controls -->
		<Card class="p-6">
			<div class="space-y-6">
				<!-- Options Section -->
				<div class="space-y-3">
					<h3 class="text-sm font-semibold text-foreground mb-3">Options</h3>
					<div class="grid gap-3">
						<label
							class="flex items-start gap-3 p-3 rounded-lg border border-border hover:bg-accent/50 cursor-pointer transition-colors"
						>
							<input
								type="checkbox"
								bind:checked={forceRefresh}
								class="mt-0.5 h-4 w-4 rounded border-gray-300 text-primary focus:ring-2 focus:ring-primary"
							/>
							<div class="flex-1">
								<span class="text-sm font-medium">Force Refresh</span>
								<p class="text-xs text-muted-foreground mt-0.5">
									Clear cache and fetch fresh metadata from scrapers
								</p>
							</div>
						</label>

						<label
							class="flex items-start gap-3 p-3 rounded-lg border border-border hover:bg-accent/50 cursor-pointer transition-colors"
						>
							<input
								type="checkbox"
								bind:checked={showScraperSelector}
								class="mt-0.5 h-4 w-4 rounded border-gray-300 text-primary focus:ring-2 focus:ring-primary"
							/>
							<div class="flex-1">
								<span class="text-sm font-medium">Manual Scraper Selection</span>
								<p class="text-xs text-muted-foreground mt-0.5">
									Choose specific scrapers instead of using default priority
								</p>
							</div>
						</label>
					</div>
				</div>

				<!-- Scraper Selector (if enabled) -->
				{#if showScraperSelector}
					<div class="pt-2 border-t">
						<ScraperSelector scrapers={availableScrapers} bind:selected={selectedScrapers} />
					</div>
				{/if}

				<!-- Action Buttons -->
				<div class="flex items-center justify-end gap-3 pt-2 border-t">
					<Button
						onclick={scanCurrentBrowserPath}
						disabled={!currentBrowserPath.trim() || scanning}
						variant="outline"
					>
						{#snippet children()}
							{#if scanning}
								<Loader2 class="h-4 w-4 mr-2 animate-spin" />
							{:else}
								<Scan class="h-4 w-4 mr-2" />
							{/if}
							{scanning ? 'Scanning...' : 'Scan Current'}
						{/snippet}
					</Button>
					<Button onclick={startBatchScrape} disabled={selectedFiles.length === 0 || scraping}>
						{#snippet children()}
							{#if scraping}
								<Loader2 class="h-4 w-4 mr-2 animate-spin" />
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
		</Card>

		<!-- Selected Files List -->
		{#if selectedFiles.length > 0}
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
						{#each selectedFiles as filePath}
							{@const fileName = filePath.split('/').pop()}
							{@const dirPath = filePath.substring(0, filePath.lastIndexOf('/'))}
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
						{/each}
					</div>
				</div>
			</Card>
		{/if}

		<!-- File Browser -->
		<FileBrowser
			{initialPath}
			onFileSelect={handleFileSelect}
			onPathChange={handleBrowserPathChange}
			multiSelect={true}
		/>

		<!-- Help Text -->
		<Card class="p-4 bg-accent/30">
			<h3 class="font-semibold mb-2">How to use:</h3>
			<ul class="text-sm text-muted-foreground space-y-1">
				<li>1. Select operation mode: <strong>Scrape & Organize</strong> (moves files) or <strong>Update Metadata</strong> (files stay in place)</li>
				<li>2. Navigate to your video files directory using the file browser</li>
				<li>3. Select one or more video files (files with matched JAV IDs are highlighted in green)</li>
				<li>4. Configure options (force refresh, scraper selection) as needed</li>
				<li>5. Click the action button to start the operation</li>
				<li>6. Monitor progress in the modal dialog (you can close it and the job will continue)</li>
			</ul>
		</Card>
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

<!-- Input Browser Modal -->
{#if showInputBrowser}
	<div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
		<div class="bg-background rounded-lg shadow-xl max-w-4xl w-full max-h-[80vh] flex flex-col">
			<!-- Modal Header -->
			<div class="p-6 border-b flex items-center justify-between">
				<div>
					<h2 class="text-xl font-bold">Select Input Folder</h2>
					<p class="text-sm text-muted-foreground mt-1">
						Navigate to and select the folder containing JAV video files
					</p>
				</div>
				<button
					onclick={cancelInputBrowser}
					class="text-muted-foreground hover:text-foreground transition-colors"
				>
					✕
				</button>
			</div>

			<!-- Modal Body -->
			<div class="flex-1 overflow-auto p-6">
				<FileBrowser
					initialPath={tempInputPath || initialPath}
					onFileSelect={handleInputSelect}
					onPathChange={handleInputPathChange}
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
						{tempInputPath || initialPath}
					</code>
				</div>
				<div class="flex items-center justify-end gap-2">
					<Button variant="outline" onclick={cancelInputBrowser}>
						{#snippet children()}
							Cancel
						{/snippet}
					</Button>
					<Button onclick={confirmInputPath}>
						{#snippet children()}
							Use This Folder
						{/snippet}
					</Button>
				</div>
			</div>
		</div>
	</div>
{/if}

<!-- Destination Browser Modal -->
{#if showDestinationBrowser}
	<div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
		<div class="bg-background rounded-lg shadow-xl max-w-4xl w-full max-h-[80vh] flex flex-col">
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
