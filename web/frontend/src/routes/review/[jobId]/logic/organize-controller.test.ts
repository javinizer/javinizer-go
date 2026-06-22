import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createOrganizeController } from './organize-controller';
import type { BatchJobResponse, Movie, UpdateRequest } from '$lib/api/types';

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
