import { cleanup, render, waitFor } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { apiClient } from '$lib/api/client';
import type { BatchJobResponse } from '$lib/api/types';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import type { createReviewState } from './review-state.svelte';
import Harness from '../../../../../test/harness/ReviewStateHarness.svelte';
vi.mock('$app/environment', () => ({ browser: true }));
vi.mock('$lib/query/queries', () => ({ createConfigQuery: () => ({ data: null }) }));
vi.mock('$lib/stores/websocket', () => ({
	websocketStore: { subscribe: () => () => {}, clearMessages: vi.fn() },
}));
const clients: QueryClient[] = [];
const filePath = '/synthetic/source.mp4';
const key = 'javinizer.review.applyRecovery.retry-job';
function fixture(): BatchJobResponse {
	return {
		id: 'retry-job',
		update: false,
		status: 'completed',
		destination: '/synthetic/output',
		apply_generation: 0,
		results: {
			[filePath]: {
				file_path: filePath,
				result_id: 'result',
				movie_id: 'movie',
				status: 'completed',
				movie: { id: 'movie', code: 'movie', title: 'Synthetic movie', actresses: [], credits: [] },
				revision: 1,
			},
		},
		total_files: 1,
		completed: 1,
		failed: 0,
		progress: 100,
		excluded: {},
		apply_plan: {
			destination: '/synthetic/output',
			operation_mode: 'organize',
			nfo_output: 'write',
			media_policy: 'skip',
		},
	} as unknown as BatchJobResponse;
}
async function mount() {
	const client = new QueryClient({
		defaultOptions: {
			queries: { retry: false, gcTime: 0 },
			mutations: { retry: false, gcTime: 0 },
		},
	});
	clients.push(client);
	let state!: ReturnType<typeof createReviewState>;
	const view = render(
		Harness,
		{
			jobId: 'retry-job',
			capture: (value) => {
				state = value;
			},
		},
		{ wrapper: QueryClientWrapper, wrapperProps: { client } },
	);
	await waitFor(() => expect(state.job?.id).toBe('retry-job'));
	state.destinationPath = '/synthetic/output';
	return { state, view, client };
}
beforeEach(() => {
	localStorage.clear();
	sessionStorage.clear();
	vi.spyOn(apiClient, 'previewOrganize').mockResolvedValue({} as never);
	vi.spyOn(apiClient, 'getExistingNFO').mockResolvedValue({} as never);
});
afterEach(() => {
	cleanup();
	clients.splice(0).forEach((c) => c.clear());
	vi.restoreAllMocks();
	localStorage.clear();
	sessionStorage.clear();
});

function failed(update = false): BatchJobResponse {
	const j = fixture();
	j.update = update;
	j.apply_generation = 1;
	j.status = 'failed';
	j.failed = 1;
	j.completed = 0;
	j.results[filePath].status = 'failed';
	j.results[filePath].error_code = 'credit_conflict';
	return j;
}
function recovery(options: unknown, accepted: unknown = 1) {
	return {
		jobId: 'retry-job',
		operation: 'update',
		preApplyGeneration: 0,
		acceptedApplyGeneration: accepted,
		destination: '',
		skipNfo: false,
		skipDownload: false,
		organizeOperation: 'move',
		updateOptions: options,
		failed: { [filePath]: 'Credit conflict' },
		succeeded: [],
		eligibleFilePaths: [filePath],
	};
}
it.each([
	undefined,
	null,
	[],
	'invalid',
	{ force_overwrite: 'true' },
	{ preserve_nfo: 'false' },
	{ preset: 'unknown' },
	{ scalar_strategy: 'unknown' },
	{ array_strategy: 'unknown' },
	{ operation_mode: 'unknown' },
	{ destination: 4 },
	{ retry_file_paths: [4] },
	{ retry_file_paths: false },
	{ overrides: { skip_nfo: 'false' } },
])('reviewer: receipt cannot authorize malformed Update snapshot %j', async (options) => {
	sessionStorage.setItem(key, JSON.stringify(recovery(options)));
	vi.spyOn(apiClient, 'getBatchJob').mockResolvedValue(failed(true));
	const apply = vi.spyOn(apiClient, 'updateBatchJob').mockResolvedValue({ message: 'unexpected' });
	const { state } = await mount();
	await waitFor(() => expect(state.organizing).toBe(false), { timeout: 2500 });
	await state.retryFailed();
	expect(apply).not.toHaveBeenCalled();
	expect(state.canRetryFailed).toBe(false);
});
it('reviewer: explicit empty Update snapshot with exact receipt is distinguishable from missing options', async () => {
	sessionStorage.setItem(key, JSON.stringify(recovery({})));
	vi.spyOn(apiClient, 'getBatchJob').mockResolvedValue(failed(true));
	const apply = vi
		.spyOn(apiClient, 'updateBatchJob')
		.mockResolvedValue({ message: 'accepted', apply_generation: 2 } as never);
	const { state } = await mount();
	await waitFor(() => expect(state.organizing).toBe(false), { timeout: 2500 });
	await state.retryFailed();
	expect(apply).toHaveBeenCalledOnce();
	expect(apply.mock.calls[0][1]).toEqual({
		retry_file_paths: [filePath],
		expected_apply_generation: 1,
	});
});
it.each([undefined, 0, -1, 1.5, '1', 2, Number.MAX_SAFE_INTEGER + 1])(
	'reviewer: Update recovery receipt %j cannot authorize current generation 1',
	async (receipt) => {
		sessionStorage.setItem(
			key,
			JSON.stringify({
				...recovery({ preserve_nfo: true, force_overwrite: false }),
				acceptedApplyGeneration: receipt,
			}),
		);
		vi.spyOn(apiClient, 'getBatchJob').mockResolvedValue(failed(true));
		const apply = vi
			.spyOn(apiClient, 'updateBatchJob')
			.mockResolvedValue({ message: 'unexpected' });
		const { state } = await mount();
		await waitFor(() => expect(state.organizing).toBe(false), { timeout: 2500 });
		await state.retryFailed();
		expect(apply).not.toHaveBeenCalled();
	},
);
it.each(['move', 'copy', 'hardlink', 'softlink'] as const)(
	'reviewer: valid %s receipt and full original options survive a reload',
	async (operation) => {
		let current = fixture();
		vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
		const apply = vi
			.spyOn(apiClient, 'organizeBatchJob')
			.mockImplementationOnce(async () => {
				current = failed();
				return { message: 'accepted', apply_generation: 1 } as never;
			})
			.mockResolvedValue({ message: 'accepted', apply_generation: 2 } as never);
		const first = await mount();
		first.state.organizeOperation = operation;
		await first.state.organizeAll(true, true);
		await first.state.retryLoad();
		first.view.unmount();
		first.client.clear();
		const { state } = await mount();
		await waitFor(() => expect(state.organizing).toBe(false), { timeout: 2500 });
		state.organizeOperation = operation === 'move' ? 'copy' : 'move';
		await state.retryFailed();
		expect(apply).toHaveBeenCalledTimes(2);
		expect(apply.mock.calls[1][1]).toMatchObject({
			copy_only: operation !== 'move',
			link_mode: operation === 'hardlink' ? 'hard' : operation === 'softlink' ? 'soft' : undefined,
			skip_nfo: true,
			skip_download: true,
			retry_file_paths: [filePath],
		});
	},
);
it.each([
	{ force_overwrite: true, preserve_nfo: false },
	{ force_overwrite: false, preserve_nfo: true },
])('reviewer: valid Update receipt retains request-only flags %j after reload', async (flags) => {
	let current = fixture();
	current.update = true;
	vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
	const options = {
		...flags,
		overwrite_existing_media: true,
		preset: 'gap-fill' as const,
		scalar_strategy: 'fill-missing-only' as const,
		array_strategy: 'replace' as const,
		skip_nfo: true,
		skip_download: false,
	};
	const apply = vi
		.spyOn(apiClient, 'updateBatchJob')
		.mockImplementationOnce(async () => {
			current = failed(true);
			return { message: 'accepted', apply_generation: 1 } as never;
		})
		.mockResolvedValue({ message: 'accepted', apply_generation: 2 } as never);
	const first = await mount();
	await first.state.updateAll(options);
	await first.state.retryLoad();
	first.view.unmount();
	first.client.clear();
	const { state } = await mount();
	await waitFor(() => expect(state.organizing).toBe(false), { timeout: 2500 });
	await state.retryFailed();
	expect(apply).toHaveBeenCalledTimes(2);
	expect(apply.mock.calls[1][1]).toEqual({
		...options,
		retry_file_paths: [filePath],
		expected_apply_generation: 1,
	});
});
it.each(['update', 'organize'] as const)(
	'reviewer: %s receipt persists the sent snapshot, not caller edits while pending',
	async (mode) => {
		let current = fixture();
		current.update = mode === 'update';
		vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
		let resolve!: (value: { message: string; apply_generation: number }) => void;
		const apply =
			mode === 'update'
				? vi
						.spyOn(apiClient, 'updateBatchJob')
						.mockImplementationOnce(
							() =>
								new Promise((r) => {
									resolve = r;
								}),
						)
						.mockResolvedValue({ message: 'accepted', apply_generation: 2 } as never)
				: vi
						.spyOn(apiClient, 'organizeBatchJob')
						.mockImplementationOnce(
							() =>
								new Promise((r) => {
									resolve = r;
								}),
						)
						.mockResolvedValue({ message: 'accepted', apply_generation: 2 } as never);
		const first = await mount();
		const updateOptions = {
			preserve_nfo: true,
			force_overwrite: false,
			overwrite_existing_media: false,
		};
		const organizeOverrides = {
			operation_mode: 'metadata-artwork' as 'metadata-artwork' | 'organize',
			destination: '/synthetic/output',
			skip_nfo: true,
			skip_download: true,
		};
		const pending =
			mode === 'update'
				? first.state.updateAll(updateOptions)
				: first.state.organizeAll(true, true, organizeOverrides);
		expect(apply).toHaveBeenCalledOnce();
		updateOptions.preserve_nfo = false;
		updateOptions.force_overwrite = true;
		updateOptions.overwrite_existing_media = true;
		organizeOverrides.operation_mode = 'organize';
		organizeOverrides.destination = '/synthetic/different-output';
		current = failed(mode === 'update');
		resolve({ message: 'accepted', apply_generation: 1 });
		await pending;
		await first.state.retryLoad();
		first.view.unmount();
		first.client.clear();
		const { state } = await mount();
		await waitFor(() => expect(state.organizing).toBe(false), { timeout: 2500 });
		await state.retryFailed();
		expect(apply).toHaveBeenCalledTimes(2);
		expect(apply.mock.calls[1][1]).toMatchObject(
			mode === 'update'
				? { preserve_nfo: true, force_overwrite: false, overwrite_existing_media: false }
				: {
						overrides: {
							operation_mode: 'metadata-artwork',
							destination: '/synthetic/output',
							skip_nfo: true,
							skip_download: true,
						},
					},
		);
	},
);

it.each(['rejection', 'network', 'receipt'] as const)(
	'reviewer: delayed local %s must not erase a competing tab recovery record',
	async (outcome) => {
		let current = fixture();
		vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
		let resolve!: (value: { message: string; apply_generation: number }) => void;
		let reject!: (error: unknown) => void;
		const apply = vi.spyOn(apiClient, 'organizeBatchJob').mockImplementationOnce(
			() =>
				new Promise((a, b) => {
					resolve = a;
					reject = b;
				}),
		);
		const { state } = await mount();
		state.organizeOperation = 'copy';
		const pending = state.organizeAll(true, true);
		expect(apply).toHaveBeenCalledOnce();
		const other = JSON.stringify({
			...recovery({ force_overwrite: false, preserve_nfo: true }, 2),
			preApplyGeneration: 1,
		});
		sessionStorage.setItem(key, other);
		current = failed(true);
		current.apply_generation = 2;
		if (outcome === 'receipt') resolve({ message: 'accepted', apply_generation: 1 });
		else
			reject(
				outcome === 'rejection'
					? Object.assign(new Error('Rejected'), { status: 409 })
					: new TypeError('Network failed'),
			);
		await pending;
		expect(sessionStorage.getItem(key)).toBe(other);
		await state.retryLoad();
		await state.retryFailed();
		expect(apply).toHaveBeenCalledOnce();
		expect(state.canRetryFailed).toBe(false);
	},
);

it.each(
	['move', 'copy', 'hardlink', 'softlink', 'update'].flatMap((mode) =>
		[false, true].map((reload) => ({ mode, reload })),
	),
)(
	'reviewer: $mode retry sends owned receipt even when another client advances after the last snapshot (reload=$reload)',
	async ({ mode, reload }) => {
		let current = fixture();
		current.update = mode === 'update';
		vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
		const launch = async () => {
			current = failed(mode === 'update');
			return { message: 'accepted', apply_generation: 1 };
		};
		const rejectStale = async (_id: string, request?: unknown) => {
			expect(request).toHaveProperty('expected_apply_generation', 1);
			throw Object.assign(new Error('Generation conflict'), {
				status: 409,
				code: 'APPLY_GENERATION_MISMATCH',
			});
		};
		const apply =
			mode === 'update'
				? vi
						.spyOn(apiClient, 'updateBatchJob')
						.mockImplementationOnce(launch)
						.mockImplementation(rejectStale)
				: vi
						.spyOn(apiClient, 'organizeBatchJob')
						.mockImplementationOnce(launch)
						.mockImplementation(rejectStale);
		const first = await mount();
		if (mode === 'update')
			await first.state.updateAll({ preserve_nfo: true, force_overwrite: false });
		else {
			first.state.organizeOperation = mode as 'move' | 'copy' | 'hardlink' | 'softlink';
			await first.state.organizeAll(true, true);
		}
		expect(
			(apply.mock.calls[0][1] as { expected_apply_generation?: number }).expected_apply_generation,
		).toBeUndefined();
		await first.state.retryLoad();
		let state = first.state;
		if (reload) {
			first.view.unmount();
			first.client.clear();
			state = (await mount()).state;
			await waitFor(() => expect(state.organizing).toBe(false), { timeout: 2500 });
		} else {
			state.organizing = false;
			state.organizeStatus = 'failed';
		}
		expect(state.canRetryFailed).toBe(true);
		expect(state.job?.apply_generation).toBe(1);
		current = failed(mode !== 'update');
		current.apply_generation = 2;
		await state.retryFailed();
		expect(apply).toHaveBeenCalledTimes(2);
		expect(state.canRetryFailed).toBe(false);
		await state.retryFailed();
		expect(apply).toHaveBeenCalledTimes(2);
	},
);

it.each(['organize', 'update'] as const)(
	'never adopts, reads, or cleans up legacy shared %s recovery even with a matching receipt',
	async (operation) => {
		const legacy = JSON.stringify({
			...recovery({ preserve_nfo: true, force_overwrite: false }),
			operation,
			organizeOperation: 'copy',
			operationMode: 'organize',
			destination: '/synthetic/original',
		});
		localStorage.setItem(key, legacy);
		const reads = vi.spyOn(Storage.prototype, 'getItem');
		const writes = vi.spyOn(Storage.prototype, 'setItem');
		const removals = vi.spyOn(Storage.prototype, 'removeItem');
		vi.spyOn(apiClient, 'getBatchJob').mockResolvedValue(failed(operation === 'update'));
		const organize = vi
			.spyOn(apiClient, 'organizeBatchJob')
			.mockResolvedValue({ message: 'unexpected' });
		const update = vi
			.spyOn(apiClient, 'updateBatchJob')
			.mockResolvedValue({ message: 'unexpected' });
		const { state } = await mount();
		await state.retryFailed();
		expect(state.canRetryFailed).toBe(false);
		expect(organize).not.toHaveBeenCalled();
		expect(update).not.toHaveBeenCalled();
		for (const spy of [reads, writes, removals]) {
			expect(
				spy.mock.calls.filter(
					(args, index) => args[0] === key && spy.mock.contexts[index] === localStorage,
				),
			).toEqual([]);
		}
		expect(sessionStorage.getItem(key)).toBeNull();
		expect(localStorage.getItem(key)).toBe(legacy);
	},
);

it.each(['receipt', 'rejection'] as const)(
	'a delayed %s touches only its tab while another tab replaces shared legacy recovery',
	async (outcome) => {
		let current = fixture();
		vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
		let resolve!: (value: { message: string; apply_generation: number }) => void;
		let reject!: (error: unknown) => void;
		vi.spyOn(apiClient, 'organizeBatchJob').mockImplementationOnce(
			() =>
				new Promise((a, b) => {
					resolve = a;
					reject = b;
				}),
		);
		const { state } = await mount();
		state.organizeOperation = 'copy';
		const pending = state.organizeAll(true, true);
		const ownPending = sessionStorage.getItem(key);
		expect(JSON.parse(ownPending!)).toMatchObject({ organizeOperation: 'copy' });
		const foreign = JSON.stringify(recovery({ preserve_nfo: true }, 2));
		localStorage.setItem(key, foreign);
		expect(sessionStorage.getItem(key)).toBe(ownPending);
		if (outcome === 'receipt') {
			current = failed();
			resolve({ message: 'accepted', apply_generation: 1 });
		} else reject(new Error('Rejected'));
		await pending;
		expect(localStorage.getItem(key)).toBe(foreign);
		if (outcome === 'receipt')
			expect(JSON.parse(sessionStorage.getItem(key)!)).toMatchObject({
				acceptedApplyGeneration: 1,
				organizeOperation: 'copy',
			});
		else expect(sessionStorage.getItem(key)).toBeNull();
	},
);

it.each(['organize', 'update'] as const)(
	'a duplicated tab keeps its copied receipt and cannot retry after the original tab advances (%s)',
	async (operation) => {
		let current = fixture();
		current.update = operation === 'update';
		const getJob = vi
			.spyOn(apiClient, 'getBatchJob')
			.mockImplementation(async () => structuredClone(current));
		const launch = vi.fn(async (_id: string, request?: unknown) => {
			const expected = (request as { expected_apply_generation?: number })
				?.expected_apply_generation;
			if (expected !== undefined && expected !== current.apply_generation) {
				throw Object.assign(new Error('Generation conflict'), {
					status: 409,
					code: 'APPLY_GENERATION_MISMATCH',
				});
			}
			const generation = current.apply_generation! + 1;
			current = failed(operation === 'update');
			current.apply_generation = generation;
			return { message: 'accepted', apply_generation: generation };
		});
		vi.spyOn(
			apiClient,
			operation === 'update' ? 'updateBatchJob' : 'organizeBatchJob',
		).mockImplementation(launch);
		const first = await mount();
		if (operation === 'update')
			await first.state.updateAll({ preserve_nfo: true, force_overwrite: false });
		else {
			first.state.organizeOperation = 'copy';
			await first.state.organizeAll(true, true);
		}
		await first.state.retryLoad();
		const staleView = structuredClone(current);
		const copied = new Map<string, string>([[key, sessionStorage.getItem(key)!]]);
		const clonedStorage: Storage = {
			get length() {
				return copied.size;
			},
			key: (index) => Array.from(copied.keys())[index] ?? null,
			getItem: (name) => copied.get(name) ?? null,
			setItem: (name, value) => {
				copied.set(name, String(value));
			},
			removeItem: (name) => {
				copied.delete(name);
			},
			clear: () => copied.clear(),
		};
		first.state.organizing = false;
		first.state.organizeStatus = 'failed';
		await first.state.retryFailed();
		expect(current.apply_generation).toBe(2);
		const originalStorage = sessionStorage;
		const latestOriginalRecord = originalStorage.getItem(key);
		expect(JSON.parse(latestOriginalRecord!)).toHaveProperty('acceptedApplyGeneration', 2);
		expect(JSON.parse(clonedStorage.getItem(key)!)).toHaveProperty('acceptedApplyGeneration', 1);
		first.view.unmount();
		first.client.clear();
		// A copied tab can still hold a cached generation-1 GET. Admission, not
		// the client's matching snapshot, must reject this receipt after N+1.
		getJob.mockResolvedValue(staleView);
		vi.stubGlobal('sessionStorage', clonedStorage);
		try {
			const clone = await mount();
			await waitFor(() => expect(clone.state.organizing).toBe(false));
			expect(clone.state.canRetryFailed).toBe(true);
			await clone.state.retryFailed();
			expect(launch).toHaveBeenCalledTimes(3);
			expect(launch.mock.calls[2][1]).toHaveProperty('expected_apply_generation', 1);
			expect(clone.state.canRetryFailed).toBe(false);
			expect(current.apply_generation).toBe(2);
			expect(originalStorage.getItem(key)).toBe(latestOriginalRecord);
			clone.view.unmount();
			clone.client.clear();
		} finally {
			vi.unstubAllGlobals();
		}
	},
);
