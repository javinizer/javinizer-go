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

	function finalizeOrganizeSuccess(message?: string) {
		if (deps.getOrganizeStatus() !== 'organizing' || organizeCompletionTimer !== null) {
			return;
		}

		clearOrganizePollTimer();
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

		clearOrganizePollTimer();
		clearOrganizeCompletionTimer();
		deps.setOrganizeStatus('failed');
		deps.setOrganizing(false);
		deps.toastError(message, 7000);
	}

	function startOrganizeCompletionPolling(operationJobId: string, operationGeneration: number) {
		const runToken = ++organizeRunToken;
		const isActiveRun = () => runToken === organizeRunToken;
		clearOrganizePollTimer();
		const startedAt = Date.now();
		let lastPollError: string | null = null;

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

				if (
					latestJob.status === 'completed' ||
					latestJob.status === 'organized' ||
					latestJob.status === 'reverted'
				) {
					// 'organized' and 'reverted' are terminal-success statuses set by the
					// backend on successful organize/revert (see BatchJob.MarkOrganized /
					// MarkReverted). The backend's real-time WebSocket broadcast of
					// {status:'organization_completed'} is the primary completion
					// signal — this poll branch is a fallback that must also recognize
					// these statuses, or the UI polls forever after a successful
					// organize (job never reaches 'completed' because MarkCompleted is
					// guarded against transitioning FROM 'organized'/'reverted').
					const action = deps.getIsUpdateMode() ? 'Update' : 'Organization';
					finalizeOrganizeSuccess(`${action} completed successfully! Redirecting in 5 seconds...`);
					return;
				}

				if (latestJob.status === 'failed') {
					const action = deps.getIsUpdateMode() ? 'update' : 'organization';
					finalizeOrganizeFailure(`The ${action} job failed.`);
					return;
				}

				if (latestJob.status === 'cancelled') {
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

		void pollOnce();
	}

	function prepareOrganizeRun(extraPaths: string[] = []) {
		organizeRunToken += 1;
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
		lastRecoveryFailedPaths = retryPaths;

		const { copyOnly, linkMode } = getOrganizeRequestOptions(operation);
		prepareOrganizeRun(retryPaths);
		const operationToken = organizeRunToken;

		try {
			if (deps.getEditedMovies().size > 0) {
				await deps.saveAllEdits();
			}

			if (operationToken !== organizeRunToken) return;
			if (deps.isCurrentOperation && !deps.isCurrentOperation(operationJobId, operationGeneration))
				return;

			await deps.api.organizeBatchJob(operationJobId, {
				destination: operationDestination,
				copy_only: copyOnly,
				link_mode: linkMode,
				operation_mode: effectiveMode as OperationMode,
				skip_nfo: skipNfo || false,
				skip_download: skipDownload || false,
				overrides,
				retry_file_paths: retryPaths.length > 0 ? retryPaths : undefined,
			});

			if (operationToken !== organizeRunToken) return;
			if (deps.isCurrentOperation && !deps.isCurrentOperation(operationJobId, operationGeneration))
				return;
			startOrganizeCompletionPolling(operationJobId, operationGeneration);
		} catch (e) {
			if (operationToken !== organizeRunToken) return;
			if (deps.isCurrentOperation && !deps.isCurrentOperation(operationJobId, operationGeneration))
				return;
			deps.clearApplyRecovery?.();
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
		lastRecoveryFailedPaths = retryPaths;
		prepareOrganizeRun(retryPaths);
		const operationToken = organizeRunToken;

		if (options) {
			lastUpdateOptions = options;
		}

		try {
			if (deps.getEditedMovies().size > 0) {
				await deps.saveAllEdits();
			}

			if (deps.isCurrentOperation && !deps.isCurrentOperation(operationJobId, operationGeneration))
				return;

			const request =
				retryPaths.length > 0 ? { ...options, retry_file_paths: retryPaths } : options;
			await deps.api.updateBatchJob(operationJobId, request);
			if (operationToken !== organizeRunToken) return;
			if (deps.isCurrentOperation && !deps.isCurrentOperation(operationJobId, operationGeneration))
				return;
			startOrganizeCompletionPolling(operationJobId, operationGeneration);
		} catch (e) {
			if (operationToken !== organizeRunToken) return;
			if (deps.isCurrentOperation && !deps.isCurrentOperation(operationJobId, operationGeneration))
				return;
			deps.clearApplyRecovery?.();
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
		if (!msg || msg.job_id !== deps.getJobId() || deps.getOrganizeStatus() !== 'organizing') {
			return;
		}

		// Drive the progress bar ONLY from the aggregate progress-stream messages,
		// not from per-file messages. The aggregate 'pending' stream (no file_path,
		// emitted by makeOrganizeProgressBroadcaster with a high-water mutex) carries
		// incremental monotonic progress (0->100 across files); the terminal
		// 'organization_completed'/'update_completed' messages carry 100. Per-file
		// messages — 'organized'/'updated'/'failed' (terminal, for fileStatuses) AND
		// the in-flight 'Organizing <file>' start message ('pending', Progress:0,
		// WITH file_path, emitted by makeOrganizeFileStartBroadcaster) — must NOT
		// drive the bar: the start message's Progress:0 would flicker the bar back
		// to 0% at each file start (the iter-9 F-1 regression). Gate on !file_path
		// so only the aggregate (no FilePath) drives the bar.
		if (
			msg.progress !== undefined &&
			msg.progress !== null &&
			!msg.file_path &&
			(msg.status === 'pending' ||
				msg.status === 'organization_completed' ||
				msg.status === 'update_completed')
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

		if (msg.status === 'error' && !msg.file_path) {
			finalizeOrganizeFailure(msg.message || 'Operation failed');
			return;
		}

		if (msg.status === 'cancelled' && !msg.file_path) {
			const action = deps.getIsUpdateMode() ? 'Update' : 'Organization';
			finalizeOrganizeFailure(`${action} was cancelled.`);
			return;
		}

		if ((msg.status === 'organized' || msg.status === 'updated') && msg.file_path) {
			updateFileStatus(msg.file_path, { status: 'success' });
			deps.recordApplySuccess?.(msg.file_path);
		}

		if (msg.status === 'organization_completed' || msg.status === 'update_completed') {
			finalizeOrganizeSuccess(msg.message);
		}
	}

	function cleanup() {
		organizeRunToken += 1;
		clearOrganizePollTimer();
		clearOrganizeCompletionTimer();
	}

	function resumePolling(recovery?: ApplyRecoveryState) {
		const operationJobId = deps.getJobId();
		const operationGeneration = deps.getRouteGeneration?.() ?? 0;
		const job = deps.getJob();
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
		startOrganizeCompletionPolling(operationJobId, operationGeneration);
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
