import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createOrganizeController } from './organize-controller';
import type { BatchJobResponse, Movie, ProgressMessage, UpdateRequest } from '$lib/api/types';

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
		excluded: {},
		progress: 100,
		destination: '/out',
		results: {},
		started_at: '2026-01-01T00:00:00Z',
		update: false,
	};
}

function makeDeps(overrides: DepsOverrides = {}) {
	const initialJob = overrides.job === undefined ? makeJob('organizing') : overrides.job;
	let currentJob: BatchJobResponse | null = initialJob;
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
		setOrganizeProgress: (p: number) => calls.setOrganizeProgress.push(p),
		getFileStatuses: () => new Map<string, { status: string; error?: string }>(),
		getExpectedOrganizeFilePaths: () => [] as string[],
		setExpectedOrganizeFilePaths: () => {},
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
			updateBatchJob: async (_jobId: string, _request?: UpdateRequest) => undefined,
		},
		pollIntervalMs: 5,
		pollTimeoutMs: 60_000,
		completionDelayMs: overrides.completionDelayMs ?? 0,
		redirectDelayMs: overrides.redirectDelayMs ?? 0,
	};
	return { deps, calls };
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
			const { deps, calls } = makeDeps({ job: makeJob(status) });
			const controller = createOrganizeController(deps);

			await controller.organizeAll();
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
});

	describe('organize-controller handleWebSocketMessage progress gating (NEW-1)', () => {
	// Regression for NEW-1: per-file 'organized'/'updated'/'failed' messages
	// carry progress:100 but must NOT drive the progress bar (they are for
	// fileStatuses display). Only the AGGREGATE 'pending' (incremental, no
	// file_path, emitted by makeOrganizeProgressBroadcaster with a high-water
	// mutex) and terminal 'organization_completed'/'update_completed' drive the
	// bar. Before the fix, every message with a progress field snapped the bar
	// to 100 then oscillated as the next 'pending' arrived.
	it('applies progress for an aggregate pending message (no file_path)', () => {
		const { deps, calls } = makeDeps();
		const controller = createOrganizeController(deps);

		controller.handleWebSocketMessage({
			job_id: 'job-1',
			file_index: 0,
			file_path: '',
			status: 'pending',
			progress: 42,
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
	it('does NOT apply progress for a per-file Organizing-start pending/0 (F-1)', () => {
		const { deps, calls } = makeDeps();
		const controller = createOrganizeController(deps);

		// First, advance the bar via an aggregate pending (no file_path).
		controller.handleWebSocketMessage({
			job_id: 'job-1',
			file_index: 0,
			file_path: '',
			status: 'pending',
			progress: 25,
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
			message: 'Organizing b.mp4',
		} satisfies ProgressMessage);

		expect(calls.setOrganizeProgress).not.toContain(0);
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

	it('applies progress for the terminal organization_completed message', () => {
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

		expect(calls.setOrganizeProgress).toContain(100);
		controller.cleanup();
	});
});
