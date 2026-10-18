import { cleanup, render, waitFor } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import { tick } from 'svelte';
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
function failedGeneration(current: BatchJobResponse, generation: number): BatchJobResponse {
	const failed = structuredClone(current);
	failed.status = 'failed';
	failed.apply_generation = generation;
	failed.failed = 1;
	failed.completed = 0;
	failed.results[filePath].status = 'failed';
	failed.results[filePath].error_code = 'credit_conflict';
	return failed;
}

async function settleAcceptedApply(
	state: ReturnType<typeof createReviewState>,
	generation: number,
	apply: () => Promise<void>,
) {
	// Keep the real controller polling path, including its normal 1500 ms interval.
	// A resolved POST receipt alone does not settle the authoritative server outcome.
	vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
	try {
		await apply();
		await vi.advanceTimersByTimeAsync(1500);
		await tick();
		expect(state.job?.apply_generation).toBe(generation);
		expect(state.organizing).toBe(false);
		expect(state.fileStatuses.get(filePath)?.status).toBe('failed');
	} finally {
		vi.useRealTimers();
	}
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
	vi.useRealTimers();
	clients.splice(0).forEach((c) => c.clear());
	vi.restoreAllMocks();
	localStorage.clear();
	sessionStorage.clear();
});

it.each(['copy', 'hardlink', 'softlink'] as const)(
	'older move recovery surviving quota cannot override a newer explicit %s attempt',
	async (operation) => {
		let current = fixture();
		vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
		const apply = vi
			.spyOn(apiClient, 'organizeBatchJob')
			.mockImplementationOnce(async () => {
				current = failedGeneration(current, 1);
				return { message: 'Original move accepted', apply_generation: 1 };
			})
			.mockImplementationOnce(async () => {
				current = failedGeneration(current, 2);
				return { message: 'New operation accepted', apply_generation: 2 };
			})
			.mockResolvedValue({ message: 'Retry accepted', apply_generation: 3 });
		const { state } = await mount();
		state.organizeOperation = 'move';
		await settleAcceptedApply(state, 1, () => state.organizeAll());
		expect(state.job?.apply_generation).toBe(1);
		expect(JSON.parse(sessionStorage.getItem(key)!).organizeOperation).toBe('move');
		const setItem = Storage.prototype.setItem;
		vi.spyOn(Storage.prototype, 'setItem').mockImplementation(function (this: Storage, k, v) {
			if (k === key) throw new DOMException('Quota full', 'QuotaExceededError');
			return setItem.call(this, k, v);
		});
		state.organizeOperation = operation;
		await settleAcceptedApply(state, 2, () => state.organizeAll(true, true));
		expect(state.canRetryFailed).toBe(true);
		expect(apply).toHaveBeenCalledTimes(2);
		expect(apply.mock.calls[1][1]).toMatchObject({
			copy_only: true,
			link_mode: operation === 'hardlink' ? 'hard' : operation === 'softlink' ? 'soft' : undefined,
		});
		await state.retryFailed();
		expect(apply).toHaveBeenCalledTimes(3);
		expect(apply.mock.calls[2][1]).toMatchObject({
			copy_only: true,
			link_mode: operation === 'hardlink' ? 'hard' : operation === 'softlink' ? 'soft' : undefined,
			skip_nfo: true,
			skip_download: true,
			retry_file_paths: [filePath],
		});
	},
);

it.each(['copy', 'hardlink', 'softlink'] as const)(
	'failed replacement and failed cleanup refuse new %s Apply and Retry',
	async (operation) => {
		let current = fixture();
		vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
		const apply = vi
			.spyOn(apiClient, 'organizeBatchJob')
			.mockImplementationOnce(async () => {
				current = failedGeneration(current, 1);
				return { message: 'Original move accepted', apply_generation: 1 };
			})
			.mockImplementationOnce(async () => {
				current = failedGeneration(current, 2);
				return { message: 'Replacement accepted', apply_generation: 2 };
			})
			.mockResolvedValue({ message: 'Retry accepted', apply_generation: 3 });
		const { state } = await mount();
		await settleAcceptedApply(state, 1, () => state.organizeAll());
		const setItem = Storage.prototype.setItem;
		const removeItem = Storage.prototype.removeItem;
		const blockedSet = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(function (
			this: Storage,
			k,
			v,
		) {
			if (k === key) throw new DOMException('Quota full', 'QuotaExceededError');
			return setItem.call(this, k, v);
		});
		const blockedRemove = vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(function (
			this: Storage,
			k,
		) {
			if (k === key) throw new DOMException('Cleanup blocked', 'SecurityError');
			return removeItem.call(this, k);
		});
		state.organizeOperation = operation;
		await state.organizeAll(true, true);
		expect(apply).toHaveBeenCalledOnce();
		state.fileStatuses.set(filePath, { status: 'failed', error: 'Old operation failure' });
		await state.retryFailed();
		expect(apply).toHaveBeenCalledOnce();
		blockedSet.mockRestore();
		blockedRemove.mockRestore();
		state.organizeOperation = operation;
		await settleAcceptedApply(state, 2, () => state.organizeAll(true, true));
		expect(state.canRetryFailed).toBe(true);
		expect(apply).toHaveBeenCalledTimes(2);
		expect(apply.mock.calls[1][1]).toMatchObject({
			copy_only: true,
			link_mode: operation === 'hardlink' ? 'hard' : operation === 'softlink' ? 'soft' : undefined,
			skip_nfo: true,
			skip_download: true,
		});
		await state.retryFailed();
		expect(apply).toHaveBeenCalledTimes(3);
		expect(apply.mock.calls[2][1]).toMatchObject({
			copy_only: true,
			link_mode: operation === 'hardlink' ? 'hard' : operation === 'softlink' ? 'soft' : undefined,
			skip_nfo: true,
			skip_download: true,
		});
	},
);

it.each(['copy', 'hardlink', 'softlink'] as const)(
	'failed %s recovery replacement removes stale move authority across reload',
	async (operation) => {
		let current = fixture();
		vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
		const apply = vi
			.spyOn(apiClient, 'organizeBatchJob')
			.mockImplementationOnce(async () => {
				current = failedGeneration(current, 1);
				return { message: 'Original move accepted', apply_generation: 1 };
			})
			.mockRejectedValueOnce(new Error('New explicit operation response lost'))
			.mockResolvedValue({} as never);
		const { state, view, client } = await mount();
		await settleAcceptedApply(state, 1, () => state.organizeAll());
		const setItem = Storage.prototype.setItem;
		vi.spyOn(Storage.prototype, 'setItem').mockImplementation(function (this: Storage, k, v) {
			if (k === key) throw new DOMException('Quota full', 'QuotaExceededError');
			return setItem.call(this, k, v);
		});
		state.organizeOperation = operation;
		await state.organizeAll(true, true);
		expect(apply).toHaveBeenCalledTimes(2);
		expect(state.canRetryFailed).toBe(false);
		expect(sessionStorage.getItem(key)).toBeNull();
		await state.retryFailed();
		expect(apply).toHaveBeenCalledTimes(2);
		view.unmount();
		client.clear();
		const restored = await mount();
		await waitFor(() => expect(restored.state.organizing).toBe(false));
		apply.mockClear();
		await restored.state.retryFailed();
		expect(apply).not.toHaveBeenCalled();
	},
);

it.each([
	{ name: 'verified missing recovery', readDenied: false, removeDenied: true, allowed: true },
	{
		name: 'unreadable and unremovable recovery',
		readDenied: true,
		removeDenied: true,
		allowed: false,
	},
	{
		name: 'unreadable but successfully removed recovery',
		readDenied: true,
		removeDenied: false,
		allowed: true,
	},
])(
	'storage boundary for $name does not guess prior authority',
	async ({ readDenied, removeDenied, allowed }) => {
		let current = fixture();
		vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
		const apply = vi
			.spyOn(apiClient, 'organizeBatchJob')
			.mockImplementationOnce(async () => {
				current = failedGeneration(current, 1);
				return { message: 'Apply accepted', apply_generation: 1 };
			})
			.mockResolvedValue({ message: 'Retry accepted', apply_generation: 2 });
		const { state } = await mount();
		const getItem = Storage.prototype.getItem;
		const setItem = Storage.prototype.setItem;
		const removeItem = Storage.prototype.removeItem;
		vi.spyOn(Storage.prototype, 'getItem').mockImplementation(function (this: Storage, k) {
			if (k === key && readDenied) throw new DOMException('Read denied', 'SecurityError');
			return getItem.call(this, k);
		});
		vi.spyOn(Storage.prototype, 'setItem').mockImplementation(function (this: Storage, k, v) {
			if (k === key) throw new DOMException('Write denied', 'QuotaExceededError');
			return setItem.call(this, k, v);
		});
		vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(function (this: Storage, k) {
			if (k === key && removeDenied) throw new DOMException('Remove denied', 'SecurityError');
			return removeItem.call(this, k);
		});
		state.organizeOperation = 'copy';
		if (allowed) {
			await settleAcceptedApply(state, 1, () => state.organizeAll(true, true));
			expect(state.canRetryFailed).toBe(true);
		} else {
			await state.organizeAll(true, true);
			expect(state.canRetryFailed).toBe(false);
		}
		expect(apply).toHaveBeenCalledTimes(allowed ? 1 : 0);
		await state.retryFailed();
		expect(apply).toHaveBeenCalledTimes(allowed ? 2 : 0);
		if (allowed)
			expect(apply.mock.calls[1][1]).toMatchObject({
				copy_only: true,
				link_mode: undefined,
				skip_nfo: true,
				skip_download: true,
			});
	},
);

it('unrevokeable stale update settings block new Update and Retry', async () => {
	let current = fixture();
	current.update = true;
	vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
	const apply = vi
		.spyOn(apiClient, 'updateBatchJob')
		.mockImplementationOnce(async () => {
			current = failedGeneration(current, 1);
			return { message: 'Original update accepted', apply_generation: 1 };
		})
		.mockResolvedValue({} as never);
	const { state } = await mount();
	await settleAcceptedApply(state, 1, () =>
		state.updateAll({ force_overwrite: true, preserve_nfo: false }),
	);
	expect(state.canRetryFailed).toBe(true);
	expect(JSON.parse(sessionStorage.getItem(key)!)).toMatchObject({
		acceptedApplyGeneration: 1,
		updateOptions: { force_overwrite: true, preserve_nfo: false },
	});
	const setItem = Storage.prototype.setItem;
	const removeItem = Storage.prototype.removeItem;
	vi.spyOn(Storage.prototype, 'setItem').mockImplementation(function (this: Storage, k, v) {
		if (k === key) throw new DOMException('Quota full', 'QuotaExceededError');
		return setItem.call(this, k, v);
	});
	vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(function (this: Storage, k) {
		if (k === key) throw new DOMException('Cleanup denied', 'SecurityError');
		return removeItem.call(this, k);
	});
	await state.updateAll({ force_overwrite: false, preserve_nfo: true });
	expect(apply).toHaveBeenCalledOnce();
	expect(state.canRetryFailed).toBe(false);
	state.fileStatuses.set(filePath, { status: 'failed', error: 'Old failure' });
	await state.retryFailed();
	expect(apply).toHaveBeenCalledOnce();
});

it('explains blocked storage and recovers explicitly without reviving an older retry context', async () => {
	vi.spyOn(apiClient, 'getBatchJob').mockResolvedValue(failedGeneration(fixture(), 1));
	const apply = vi.spyOn(apiClient, 'organizeBatchJob').mockResolvedValue({} as never);
	const { state } = await mount();
	sessionStorage.setItem(
		key,
		JSON.stringify({
			jobId: 'retry-job',
			operation: 'organize',
			operationMode: 'organize',
			preApplyGeneration: 0,
			acceptedApplyGeneration: 1,
			destination: '/synthetic/output',
			skipNfo: false,
			skipDownload: false,
			organizeOperation: 'move',
			failed: {},
			succeeded: [],
			eligibleFilePaths: [filePath],
		}),
	);
	const setItem = Storage.prototype.setItem;
	vi.spyOn(Storage.prototype, 'setItem').mockImplementation(function (this: Storage, k, v) {
		if (k === key) throw new DOMException('Quota full', 'QuotaExceededError');
		return setItem.call(this, k, v);
	});
	const removeItem = Storage.prototype.removeItem;
	const removal = vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(function (
		this: Storage,
		k,
	) {
		if (k === key) throw new DOMException('Cleanup denied', 'SecurityError');
		return removeItem.call(this, k);
	});
	state.organizeOperation = 'copy';
	await state.organizeAll();
	expect(state.refreshError).toMatch(/no new operation was started/i);
	expect(state.canRetryFailed).toBe(false);
	await state.retryLoad();
	expect(state.refreshError).toMatch(/restore access/i);
	expect(apply).not.toHaveBeenCalled();
	removal.mockRestore();
	await state.retryLoad();
	expect(state.refreshError).toBeNull();
	expect(state.canRetryFailed).toBe(false);
	await state.organizeAll();
	expect(apply).toHaveBeenCalledOnce();
	expect(apply.mock.calls[0][1].copy_only).toBe(true);
});

it('keeps newer controller options authoritative over an externally restored older record', async () => {
	let current = fixture();
	vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
	const apply = vi
		.spyOn(apiClient, 'organizeBatchJob')
		.mockImplementationOnce(async () => {
			current = failedGeneration(current, 1);
			return { message: 'Original move accepted', apply_generation: 1 };
		})
		.mockImplementationOnce(async () => {
			current = failedGeneration(current, 2);
			return { message: 'New copy accepted', apply_generation: 2 };
		})
		.mockResolvedValue({ message: 'Retry accepted', apply_generation: 3 });
	const { state } = await mount();
	await settleAcceptedApply(state, 1, () => state.organizeAll());
	const olderRecord = sessionStorage.getItem(key)!;
	state.organizeOperation = 'copy';
	await settleAcceptedApply(state, 2, () => state.organizeAll(true, true));
	sessionStorage.setItem(key, olderRecord);
	await state.retryLoad();
	expect(state.organizeOperation).toBe('copy');
	await state.retryFailed();
	expect(apply).toHaveBeenCalledTimes(3);
	expect(apply.mock.calls[2][1]).toMatchObject({
		copy_only: true,
		skip_nfo: true,
		skip_download: true,
		retry_file_paths: [filePath],
	});
});

it.each(['organize', 'update'] as const)(
	'accepts a validated %s record before recovery hydration',
	async (operation) => {
		const current = failedGeneration(fixture(), 1);
		current.update = operation === 'update';
		vi.spyOn(apiClient, 'getBatchJob').mockResolvedValue(current);
		const apply = vi
			.spyOn(apiClient, operation === 'update' ? 'updateBatchJob' : 'organizeBatchJob')
			.mockResolvedValue({ message: 'Retry accepted', apply_generation: 2 });
		const { state } = await mount();
		sessionStorage.setItem(
			key,
			JSON.stringify({
				jobId: 'retry-job',
				operation,
				operationMode: 'organize',
				preApplyGeneration: 0,
				acceptedApplyGeneration: 1,
				destination: '/synthetic/output',
				skipNfo: true,
				skipDownload: true,
				organizeOperation: 'copy',
				failed: { [filePath]: 'Prior failure' },
				succeeded: [],
				eligibleFilePaths: [filePath],
				updateOptions: { preserve_nfo: true },
			}),
		);
		await state.retryFailed();
		expect(apply).toHaveBeenCalledOnce();
		expect(apply.mock.calls[0][1]).toMatchObject({ retry_file_paths: [filePath] });
	},
);
