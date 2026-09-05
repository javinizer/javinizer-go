import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createOrganizeController } from './organize-controller';
import type {
	BatchJobResponse,
	FileResult,
	Movie,
	ProgressMessage,
	UpdateRequest,
} from '$lib/api/types';

/**
 * Regression coverage for the infinite-poll bug fixed in commit cec74d43:
 * "fix(organize): stop infinite poll when organize succeeds (status: organized)".
 *
 * Bug: organize sets job status to 'organized' (BatchJob.MarkOrganized),
 * which the pre-fix pollOnce terminal-success branch did not recognize —
 * only 'completed' did — so the /review/[jobId] page polled
 * GET /batch/{id}?include_data=true every ~1.5s forever.
 *
 * These tests pin the { status ∈ ['completed','organized','reverted'] }
 * terminal-success contract by observing the side effects of
 * finalizeOrganizeSuccess (setOrganizeStatus('completed') + toastSuccess)
 * without coupling to the internal timer plumbing.
 */

interface DepsOverrides {
	jobId?: string;
	job?: BatchJobResponse | null;
	isUpdateMode?: boolean;
	pollIntervalMs?: number;
	pollTimeoutMs?: number;
	completionDelayMs?: number;
	redirectDelayMs?: number;
	organizeBatchJob?: typeof defaultOrganizeBatchJob;
	updateBatchJob?: (jobId: string, request?: UpdateRequest) => Promise<void>;
	clearApplyRecovery?: () => void;
}

const defaultOrganizeBatchJob = vi.fn().mockResolvedValue(undefined);

function makeJob(status: string): BatchJobResponse {
	return {
		id: 'job-1',
		status,
		total_files: 1,
		completed: 1,
		failed: 0,
		operation_count: 0,
		reverted_count: 0,
		noop_count: 0,
		excluded: {},
		progress: 100,
		destination: '/out',
		results: {},
		started_at: '2026-01-01T00:00:00Z',
		apply_generation: 0,
		update: false,
	};
}

function makeDeps(overrides: DepsOverrides = {}) {
	const initialJob = overrides.job === undefined ? makeJob('organizing') : overrides.job;
	let currentJob: BatchJobResponse | null = initialJob;
	const fileStatuses = new Map<string, { status: string; error?: string }>();
	let expectedOrganizeFilePaths: string[] = [];
	const calls = {
		setJob: [] as BatchJobResponse[],
		setOrganizeStatus: [] as string[],
		setOrganizeProgress: [] as number[],
		toastSuccess: [] as string[],
		toastError: [] as string[],
		navigateBrowse: 0,
	};

	const deps = {
		getJobId: () => overrides.jobId ?? 'job-1',
		getIsUpdateMode: () => overrides.isUpdateMode ?? false,
		getJob: () => currentJob,
		setJob: (job: BatchJobResponse) => {
			calls.setJob.push(job);
			currentJob = job;
		},
		getDestinationPath: () => '/out',
		getOrganizeOperation: () => 'move' as const,
		getOperationMode: () => 'organize',
		getEditedMovies: () => new Map<string, Movie>(),
		saveAllEdits: () => Promise.resolve(),
		getOrganizeStatus: () => 'organizing' as const,
		setOrganizeStatus: (status: string) => calls.setOrganizeStatus.push(status),
		setOrganizing: () => {},
		clearApplyRecovery: overrides.clearApplyRecovery ?? (() => {}),
		setOrganizeProgress: (p: number) => calls.setOrganizeProgress.push(p),
		getFileStatuses: () => fileStatuses,
		getExpectedOrganizeFilePaths: () => expectedOrganizeFilePaths,
		setExpectedOrganizeFilePaths: (paths: string[]) => {
			expectedOrganizeFilePaths = paths;
		},
		clearWebSocketMessages: () => {},
		toastSuccess: (message: string) => calls.toastSuccess.push(message),
		toastError: (message: string) => calls.toastError.push(message),
		toastInfo: () => {},
		navigateBrowse: () => {
			calls.navigateBrowse++;
		},
		api: {
			getBatchJob: async () => {
				// pollOnce reads getBatchJob(id, true) → returns the job
				// currently installed by the test scenario. The poll loop
				// will re-evaluate status and finalize.
				return currentJob ?? makeJob('organizing');
			},
			organizeBatchJob: overrides.organizeBatchJob ?? defaultOrganizeBatchJob,
			updateBatchJob:
				overrides.updateBatchJob ?? (async (_jobId: string, _request?: UpdateRequest) => undefined),
		},
		pollIntervalMs: 5,
		pollTimeoutMs: 60_000,
		completionDelayMs: overrides.completionDelayMs ?? 0,
		redirectDelayMs: overrides.redirectDelayMs ?? 0,
	};
	return { deps, calls, fileStatuses };
}

describe('organize-controller pollOnce terminal-success branches', () => {
	beforeEach(() => {
		vi.useFakeTimers();
	});
	afterEach(() => {
		vi.useRealTimers();
	});

	it.each([['completed'], ['organized'], ['reverted']])(
		'finalizes organize when polled job status is %s',
		async (status) => {
			const job = makeJob(status);
			const { deps, calls } = makeDeps({ job });
			const controller = createOrganizeController(deps);

			const organizeRequest = controller.organizeAll();
			job.apply_generation = 1;
			await organizeRequest;
			// Allow the organizeBatchJob promise + the first pollOnce tick to run.
			await vi.advanceTimersByTimeAsync(10);
			// finalizeOrganizeSuccess schedules a completion timer (0ms in tests).
			await vi.advanceTimersByTimeAsync(5);

			expect(calls.setOrganizeProgress).toContain(100);
			expect(calls.setOrganizeStatus).toContain('completed');
			expect(calls.toastSuccess.some((m) => /successfully/.test(m))).toBe(true);
		},
	);

	it('does not finalize on non-terminal status (stays organizing)', async () => {
		const { deps, calls } = makeDeps({ job: makeJob('running') });
		const controller = createOrganizeController(deps);

		await controller.organizeAll();
		await vi.advanceTimersByTimeAsync(10);

		// No finalizeOrganizeSuccess side effects should fire while status
		// is non-terminal.
		expect(calls.setOrganizeStatus).not.toContain('completed');
		expect(calls.toastSuccess).toHaveLength(0);

		controller.cleanup();
	});

	it('does not accept a pre-apply terminal snapshot while the POST is pending', async () => {
		let resolvePost!: () => void;
		const post = vi.fn(
			() =>
				new Promise<void>((resolve) => {
					resolvePost = resolve;
				}),
		);
		const job = makeJob('completed');
		job.apply_generation = 2;
		const { deps, calls } = makeDeps({ job, organizeBatchJob: post });
		const controller = createOrganizeController(deps);

		const organizeRequest = controller.organizeAll();
		await vi.advanceTimersByTimeAsync(30);

		expect(calls.setOrganizeStatus).not.toContain('completed');
		controller.cleanup();
		resolvePost();
		await organizeRequest;
	});

	it('polls to completion while the apply generation advances with a pending POST', async () => {
		let resolvePost!: () => void;
		const post = vi.fn(
			() =>
				new Promise<void>((resolve) => {
					resolvePost = resolve;
				}),
		);
		const job = makeJob('organized');
		job.apply_generation = 2;
		const { deps, calls } = makeDeps({
			job,
			organizeBatchJob: post,
		});
		const controller = createOrganizeController(deps);

		const organizeRequest = controller.organizeAll();
		job.apply_generation = 3;
		await vi.advanceTimersByTimeAsync(30);

		expect(calls.setOrganizeStatus).toContain('completed');
		resolvePost();
		await organizeRequest;
		controller.cleanup();
	});

	it('does not adopt a newer unrelated apply generation', async () => {
		let resolvePost!: () => void;
		const post = vi.fn(
			() =>
				new Promise<void>((resolve) => {
					resolvePost = resolve;
				}),
		);
		const job = makeJob('organized');
		job.apply_generation = 2;
		const { deps, calls } = makeDeps({ job, organizeBatchJob: post });
		const controller = createOrganizeController(deps);

		const organizeRequest = controller.organizeAll();
		job.apply_generation = 4; // N+2 belongs to another operation.
		await vi.advanceTimersByTimeAsync(30);

		expect(calls.setOrganizeStatus).not.toContain('completed');
		expect(calls.toastSuccess).toHaveLength(0);
		controller.cleanup();
		resolvePost();
		await organizeRequest;
	});

	it('does not resume an unrelated newer apply generation', async () => {
		const job = makeJob('organized');
		job.apply_generation = 4; // Recovery was recorded at N=2; expected is N+1=3.
		const { deps, calls } = makeDeps({ job });
		const controller = createOrganizeController(deps);

		controller.resumePolling({
			jobId: 'job-1',
			operation: 'organize',
			preApplyGeneration: 2,
			destination: '/out',
			skipNfo: false,
			skipDownload: false,
			failed: {},
			succeeded: [],
			organizeOperation: 'move',
			eligibleFilePaths: [],
		});
		await vi.advanceTimersByTimeAsync(10);

		expect(calls.setOrganizeStatus).not.toContain('completed');
		expect(calls.toastSuccess).toHaveLength(0);
		controller.cleanup();
	});

	it('clears recovery when launch is rejected with a 409 conflict', async () => {
		const clearApplyRecovery = vi.fn();
		const { deps, calls } = makeDeps({
			clearApplyRecovery,
			organizeBatchJob: vi.fn().mockRejectedValue({ status: 409 }),
		});
		const controller = createOrganizeController(deps);

		await controller.organizeAll();

		expect(clearApplyRecovery).toHaveBeenCalledOnce();
		expect(calls.setOrganizeStatus).toContain('failed');
		controller.cleanup();
	});

	it('does not retry a path after a success event removes it from recovery', async () => {
		const firstPath = '/src/already-succeeded.mp4';
		const requests: Array<{ retry_file_paths?: string[] }> = [];
		const job = makeJob('organizing');
		const { deps, fileStatuses } = makeDeps({
			job,
			organizeBatchJob: vi.fn((_jobId, request) => {
				requests.push(request);
				return Promise.resolve();
			}),
		});
		const controller = createOrganizeController(deps);

		const firstRun = controller.organizeAll(false, false, undefined, [firstPath]);
		job.apply_generation = 1;
		await firstRun;
		await vi.advanceTimersByTimeAsync(10);
		controller.handleWebSocketMessage({
			job_id: 'job-1',
			file_index: 0,
			file_path: firstPath,
			status: 'organized',
			progress: 100,
			apply_generation: 1,
			message: 'Organized',
		} satisfies ProgressMessage);

		const secondPath = '/src/still-failed.mp4';
		fileStatuses.set(secondPath, { status: 'failed', error: 'retry me' });
		await controller.retryFailed();

		expect(requests).toHaveLength(2);
		expect(requests[1].retry_file_paths).toEqual([secondPath]);
		expect(requests[1].retry_file_paths).not.toContain(firstPath);
		controller.cleanup();
	});

	it('preserves a recorded apply failure when the terminal row is still completed', async () => {
		const failedPath = '/src/failed-writeback.mp4';
		const job = makeJob('completed');
		job.apply_generation = 1;
		job.results[failedPath] = {
			result_id: 'result-1',
			file_path: failedPath,
			movie_id: 'MOV-1',
			status: 'completed',
			movie: { id: 'MOV-1', title: 'Test Movie' },
			started_at: '2026-01-01T00:00:00Z',
			is_multi_part: false,
			part_number: 0,
			part_suffix: '',
		} satisfies FileResult;
		const { deps, calls, fileStatuses } = makeDeps({ job });
		const controller = createOrganizeController(deps);

		const organizeRequest = controller.organizeAll();
		fileStatuses.set(failedPath, { status: 'failed', error: 'apply write-back skipped' });
		job.apply_generation = 2;
		await organizeRequest;
		await vi.advanceTimersByTimeAsync(10);
		await vi.advanceTimersByTimeAsync(5);

		expect(fileStatuses.get(failedPath)).toEqual({
			status: 'failed',
			error: 'apply write-back skipped',
		});
		expect(calls.toastSuccess).toHaveLength(0);
		controller.cleanup();
	});
});

describe('organize-controller handleWebSocketMessage progress gating (NEW-1)', () => {
	beforeEach(() => vi.useFakeTimers());
	afterEach(() => vi.useRealTimers());

	// Regression for NEW-1: per-file 'organized'/'updated'/'failed' messages
	// carry progress:100 but must NOT drive the progress bar (they are for
	// fileStatuses display). Only the AGGREGATE 'pending' (incremental, no
	// file_path, emitted by makeOrganizeProgressBroadcaster with a high-water
	// mutex) and terminal 'organization_completed'/'update_completed' drive the
	// bar. Before the fix, every message with a progress field snapped the bar
	// to 100 then oscillated as the next 'pending' arrived.
	it('applies progress for an aggregate pending message after generation binding', async () => {
		const job = makeJob('organizing');
		const { deps, calls } = makeDeps({ job });
		const controller = createOrganizeController(deps);

		const request = controller.organizeAll();
		job.apply_generation = 1;
		await request;
		await vi.advanceTimersByTimeAsync(10);

		controller.handleWebSocketMessage({
			job_id: 'job-1',
			file_index: 0,
			file_path: '',
			status: 'pending',
			progress: 42,
			apply_generation: 1,
			message: 'Organizing 1 of 4 files',
		} satisfies ProgressMessage);

		expect(calls.setOrganizeProgress).toContain(42);
		controller.cleanup();
	});

	// Regression for F-1 (iter-9): the verbose per-file 'Organizing <file>'
	// start message is 'pending' with Progress:0 AND a file_path (emitted by
	// makeOrganizeFileStartBroadcaster). It must NOT drive the bar — doing so
	// flickers the bar back to 0% at the start of every file (defeating NEW-1 and
	// the aggregate high-water broadcaster). The bar-drive filter gates on
	// !file_path so only the aggregate (no FilePath) drives the bar.
	it('does NOT apply progress for a per-file Organizing-start pending/0 (F-1)', async () => {
		const job = makeJob('organizing');
		const { deps, calls } = makeDeps({ job });
		const controller = createOrganizeController(deps);
		const request = controller.organizeAll();
		job.apply_generation = 1;
		await request;
		await vi.advanceTimersByTimeAsync(10);

		// First, advance the bar via an aggregate pending (no file_path).
		controller.handleWebSocketMessage({
			job_id: 'job-1',
			file_index: 0,
			file_path: '',
			status: 'pending',
			progress: 25,
			apply_generation: 1,
			message: 'Organizing 1 of 4 files',
		} satisfies ProgressMessage);
		expect(calls.setOrganizeProgress).toContain(25);

		// Then a per-file start message (pending/Progress:0 WITH file_path) arrives.
		// It must NOT drive the bar (would flicker back to 0%).
		controller.handleWebSocketMessage({
			job_id: 'job-1',
			file_index: 0,
			file_path: '/src/b.mp4',
			status: 'pending',
			progress: 0,
			apply_generation: 1,
			message: 'Organizing b.mp4',
		} satisfies ProgressMessage);

		expect(calls.setOrganizeProgress).toEqual([0, 25]);
		controller.cleanup();
	});

	it('ignores aggregate progress until the apply generation is established', () => {
		const { deps, calls } = makeDeps();
		const controller = createOrganizeController(deps);

		controller.handleWebSocketMessage({
			job_id: 'job-1',
			file_index: 0,
			file_path: '',
			status: 'pending',
			progress: 100,
			message: 'stale aggregate frame',
		} satisfies ProgressMessage);

		expect(calls.setOrganizeProgress).toHaveLength(0);
		controller.cleanup();
	});

	it('does NOT apply progress for a per-file organized message (progress:100)', () => {
		const { deps, calls } = makeDeps();
		const controller = createOrganizeController(deps);

		controller.handleWebSocketMessage({
			job_id: 'job-1',
			file_index: 0,
			file_path: '/src/a.mp4',
			status: 'organized',
			progress: 100,
			message: 'Organized /src/a.mp4',
		} satisfies ProgressMessage);

		expect(calls.setOrganizeProgress).not.toContain(100);
		expect(calls.setOrganizeProgress).toHaveLength(0);
		controller.cleanup();
	});

	it('does not finalize from a terminal WebSocket message', () => {
		const { deps, calls } = makeDeps();
		const controller = createOrganizeController(deps);

		controller.handleWebSocketMessage({
			job_id: 'job-1',
			file_index: 0,
			file_path: '',
			status: 'organization_completed',
			progress: 100,
			message: 'Organized 4 files, 0 failed',
		} satisfies ProgressMessage);

		expect(calls.setOrganizeStatus).not.toContain('completed');
		expect(calls.setOrganizeProgress).toHaveLength(0);
		controller.cleanup();
	});

	it('reconciles terminal failed results missed by WebSocket', async () => {
		vi.useFakeTimers();
		const failedPath = '/src/failed.mp4';
		const job = makeJob('completed');
		job.total_files = 1;
		job.completed = 1;
		job.failed = 0;
		job.results[failedPath] = {
			result_id: 'result-1',
			file_path: failedPath,
			movie_id: 'MOV-1',
			status: 'completed',
			movie: { id: 'MOV-1', title: 'Test Movie' },
			started_at: '2026-01-01T00:00:00Z',
			is_multi_part: false,
			part_number: 0,
			part_suffix: '',
		} satisfies FileResult;
		const { deps, calls, fileStatuses } = makeDeps({ job });
		const controller = createOrganizeController(deps);

		const organizeRequest = controller.organizeAll();
		job.apply_generation = 1;
		(job.results[failedPath] as FileResult).status = 'failed';
		(job.results[failedPath] as FileResult).error = 'disk full';
		await organizeRequest;
		await vi.advanceTimersByTimeAsync(10);
		await vi.advanceTimersByTimeAsync(5);

		expect(fileStatuses.get(failedPath)).toEqual({ status: 'failed', error: 'disk full' });
		expect(calls.setOrganizeStatus).toContain('completed');
		expect(calls.toastSuccess).toHaveLength(0);
		controller.cleanup();
		vi.useRealTimers();
	});

	describe('update payloads', () => {
		it('passes overwrite_existing_media to the update API', async () => {
			const requests: UpdateRequest[] = [];
			const { deps } = makeDeps({
				isUpdateMode: true,
				updateBatchJob: async (_jobId, request) => {
					if (request) requests.push(request);
				},
			});
			const controller = createOrganizeController(deps);

			await controller.updateAll({ overwrite_existing_media: true });

			expect(requests).toEqual([{ overwrite_existing_media: true }]);
			controller.cleanup();
		});

		it('passes explicitly retried failed paths to the update API', async () => {
			const requests: UpdateRequest[] = [];
			const { deps } = makeDeps({
				isUpdateMode: true,
				updateBatchJob: async (_jobId, request) => {
					if (request) requests.push(request);
				},
			});
			const controller = createOrganizeController(deps);

			await controller.updateAll({ overwrite_existing_media: true }, ['/src/failed.mp4']);

			expect(requests).toEqual([
				{
					overwrite_existing_media: true,
					retry_file_paths: ['/src/failed.mp4'],
				},
			]);
			controller.cleanup();
		});
	});
});
