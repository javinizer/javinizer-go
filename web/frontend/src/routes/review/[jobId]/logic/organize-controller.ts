import type {
	BatchJobResponse,
	Movie,
	OperationMode,
	ProgressMessage,
	UpdateRequest,
	ReviewApplyOverrides,
} from '$lib/api/types';

export type OrganizeOperation = 'move' | 'copy' | 'hardlink' | 'softlink';
export type OrganizeStatus = 'idle' | 'organizing' | 'completed' | 'failed';

export interface ApplyRecoveryState {
	jobId: string;
	operation: 'organize' | 'update';
	preApplyGeneration?: number;
	destination: string;
	skipNfo: boolean;
	skipDownload: boolean;
	overrides?: ReviewApplyOverrides;
	updateOptions?: UpdateRequest;
	failed: Record<string, string>;
	succeeded: string[];
	organizeOperation: OrganizeOperation;
	eligibleFilePaths: string[];
}

export interface FileStatus {
	status: string;
	error?: string;
}

interface OrganizeControllerDeps {
	getJobId: () => string;
	getIsUpdateMode: () => boolean;
	getJob: () => BatchJobResponse | null;
	setJob: (job: BatchJobResponse, expectedJobId?: string, expectedGeneration?: number) => void;
	getDestinationPath: () => string;
	getOrganizeOperation: () => OrganizeOperation;
	getOperationMode: () => string;
	getEditedMovies: () => Map<string, Movie>;
	saveAllEdits: () => Promise<void>;
	getOrganizeStatus: () => OrganizeStatus;
	setOrganizeStatus: (status: OrganizeStatus) => void;
	setOrganizing: (organizing: boolean) => void;
	setOrganizeProgress: (progress: number) => void;
	getFileStatuses: () => Map<string, FileStatus>;
	getExpectedOrganizeFilePaths: () => string[];
	setExpectedOrganizeFilePaths: (paths: string[]) => void;
	clearWebSocketMessages: () => void;
	toastSuccess: (message: string, duration?: number) => void;
	toastError: (message: string, duration?: number) => void;
	toastInfo: (message: string, duration?: number) => void;
	recordApplyFailure?: (filePath: string, error?: string) => void;
	recordApplySuccess?: (filePath: string) => void;
	clearApplyRecovery?: () => void;
	navigateBrowse: () => void;
	isCurrentOperation?: (jobId: string, generation: number) => boolean;
	getRouteGeneration?: () => number;
	api: {
		getBatchJob: (jobId: string, includeData?: boolean) => Promise<BatchJobResponse>;
		organizeBatchJob: (
			jobId: string,
			request: {
				destination: string;
				copy_only: boolean;
				link_mode?: 'hard' | 'soft';
				operation_mode?: OperationMode;
				skip_nfo?: boolean;
				skip_download?: boolean;
				overrides?: ReviewApplyOverrides;
				retry_file_paths?: string[];
			},
		) => Promise<unknown>;
		updateBatchJob: (jobId: string, request?: UpdateRequest) => Promise<unknown>;
	};
	pollIntervalMs?: number;
	pollTimeoutMs?: number;
	completionDelayMs?: number;
	redirectDelayMs?: number;
}

function isDefinitiveApplyLaunchRejection(error: unknown): boolean {
	if (!error || typeof error !== 'object') return false;
	if ('code' in error && error.code === 'APPLY_NOT_STARTED') return true;
	if (!('status' in error)) return false;
	const status = error.status;
	return status === 400 || status === 403 || status === 404 || status === 409;
}

function getOrganizeRequestOptions(operation: OrganizeOperation): {
	copyOnly: boolean;
	linkMode?: 'hard' | 'soft';
} {
	return {
		copyOnly: operation !== 'move',
		linkMode: operation === 'hardlink' ? 'hard' : operation === 'softlink' ? 'soft' : undefined,
	};
}

function getOrganizeEligibleFilePaths(batchJob: BatchJobResponse | null): string[] {
	if (!batchJob) return [];
	const excluded = batchJob.excluded || {};
	return Object.entries(batchJob.results || {})
		.filter(
			([filePath, result]) =>
				!excluded[filePath] && result.status === 'completed' && !!result.movie,
		)
		.map(([filePath]) => filePath);
}

export function createOrganizeController(deps: OrganizeControllerDeps) {
	const pollIntervalMs = deps.pollIntervalMs ?? 1500;
	const pollTimeoutMs = deps.pollTimeoutMs ?? 10 * 60 * 1000;
	const completionDelayMs = deps.completionDelayMs ?? 800;
	const redirectDelayMs = deps.redirectDelayMs ?? 5000;

	let organizePollTimer: ReturnType<typeof setTimeout> | null = null;
	let organizeRunToken = 0;
	let ignoreWebSocketMessages = false;
	let activeApplyGeneration: number | undefined;
	let organizeCompletionTimer: ReturnType<typeof setTimeout> | null = null;
	let organizeRedirectTimer: ReturnType<typeof setTimeout> | null = null;

	function clearOrganizePollTimer() {
		if (organizePollTimer !== null) {
			clearTimeout(organizePollTimer);
			organizePollTimer = null;
		}
	}

	function clearOrganizeCompletionTimer() {
		if (organizeCompletionTimer !== null) {
			clearTimeout(organizeCompletionTimer);
			organizeCompletionTimer = null;
		}
		if (organizeRedirectTimer !== null) {
			clearTimeout(organizeRedirectTimer);
			organizeRedirectTimer = null;
		}
	}

	function updateFileStatus(filePath: string, status: FileStatus) {
		deps.getFileStatuses().set(filePath, status);
	}

	function reconcileTerminalResults(job: BatchJobResponse, includeCompleted = true) {
		const expectedPaths = new Set(deps.getExpectedOrganizeFilePaths());
		for (const [filePath, result] of Object.entries(job.results ?? {})) {
			if (job.excluded?.[filePath] || !result.movie || !expectedPaths.has(filePath)) continue;
			if (result.status === 'failed') {
				updateFileStatus(filePath, { status: 'failed', error: result.error });
				deps.recordApplyFailure?.(filePath, result.error);
			} else if (
				includeCompleted &&
				result.status === 'completed' &&
				deps.getFileStatuses().get(filePath)?.status !== 'failed'
			) {
				updateFileStatus(filePath, { status: 'success' });
				deps.recordApplySuccess?.(filePath);
				forgetRecoveryRetryPath(filePath);
			}
		}
	}

	function finalizeOrganizeSuccess(message?: string) {
		if (deps.getOrganizeStatus() !== 'organizing' || organizeCompletionTimer !== null) {
			return;
		}

		clearOrganizePollTimer();
		ignoreWebSocketMessages = true;
		deps.setOrganizeProgress(100);

		organizeCompletionTimer = setTimeout(() => {
			organizeCompletionTimer = null;
			if (deps.getOrganizeStatus() !== 'organizing') return;

			deps.setOrganizeStatus('completed');
			deps.setOrganizing(false);

			if (deps.getFileStatuses().size === 0 && deps.getExpectedOrganizeFilePaths().length > 0) {
				const statuses = deps.getFileStatuses();
				for (const filePath of deps.getExpectedOrganizeFilePaths()) {
					statuses.set(filePath, { status: 'success' });
				}
			}

			const failures = Array.from(deps.getFileStatuses().values()).filter(
				(s) => s.status === 'failed',
			).length;
			if (failures === 0) {
				deps.clearApplyRecovery?.();
				const action = deps.getIsUpdateMode() ? 'updated' : 'organized';
				deps.toastSuccess(
					message || `All files ${action} successfully! Redirecting in 5 seconds...`,
					8000,
				);
				organizeRedirectTimer = setTimeout(() => deps.navigateBrowse(), redirectDelayMs);
			}
		}, completionDelayMs);
	}

	function finalizeOrganizeFailure(message: string) {
		if (deps.getOrganizeStatus() !== 'organizing') return;

		ignoreWebSocketMessages = true;
		clearOrganizePollTimer();
		clearOrganizeCompletionTimer();
		deps.setOrganizeStatus('failed');
		deps.setOrganizing(false);
		deps.toastError(message, 7000);
	}

	function startOrganizeCompletionPolling(
		operationJobId: string,
		operationGeneration: number,
		requestPending: () => boolean = () => false,
		preApplyGeneration?: number,
		applyAlreadyStarted = false,
	) {
		const runToken = ++organizeRunToken;
		const isActiveRun = () => runToken === organizeRunToken;
		clearOrganizePollTimer();
		const startedAt = Date.now();
		let lastPollError: string | null = null;
		let applyTransitionObserved = applyAlreadyStarted;

		const pollOnce = async () => {
			if (!isActiveRun()) return;
			if (
				deps.isCurrentOperation &&
				!deps.isCurrentOperation(operationJobId, operationGeneration)
			) {
				if (isActiveRun()) clearOrganizePollTimer();
				return;
			}
			if (deps.getOrganizeStatus() !== 'organizing') {
				clearOrganizePollTimer();
				return;
			}

			try {
				const latestJob = await deps.api.getBatchJob(operationJobId, true);
				if (!isActiveRun()) return;
				if (
					deps.isCurrentOperation &&
					!deps.isCurrentOperation(operationJobId, operationGeneration)
				) {
					if (isActiveRun()) clearOrganizePollTimer();
					return;
				}
				deps.setJob(latestJob, operationJobId, operationGeneration);
				lastPollError = null;
				const expectedApplyGeneration =
					preApplyGeneration !== undefined ? preApplyGeneration + 1 : undefined;
				// A request recorded at generation N owns only the next atomic claim,
				// N+1. A newer generation may belong to another tab or API client and
				// must never be adopted as this run's terminal result.
				const generationAdvanced =
					expectedApplyGeneration !== undefined &&
					latestJob.apply_generation === expectedApplyGeneration;
				const generationIsCurrent =
					expectedApplyGeneration !== undefined
						? latestJob.apply_generation === expectedApplyGeneration
						: latestJob.apply_generation !== undefined
							? activeApplyGeneration !== undefined
								? latestJob.apply_generation === activeApplyGeneration
								: applyAlreadyStarted
							: activeApplyGeneration === undefined && applyAlreadyStarted;
				if (generationAdvanced || (applyAlreadyStarted && generationIsCurrent)) {
					applyTransitionObserved = true;
					if (generationIsCurrent) {
						activeApplyGeneration = latestJob.apply_generation;
					}
				}
				const applyPhaseReached = applyAlreadyStarted ? generationIsCurrent : generationAdvanced;

				const terminalSuccess =
					latestJob.status === 'completed' ||
					latestJob.status === 'organized' ||
					latestJob.status === 'reverted';
				if (terminalSuccess && (!applyTransitionObserved || !generationIsCurrent)) {
					// A GET can race the POST and see the scrape-completed state. Do
					// not report apply success until the server exposes a new apply
					// generation.
				} else if (terminalSuccess) {
					reconcileTerminalResults(latestJob);
					const action = deps.getIsUpdateMode() ? 'Update' : 'Organization';
					finalizeOrganizeSuccess(`${action} completed successfully! Redirecting in 5 seconds...`);
					return;
				}

				if (latestJob.status === 'failed' && applyPhaseReached) {
					reconcileTerminalResults(latestJob, applyPhaseReached);
					const action = deps.getIsUpdateMode() ? 'update' : 'organization';
					finalizeOrganizeFailure(`The ${action} job failed.`);
					return;
				}

				if (latestJob.status === 'cancelled' && applyPhaseReached) {
					reconcileTerminalResults(latestJob, applyPhaseReached);
					const action = deps.getIsUpdateMode() ? 'Update' : 'Organization';
					finalizeOrganizeFailure(`${action} was cancelled.`);
					return;
				}
			} catch (e) {
				lastPollError = e instanceof Error ? e.message : String(e);
			}

			if (Date.now() - startedAt >= pollTimeoutMs) {
				const action = deps.getIsUpdateMode() ? 'Update' : 'Organization';
				const detail = lastPollError ? ` Last error: ${lastPollError}` : '';
				finalizeOrganizeFailure(`${action} timed out while waiting for completion.${detail}`);
				return;
			}

			if (!isActiveRun()) return;
			organizePollTimer = setTimeout(() => {
				void pollOnce();
			}, pollIntervalMs);
		};

		if (requestPending()) {
			organizePollTimer = setTimeout(() => {
				void pollOnce();
			}, pollIntervalMs);
		} else {
			void pollOnce();
		}
		return runToken;
	}

	function prepareOrganizeRun(extraPaths: string[] = []) {
		organizeRunToken += 1;
		ignoreWebSocketMessages = false;
		activeApplyGeneration = undefined;
		deps.clearWebSocketMessages();
		deps.setOrganizeStatus('organizing');
		deps.setOrganizing(true);
		deps.setOrganizeProgress(0);
		deps.getFileStatuses().clear();
		const eligiblePaths = new Set(getOrganizeEligibleFilePaths(deps.getJob()));
		for (const filePath of extraPaths) eligiblePaths.add(filePath);
		deps.setExpectedOrganizeFilePaths(Array.from(eligiblePaths));
		clearOrganizePollTimer();
		clearOrganizeCompletionTimer();
	}

	let lastUpdateOptions: UpdateRequest | undefined;
	let lastSkipNfo = false;
	let lastSkipDownload = false;
	let lastOrganizeOverrides: ReviewApplyOverrides | undefined;
	let lastOrganizeOperation: OrganizeOperation = 'move';
	let lastRecoveryFailedPaths: string[] = [];

	function forgetRecoveryRetryPath(filePath: string) {
		lastRecoveryFailedPaths = lastRecoveryFailedPaths.filter((path) => path !== filePath);
	}

	async function organizeAll(
		skipNfo?: boolean,
		skipDownload?: boolean,
		overrides?: ReviewApplyOverrides,
		retryPaths: string[] = [],
	) {
		const operationJobId = deps.getJobId();
		const operationGeneration = deps.getRouteGeneration?.() ?? 0;
		const operation = deps.getOrganizeOperation();
		const effectiveMode = deps.getOperationMode();
		const operationDestination = deps.getDestinationPath();
		const needsDestination = effectiveMode === 'organize';
		if (needsDestination && !operationDestination.trim()) {
			deps.clearApplyRecovery?.();
			deps.toastError('Please enter a destination path');
			return;
		}

		lastSkipNfo = skipNfo ?? false;
		lastSkipDownload = skipDownload ?? false;
		lastOrganizeOverrides = overrides;
		lastOrganizeOperation = operation;
		lastRecoveryFailedPaths = Array.from(new Set(retryPaths));

		const { copyOnly, linkMode } = getOrganizeRequestOptions(operation);
		const preApplyGeneration = deps.getJob()?.apply_generation;
		prepareOrganizeRun(retryPaths);
		const operationToken = organizeRunToken;
		let requestPending = false;
		let pollingRunToken = operationToken;

		try {
			if (deps.getEditedMovies().size > 0) {
				await deps.saveAllEdits();
			}

			if (operationToken !== organizeRunToken) return;
			if (deps.isCurrentOperation && !deps.isCurrentOperation(operationJobId, operationGeneration))
				return;

			requestPending = true;
			const request = deps.api.organizeBatchJob(operationJobId, {
				destination: operationDestination,
				copy_only: copyOnly,
				link_mode: linkMode,
				operation_mode: effectiveMode as OperationMode,
				skip_nfo: skipNfo || false,
				skip_download: skipDownload || false,
				overrides,
				retry_file_paths: retryPaths.length > 0 ? retryPaths : undefined,
			});
			pollingRunToken = startOrganizeCompletionPolling(
				operationJobId,
				operationGeneration,
				() => requestPending,
				preApplyGeneration,
			);
			await request;
			requestPending = false;

			if (pollingRunToken !== organizeRunToken) return;
			if (deps.isCurrentOperation && !deps.isCurrentOperation(operationJobId, operationGeneration))
				return;
			if (deps.getOrganizeStatus() !== 'organizing') return;
		} catch (e) {
			requestPending = false;
			if (pollingRunToken !== organizeRunToken) return;
			if (deps.isCurrentOperation && !deps.isCurrentOperation(operationJobId, operationGeneration))
				return;
			if (ignoreWebSocketMessages || deps.getOrganizeStatus() !== 'organizing') return;
			if (isDefinitiveApplyLaunchRejection(e)) deps.clearApplyRecovery?.();
			deps.setOrganizeStatus('failed');
			deps.setOrganizing(false);
			clearOrganizePollTimer();
			const errorMessage = e instanceof Error ? e.message : 'Failed to start organize';
			deps.toastError(errorMessage, 7000);
		}
	}

	async function updateAll(options?: UpdateRequest, retryPaths: string[] = []) {
		const operationJobId = deps.getJobId();
		const operationGeneration = deps.getRouteGeneration?.() ?? 0;
		lastRecoveryFailedPaths = Array.from(new Set(retryPaths));
		const preApplyGeneration = deps.getJob()?.apply_generation;
		prepareOrganizeRun(retryPaths);
		const operationToken = organizeRunToken;
		let requestPending = false;
		let pollingRunToken = operationToken;

		if (options) {
			lastUpdateOptions = options;
		}

		try {
			if (deps.getEditedMovies().size > 0) {
				await deps.saveAllEdits();
			}

			if (deps.isCurrentOperation && !deps.isCurrentOperation(operationJobId, operationGeneration))
				return;

			requestPending = true;
			const request =
				retryPaths.length > 0 ? { ...options, retry_file_paths: retryPaths } : options;
			pollingRunToken = startOrganizeCompletionPolling(
				operationJobId,
				operationGeneration,
				() => requestPending,
				preApplyGeneration,
			);
			await deps.api.updateBatchJob(operationJobId, request);
			requestPending = false;
			if (pollingRunToken !== organizeRunToken) return;
			if (deps.isCurrentOperation && !deps.isCurrentOperation(operationJobId, operationGeneration))
				return;
			if (deps.getOrganizeStatus() !== 'organizing') return;
		} catch (e) {
			requestPending = false;
			if (pollingRunToken !== organizeRunToken) return;
			if (deps.isCurrentOperation && !deps.isCurrentOperation(operationJobId, operationGeneration))
				return;
			if (ignoreWebSocketMessages || deps.getOrganizeStatus() !== 'organizing') return;
			if (isDefinitiveApplyLaunchRejection(e)) deps.clearApplyRecovery?.();
			deps.setOrganizeStatus('failed');
			deps.setOrganizing(false);
			clearOrganizePollTimer();
			const errorMessage = e instanceof Error ? e.message : 'Failed to start update';
			deps.toastError(errorMessage, 7000);
		}
	}

	async function retryFailed() {
		const failedCount = Array.from(deps.getFileStatuses().values()).filter(
			(s) => s.status === 'failed',
		).length;
		if (failedCount === 0) return;

		deps.toastInfo(`Retrying ${failedCount} failed file${failedCount > 1 ? 's' : ''}...`);

		const retryPaths = Array.from(deps.getFileStatuses().entries())
			.filter(([, status]) => status.status === 'failed')
			.map(([filePath]) => filePath);
		const allRetryPaths = Array.from(new Set([...lastRecoveryFailedPaths, ...retryPaths]));
		if (deps.getIsUpdateMode()) {
			await updateAll(lastUpdateOptions, allRetryPaths);
		} else {
			await organizeAll(lastSkipNfo, lastSkipDownload, lastOrganizeOverrides, allRetryPaths);
		}
	}

	function handleWebSocketMessage(msg: ProgressMessage | undefined) {
		if (
			!msg ||
			ignoreWebSocketMessages ||
			msg.job_id !== deps.getJobId() ||
			deps.getOrganizeStatus() !== 'organizing' ||
			activeApplyGeneration === undefined ||
			msg.apply_generation !== activeApplyGeneration
		) {
			return;
		}

		// Drive the progress bar ONLY from aggregate pending messages, not from
		// per-file or terminal messages. REST polling is authoritative for terminal
		// state, so delayed events from an older attempt cannot complete a retry. The
		// aggregate 'pending' stream (no file_path, emitted by
		// makeOrganizeProgressBroadcaster with a high-water mutex) carries incremental
		// monotonic progress (0->100 across files). Per-file messages —
		// 'organized'/'updated'/'failed' (terminal, for fileStatuses) AND
		// the in-flight 'Organizing <file>' start message ('pending', Progress:0,
		// WITH file_path, emitted by makeOrganizeFileStartBroadcaster) — must NOT
		// drive the bar: the start message's Progress:0 would flicker the bar back
		// to 0% at each file start (the iter-9 F-1 regression). Gate on !file_path
		// so only the aggregate (no FilePath) drives the bar.
		if (
			msg.progress !== undefined &&
			msg.progress !== null &&
			!msg.file_path &&
			msg.status === 'pending'
		) {
			deps.setOrganizeProgress(msg.progress);
		}

		if (msg.status === 'failed' && msg.file_path) {
			updateFileStatus(msg.file_path, { status: 'failed', error: msg.error });
			deps.recordApplyFailure?.(msg.file_path, msg.error);
			const fileName = msg.file_path.split(/[\\/]/).pop();
			const action = deps.getIsUpdateMode() ? 'update' : 'organize';
			deps.toastError(`Failed to ${action} ${fileName}: ${msg.error}`, 7000);
		}

		if ((msg.status === 'organized' || msg.status === 'updated') && msg.file_path) {
			updateFileStatus(msg.file_path, { status: 'success' });
			deps.recordApplySuccess?.(msg.file_path);
			forgetRecoveryRetryPath(msg.file_path);
		}
	}

	function cleanup() {
		organizeRunToken += 1;
		ignoreWebSocketMessages = true;
		activeApplyGeneration = undefined;
		clearOrganizePollTimer();
		clearOrganizeCompletionTimer();
	}

	function resumePolling(recovery?: ApplyRecoveryState) {
		const operationJobId = deps.getJobId();
		const operationGeneration = deps.getRouteGeneration?.() ?? 0;
		const job = deps.getJob();
		ignoreWebSocketMessages = false;
		activeApplyGeneration = job?.apply_generation;
		const hasRecordedApplyOutcome =
			(recovery && Object.keys(recovery.failed).length > 0) ||
			(recovery && recovery.succeeded.length > 0);
		const expectedApplyGeneration =
			recovery?.preApplyGeneration !== undefined ? recovery.preApplyGeneration + 1 : undefined;
		const applyAlreadyStarted =
			!recovery ||
			recovery.preApplyGeneration === undefined ||
			job?.apply_generation === undefined ||
			(expectedApplyGeneration !== undefined && job.apply_generation === expectedApplyGeneration) ||
			!!hasRecordedApplyOutcome;
		if (recovery) {
			lastSkipNfo = recovery.skipNfo;
			lastSkipDownload = recovery.skipDownload;
			lastOrganizeOverrides = recovery.overrides;
			lastOrganizeOperation = recovery.organizeOperation;
			lastRecoveryFailedPaths = Object.keys(recovery.failed);
			for (const [filePath, result] of Object.entries(job?.results ?? {})) {
				if (result.status === 'failed' && result.movie) lastRecoveryFailedPaths.push(filePath);
			}
			lastRecoveryFailedPaths = Array.from(new Set(lastRecoveryFailedPaths));
			lastUpdateOptions = recovery.updateOptions;
		}
		deps.clearWebSocketMessages();
		deps.setOrganizeStatus('organizing');
		deps.setOrganizing(true);
		// Scrape progress is normally 100 by the time apply starts. Apply progress
		// begins at zero and is driven by fresh aggregate WebSocket messages.
		deps.setOrganizeProgress(0);
		deps.getFileStatuses().clear();
		const eligiblePaths = new Set(getOrganizeEligibleFilePaths(job));
		for (const filePath of recovery?.eligibleFilePaths ?? []) eligiblePaths.add(filePath);
		deps.setExpectedOrganizeFilePaths(Array.from(eligiblePaths));
		for (const filePath of recovery?.succeeded ?? []) {
			deps.getFileStatuses().set(filePath, { status: 'success' });
		}
		for (const [filePath, error] of Object.entries(recovery?.failed ?? {})) {
			deps.getFileStatuses().set(filePath, { status: 'failed', error });
		}
		for (const [filePath, result] of Object.entries(job?.results ?? {})) {
			if (
				result.status === 'failed' &&
				eligiblePaths.has(filePath) &&
				!recovery?.failed[filePath]
			) {
				deps.getFileStatuses().set(filePath, { status: 'failed', error: result.error });
			}
		}
		startOrganizeCompletionPolling(
			operationJobId,
			operationGeneration,
			undefined,
			recovery?.preApplyGeneration,
			applyAlreadyStarted,
		);
	}

	return {
		organizeAll,
		updateAll,
		retryFailed,
		handleWebSocketMessage,
		cleanup,
		resumePolling,
	};
}
