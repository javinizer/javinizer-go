import type { BatchJobResponse, Movie, OperationMode, ProgressMessage, UpdateRequest } from '$lib/api/types';

export type OrganizeOperation = 'move' | 'copy' | 'hardlink' | 'softlink';
export type OrganizeStatus = 'idle' | 'organizing' | 'completed' | 'failed';

export interface FileStatus {
	status: string;
	error?: string;
}

interface OrganizeControllerDeps {
	getJobId: () => string;
	getIsUpdateMode: () => boolean;
	getJob: () => BatchJobResponse | null;
	setJob: (job: BatchJobResponse) => void;
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
	navigateBrowse: () => void;
	api: {
		getBatchJob: (jobId: string, includeData?: boolean) => Promise<BatchJobResponse>;
		organizeBatchJob: (
			jobId: string,
			request: { destination: string; copy_only: boolean; link_mode?: 'hard' | 'soft'; operation_mode?: OperationMode; skip_nfo?: boolean; skip_download?: boolean }
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
		linkMode:
			operation === 'hardlink'
				? 'hard'
				: operation === 'softlink'
					? 'soft'
					: undefined
	};
}

function getOrganizeEligibleFilePaths(batchJob: BatchJobResponse | null): string[] {
	if (!batchJob) return [];
	const excluded = batchJob.excluded || {};
	return Object.entries(batchJob.results || {})
		.filter(([filePath, result]) => !excluded[filePath] && result.status === 'completed' && !!result.data)
		.map(([filePath]) => filePath);
}

export function createOrganizeController(deps: OrganizeControllerDeps) {
	const pollIntervalMs = deps.pollIntervalMs ?? 1500;
	const pollTimeoutMs = deps.pollTimeoutMs ?? 10 * 60 * 1000;
	const completionDelayMs = deps.completionDelayMs ?? 800;
	const redirectDelayMs = deps.redirectDelayMs ?? 5000;

	let organizePollTimer: ReturnType<typeof setTimeout> | null = null;
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

			const failures = Array.from(deps.getFileStatuses().values()).filter((s) => s.status === 'failed').length;
			if (failures === 0) {
				const action = deps.getIsUpdateMode() ? 'updated' : 'organized';
				deps.toastSuccess(message || `All files ${action} successfully! Redirecting in 5 seconds...`, 8000);
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

	function startOrganizeCompletionPolling() {
		clearOrganizePollTimer();
		const startedAt = Date.now();
		let lastPollError: string | null = null;

		const pollOnce = async () => {
			if (deps.getOrganizeStatus() !== 'organizing') {
				clearOrganizePollTimer();
				return;
			}

			try {
				const latestJob = await deps.api.getBatchJob(deps.getJobId(), true);
				deps.setJob(latestJob);
				lastPollError = null;

				if (latestJob.status === 'completed') {
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

			organizePollTimer = setTimeout(() => {
				void pollOnce();
			}, pollIntervalMs);
		};

		void pollOnce();
	}

	function prepareOrganizeRun() {
		deps.clearWebSocketMessages();
		deps.setOrganizeStatus('organizing');
		deps.setOrganizing(true);
		deps.setOrganizeProgress(0);
		deps.getFileStatuses().clear();
		deps.setExpectedOrganizeFilePaths(getOrganizeEligibleFilePaths(deps.getJob()));
		clearOrganizePollTimer();
		clearOrganizeCompletionTimer();
	}

	let lastUpdateOptions: UpdateRequest | undefined;
	let lastSkipNfo = false;
	let lastSkipDownload = false;

	async function organizeAll(skipNfo?: boolean, skipDownload?: boolean) {
		const effectiveMode = deps.getOperationMode();
		const needsDestination = effectiveMode === 'organize';
		if (needsDestination && !deps.getDestinationPath().trim()) {
			deps.toastError('Please enter a destination path');
			return;
		}

		lastSkipNfo = skipNfo ?? false;
		lastSkipDownload = skipDownload ?? false;

		const { copyOnly, linkMode } = getOrganizeRequestOptions(deps.getOrganizeOperation());
		prepareOrganizeRun();

		try {
			if (deps.getEditedMovies().size > 0) {
				await deps.saveAllEdits();
			}

			await deps.api.organizeBatchJob(deps.getJobId(), {
				destination: deps.getDestinationPath(),
				copy_only: copyOnly,
				link_mode: linkMode,
				operation_mode: deps.getOperationMode() as OperationMode,
				skip_nfo: skipNfo || false,
				skip_download: skipDownload || false
			});

			startOrganizeCompletionPolling();
		} catch (e) {
			deps.setOrganizeStatus('failed');
			deps.setOrganizing(false);
			clearOrganizePollTimer();
			const errorMessage = e instanceof Error ? e.message : 'Failed to start organize';
			deps.toastError(errorMessage, 7000);
		}
	}

	async function updateAll(options?: UpdateRequest) {
		prepareOrganizeRun();

		if (options) {
			lastUpdateOptions = options;
		}

		try {
			if (deps.getEditedMovies().size > 0) {
				await deps.saveAllEdits();
			}

			await deps.api.updateBatchJob(deps.getJobId(), options);
			startOrganizeCompletionPolling();
		} catch (e) {
			deps.setOrganizeStatus('failed');
			deps.setOrganizing(false);
			clearOrganizePollTimer();
			const errorMessage = e instanceof Error ? e.message : 'Failed to start update';
			deps.toastError(errorMessage, 7000);
		}
	}

	async function retryFailed() {
		const failedCount = Array.from(deps.getFileStatuses().values()).filter((s) => s.status === 'failed').length;
		if (failedCount === 0) return;

		deps.toastInfo(`Retrying ${failedCount} failed file${failedCount > 1 ? 's' : ''}...`);

		if (deps.getIsUpdateMode()) {
			await updateAll(lastUpdateOptions);
		} else {
			await organizeAll(lastSkipNfo, lastSkipDownload);
		}
	}

	function handleWebSocketMessage(msg: ProgressMessage | undefined) {
		if (!msg || msg.job_id !== deps.getJobId() || deps.getOrganizeStatus() !== 'organizing') {
			return;
		}

		if (msg.progress !== undefined && msg.progress !== null) {
			deps.setOrganizeProgress(msg.progress);
		}

		if (msg.status === 'failed' && msg.file_path) {
			updateFileStatus(msg.file_path, { status: 'failed', error: msg.error });
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
		}

		if (msg.status === 'organization_completed' || msg.status === 'update_completed') {
			finalizeOrganizeSuccess(msg.message);
		}
	}

	function cleanup() {
		clearOrganizePollTimer();
		clearOrganizeCompletionTimer();
	}

	return {
		organizeAll,
		updateAll,
		retryFailed,
		handleWebSocketMessage,
		cleanup
	};
}
