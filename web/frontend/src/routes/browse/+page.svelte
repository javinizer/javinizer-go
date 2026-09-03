<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import { untrack } from 'svelte';
	import { MediaQuery } from 'svelte/reactivity';
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
	import VideoOperationSelector from './VideoOperationSelector.svelte';
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
	import { Play, FolderOutput, FolderOpen, FileEdit, FileText, RotateCcw, LoaderCircle, RefreshCw, Settings, ChevronUp, ChevronDown, X, Scan, FileX, Download, Replace, ImageOff, ListChecks, CircleAlert, TriangleAlert } from 'lucide-svelte';
	import type { Scraper, FileInfo, Config, BrowseResponse } from '$lib/api/types';
	import type { BatchApplyPlan, MediaPolicy, MergePreset, NFOOutputPolicy, OperationMode, ScalarMergeStrategy, ArrayMergeStrategy, VideoOperation } from '$lib/api/types';
	import { applyPlanSummary, applyPreset as applyPlanPreset, arrayStrategyLabel, defaultApplyPlan, initialApplyPlan, migrateLegacyPlan, normalizeApplyPlan, projectLegacyPlan, scalarStrategyLabel, setMergeStrategies, validateApplyPlan } from '$lib/apply-plan';
	import { BrowseBootstrapCookie, encodeBrowseBootstrap, type BrowseBootstrap } from '$lib/browse-bootstrap';

	let { data = {} }: { data?: { browseBootstrap?: BrowseBootstrap | null; initialPath?: string; initialBrowse?: BrowseResponse | null } } = $props();
	const browseBootstrap = untrack(() => {
		if (data.browseBootstrap) return data.browseBootstrap;
		if (typeof window !== 'undefined') {
			const ssr = (window as unknown as { __JAVINIZER_SSR__?: { browseBootstrap?: BrowseBootstrap | null } }).__JAVINIZER_SSR__;
			return ssr?.browseBootstrap ?? null;
		}
		return null;
	});
	const serverInitialPath = untrack(() => data.initialPath ?? '');
	const serverInitialBrowse = untrack(() => data.initialBrowse ?? null);
	let bootstrappedPlan: BatchApplyPlan | null = null;
	if (browseBootstrap?.applyPlan) {
		try { bootstrappedPlan = normalizeApplyPlan(browseBootstrap.applyPlan); } catch {}
	}

	type BrowseMode = 'scrape' | 'update';
	let selectedFiles: string[] = $state([]);
	let scraping = $state(false);
	let forceRefresh = $state(browseBootstrap?.forceRefresh ?? false);
	let operationMode: BrowseMode = $state('scrape');
	let scanning = $state(false);
	let recursiveScan = $state(false);
	let selectedFolders: string[] = $state([]);
	let triggerScan = $state(0);
	let initialPath = $state(serverInitialPath || browseBootstrap?.initialPath || '');
	let destinationPath = $state(browseBootstrap?.destinationPath ?? '');
	let showDestinationBrowser = $state(false);
	let tempDestinationPath = $state('');
	let currentBrowserPath = $state('');
	const configQuery = createConfigQuery();
	const scrapersQuery = createScrapersQuery();
	const queryClient = useQueryClient();
	const cwdQuery = createQuery(() => ({
		queryKey: ['cwd'],
		queryFn: () => apiClient.getCurrentWorkingDirectory(),
		enabled: !initialPath,
	}));

	let config = $derived(configQuery.data ?? null);
	let availableScrapers = $derived(scrapersQuery.data ?? []);
	let selectedScrapers: string[] = $state(browseBootstrap?.selectedScrapers ?? []);
	let showScraperSelector = $state(browseBootstrap?.showScraperSelector ?? false);
	let scrapersInitialized = $state((browseBootstrap?.selectedScrapers.length ?? 0) > 0 || browseBootstrap?.showScraperSelector === true);

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
		if (pathInitialized) return;
		const savedInputPath = localStorage.getItem(STORAGE_KEY_INPUT);
		const cwd = cwdQuery.data?.path;
		if (!initialPath) {
			initialPath = savedInputPath || cwd || '';
		}
		// Restore destination from localStorage even when cwdQuery is disabled
		// (initialPath was supplied by the SSR bootstrap). Without this, a
		// returning user's saved output dir is silently dropped.
		const savedOutputPath = localStorage.getItem(STORAGE_KEY_OUTPUT);
		if (!destinationPath) {
			destinationPath = savedOutputPath || initialPath;
		}
		// Only mark initialized once we've attempted the restore — if cwd
		// hasn't loaded yet and we have no initialPath, keep the effect open.
		if (initialPath || cwd) {
			pathInitialized = true;
		}
	});
	type ScalarStrategy = 'prefer-nfo' | 'prefer-scraper' | 'preserve-existing' | 'fill-missing-only';
	type ArrayStrategy = 'merge' | 'replace';

	let selectedPreset: string | undefined = $state(undefined);  // Merge strategy preset: conservative, gap-fill, aggressive
	let scalarStrategy: ScalarStrategy = $state('prefer-nfo');  // For scalar fields
	let arrayStrategy: ArrayStrategy = $state('merge');        // For array fields
	let showOptionsPanel = $state(false);  // Expandable options panel in sticky bar

	function closeOptionsPanel() {
		showOptionsPanel = false;
		queueMicrotask(() => document.getElementById('browse-options-trigger')?.focus());
	}

	function handleOptionsKeydown(event: KeyboardEvent) {
		if (event.key === 'Escape' && showOptionsPanel && !showDestinationBrowser) {
			event.preventDefault();
			closeOptionsPanel();
		}
	}
	let operationModeOverride: OperationMode = $state('organize');
	let operationModeOverrideTouched: boolean = $state(false);
	let manualScrapeMode: boolean = $state(browseBootstrap?.manualScrapeMode ?? false);
	let applyPlan: BatchApplyPlan | null = $state(bootstrappedPlan);
	let selectedVideoOperation: VideoOperation | null = $state(bootstrappedPlan?.video_operation ?? null);
	let planInitialized = $state(browseBootstrap !== null);
	let planMigrationWarning: string | undefined = $state(browseBootstrap?.planMigrationWarning);
	const prefersReducedMotion = new MediaQuery('(prefers-reduced-motion: reduce)');
	let planExpanded = $state(browseBootstrap?.planExpanded ?? true);
	let planErrors = $derived(validateApplyPlan(applyPlan));
	let planSummary = $derived(applyPlan ? applyPlanSummary(applyPlan) : []);
	let planDigest = $derived(planSummary.length > 0 ? planSummary.join(' · ') : m.browse_plan_digest_empty());
	let destinationMatchesSource = $derived.by(() => {
		const plan = applyPlan as BatchApplyPlan | null;
		return plan?.video_operation === 'organize' && selectedFiles.some((file) => { const normalized = file.replaceAll('\\', '/'); return normalized.slice(0, normalized.lastIndexOf('/')) === destinationPath.replaceAll('\\', '/').replace(/\/$/, ''); });
	});

	// D4: persist /browse scrape state to sessionStorage + hydrate on mount so a
	// Back round-trip from /manual preserves selection + globals. When hydrated
	// selectedScrapers is non-empty, scrapersInitialized is locked so the
	// all-enabled re-init on remount is skipped.
	const STORAGE_KEY_SCRAPE_STATE = 'javinizer_browse_scrape_state';

	interface BrowseScrapeState {
		version?: 2;
		applyPlan?: BatchApplyPlan | null;
		planMigrationWarning?: string;
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
		planExpanded?: boolean;
	}

	let scrapeStateHydrated = $state(false);
	let pendingLegacyState = $state<Partial<BrowseScrapeState> | null>(null);

	$effect(() => {
		if (scrapeStateHydrated) return;
		scrapeStateHydrated = true;
		if (typeof sessionStorage === 'undefined') return;
		try {
			const raw = sessionStorage.getItem(STORAGE_KEY_SCRAPE_STATE);
			if (!raw) return;
			const saved = JSON.parse(raw) as Partial<BrowseScrapeState>;
			if (saved.version === 2) {
				try {
					applyPlan = saved.applyPlan ? normalizeApplyPlan(saved.applyPlan) : null;
					planMigrationWarning = saved.planMigrationWarning;
				} catch {
					applyPlan = null;
					planMigrationWarning = m.apply_plan_warn_saved_unsupported();
				}
				selectedVideoOperation = applyPlan?.video_operation ?? null;
				if (applyPlan?.video_operation === 'organize') destinationPath = applyPlan.destination ?? '';
				planInitialized = true;
			} else {
				pendingLegacyState = saved;
			}
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
			if (typeof saved.planExpanded === 'boolean') planExpanded = saved.planExpanded;
		} catch {}
	});

	$effect(() => {
		if (!scrapeStateHydrated || !planInitialized) return;
		if (typeof sessionStorage === 'undefined') return;
		const state: BrowseScrapeState = {
			version: 2,
			applyPlan: applyPlan ? normalizeApplyPlan(applyPlan) : null,
			planMigrationWarning,
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
			manualScrapeMode,
			planExpanded
		};
		try {
			sessionStorage.setItem(STORAGE_KEY_SCRAPE_STATE, JSON.stringify(state));
			const bootstrap: BrowseBootstrap = {
				version: 1,
				applyPlan: state.applyPlan ?? null,
				planMigrationWarning,
				initialPath: currentBrowserPath || initialPath,
				destinationPath,
				forceRefresh,
				showScraperSelector,
				selectedScrapers,
				manualScrapeMode,
				planExpanded
			};
			document.cookie = `${BrowseBootstrapCookie}=${encodeBrowseBootstrap(bootstrap)}; Path=/; SameSite=Lax${typeof location !== 'undefined' && location.protocol === 'https:' ? '; Secure' : ''}`;
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

	$effect(() => {
		if (!config || !pathInitialized || planInitialized) return;
		if (pendingLegacyState) {
			const saved = pendingLegacyState;
			const impliedInPlace = destinationPath.trim() !== '' && destinationPath.trim() === initialPath.trim() && !config.output?.folder_format && (!config.output?.subfolder_format || config.output.subfolder_format.length === 0);
			let effectiveOperationMode: OperationMode | undefined;
			if (saved.operationMode !== 'update') {
				const savedOverride = saved.operationModeOverride;
				if (impliedInPlace && (savedOverride === 'organize' || savedOverride === 'in-place')) effectiveOperationMode = 'in-place-norenamefolder';
				else effectiveOperationMode = saved.operationModeOverrideTouched ? savedOverride : config.output?.operation_mode as OperationMode | undefined;
			}
			const migrated = migrateLegacyPlan({ browseMode: saved.operationMode, update: saved.operationMode === 'update', effectiveOperationMode, destination: destinationPath, scalarStrategy: saved.scalarStrategy, arrayStrategy: saved.arrayStrategy });
			applyPlan = migrated.plan;
			selectedVideoOperation = migrated.plan?.video_operation ?? null;
			planMigrationWarning = migrated.warning;
			pendingLegacyState = null;
		} else {
			applyPlan = initialApplyPlan(config.output?.operation_mode as OperationMode | undefined, destinationPath);
			selectedVideoOperation = applyPlan?.video_operation ?? null;
		}
		planInitialized = true;
	});

	$effect(() => {
		if (!selectedVideoOperation) {
			applyPlan = null;
			return;
		}
		if (!applyPlan || applyPlan.video_operation !== selectedVideoOperation) {
			applyPlan = defaultApplyPlan(selectedVideoOperation, destinationPath);
			planMigrationWarning = undefined;
		}
	});

	$effect(() => {
		if (applyPlan?.video_operation === 'organize' && applyPlan.destination !== destinationPath.trim()) {
			applyPlan = normalizeApplyPlan({ ...applyPlan, destination: destinationPath });
		}
	});

	function setNFOOutput(value: NFOOutputPolicy) { if (applyPlan) applyPlan = normalizeApplyPlan({ ...applyPlan, nfo_output: value }); }
	function setMediaPolicy(value: MediaPolicy) { if (applyPlan) applyPlan = normalizeApplyPlan({ ...applyPlan, media_policy: value }); }
	function choosePreset(value: MergePreset) { if (applyPlan) applyPlan = applyPlanPreset(applyPlan, value); }
	function chooseScalar(value: ScalarMergeStrategy) { if (applyPlan?.merge) applyPlan = setMergeStrategies(applyPlan, value, applyPlan.merge.array_strategy); }
	function chooseArray(value: ArrayMergeStrategy) { if (applyPlan?.merge) applyPlan = setMergeStrategies(applyPlan, applyPlan.merge.scalar_strategy, value); }

	const nfoChoices = [
		{ value: 'write' as NFOOutputPolicy, label: m.browse_plan_nfo_write(), icon: FileText },
		{ value: 'skip' as NFOOutputPolicy, label: m.browse_plan_nfo_skip(), icon: FileX }
	];
	let mediaChoices = $derived.by(() => {
		const plan = applyPlan as BatchApplyPlan | null;
		return [
			{ value: 'missing' as MediaPolicy, label: m.browse_plan_media_missing(), icon: Download, destructive: false },
			...(plan?.video_operation === 'leave-in-place' ? [{ value: 'replace' as MediaPolicy, label: m.browse_plan_media_replace(), icon: Replace, destructive: true }] : []),
			{ value: 'skip' as MediaPolicy, label: m.browse_plan_media_skip(), icon: ImageOff, destructive: false }
		];
	});
	const presetChoices = [
		{ value: 'conservative' as MergePreset, name: m.browse_plan_preset_conservative(), desc: m.browse_preset_conservative_desc(), mapping: `${scalarStrategyLabel('preserve-existing')} · ${arrayStrategyLabel('merge')}` },
		{ value: 'gap-fill' as MergePreset, name: m.browse_plan_preset_gap_fill(), desc: m.browse_preset_gap_fill_desc(), mapping: `${scalarStrategyLabel('fill-missing-only')} · ${arrayStrategyLabel('merge')}` },
		{ value: 'aggressive' as MergePreset, name: m.browse_plan_preset_aggressive(), desc: m.browse_preset_aggressive_desc(), mapping: `${scalarStrategyLabel('prefer-scraper')} · ${arrayStrategyLabel('replace')}` }
	];

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
		if (selectedFiles.length === 0 || !applyPlan || planErrors.length > 0) return;
		setPendingScrape(buildPendingScrapeSnapshot({
			files: selectedFiles,
			applyPlan: normalizeApplyPlan(applyPlan),
			migrationWarning: planMigrationWarning,
			showScraperSelector,
			selectedScrapers: showScraperSelector ? selectedScrapers : [],
			force: forceRefresh
		}));
		void goto('/manual');
	}

	async function startBatchScrape() {
		if (selectedFiles.length === 0 || !applyPlan || planErrors.length > 0) return;
		const plan = normalizeApplyPlan(applyPlan);
		const legacy = projectLegacyPlan(plan);
		const isUpdateMode = plan.video_operation === 'leave-in-place';
		scraping = true;
		try {
			const response = await apiClient.batchScrape({
				files: selectedFiles,
				strict: false,
				force: forceRefresh,
				...legacy,
				apply_plan: plan,
				selected_scrapers: showScraperSelector ? selectedScrapers : undefined
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
		currentBrowserPath = '';
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

<svelte:window onkeydown={handleOptionsKeydown} />

<div class="w-full px-4 py-8 pb-32 lg:px-6">
	<div class="space-y-6">
		<!-- Header -->
		<div class="flex flex-wrap items-start justify-between gap-3">
			<div class="min-w-0">
				<p class="font-mono text-[0.6875rem] font-medium uppercase tracking-[0.18em] text-muted-foreground">{m.browse_eyebrow()}</p>
				<h1 class="mt-1 text-2xl font-semibold tracking-tight sm:text-3xl">{m.browse_title()}</h1>
				<p class="mt-1 max-w-2xl text-sm text-muted-foreground">
					{m.browse_subtitle()}
				</p>
			</div>
			<Button variant="outline" onclick={resetDirectories} class="shrink-0">
				{#snippet children()}
					<RotateCcw class="h-4 w-4 mr-2" />
					{m.browse_reset_paths()}
				{/snippet}
			</Button>
		</div>

		<!-- Canonical apply plan -->
		<Card class="overflow-hidden">
			<div class="flex flex-wrap items-start justify-between gap-3 border-b bg-muted-faint px-5 py-4 {planExpanded ? 'border-border' : 'border-transparent'}">
				<div class="min-w-0 flex-1">
					<h2 id="apply-plan-title" class="font-mono text-[0.6875rem] font-semibold uppercase tracking-[0.18em] text-muted-foreground">{m.browse_plan_eyebrow()}</h2>
					{#if planExpanded}
						<p class="mt-1 min-h-5 max-w-2xl text-sm leading-5 text-muted-foreground">{m.browse_plan_intro()}</p>
					{:else}
						<p class="plan-motion mt-1 flex min-h-5 items-center gap-2 font-mono text-xs leading-5 text-muted-foreground" in:fade|local={{ duration: prefersReducedMotion.current ? 0 : 140 }}>
							<ListChecks class="h-3.5 w-3.5 shrink-0 text-primary" aria-hidden="true" />
							<span class="truncate" title={planDigest}>{planDigest}</span>
						</p>
					{/if}
				</div>
				<div class="flex shrink-0 items-center gap-2">
					{#if !planExpanded && planErrors.length > 0}
						<span role="status" class="flex items-center gap-1.5 rounded border border-destructive-soft bg-destructive-faint px-1.5 py-0.5 font-mono text-[0.6875rem] text-destructive" aria-label={planErrors.join('. ')}>
							<CircleAlert class="h-3 w-3" aria-hidden="true" />
							<span class="tabular-nums">{planErrors.length}</span>
						</span>
					{/if}
					<Button variant="outline" size="sm" class="w-36 justify-between" aria-expanded={planExpanded} aria-controls="apply-plan-body" onclick={() => planExpanded = !planExpanded}>
						{#snippet children()}
							{planExpanded ? m.browse_plan_collapse() : m.browse_plan_expand()}
							{#if planExpanded}<ChevronUp class="h-4 w-4 ml-1" aria-hidden="true" />{:else}<ChevronDown class="h-4 w-4 ml-1" aria-hidden="true" />{/if}
						{/snippet}
					</Button>
				</div>
			</div>

			{#if planMigrationWarning}
				<div class="border-b border-amber-500/30 bg-amber-500/10 px-5 py-3 text-sm" role="alert">
					<span class="flex items-start gap-2"><TriangleAlert class="mt-0.5 h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400" aria-hidden="true" /><span class="min-w-0">{planMigrationWarning}</span></span>
				</div>
			{/if}

			{#if planExpanded}
				<div id="apply-plan-body" role="region" aria-labelledby="apply-plan-title" class="plan-motion space-y-8 p-5 sm:p-6" transition:slide|local={{ duration: prefersReducedMotion.current ? 0 : 180, easing: quintOut }}>
				<div class="grid gap-3 sm:grid-cols-[1.75rem_minmax(0,1fr)] sm:gap-5">
					<div class="flex sm:flex-col sm:items-center" aria-hidden="true">
						<span class="font-mono text-xs font-medium tabular-nums text-muted-foreground sm:pt-0.5">01</span>
						<span class="mt-2 hidden w-px flex-1 bg-border sm:block"></span>
					</div>
					<div class="min-w-0">
						<VideoOperationSelector bind:value={selectedVideoOperation} errorId={planErrors.length > 0 ? 'apply-plan-errors' : undefined} renameFile={config?.output?.rename_file} />
					</div>
				</div>

				{#if applyPlan}
					<div class="grid gap-3 sm:grid-cols-[1.75rem_minmax(0,1fr)] sm:gap-5">
						<div class="flex sm:flex-col sm:items-center" aria-hidden="true">
							<span class="font-mono text-xs font-medium tabular-nums text-muted-foreground sm:pt-0.5">02</span>
							<span class="mt-2 hidden w-px flex-1 bg-border sm:block"></span>
						</div>
						<div class="min-w-0 space-y-3">
							<h3 class="text-sm font-semibold">{m.browse_plan_outputs()}</h3>
							<div class="grid gap-4 lg:grid-cols-2">
								<fieldset class="min-w-0 space-y-2">
									<legend class="text-sm font-medium">{m.browse_plan_nfo_output()}</legend>
									<div class="flex flex-col gap-1 rounded-lg border border-border bg-muted-soft p-1 sm:flex-row">
										{#each nfoChoices as choice}
											{@const active = applyPlan.nfo_output === choice.value}
											<label class="min-w-0 flex-1">
												<input type="radio" name="nfo-output" class="peer sr-only" checked={active} onchange={() => setNFOOutput(choice.value)} />
												<span class="flex min-h-10 cursor-pointer select-none items-center justify-center gap-2 rounded-md px-3 text-sm font-medium transition-colors peer-focus-visible:ring-2 peer-focus-visible:ring-ring peer-focus-visible:ring-offset-1 peer-focus-visible:ring-offset-background {active ? 'bg-background text-foreground shadow-sm ring-1 ring-primary-soft' : 'text-muted-foreground hover:bg-canvas-soft hover:text-foreground'}">
													<choice.icon class="h-4 w-4 shrink-0 {active ? 'text-primary' : ''}" aria-hidden="true" />
													<span class="truncate">{choice.label}</span>
												</span>
											</label>
										{/each}
									</div>
								</fieldset>

								<fieldset class="min-w-0 space-y-2">
									<legend class="text-sm font-medium">{m.browse_plan_media_policy()}</legend>
									<div class="flex flex-col gap-1 rounded-lg border border-border bg-muted-soft p-1 sm:flex-row">
										{#each mediaChoices as choice}
											{@const active = applyPlan.media_policy === choice.value}
											<label class="min-w-0 flex-1">
												<input type="radio" name="media-policy" class="peer sr-only" checked={active} onchange={() => setMediaPolicy(choice.value)} />
												<span class="flex min-h-10 cursor-pointer select-none items-center justify-center gap-2 rounded-md px-3 text-sm font-medium transition-colors peer-focus-visible:ring-2 peer-focus-visible:ring-ring peer-focus-visible:ring-offset-1 peer-focus-visible:ring-offset-background {active ? choice.destructive ? 'bg-destructive-soft text-destructive shadow-sm ring-1 ring-destructive-soft' : 'bg-background text-foreground shadow-sm ring-1 ring-primary-soft' : 'text-muted-foreground hover:bg-canvas-soft hover:text-foreground'}">
													<choice.icon class="h-4 w-4 shrink-0 {active ? choice.destructive ? 'text-destructive' : 'text-primary' : ''}" aria-hidden="true" />
													<span class="truncate">{choice.label}</span>
												</span>
											</label>
										{/each}
									</div>
								</fieldset>
							</div>
						</div>
					</div>

					{#if applyPlan.video_operation === 'organize'}
						<div class="grid gap-3 sm:grid-cols-[1.75rem_minmax(0,1fr)] sm:gap-5">
							<div class="flex sm:flex-col sm:items-center" aria-hidden="true">
								<span class="font-mono text-xs font-medium tabular-nums text-muted-foreground sm:pt-0.5">03</span>
								<span class="mt-2 hidden w-px flex-1 bg-border sm:block"></span>
							</div>
							<div class="min-w-0 space-y-2">
								<div class="flex items-center gap-2">
									<FolderOutput class="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
									<label class="text-sm font-semibold" for="apply-destination">{m.browse_output_destination()}</label>
								</div>
								<div class="flex flex-col gap-2 sm:flex-row">
									<PathInput id="apply-destination" bind:value={destinationPath} onchange={(v) => localStorage.setItem(STORAGE_KEY_OUTPUT, v)} placeholder={m.browse_destination_placeholder()} whitelistPaths={config?.api?.security?.allowed_directories ?? []} ariaDescribedby={planErrors.length > 0 ? 'apply-plan-errors' : undefined} ariaInvalid={planErrors.some((error) => error.toLowerCase().includes('destination'))} required={true} class="px-3 py-2" />
									<Button onclick={openDestinationBrowser} class="w-full shrink-0 sm:w-auto">{#snippet children()}<FolderOpen class="h-4 w-4 mr-2" />{m.browse_browse_button()}{/snippet}</Button>
								</div>
								{#if destinationMatchesSource}<p class="flex items-start gap-2 text-sm text-amber-700 dark:text-amber-300" role="status"><TriangleAlert class="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" /><span class="min-w-0">{m.browse_plan_same_source_warning()}</span></p>{/if}
							</div>
						</div>
					{:else if applyPlan.video_operation === 'leave-in-place' && applyPlan.merge}
						<div class="grid gap-3 sm:grid-cols-[1.75rem_minmax(0,1fr)] sm:gap-5">
							<div class="flex sm:flex-col sm:items-center" aria-hidden="true">
								<span class="font-mono text-xs font-medium tabular-nums text-muted-foreground sm:pt-0.5">03</span>
								<span class="mt-2 hidden w-px flex-1 bg-border sm:block"></span>
							</div>
							<div class="min-w-0 space-y-4">
								<div>
									<h3 class="text-sm font-semibold">{m.browse_plan_existing_merge()}</h3>
									<p class="mt-0.5 text-sm text-muted-foreground">{m.browse_merge_strategy_desc()}</p>
								</div>
								<div class="grid gap-2 sm:grid-cols-3" role="group" aria-label={m.browse_quick_presets()}>
									{#each presetChoices as preset}
										{@const active = applyPlan.merge?.source_preset === preset.value}
										<button type="button" aria-pressed={active} class="rounded-lg border p-3 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:ring-offset-background {active ? 'border-primary-strong bg-primary-soft shadow-sm' : 'border-border hover:border-primary-soft hover:bg-accent-soft'}" onclick={() => choosePreset(preset.value)}>
											<span class="block text-sm font-semibold">{preset.name}</span>
											<span class="mt-0.5 block text-xs text-muted-foreground">{preset.desc}</span>
											<span class="mt-2 block font-mono text-[0.6875rem] tracking-tight text-muted-foreground opacity-80">{preset.mapping}</span>
										</button>
									{/each}
								</div>
								<div class="grid gap-3 sm:grid-cols-2">
									<label class="block text-sm font-medium">{m.browse_scalar_fields()}<select class="mt-1.5 h-10 w-full rounded-md border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" value={applyPlan.merge.scalar_strategy} onchange={(e) => chooseScalar(e.currentTarget.value as ScalarMergeStrategy)}><option value="prefer-nfo">{m.browse_prefer_nfo()}</option><option value="prefer-scraper">{m.browse_prefer_scraped()}</option><option value="preserve-existing">{m.browse_preserve_existing()}</option><option value="fill-missing-only">{m.browse_fill_missing_only()}</option></select></label>
									<label class="block text-sm font-medium">{m.browse_array_fields()}<select class="mt-1.5 h-10 w-full rounded-md border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" value={applyPlan.merge.array_strategy} onchange={(e) => chooseArray(e.currentTarget.value as ArrayMergeStrategy)}><option value="merge">{m.browse_merge()}</option><option value="replace">{m.browse_replace()}</option></select></label>
								</div>
							</div>
						</div>
					{/if}

					<div class="grid gap-3 sm:grid-cols-[1.75rem_minmax(0,1fr)] sm:gap-5">
						<div class="hidden sm:flex sm:flex-col sm:items-center" aria-hidden="true">
							<span class="grid h-5 w-5 place-items-center rounded border border-primary-soft bg-primary-soft text-primary"><ListChecks class="h-3 w-3" /></span>
						</div>
						<div class="min-w-0">
							<div class="rounded-lg border border-dashed border-primary-soft bg-primary-faint p-4" aria-live="polite">
								<div class="flex items-center gap-2">
									<ListChecks class="h-4 w-4 shrink-0 text-primary" aria-hidden="true" />
									<h3 class="font-mono text-xs font-semibold uppercase tracking-[0.14em]">{m.browse_plan_summary_title()}</h3>
								</div>
								<ul class="mt-3 space-y-1.5 font-mono text-[0.8125rem] leading-relaxed text-foreground">
									{#each planSummary as line}<li class="flex gap-2"><span class="shrink-0 text-primary" aria-hidden="true">✓</span><span class="min-w-0 break-words">{line}</span></li>{/each}
								</ul>
							</div>
						</div>
					</div>
				{/if}

				{#if planErrors.length > 0}
						<div class="rounded-lg border border-destructive-soft bg-destructive-faint px-4 py-3">
							<ul id="apply-plan-errors" class="space-y-1 text-sm text-destructive" role="alert">
								{#each planErrors as error}<li class="flex items-start gap-2"><CircleAlert class="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" /><span class="min-w-0">{error}</span></li>{/each}
							</ul>
						</div>
					{/if}
				</div>
			{/if}
		</Card>

	<!-- Selected Files List -->
	{#if selectedFiles.length > 0}
		<div transition:fade|local={{ duration: 180 }}>
		<Card class="overflow-hidden">
				<div class="flex items-center justify-between gap-3 border-b border-border bg-muted-faint px-4 py-3">
					<div class="flex min-w-0 items-center gap-2">
						<div class="plan-motion w-2 h-2 rounded-full bg-primary animate-pulse"></div>
						<h3 class="truncate text-sm font-semibold">
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
				<div class="max-h-60 overflow-y-auto space-y-1 p-2">
					{#each selectedFiles as filePath (filePath)}
					{@const fileName = filePath.split(/[\\/]/).pop()}
					{@const dirPath = filePath.substring(0, Math.max(filePath.lastIndexOf('/'), filePath.lastIndexOf('\\')))}
						<div animate:flip={{ duration: 220, easing: quintOut }}>
							<div
								class="flex items-center justify-between rounded-md border border-border bg-background px-3 py-2 transition-colors hover:border-primary-soft group"
							>
								<div class="flex-1 min-w-0">
									<div class="truncate font-mono text-[0.8125rem] font-medium" title={fileName}>{fileName}</div>
									<div class="truncate font-mono text-xs text-muted-foreground" title={dirPath}>
										{dirPath}
									</div>
								</div>
								<button
									onclick={(e) => {
										e.stopPropagation();
										selectedFiles = selectedFiles.filter((f) => f !== filePath);
									}}
									class="ml-2 grid h-11 w-11 shrink-0 place-items-center rounded text-destructive transition-colors hover:bg-destructive-soft focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
									title={m.browse_remove_file()}
								>
									<X class="h-4 w-4" aria-hidden="true" />
								</button>
							</div>
						</div>
					{/each}
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
			initialData={serverInitialBrowse ?? undefined}
		/>

		<!-- Help Text -->
		<Card class="p-5">
			<h3 class="font-mono text-[0.6875rem] font-semibold uppercase tracking-[0.18em] text-muted-foreground">{m.browse_how_to_use()}</h3>
			<ul class="mt-3 text-sm text-muted-foreground space-y-1.5">
				<li>{m.browse_howto_1()}</li>
				<li>{m.browse_howto_2()}</li>
				<li>{m.browse_howto_3()}</li>
				<li>{m.browse_howto_4()}</li>
				<li>{m.browse_howto_5()}</li>
			</ul>
			<p class="text-xs text-muted-foreground mt-3 pt-3 border-t border-border/50 font-mono">
				{m.browse_tip_filter()}
			</p>
		</Card>
	</div>
</div>

<!-- Sticky Bottom Action Bar -->
<div class="sticky bottom-0 left-0 right-0 z-40 border-t border-border bg-bar shadow-[0_-12px_32px_-16px_rgb(0_0_0/0.35)] backdrop-blur-sm">
	<!-- Expandable Options Panel -->
	{#if showOptionsPanel}
		<div id="browse-options-panel" class="plan-motion absolute bottom-full left-3 right-3 mb-2 max-h-[min(65vh,32rem)] overflow-y-auto rounded-lg border border-border bg-background shadow-xl sm:left-auto sm:right-4 sm:w-[32rem]" role="region" aria-labelledby="browse-options-title" transition:slide|local={{ duration: prefersReducedMotion.current ? 0 : 180, easing: quintOut }}>
			<div class="p-3">
				<div class="mb-2 flex items-center justify-between">
					<h3 id="browse-options-title" class="text-sm font-semibold">{m.browse_options()}</h3>
					<button
						type="button"
						onclick={closeOptionsPanel}
						class="grid h-9 w-9 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-accent-soft hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
						aria-label={m.common_close()}
					>
						<X class="h-4 w-4" aria-hidden="true" />
					</button>
				</div>
				<div class="grid gap-2 sm:grid-cols-2">
					<label
						class="flex min-h-16 cursor-pointer items-center gap-3 rounded-md border border-border bg-background p-2.5 transition-colors hover:border-primary-soft hover:bg-accent-soft"
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
						class="flex min-h-16 cursor-pointer items-center gap-3 rounded-md border border-border bg-background p-2.5 transition-colors hover:border-primary-soft hover:bg-accent-soft"
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
						class="flex min-h-16 cursor-pointer items-center gap-3 rounded-md border border-border bg-background p-2.5 transition-colors hover:border-primary-soft hover:bg-accent-soft"
					>
						<input
							type="checkbox"
							bind:checked={manualScrapeMode}
							class="h-4 w-4 rounded border-input text-primary focus:ring-2 focus:ring-primary"
						/>
						<div class="flex-1">
							<span class="text-sm font-medium">{m.browse_plan_manual_ids_urls()}</span>
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
	<div class="w-full px-4 py-3">
		<div class="flex justify-end">
			<!-- Right: Scan, options toggle and action button -->
			<div class="flex flex-wrap items-center justify-end gap-2 sm:flex-nowrap sm:gap-3">
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
					id="browse-options-trigger"
					aria-expanded={showOptionsPanel}
					aria-controls="browse-options-panel"
					aria-haspopup="true"
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
							<span class="rounded border border-primary-soft bg-primary-tint px-1.5 py-0.5 font-mono text-[0.6875rem] text-primary">{m.browse_manual_badge()}</span>
						{/if}
						{#if forceRefresh}
							<span class="rounded border border-primary-soft bg-primary-tint px-1.5 py-0.5 font-mono text-[0.6875rem] text-primary">{m.browse_force_badge()}</span>
						{/if}
						{#if showScraperSelector}
							<span class="rounded border border-primary-soft bg-primary-tint px-1.5 py-0.5 font-mono text-[0.6875rem] text-primary">{m.browse_scrapers_count({ count: selectedScrapers.length })}</span>
						{/if}
					</div>
				{/if}

				<!-- Action button -->
				<Button onclick={manualScrapeMode ? continueToManual : startBatchScrape} disabled={selectedFiles.length === 0 || scraping || !applyPlan || planErrors.length > 0}>
					{#snippet children()}
						{#if manualScrapeMode && !scraping}
							<FileEdit class="h-4 w-4 mr-2" />
						{:else if scraping}
							<LoaderCircle class="h-4 w-4 mr-2 animate-spin" />
						{:else if applyPlan?.video_operation === 'leave-in-place'}
							<RefreshCw class="h-4 w-4 mr-2" />
						{:else}
							<Play class="h-4 w-4 mr-2" />
						{/if}
						{#if manualScrapeMode && !scraping}
							{m.browse_continue_to_manual()}
						{:else if scraping}
							{m.browse_starting()}
						{:else if applyPlan?.video_operation === 'leave-in-place'}
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

<style>
	@media (prefers-reduced-motion: reduce) {
		.plan-motion {
			animation: none !important;
			transition: none !important;
		}
	}
</style>
