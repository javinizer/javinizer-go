import { beforeEach, afterEach, expect, it, vi } from 'vitest';
import {
	createOrganizeController,
	type OrganizeOperation,
	type OrganizeStatus,
} from './organize-controller';
import type { BatchJobResponse, Movie, UpdateRequest } from '$lib/api/types';
const path = '/synthetic/receipt.mp4';
const controllers: Array<ReturnType<typeof createOrganizeController>> = [];
function fixture(generation = 0, status = 'completed'): BatchJobResponse {
	return {
		id: 'receipt-job',
		apply_generation: generation,
		status,
		update: false,
		total_files: 1,
		completed: status === 'failed' ? 0 : 1,
		failed: status === 'failed' ? 1 : 0,
		progress: 100,
		excluded: {},
		destination: '/synthetic/out',
		results: {
			[path]: {
				file_path: path,
				result_id: 'result',
				movie_id: 'movie',
				status: status === 'failed' ? 'failed' : 'completed',
				movie: { id: 'movie', code: 'movie', title: 'Synthetic movie', actresses: [], credits: [] },
				error_code: status === 'failed' ? 'credit_conflict' : undefined,
			},
		},
	} as unknown as BatchJobResponse;
}
function harness() {
	let client = fixture();
	let server = fixture();
	let status: OrganizeStatus = 'idle';
	let operation: OrganizeOperation = 'move';
	let destination = '/synthetic/out';
	let operationMode = 'organize';
	let route = 0;
	let id = 'receipt-job';
	let expected: string[] = [];
	const statuses = new Map<string, { status: string; error?: string }>();
	const edits = new Map<string, Movie>();
	const save = vi.fn(async () => {});
	const post = vi.fn<(jobId: string, request?: unknown) => Promise<unknown>>();
	const clear = vi.fn();
	const failure = vi.fn();
	const success = vi.fn();
	const deps = {
		getJobId: () => id,
		getIsUpdateMode: () => client.update,
		getJob: () => client,
		setJob: (job: BatchJobResponse) => {
			client = structuredClone(job);
		},
		getDestinationPath: () => destination,
		getOrganizeOperation: () => operation,
		getOperationMode: () => operationMode,
		getEditedMovies: () => edits,
		saveAllEdits: save,
		getOrganizeStatus: () => status,
		setOrganizeStatus: (s: OrganizeStatus) => {
			status = s;
		},
		setOrganizing: vi.fn(),
		setOrganizeProgress: vi.fn(),
		getFileStatuses: () => statuses,
		getExpectedOrganizeFilePaths: () => expected,
		setExpectedOrganizeFilePaths: (p: string[]) => {
			expected = p;
		},
		clearWebSocketMessages: vi.fn(),
		toastSuccess: vi.fn(),
		toastError: vi.fn(),
		toastInfo: vi.fn(),
		clearApplyRecovery: clear,
		recordApplyFailure: failure,
		recordApplySuccess: success,
		navigateBrowse: vi.fn(),
		getRouteGeneration: () => route,
		isCurrentOperation: (jobId: string, generation: number) => jobId === id && generation === route,
		api: {
			getBatchJob: async () => structuredClone(server),
			organizeBatchJob: post,
			updateBatchJob: post,
		},
		pollIntervalMs: 1,
		pollTimeoutMs: 20,
		completionDelayMs: 0,
		redirectDelayMs: 500,
	};
	const controller = createOrganizeController(deps);
	controllers.push(controller);
	return {
		controller,
		post,
		clear,
		failure,
		success,
		statuses,
		edits,
		save,
		setServer: (g: number, s = 'failed') => {
			server = { ...fixture(g, s), id };
		},
		setClient: (g: number, s = 'failed') => {
			client = { ...fixture(g, s), id };
		},
		setOperation: (op: OrganizeOperation) => {
			operation = op;
		},
		setControls: (dest: string, mode: string) => {
			destination = dest;
			operationMode = mode;
		},
		setRoute: (next: string) => {
			route++;
			id = next;
			client = { ...fixture(), id: next };
			server = { ...fixture(), id: next };
		},
		get status() {
			return status;
		},
	};
}
beforeEach(() => vi.useFakeTimers());
afterEach(() => {
	controllers.splice(0).forEach((c) => c.cleanup());
	vi.useRealTimers();
	vi.restoreAllMocks();
});
const modes = ['move', 'copy', 'hardlink', 'softlink', 'update'] as const;
const flags: UpdateRequest = {
	force_overwrite: false,
	preserve_nfo: true,
	overwrite_existing_media: false,
	preset: 'conservative',
	scalar_strategy: 'prefer-nfo',
	array_strategy: 'merge',
	skip_nfo: true,
	skip_download: true,
};
async function launch(h: ReturnType<typeof harness>, mode: (typeof modes)[number]) {
	if (mode === 'update') await h.controller.updateAll(flags, [path]);
	else {
		h.setOperation(mode);
		await h.controller.organizeAll(true, true, undefined, [path]);
	}
}
it.each(
	[undefined, null, 0, -1, 1.5, '1', Number.NaN, Number.MAX_SAFE_INTEGER + 1].flatMap((receipt) =>
		['copy', 'update'].map((mode) => ({ receipt, mode: mode as 'copy' | 'update' })),
	),
)('reviewer: $mode refuses invalid/legacy receipt $receipt', async ({ receipt, mode }) => {
	const h = harness();
	h.post.mockResolvedValue({ message: 'accepted', apply_generation: receipt });
	h.setServer(1);
	await launch(h, mode);
	await vi.advanceTimersByTimeAsync(25);
	expect(h.controller.hasRetryContext).toBe(false);
	expect(h.failure).not.toHaveBeenCalled();
	h.statuses.set(path, { status: 'failed' });
	await h.controller.retryFailed();
	expect(h.post).toHaveBeenCalledOnce();
});
it.each(modes)(
	'reviewer: exact accepted receipt preserves all original %s options',
	async (mode) => {
		const h = harness();
		h.post
			.mockResolvedValueOnce({ message: 'accepted', apply_generation: 1 })
			.mockResolvedValue({ message: 'accepted', apply_generation: 2 });
		h.setServer(1);
		await launch(h, mode);
		await vi.advanceTimersByTimeAsync(5);
		expect(h.controller.hasRetryContext).toBe(true);
		h.setOperation(mode === 'move' ? 'copy' : 'move');
		await h.controller.retryFailed();
		expect(h.post).toHaveBeenCalledTimes(2);
		expect(
			(h.post.mock.calls[0][1] as { expected_apply_generation?: number }).expected_apply_generation,
		).toBeUndefined();
		expect(h.post.mock.calls[1][1]).toHaveProperty('expected_apply_generation', 1);
		expect(h.post.mock.calls[1][1]).toMatchObject(
			mode === 'update'
				? { ...flags, retry_file_paths: [path] }
				: {
						copy_only: mode !== 'move',
						link_mode: mode === 'hardlink' ? 'hard' : mode === 'softlink' ? 'soft' : undefined,
						skip_nfo: true,
						skip_download: true,
						retry_file_paths: [path],
					},
		);
	},
);
it.each(modes)(
	'reviewer: delayed %s receipt cannot claim a newer external generation',
	async (mode) => {
		const h = harness();
		let resolve!: (value: unknown) => void;
		h.post
			.mockImplementationOnce(
				() =>
					new Promise((r) => {
						resolve = r;
					}),
			)
			.mockResolvedValue({ apply_generation: 3 });
		const pending = launch(h, mode);
		h.setServer(2);
		await vi.advanceTimersByTimeAsync(5);
		expect(h.failure).not.toHaveBeenCalled();
		expect(h.success).not.toHaveBeenCalled();
		resolve({ apply_generation: 1 });
		await pending;
		await vi.advanceTimersByTimeAsync(25);
		expect(h.controller.hasRetryContext).toBe(false);
		h.statuses.set(path, { status: 'failed' });
		await h.controller.retryFailed();
		expect(h.post).toHaveBeenCalledOnce();
	},
);
it.each(['success', 'rejection', 'network'] as const)(
	'reviewer: old %s callback after cleanup cannot erase newer accepted context',
	async (outcome) => {
		const h = harness();
		let resolve!: (value: unknown) => void;
		let reject!: (error: unknown) => void;
		h.post
			.mockImplementationOnce(
				() =>
					new Promise((a, b) => {
						resolve = a;
						reject = b;
					}),
			)
			.mockResolvedValue({ apply_generation: 1 });
		const old = launch(h, 'move');
		h.controller.cleanup();
		h.setRoute('new-job');
		h.setServer(1);
		await launch(h, 'copy');
		await vi.advanceTimersByTimeAsync(5);
		expect(h.controller.hasRetryContext).toBe(true);
		const cleared = h.clear.mock.calls.length;
		if (outcome === 'success') resolve({ apply_generation: 1 });
		else
			reject(
				outcome === 'rejection'
					? Object.assign(new Error('Old 409'), { status: 409 })
					: new Error('Old network error'),
			);
		await old;
		expect(h.clear).toHaveBeenCalledTimes(cleared);
		expect(h.controller.hasRetryContext).toBe(true);
		expect(h.controller.retryOptions?.organizeOperation).toBe('copy');
	},
);

it.each(modes)(
	'reviewer: pending %s cannot adopt foreign N+1 before its own receipt',
	async (mode) => {
		const h = harness();
		let resolve!: (value: unknown) => void;
		h.post.mockImplementationOnce(
			() =>
				new Promise((r) => {
					resolve = r;
				}),
		);
		const pending = launch(h, mode);
		h.setServer(1);
		await vi.advanceTimersByTimeAsync(5);
		expect(h.failure).not.toHaveBeenCalled();
		expect(h.success).not.toHaveBeenCalled();
		expect(h.controller.hasRetryContext).toBe(false);
		resolve({ apply_generation: 2 });
		await pending;
		expect(h.controller.hasRetryContext).toBe(false);
		h.setServer(2);
		await vi.advanceTimersByTimeAsync(5);
		expect(h.controller.hasRetryContext).toBe(true);
		expect(h.failure).toHaveBeenCalledOnce();
	},
);
it.each(modes)(
	'reviewer: %s receipt owns its exact later generation despite stale pre-request view',
	async (mode) => {
		const h = harness();
		h.post.mockResolvedValue({ apply_generation: 7 });
		h.setServer(7);
		await launch(h, mode);
		await vi.advanceTimersByTimeAsync(5);
		expect(h.controller.hasRetryContext).toBe(true);
		expect(h.failure).toHaveBeenCalledOnce();
	},
);

it.each(['move', 'copy', 'hardlink', 'softlink'] as const)(
	'reviewer: %s receipt binds original destination and effective operation mode',
	async (mode) => {
		const h = harness();
		h.post
			.mockResolvedValueOnce({ apply_generation: 1 })
			.mockResolvedValue({ apply_generation: 2 });
		h.setServer(1);
		await launch(h, mode);
		await vi.advanceTimersByTimeAsync(5);
		h.setControls('/synthetic/different-output', 'metadata-artwork');
		await h.controller.retryFailed();
		expect(h.post).toHaveBeenCalledTimes(2);
		const request = h.post.mock.calls[1][1] as {
			destination: string;
			operation_mode: string;
			overrides?: { destination?: string; operation_mode?: string };
		};
		expect(request.overrides?.destination ?? request.destination).toBe('/synthetic/out');
		expect(request.overrides?.operation_mode ?? request.operation_mode).toBe('organize');
	},
);

it.each(['success', 'rejection', 'network'] as const)(
	'reviewer: same-ID route recreation rejects old %s callback authority',
	async (outcome) => {
		const h = harness();
		let resolve!: (value: unknown) => void;
		let reject!: (error: unknown) => void;
		h.post
			.mockImplementationOnce(
				() =>
					new Promise((a, b) => {
						resolve = a;
						reject = b;
					}),
			)
			.mockResolvedValue({ apply_generation: 2 });
		const old = launch(h, 'move');
		h.controller.cleanup();
		h.setRoute('receipt-job');
		h.setServer(2);
		await launch(h, 'softlink');
		await vi.advanceTimersByTimeAsync(5);
		expect(h.controller.hasRetryContext).toBe(true);
		const cleared = h.clear.mock.calls.length;
		if (outcome === 'success') resolve({ apply_generation: 1 });
		else
			reject(
				outcome === 'rejection'
					? Object.assign(new Error('Old 409'), { status: 409 })
					: new Error('Old network error'),
			);
		await old;
		expect(h.clear).toHaveBeenCalledTimes(cleared);
		expect(h.controller.hasRetryContext).toBe(true);
		expect(h.controller.retryOptions?.organizeOperation).toBe('softlink');
	},
);

it.each(['copy', 'update'] as const)(
	'reconciles cancellation only for the acknowledged %s generation',
	async (mode) => {
		const h = harness();
		h.post.mockResolvedValue({ apply_generation: 1 });
		h.setServer(1, 'cancelled');
		await launch(h, mode);
		await vi.advanceTimersByTimeAsync(5);
		expect(h.status).toBe('failed');
		expect(h.controller.hasRetryContext).toBe(true);
		expect(h.success).toHaveBeenCalledWith(path);
		expect(h.failure).not.toHaveBeenCalled();
	},
);

it('a canceled Update save cannot dispatch or erase a newer same-route operation', async () => {
	const h = harness();
	let resolveSave!: () => void;
	h.edits.set(path, fixture().results[path].movie!);
	h.save.mockImplementationOnce(
		() =>
			new Promise<void>((resolve) => {
				resolveSave = resolve;
			}),
	);
	const oldUpdate = launch(h, 'update');
	expect(h.save).toHaveBeenCalledOnce();
	expect(h.post).not.toHaveBeenCalled();
	h.controller.cleanup();
	h.edits.clear();
	h.post.mockResolvedValue({ apply_generation: 2 });
	h.setServer(2);
	await launch(h, 'softlink');
	await vi.advanceTimersByTimeAsync(5);
	expect(h.controller.hasRetryContext).toBe(true);
	resolveSave();
	await oldUpdate;
	expect(h.post).toHaveBeenCalledOnce();
	expect(h.controller.retryOptions?.organizeOperation).toBe('softlink');
	expect(h.controller.retryOptions?.acceptedApplyGeneration).toBe(2);
});
