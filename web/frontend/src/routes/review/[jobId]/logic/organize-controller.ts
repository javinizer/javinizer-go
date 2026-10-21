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
	acceptedApplyGeneration?: number;
	destination: string;
	operationMode?: string;
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
	recordApplyAccepted?: (generation: number) => void;
	recordApplyFailure?: (filePath: string, error?: string) => void;
	recordApplySuccess?: (filePath: string) => void;
	clearApplyRecovery?: () => void;
	onApplyRejected?: () => void;
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
				expected_apply_generation?: number;
			},
		) => Promise<unknown>;
		updateBatchJob: (jobId: string, request?: UpdateRequest) => Promise<unknown>;
	};
	pollIntervalMs?: number;
	pollTimeoutMs?: number;
	completionDelayMs?: number;
	redirectDelayMs?: number;
}

function acceptedGeneration(response: unknown): number | undefined {
	if (!response || typeof response !== 'object' || !('apply_generation' in response)) return;
	const generation = response.apply_generation;
	return typeof generation === 'number' && Number.isSafeInteger(generation) && generation > 0
		? generation
		: undefined;
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
				(deps.getFileStatuses().get(filePath)?.status !== 'failed' ||
					result.video_outcome === 'skipped' ||
					result.video_outcome === 'replaced' ||
					result.video_outcome === 'installed' ||
					result.video_outcome === 'same-file')
			) {
				updateFileStatus(filePath, {
					status:
						result.video_outcome === 'skipped'
							? 'skipped'
							: result.video_outcome === 'replaced'
								? 'replaced'
								: 'success',
				});
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
			const skipped = Array.from(deps.getFileStatuses().values()).filter(
				(s) => s.status === 'skipped',
			).length;
			if (failures === 0 && skipped > 0) {
				deps.clearApplyRecovery?.();
				deps.toastInfo(
					`${skipped} file(s) skipped; their videos and metadata were left unchanged.`,
				);
				return;
			}
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
	) {
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
				// Only this request's admission receipt owns a generation. A GET or
				// WebSocket N+1 may belong to another tab, even while our POST waits.
				const generationIsCurrent =
					acceptedApplyGeneration !== undefined &&
					latestJob.apply_generation === acceptedApplyGeneration;
				activeApplyGeneration = generationIsCurrent ? acceptedApplyGeneration : undefined;

				const terminalSuccess =
					latestJob.status === 'completed' ||
					latestJob.status === 'organized' ||
					latestJob.status === 'reverted';
				if (terminalSuccess && !generationIsCurrent) {
					// A GET can race the POST and see the scrape-completed state. Do
					// not report apply success without the exact admission receipt.
				} else if (terminalSuccess) {
					reconcileTerminalResults(latestJob);
					const action = deps.getIsUpdateMode() ? 'Update' : 'Organization';
					finalizeOrganizeSuccess(`${action} completed successfully! Redirecting in 5 seconds...`);
					return;
				}

				if (latestJob.status === 'failed' && generationIsCurrent) {
					reconcileTerminalResults(latestJob, true);
					const action = deps.getIsUpdateMode() ? 'update' : 'organization';
					finalizeOrganizeFailure(`The ${action} job failed.`);
					return;
				}

				if (latestJob.status === 'cancelled' && generationIsCurrent) {
					reconcileTerminalResults(latestJob, true);
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
	let lastDestination = '';
	let lastOperationMode = '';
	let lastApplyOperation: 'organize' | 'update' | null = null;
	let acceptedApplyGeneration: number | undefined;
	let hasLocalAttempt = false;
	let lastRecoveryFailedPaths: string[] = [];

	function hasRetryContext(): boolean {
		return (
			lastApplyOperation !== null &&
			acceptedApplyGeneration !== undefined &&
			deps.getJob()?.apply_generation === acceptedApplyGeneration
		);
	}

	function revokeRetryContext() {
		lastApplyOperation = null;
		acceptedApplyGeneration = undefined;
		deps.clearApplyRecovery?.();
	}

	function confirmApplyReceipt(response: unknown) {
		const generation = acceptedGeneration(response);
		if (generation === undefined) {
			throw new Error(
				'The server did not confirm this apply attempt. Refresh the job, review the settings, and start a new attempt.',
			);
		}
		acceptedApplyGeneration = generation;
		deps.recordApplyAccepted?.(generation);
	}

	function forgetRecoveryRetryPath(filePath: string) {
		lastRecoveryFailedPaths = lastRecoveryFailedPaths.filter((path) => path !== filePath);
	}

	async function organizeAll(
		skipNfo?: boolean,
		skipDownload?: boolean,
		overrides?: ReviewApplyOverrides,
		retryPaths: string[] = [],
		operationOverride?: OrganizeOperation,
		destinationOverride?: string,
		modeOverride?: string,
		expectedApplyGeneration?: number,
	) {
		const operationJobId = deps.getJobId();
		const operationGeneration = deps.getRouteGeneration?.() ?? 0;
		hasLocalAttempt = true;
		lastApplyOperation = null;
		acceptedApplyGeneration = undefined;
		const operation = operationOverride ?? deps.getOrganizeOperation();
		const effectiveMode = modeOverride ?? deps.getOperationMode();
		const operationDestination = destinationOverride ?? deps.getDestinationPath();
		const needsDestination = effectiveMode === 'organize';
		if (needsDestination && !operationDestination.trim()) {
			deps.clearApplyRecovery?.();
			deps.toastError('Please enter a destination path');
			return;
		}

		lastSkipNfo = skipNfo ?? false;
		lastSkipDownload = skipDownload ?? false;
		lastOrganizeOverrides = structuredClone(overrides);
		lastDestination = operationDestination;
		lastOperationMode = effectiveMode;
		lastOrganizeOperation = operation;
		lastApplyOperation = 'organize';
		lastRecoveryFailedPaths = Array.from(new Set(retryPaths));

		const { copyOnly, linkMode } = getOrganizeRequestOptions(operation);
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
				...(expectedApplyGeneration !== undefined
					? { expected_apply_generation: expectedApplyGeneration }
					: {}),
				destination: operationDestination,
				copy_only: copyOnly,
				link_mode: linkMode,
				operation_mode: effectiveMode as OperationMode,
				skip_nfo: skipNfo || false,
				skip_download: skipDownload || false,
				overrides: lastOrganizeOverrides,
				retry_file_paths: retryPaths.length > 0 ? retryPaths : undefined,
			});
			pollingRunToken = startOrganizeCompletionPolling(
				operationJobId,
				operationGeneration,
				() => requestPending,
			);
			const response = await request;
			requestPending = false;

			if (pollingRunToken !== organizeRunToken) return;
			if (deps.isCurrentOperation && !deps.isCurrentOperation(operationJobId, operationGeneration))
				return;
			if (deps.getOrganizeStatus() !== 'organizing') return;
			confirmApplyReceipt(response);
		} catch (e) {
			requestPending = false;
			if (pollingRunToken !== organizeRunToken) return;
			if (deps.isCurrentOperation && !deps.isCurrentOperation(operationJobId, operationGeneration))
				return;
			if (ignoreWebSocketMessages || deps.getOrganizeStatus() !== 'organizing') return;
			revokeRetryContext();
			deps.setOrganizeStatus('failed');
			deps.setOrganizing(false);
			clearOrganizePollTimer();
			deps.onApplyRejected?.();
			const errorMessage = e instanceof Error ? e.message : 'Failed to start organize';
			deps.toastError(errorMessage, 7000);
		}
	}

	async function updateAll(
		options?: UpdateRequest,
		retryPaths: string[] = [],
		expectedApplyGeneration?: number,
	) {
		hasLocalAttempt = true;
		acceptedApplyGeneration = undefined;
		lastApplyOperation = 'update';
		const operationJobId = deps.getJobId();
		const operationGeneration = deps.getRouteGeneration?.() ?? 0;
		lastRecoveryFailedPaths = Array.from(new Set(retryPaths));
		prepareOrganizeRun(retryPaths);
		const operationToken = organizeRunToken;
		let requestPending = false;
		let pollingRunToken = operationToken;

		lastUpdateOptions = structuredClone(options ?? {});

		try {
			if (deps.getEditedMovies().size > 0) {
				await deps.saveAllEdits();
			}

			if (operationToken !== organizeRunToken) return;
			if (deps.isCurrentOperation && !deps.isCurrentOperation(operationJobId, operationGeneration))
				return;

			requestPending = true;
			const request = {
				...lastUpdateOptions,
				...(retryPaths.length > 0 ? { retry_file_paths: retryPaths } : {}),
				...(expectedApplyGeneration !== undefined
					? { expected_apply_generation: expectedApplyGeneration }
					: {}),
			};
			pollingRunToken = startOrganizeCompletionPolling(
				operationJobId,
				operationGeneration,
				() => requestPending,
			);
			const response = await deps.api.updateBatchJob(operationJobId, request);
			requestPending = false;
			if (pollingRunToken !== organizeRunToken) return;
			if (deps.isCurrentOperation && !deps.isCurrentOperation(operationJobId, operationGeneration))
				return;
			if (deps.getOrganizeStatus() !== 'organizing') return;
			confirmApplyReceipt(response);
		} catch (e) {
			requestPending = false;
			if (pollingRunToken !== organizeRunToken) return;
			if (deps.isCurrentOperation && !deps.isCurrentOperation(operationJobId, operationGeneration))
				return;
			if (ignoreWebSocketMessages || deps.getOrganizeStatus() !== 'organizing') return;
			revokeRetryContext();
			deps.setOrganizeStatus('failed');
			deps.setOrganizing(false);
			clearOrganizePollTimer();
			const errorMessage = e instanceof Error ? e.message : 'Failed to start update';
			deps.toastError(errorMessage, 7000);
		}
	}

	async function retryFailed() {
		if (!hasRetryContext()) return;
		const retryPaths = Array.from(deps.getFileStatuses().entries())
			.filter(([, status]) => status.status === 'failed')
			.map(([filePath]) => filePath);
		const failedCount = retryPaths.length;
		if (failedCount === 0) return;

		deps.toastInfo(`Retrying ${failedCount} failed file${failedCount > 1 ? 's' : ''}...`);

		const allRetryPaths = Array.from(new Set([...lastRecoveryFailedPaths, ...retryPaths]));
		const expectedGeneration = acceptedApplyGeneration;
		if (lastApplyOperation === 'update') {
			await updateAll(lastUpdateOptions, allRetryPaths, expectedGeneration);
		} else {
			await organizeAll(
				lastSkipNfo,
				lastSkipDownload,
				lastOrganizeOverrides,
				allRetryPaths,
				lastOrganizeOperation,
				lastDestination,
				lastOperationMode,
				expectedGeneration,
			);
		}
	}

	function handleWebSocketMessage(msg: ProgressMessage | undefined) {
		if (
			!msg ||
			ignoreWebSocketMessages ||
			msg.job_id !== deps.getJobId() ||
			deps.getOrganizeStatus() !== 'organizing' ||
			!hasRetryContext() ||
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

		if (msg.status === 'skipped' && msg.file_path) {
			updateFileStatus(msg.file_path, { status: 'skipped' });
		}
		if (
			(msg.status === 'organized' || msg.status === 'updated' || msg.status === 'replaced') &&
			msg.file_path
		) {
			updateFileStatus(msg.file_path, {
				status: msg.status === 'replaced' ? 'replaced' : 'success',
			});
			deps.recordApplySuccess?.(msg.file_path);
			forgetRecoveryRetryPath(msg.file_path);
		}
	}

	function cleanup() {
		hasLocalAttempt = false;
		lastApplyOperation = null;
		acceptedApplyGeneration = undefined;
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

		if (recovery) {
			lastApplyOperation = recovery.operation;
			acceptedApplyGeneration = acceptedGeneration({
				apply_generation: recovery.acceptedApplyGeneration,
			});
			lastSkipNfo = recovery.skipNfo;
			lastSkipDownload = recovery.skipDownload;
			lastOrganizeOverrides = structuredClone(recovery.overrides);
			lastDestination = recovery.destination;
			lastOperationMode = recovery.operationMode ?? '';
			lastOrganizeOperation = recovery.organizeOperation;
			lastRecoveryFailedPaths = Object.keys(recovery.failed);
			for (const [filePath, result] of Object.entries(job?.results ?? {})) {
				if (result.status === 'failed' && result.movie) lastRecoveryFailedPaths.push(filePath);
			}
			lastRecoveryFailedPaths = Array.from(new Set(lastRecoveryFailedPaths));
			lastUpdateOptions = structuredClone(recovery.updateOptions);
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
		startOrganizeCompletionPolling(operationJobId, operationGeneration, undefined);
	}

	return {
		get hasApplyContext() {
			return hasLocalAttempt || lastApplyOperation !== null;
		},
		get hasRetryContext() {
			return hasRetryContext();
		},
		get retryOptions() {
			if (!hasRetryContext() || !lastApplyOperation) return null;
			return {
				operation: lastApplyOperation,
				acceptedApplyGeneration,
				organizeOperation: lastOrganizeOperation,
				destination: lastDestination,
				operationMode: lastOperationMode,
				skipNfo: lastSkipNfo,
				skipDownload: lastSkipDownload,
				overrides: lastOrganizeOverrides,
				updateOptions: lastUpdateOptions,
			};
		},
		organizeAll,
		updateAll,
		retryFailed,
		handleWebSocketMessage,
		cleanup,
		resumePolling,
	};
}
