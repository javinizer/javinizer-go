<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import { untrack } from 'svelte';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { flip } from 'svelte/animate';
	import { quintOut } from 'svelte/easing';
	import { fade, scale, slide } from 'svelte/transition';
	import { portalToBody } from '$lib/actions/portal';
	import FileBrowser from '$lib/components/FileBrowser.svelte';
	import PathInput from '$lib/components/PathInput.svelte';
	import ScraperSelector from '$lib/components/ScraperSelector.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import { apiClient } from '$lib/api/client';
	import { toastStore } from '$lib/stores/toast';
	import { startJob } from '$lib/stores/background-job.svelte';
	import { goto } from '$app/navigation';
	import {
		setPendingScrape,
		clearPendingScrape,
		buildPendingScrapeSnapshot
	} from '$lib/stores/pending-scrape.svelte';
	import { clearManualInputs } from '$lib/stores/manual-inputs-session';
	import { createConfigQuery, createScrapersQuery } from '$lib/query/queries';
	import { isTerminalStatus } from '$lib/utils/job-progress';
	import { Play, FolderOutput, FolderOpen, FileEdit, FileText, RotateCcw, LoaderCircle, RefreshCw, Settings, ChevronUp, ChevronDown, X, Scan } from 'lucide-svelte';
	import type { Scraper, FileInfo, Config } from '$lib/api/types';
	import type { OperationMode } from '$lib/api/types';

	type BrowseMode = 'scrape' | 'update';
	let selectedFiles: string[] = $state([]);
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

	let pathInitialized = $state(false);

	$effect(() => {
		const cwd = cwdQuery.data?.path;
		if (!cwd || pathInitialized) return;
		pathInitialized = true;
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
	let manualScrapeMode: boolean = $state(false);

	// D4: persist /browse scrape state to sessionStorage + hydrate on mount so a
	// Back round-trip from /manual preserves selection + globals. When hydrated
	// selectedScrapers is non-empty, scrapersInitialized is locked so the
	// all-enabled re-init on remount is skipped.
	const STORAGE_KEY_SCRAPE_STATE = 'javinizer_browse_scrape_state';

	interface BrowseScrapeState {
		selectedFiles: string[];
		operationMode: BrowseMode;
		operationModeOverride: OperationMode;
		operationModeOverrideTouched: boolean;
		forceRefresh: boolean;
		showScraperSelector: boolean;
		selectedScrapers: string[];
		selectedPreset: string | undefined;
		scalarStrategy: ScalarStrategy;
		arrayStrategy: ArrayStrategy;
		manualScrapeMode: boolean;
	}

	let scrapeStateHydrated = $state(false);

	$effect(() => {
		if (scrapeStateHydrated) return;
		scrapeStateHydrated = true;
		if (typeof sessionStorage === 'undefined') return;
		try {
			const raw = sessionStorage.getItem(STORAGE_KEY_SCRAPE_STATE);
			if (!raw) return;
			const saved = JSON.parse(raw) as Partial<BrowseScrapeState>;
			if (Array.isArray(saved.selectedFiles)) selectedFiles = saved.selectedFiles;
			if (saved.operationMode === 'scrape' || saved.operationMode === 'update') operationMode = saved.operationMode;
			if (saved.operationModeOverride) operationModeOverride = saved.operationModeOverride;
			if (typeof saved.operationModeOverrideTouched === 'boolean') operationModeOverrideTouched = saved.operationModeOverrideTouched;
			if (typeof saved.forceRefresh === 'boolean') forceRefresh = saved.forceRefresh;
			if (typeof saved.showScraperSelector === 'boolean') showScraperSelector = saved.showScraperSelector;
			if (
				Array.isArray(saved.selectedScrapers) &&
				saved.selectedScrapers.every((s) => typeof s === 'string')
			) {
				selectedScrapers = saved.selectedScrapers;
				// A saved empty array is a deliberate user choice (no scrapers);
				// only re-run the default “all enabled” initializer when the
				// saved value is truly absent.
				if (saved.showScraperSelector || saved.selectedScrapers.length > 0) {
					scrapersInitialized = true;
				}
			}
			if (saved.selectedPreset !== undefined) selectedPreset = saved.selectedPreset;
			if (saved.scalarStrategy) scalarStrategy = saved.scalarStrategy;
			if (saved.arrayStrategy) arrayStrategy = saved.arrayStrategy;
			if (typeof saved.manualScrapeMode === 'boolean') manualScrapeMode = saved.manualScrapeMode;
		} catch {}
	});

	$effect(() => {
		if (!scrapeStateHydrated) return;
		if (typeof sessionStorage === 'undefined') return;
		const state: BrowseScrapeState = {
			selectedFiles,
			operationMode,
			operationModeOverride,
			operationModeOverrideTouched,
			forceRefresh,
			showScraperSelector,
			selectedScrapers,
			selectedPreset,
			scalarStrategy,
			arrayStrategy,
			manualScrapeMode
		};
		try {
			sessionStorage.setItem(STORAGE_KEY_SCRAPE_STATE, JSON.stringify(state));
		} catch {}
	});

	function clearSelection() {
		selectedFiles = [];
		clearPendingScrape();
		clearManualInputs();
	}

	// Track the batch job started from this browse page so we can clear the
	// file selection once the job reaches a terminal SUCCESS state. Persisted to
	// sessionStorage so a remount (e.g. user navigated away before completion)
	// can re-check the job and clear stale selection.
	const STORAGE_KEY_PENDING_JOB = 'javinizer_browse_pending_job';
	const JOB_SUCCESS_STATUSES = new Set(['completed', 'organized', 'reverted']);
	let pendingJobId: string | null = $state(null);
	let launchedFiles: string[] | null = $state(null);
	let completionPoll: ReturnType<typeof setInterval> | null = null;

	function stopCompletionPoll() {
		if (completionPoll) {
			clearInterval(completionPoll);
			completionPoll = null;
		}
	}

	function sameSelection(a: string[], b: string[]): boolean {
		if (a.length !== b.length) return false;
		const setB = new Set(b);
		return a.every((f) => setB.has(f));
	}

	function clearPendingJob() {
		pendingJobId = null;
		launchedFiles = null;
		try {
			sessionStorage.removeItem(STORAGE_KEY_PENDING_JOB);
		} catch {}
	}

	async function pollJobCompletion(jobId: string) {
		stopCompletionPoll();
		const tick = async () => {
			try {
				const job = await apiClient.getBatchJob(jobId);
				const status = job.status?.toLowerCase();
				if (status && JOB_SUCCESS_STATUSES.has(status)) {
					stopCompletionPoll();
					if ((job.failed ?? 0) === 0 && launchedFiles && sameSelection(launchedFiles, selectedFiles)) {
						clearSelection();
					}
					clearPendingJob();
				} else if (isTerminalStatus(status)) {
					// failed / cancelled — keep selection so the user can retry
					stopCompletionPoll();
					clearPendingJob();
				}
			} catch {
				// transient network error — keep polling
			}
		};
		void tick();
		completionPoll = setInterval(() => { void tick(); }, 2000);
	}

	// On mount, if a pending job was recorded (e.g. user navigated away before
	// completion), resume polling so a since-completed job clears the selection.
	$effect(() => {
		if (typeof sessionStorage === 'undefined') return;
		let saved: string | null = null;
		try {
			saved = sessionStorage.getItem(STORAGE_KEY_PENDING_JOB);
		} catch {}
		if (saved && !pendingJobId) {
			try {
				const parsed = JSON.parse(saved) as { jobId: string; launchedFiles?: string[] };
				pendingJobId = parsed.jobId;
				if (Array.isArray(parsed.launchedFiles)) {
					launchedFiles = parsed.launchedFiles;
				}
			} catch {
				// Legacy format — just a job ID string
				pendingJobId = saved;
			}
			if (pendingJobId) {
				pollJobCompletion(pendingJobId);
			}
		}
	});

	// Stop polling when the component unmounts (sessionStorage marker is kept so
	// a remount can finish the check).
	$effect(() => {
		return () => stopCompletionPoll();
	});

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
		const output = config?.output;
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
					toastStore.success(
						failedFolders.length > 0
							? m.browse_added_files_folders({ fileCount: allMatched.length, folderCount: fulfilledCount, failed: failedFolders.length })
							: m.browse_added_files_folders_no_failed({ fileCount: allMatched.length, folderCount: fulfilledCount }),
						3000
					);
				} else if (failedFolders.length === selectedFolders.length) {
					toastStore.error(m.browse_scan_failed_all({ count: failedFolders.length }), 5000);
				} else if (failedFolders.length > 0) {
					toastStore.warning(m.browse_no_files_folders(), 5000);
				} else {
					toastStore.warning(m.browse_no_files_folders(), 5000);
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
					const scanType = recursive ? m.browse_scan_type_recursive() : m.browse_scan_type_current();
					toastStore.success(
						recursive && filter
							? m.browse_added_files_filtered({ fileCount: matchedFiles.length, filter, scanType })
							: m.browse_added_files_single_folder({ fileCount: matchedFiles.length, scanType }),
						3000
					);
				} else {
					if (!recursive) {
						const totalMatched = response.files.filter((f) => f.matched && !f.is_dir).length;
						if (totalMatched > 0) {
							toastStore.warning(m.browse_no_files_filter_match({ count: totalMatched }), 5000);
							return;
						}
					}
					toastStore.warning(
						recursive
							? (filter ? m.browse_no_files_recursive({ filter }) : m.browse_no_files_no_filter_subfolder())
							: (filter ? m.browse_no_files_current({ filter }) : m.browse_no_files_no_filter()),
						5000
					);
				}
			}
		} catch (error) {
			toastStore.error(error instanceof Error ? error.message : m.browse_scan_dir_failed(), 5000);
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

	function continueToManual() {
		if (selectedFiles.length === 0) return;
		setPendingScrape(
			buildPendingScrapeSnapshot({
				files: selectedFiles,
				browseMode: operationMode,
				effectiveOperationMode: effectiveOperationMode,
				isInPlaceImplied: isInPlaceImplied,
				showScraperSelector: showScraperSelector,
				destination: operationMode === 'update' ? '' : destinationPath,
				selectedScrapers: showScraperSelector ? selectedScrapers : [],
				force: forceRefresh,
				preset: operationMode === 'update' ? (selectedPreset as 'conservative' | 'gap-fill' | 'aggressive' | undefined) : undefined,
				scalarStrategy: operationMode === 'update' ? scalarStrategy : undefined,
				arrayStrategy: operationMode === 'update' ? arrayStrategy : undefined
			})
		);
		void goto('/manual');
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
			startJob(response.job_id);
			launchedFiles = [...selectedFiles];
			pendingJobId = response.job_id;
			try {
				sessionStorage.setItem(
				STORAGE_KEY_PENDING_JOB,
				JSON.stringify({ jobId: response.job_id, launchedFiles }),
			);
			} catch {}
			pollJobCompletion(response.job_id);
			void queryClient.invalidateQueries({ queryKey: ['batch-jobs'] });

			toastStore.success(
				isUpdateMode
					? m.browse_updating_started({ count: selectedFiles.length })
					: m.browse_scraping_started({ count: selectedFiles.length }),
				5000
			);
		} catch (error) {
			// Show error toast
			const errorMessage = error instanceof Error ? error.message : m.browse_batch_failed_generic();
			toastStore.error(errorMessage, 7000);
		} finally {
			scraping = false;
		}
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
			toastStore.error(m.browse_get_cwd_failed());
		}
	}
</script>

<div class="container mx-auto px-4 py-8 pb-32">
	<div class="max-w-7xl mx-auto space-y-6">
		<!-- Header -->
		<div class="flex items-center justify-between">
			<div>
				<h1 class="text-3xl font-bold">{m.browse_title()}</h1>
				<p class="text-muted-foreground mt-1">
					{m.browse_subtitle()}
				</p>
			</div>
			<div class="flex gap-2">
				<Button variant="outline" onclick={resetDirectories}>
					{#snippet children()}
						<RotateCcw class="h-4 w-4 mr-2" />
						{m.browse_reset_paths()}
					{/snippet}
				</Button>
			</div>
		</div>

		<!-- Operation Mode Selection -->
		<Card class="p-4">
			<div class="space-y-3">
				<h3 class="font-semibold">{m.browse_operation_mode()}</h3>
				<div class="grid grid-cols-2 gap-3">
					<button
						onclick={() => operationMode = 'scrape'}
						class="flex flex-col items-start gap-2 p-4 rounded-lg border-2 transition-all {operationMode === 'scrape' ? 'border-primary bg-primary/5' : 'border-border hover:border-primary/50'}"
					>
						<div class="flex items-center gap-2">
							<Play class="h-5 w-5 {operationMode === 'scrape' ? 'text-primary' : 'text-muted-foreground'}" />
							<span class="font-medium {operationMode === 'scrape' ? 'text-primary' : ''}">{m.browse_mode_scrape_organize()}</span>
						</div>
						<p class="text-xs text-muted-foreground text-left">
							{m.browse_mode_scrape_desc()}
						</p>
					</button>

					<button
						onclick={() => operationMode = 'update'}
						class="flex flex-col items-start gap-2 p-4 rounded-lg border-2 transition-all {operationMode === 'update' ? 'border-primary bg-primary/5' : 'border-border hover:border-primary/50'}"
					>
						<div class="flex items-center gap-2">
							<RefreshCw class="h-5 w-5 {operationMode === 'update' ? 'text-primary' : 'text-muted-foreground'}" />
							<span class="font-medium {operationMode === 'update' ? 'text-primary' : ''}">{m.browse_mode_update_metadata()}</span>
						</div>
						<p class="text-xs text-muted-foreground text-left">
							{m.browse_mode_update_desc()}
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
					<h3 class="font-semibold">{m.browse_nfo_merge_strategy()}</h3>
					<p class="text-sm text-muted-foreground">{m.browse_merge_strategy_desc()}</p>
				</div>

				<!-- Preset Selection -->
				<div class="space-y-2">
					<div class="flex items-center justify-between">
						<h4 class="text-sm font-medium">{m.browse_quick_presets()}</h4>
						{#if selectedPreset}
							<button
								onclick={() => { selectedPreset = undefined; }}
								class="text-xs text-primary hover:underline"
							>
								{m.browse_clear_preset()}
							</button>
						{/if}
					</div>
					<div class="grid grid-cols-3 gap-2">
						<button
							onclick={() => applyPreset('conservative')}
							class="p-3 rounded-lg border-2 text-sm transition-all {selectedPreset === 'conservative' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
						>
							<div class="font-medium">{m.browse_preset_conservative()}</div>
							<div class="text-xs text-muted-foreground mt-1">{m.browse_preset_conservative_desc()}</div>
						</button>
						<button
							onclick={() => applyPreset('gap-fill')}
							class="p-3 rounded-lg border-2 text-sm transition-all {selectedPreset === 'gap-fill' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
						>
							<div class="font-medium">{m.browse_preset_gap_fill()}</div>
							<div class="text-xs text-muted-foreground mt-1">{m.browse_preset_gap_fill_desc()}</div>
						</button>
						<button
							onclick={() => applyPreset('aggressive')}
							class="p-3 rounded-lg border-2 text-sm transition-all {selectedPreset === 'aggressive' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
						>
							<div class="font-medium">{m.browse_preset_aggressive()}</div>
							<div class="text-xs text-muted-foreground mt-1">{m.browse_preset_aggressive_desc()}</div>
						</button>
					</div>
				</div>

				<!-- Scalar Fields Strategy -->
				<div class="space-y-2">
					<h4 class="text-sm font-medium">{m.browse_scalar_fields()}</h4>
					<div class="grid grid-cols-2 gap-2">
						<button
							onclick={() => { scalarStrategy = 'prefer-nfo'; selectedPreset = undefined; }}
							class="p-3 rounded-lg border-2 text-sm transition-all {scalarStrategy === 'prefer-nfo' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
						>
							<div class="font-medium">{m.browse_prefer_nfo()}</div>
							<div class="text-xs text-muted-foreground mt-1">{m.browse_prefer_nfo_desc()}</div>
						</button>
						<button
							onclick={() => { scalarStrategy = 'prefer-scraper'; selectedPreset = undefined; }}
							class="p-3 rounded-lg border-2 text-sm transition-all {scalarStrategy === 'prefer-scraper' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
						>
							<div class="font-medium">{m.browse_prefer_scraped()}</div>
							<div class="text-xs text-muted-foreground mt-1">{m.browse_prefer_scraped_desc()}</div>
						</button>
						<button
							onclick={() => { scalarStrategy = 'preserve-existing'; selectedPreset = undefined; }}
							class="p-3 rounded-lg border-2 text-sm transition-all {scalarStrategy === 'preserve-existing' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
						>
							<div class="font-medium">{m.browse_preserve_existing()}</div>
							<div class="text-xs text-muted-foreground mt-1">{m.browse_preserve_existing_desc()}</div>
						</button>
						<button
							onclick={() => { scalarStrategy = 'fill-missing-only'; selectedPreset = undefined; }}
							class="p-3 rounded-lg border-2 text-sm transition-all {scalarStrategy === 'fill-missing-only' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
						>
							<div class="font-medium">{m.browse_fill_missing_only()}</div>
							<div class="text-xs text-muted-foreground mt-1">{m.browse_fill_missing_only_desc()}</div>
						</button>
					</div>
				</div>

				<!-- Array Fields Strategy -->
				<div class="space-y-2">
					<h4 class="text-sm font-medium">{m.browse_array_fields()}</h4>
					<div class="grid grid-cols-2 gap-2">
						<button
							onclick={() => { arrayStrategy = 'merge'; selectedPreset = undefined; }}
							class="p-3 rounded-lg border-2 text-sm transition-all {arrayStrategy === 'merge' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
						>
							<div class="font-medium">{m.browse_merge()}</div>
							<div class="text-xs text-muted-foreground mt-1">{m.browse_merge_desc()}</div>
						</button>
						<button
							onclick={() => { arrayStrategy = 'replace'; selectedPreset = undefined; }}
							class="p-3 rounded-lg border-2 text-sm transition-all {arrayStrategy === 'replace' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
						>
							<div class="font-medium">{m.browse_replace()}</div>
							<div class="text-xs text-muted-foreground mt-1">{m.browse_replace_desc()}</div>
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
					<h3 class="font-semibold">{m.browse_file_operations()}</h3>
					<p class="text-sm text-muted-foreground">{m.browse_file_operations_desc()}</p>
				</div>
				<div class="grid grid-cols-2 gap-2 md:grid-cols-3 lg:grid-cols-4">
					{#each [
						{ value: 'organize' as OperationMode, label: m.browse_op_organize(), desc: m.browse_op_organize_desc(), icon: FolderOutput },
						{ value: 'in-place' as OperationMode, label: m.browse_op_reorganize(), desc: m.browse_op_reorganize_desc(), icon: FolderOpen },
						{ value: 'in-place-norenamefolder' as OperationMode, label: m.browse_op_rename_only(), desc: m.browse_op_rename_only_desc(), icon: FileEdit },
						{ value: 'metadata-artwork' as OperationMode, label: m.browse_op_metadata_artwork(), desc: m.browse_op_metadata_artwork_desc(), icon: FileText },
					] as mode}
						{@const disabled = isInPlaceImplied && (mode.value === 'organize' || mode.value === 'in-place')}
						<button
							onclick={() => { if (!disabled) { operationModeOverride = mode.value; operationModeOverrideTouched = true; } }}
							disabled={disabled}
							class="relative flex flex-col items-start gap-1 p-3 rounded-lg border-2 text-sm transition-all {disabled ? 'border-border opacity-40 cursor-not-allowed' : effectiveOperationMode === mode.value ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
						>
							{#if !operationModeOverrideTouched && getSettingsOperationMode() === mode.value}
								<span class="absolute top-1 right-1 text-[10px] text-primary bg-primary/10 px-1.5 py-0.5 rounded">{m.browse_op_default_badge()}</span>
							{/if}
							<div class="font-medium">{mode.label}</div>
							<div class="text-xs text-muted-foreground">{mode.desc}</div>
						</button>
					{/each}
				</div>
				{#if isInPlaceImplied}
					<p class="text-xs text-muted-foreground">
						{m.browse_in_place_implied_notice()} <button class="underline text-primary" onclick={() => { destinationPath = ''; localStorage.removeItem(STORAGE_KEY_OUTPUT); }}>{m.browse_change_destination()}</button>
					</p>
				{:else if operationModeOverrideTouched && effectiveOperationMode !== getSettingsOperationMode()}
					<p class="text-xs text-primary">
						{m.browse_overriding_settings()} <button class="underline" onclick={() => operationModeOverrideTouched = false}>{m.browse_reset_to_default()}</button>
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
						<h3 class="font-semibold">{m.browse_output_destination()}</h3>
					</div>
					<div class="flex gap-2">
						<PathInput
							bind:value={destinationPath}
							onchange={(v) => {
								localStorage.setItem(STORAGE_KEY_OUTPUT, v);
							}}
							placeholder={m.browse_destination_placeholder()}
							whitelistPaths={config?.api?.security?.allowed_directories ?? []}
							class="px-3 py-2"
						/>
						<Button onclick={openDestinationBrowser}>
							{#snippet children()}
								<FolderOpen class="h-4 w-4 mr-2" />
								{m.browse_browse_button()}
							{/snippet}
						</Button>
					</div>
					<p class="text-xs text-muted-foreground">
					{#if isInPlaceImplied}
						{m.browse_dest_in_place_note()}
					{:else}
						{m.browse_dest_organize_note()}
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
								{m.browse_selected_for_scraping({ count: selectedFiles.length })}
							</h3>
						</div>
						<Button
							variant="ghost"
							size="sm"
								onclick={clearSelection}
						>
							{#snippet children()}
								{m.browse_clear_all()}
							{/snippet}
						</Button>
					</div>

					<!-- Files List -->
					<div class="max-h-60 overflow-y-auto space-y-1 border rounded-md p-2 bg-accent/20">
						{#each selectedFiles as filePath (filePath)}
						{@const fileName = filePath.split(/[\\/]/).pop()}
						{@const dirPath = filePath.substring(0, Math.max(filePath.lastIndexOf('/'), filePath.lastIndexOf('\\')))}
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
										title={m.browse_remove_file()}
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
			whitelistPaths={config?.api?.security?.allowed_directories ?? []}
		/>

		<!-- Help Text -->
		<Card class="p-4 bg-accent/30">
			<h3 class="font-semibold mb-2">{m.browse_how_to_use()}</h3>
			<ul class="text-sm text-muted-foreground space-y-1">
				<li>{m.browse_howto_1()}</li>
				<li>{m.browse_howto_2()}</li>
				<li>{m.browse_howto_3()}</li>
				<li>{m.browse_howto_4()}</li>
				<li>{m.browse_howto_5()}</li>
			</ul>
			<p class="text-xs text-muted-foreground mt-3 pt-3 border-t border-border/50">
				{m.browse_tip_filter()}
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
					<h3 class="text-sm font-semibold">{m.browse_options()}</h3>
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
							class="h-4 w-4 rounded border-input text-primary focus:ring-2 focus:ring-primary"
						/>
						<div class="flex-1">
							<span class="text-sm font-medium">{m.browse_force_refresh()}</span>
							<p class="text-xs text-muted-foreground">{m.browse_force_refresh_desc()}</p>
						</div>
					</label>

					<label
						class="flex items-center gap-3 p-3 rounded-lg border border-border bg-background hover:bg-accent/50 cursor-pointer transition-colors"
					>
						<input
							type="checkbox"
							bind:checked={showScraperSelector}
							class="h-4 w-4 rounded border-input text-primary focus:ring-2 focus:ring-primary"
						/>
						<div class="flex-1">
							<span class="text-sm font-medium">{m.browse_manual_scraper_selection()}</span>
							<p class="text-xs text-muted-foreground">{m.browse_manual_scraper_selection_desc()}</p>
						</div>
					</label>

					<label
						class="flex items-center gap-3 p-3 rounded-lg border border-border bg-background hover:bg-accent/50 cursor-pointer transition-colors"
					>
						<input
							type="checkbox"
							bind:checked={manualScrapeMode}
							class="h-4 w-4 rounded border-input text-primary focus:ring-2 focus:ring-primary"
						/>
						<div class="flex-1">
							<span class="text-sm font-medium">{m.browse_manual_scrape()}</span>
							<p class="text-xs text-muted-foreground">{m.browse_manual_scrape_desc()}</p>
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
							{m.browse_files_selected_count({ count: selectedFiles.length })}
						</span>
						<button
									onclick={clearSelection}
							class="text-xs text-muted-foreground hover:text-destructive transition-colors"
						>
							{m.browse_clear_selection_link()}
						</button>
					</div>
				{:else}
					<span class="text-sm text-muted-foreground">{m.browse_no_files_selected()}</span>
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
							class="h-3.5 w-3.5 rounded border-input text-primary focus:ring-1 focus:ring-primary"
						/>
						<span class="text-muted-foreground hidden sm:inline">{m.browse_recursive()}</span>
					</label>
					<Button
						variant="outline"
						size="sm"
						onclick={() => triggerScan++}
						disabled={scanning}
						title={recursiveScan ? m.browse_scan_title_recursive() : m.browse_scan_title_current()}
					>
						{#snippet children()}
							{#if scanning}
								<LoaderCircle class="h-3.5 w-3.5 mr-1.5 animate-spin" />
							{:else}
								<Scan class="h-3.5 w-3.5 mr-1.5" />
							{/if}
							{scanning ? m.browse_scanning() : m.browse_scan_button()}
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
						{m.browse_options()}
						{#if showOptionsPanel}
							<ChevronDown class="h-4 w-4 ml-1" />
						{:else}
							<ChevronUp class="h-4 w-4 ml-1" />
						{/if}
					{/snippet}
				</Button>

				<!-- Active options indicators -->
				{#if manualScrapeMode || forceRefresh || showScraperSelector}
					<div class="hidden sm:flex items-center gap-1 text-xs">
						{#if manualScrapeMode}
							<span class="px-2 py-0.5 bg-primary/10 text-primary rounded">{m.browse_manual_badge()}</span>
						{/if}
						{#if forceRefresh}
							<span class="px-2 py-0.5 bg-primary/10 text-primary rounded">{m.browse_force_badge()}</span>
						{/if}
						{#if showScraperSelector}
							<span class="px-2 py-0.5 bg-primary/10 text-primary rounded">{m.browse_scrapers_count({ count: selectedScrapers.length })}</span>
						{/if}
					</div>
				{/if}

				<!-- Action button -->
				<Button onclick={manualScrapeMode ? continueToManual : startBatchScrape} disabled={selectedFiles.length === 0 || scraping}>
					{#snippet children()}
						{#if manualScrapeMode && !scraping}
							<FileEdit class="h-4 w-4 mr-2" />
						{:else if scraping}
							<LoaderCircle class="h-4 w-4 mr-2 animate-spin" />
						{:else if operationMode === 'update'}
							<RefreshCw class="h-4 w-4 mr-2" />
						{:else}
							<Play class="h-4 w-4 mr-2" />
						{/if}
						{#if manualScrapeMode && !scraping}
							{m.browse_continue_to_manual()}
						{:else if scraping}
							{m.browse_starting()}
						{:else if operationMode === 'update'}
							{m.browse_action_update({ count: selectedFiles.length })}
						{:else}
							{m.browse_action_scrape({ count: selectedFiles.length })}
						{/if}
					{/snippet}
				</Button>
			</div>
		</div>
	</div>
</div>

<!-- Destination Browser Modal -->
{#if showDestinationBrowser}
	<div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4" use:portalToBody in:fade|local={{ duration: 140 }} out:fade|local={{ duration: 120 }}>
		<div class="bg-background rounded-lg shadow-xl max-w-4xl w-full max-h-[80vh] flex flex-col" in:scale|local={{ start: 0.97, duration: 180, easing: quintOut }} out:scale|local={{ start: 1, opacity: 0.7, duration: 140, easing: quintOut }}>
			<!-- Modal Header -->
			<div class="p-6 border-b flex items-center justify-between">
				<div>
					<h2 class="text-xl font-bold">{m.browse_select_destination()}</h2>
					<p class="text-sm text-muted-foreground mt-1">
						{m.common_select_folder_desc()}
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
					whitelistPaths={config?.api?.security?.allowed_directories ?? []}
				/>
			</div>

			<!-- Modal Footer -->
			<div class="p-6 border-t space-y-3">
				<div class="flex items-center gap-2">
					<span class="text-sm font-medium text-muted-foreground">{m.browse_selected_path()}</span>
					<code
						class="flex-1 px-3 py-1.5 bg-accent rounded text-sm font-mono text-foreground overflow-x-auto"
					>
						{tempDestinationPath || initialPath}
					</code>
				</div>
				<div class="flex items-center justify-end gap-2">
					<Button variant="outline" onclick={cancelDestination}>
						{#snippet children()}
							{m.common_cancel()}
						{/snippet}
					</Button>
					<Button onclick={confirmDestination}>
						{#snippet children()}
							{m.browse_use_this_folder()}
						{/snippet}
					</Button>
				</div>
			</div>
		</div>
	</div>
{/if}
