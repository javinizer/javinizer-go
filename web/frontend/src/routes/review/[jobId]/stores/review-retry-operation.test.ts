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
const operations = ['move', 'copy', 'hardlink', 'softlink'] as const;
const cases = operations.flatMap((operation) =>
	['missing', 'corrupt', 'quota'].map((storage) => ({ operation, storage })),
);
it.each(cases)(
	'restored $operation with $storage recovery cannot silently dispatch default move',
	async ({ operation, storage }) => {
		let current = fixture();
		vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
		const apply = vi
			.spyOn(apiClient, 'organizeBatchJob')
			.mockImplementationOnce(async () => {
				current.status = 'failed';
				current.apply_generation = 1;
				current.failed = 1;
				current.completed = 0;
				current.results[filePath].status = 'failed';
				current.results[filePath].error_code = 'credit_conflict';
				throw new Error('Synthetic response lost after original operation failed credit gate');
			})
			.mockResolvedValue({} as never);
		const first = await mount();
		first.state.organizeOperation = operation;
		if (storage === 'quota') {
			const setItem = Storage.prototype.setItem;
			vi.spyOn(Storage.prototype, 'setItem').mockImplementation(function (this: Storage, k, v) {
				if (k === key) throw new DOMException('Quota full', 'QuotaExceededError');
				return setItem.call(this, k, v);
			});
		}
		await first.state.organizeAll();
		expect(apply).toHaveBeenCalledOnce();
		expect(apply.mock.calls[0][1]).toMatchObject({
			copy_only: operation !== 'move',
			...(operation === 'hardlink' || operation === 'softlink'
				? { link_mode: operation === 'hardlink' ? 'hard' : 'soft' }
				: {}),
		});
		first.view.unmount();
		first.client.clear();
		if (storage === 'missing') localStorage.removeItem(key);
		if (storage === 'corrupt') localStorage.setItem(key, '{broken');
		const restored = await mount();
		expect(restored.state.editedMovies.size).toBe(0);
		apply.mockClear();
		await restored.state.retryFailed();
		// The server fixture intentionally carries no original copy/link operation.
		// A restored retry has no authority to choose move, even if that happened to be the old choice.
		expect(apply).not.toHaveBeenCalled();
	},
);

it.each(
	operations.flatMap((operation) =>
		['readable', 'quota'].map((storage) => ({ operation, storage })),
	),
)(
	'same-session $operation retry preserves original operation with $storage storage',
	async ({ operation, storage }) => {
		const current = fixture();
		vi.spyOn(apiClient, 'getBatchJob').mockResolvedValue(current);
		const apply = vi
			.spyOn(apiClient, 'organizeBatchJob')
			.mockRejectedValueOnce(new Error('Original request lost'))
			.mockResolvedValue({} as never);
		const { state } = await mount();
		state.organizeOperation = operation;
		if (storage === 'quota') {
			const setItem = Storage.prototype.setItem;
			vi.spyOn(Storage.prototype, 'setItem').mockImplementation(function (this: Storage, k, v) {
				if (k === key) throw new DOMException('Quota full', 'QuotaExceededError');
				return setItem.call(this, k, v);
			});
		}
		await state.organizeAll(true, true, {
			operation_mode: 'metadata-artwork',
			destination: '',
			skip_nfo: true,
			skip_download: true,
		});
		state.fileStatuses.set(filePath, { status: 'failed', error: 'Credit gate' });
		state.organizeOperation = operation === 'move' ? 'copy' : 'move';
		await state.retryFailed();
		expect(apply).toHaveBeenCalledTimes(2);
		expect(apply.mock.calls[1][1]).toMatchObject({
			copy_only: operation !== 'move',
			link_mode: operation === 'hardlink' ? 'hard' : operation === 'softlink' ? 'soft' : undefined,
			skip_nfo: true,
			skip_download: true,
			retry_file_paths: [filePath],
			overrides: { operation_mode: 'metadata-artwork' },
		});
	},
);

it('status rows alone cannot invent a known retry operation', async () => {
	const current = fixture();
	current.status = 'failed';
	current.failed = 1;
	current.completed = 0;
	current.results[filePath].status = 'failed';
	current.results[filePath].error_code = 'credit_conflict';
	vi.spyOn(apiClient, 'getBatchJob').mockResolvedValue(current);
	const apply = vi.spyOn(apiClient, 'organizeBatchJob').mockResolvedValue({} as never);
	const { state } = await mount();
	state.fileStatuses.set(filePath, { status: 'failed', error: 'Credit gate' });
	await state.retryFailed();
	expect(apply).not.toHaveBeenCalled();
});

it('an old recovery generation cannot authorize the newer server attempt', async () => {
	const current = fixture();
	current.status = 'failed';
	current.apply_generation = 2;
	current.failed = 1;
	current.completed = 0;
	current.results[filePath].status = 'failed';
	current.results[filePath].error_code = 'credit_conflict';
	// This record describes generation 1's move. Generation 2 was started elsewhere,
	// and its copy/link operation is absent from the server response.
	localStorage.setItem(
		key,
		JSON.stringify({
			jobId: 'retry-job',
			operation: 'organize',
			preApplyGeneration: 0,
			destination: '/synthetic/output',
			skipNfo: true,
			skipDownload: true,
			failed: { [filePath]: 'Old failure' },
			succeeded: [],
			organizeOperation: 'move',
			eligibleFilePaths: [filePath],
		}),
	);
	vi.spyOn(apiClient, 'getBatchJob').mockResolvedValue(current);
	const apply = vi.spyOn(apiClient, 'organizeBatchJob').mockResolvedValue({} as never);
	const { state } = await mount();
	vi.spyOn(Date, 'now').mockReturnValue(Date.now() + 11 * 60 * 1000);
	await waitFor(() => expect(state.organizing).toBe(false), { timeout: 5000 });
	await state.retryFailed();
	expect(apply).not.toHaveBeenCalled();
});

it('route changes and A-B-A do not inherit a lost-storage retry context', async () => {
	const a = fixture();
	const b = fixture();
	b.id = 'other-job';
	b.status = 'failed';
	b.apply_generation = 1;
	b.results[filePath].status = 'failed';
	b.results[filePath].error_code = 'credit_conflict';
	vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async (id) =>
		structuredClone(id === 'other-job' ? b : a),
	);
	const apply = vi
		.spyOn(apiClient, 'organizeBatchJob')
		.mockRejectedValueOnce(new Error('Request failed'))
		.mockResolvedValue({} as never);
	const { state, view } = await mount();
	const setItem = Storage.prototype.setItem;
	vi.spyOn(Storage.prototype, 'setItem').mockImplementation(function (this: Storage, k, v) {
		if (k.startsWith('javinizer.review.applyRecovery.'))
			throw new DOMException('Quota full', 'QuotaExceededError');
		return setItem.call(this, k, v);
	});
	state.organizeOperation = 'copy';
	await state.organizeAll();
	apply.mockClear();
	a.status = 'failed';
	a.apply_generation = 1;
	a.results[filePath].status = 'failed';
	a.results[filePath].error_code = 'credit_conflict';
	await view.rerender({ jobId: 'other-job' });
	await waitFor(() => expect(state.job?.id).toBe('other-job'));
	state.fileStatuses.set(filePath, { status: 'failed', error: 'Server failure' });
	await state.retryFailed();
	expect(apply).not.toHaveBeenCalled();
	await view.rerender({ jobId: 'retry-job' });
	await state.retryLoad();
	await waitFor(() => expect(state.job?.id).toBe('retry-job'));
	state.fileStatuses.set(filePath, { status: 'failed', error: 'Server failure' });
	await state.retryFailed();
	expect(apply).not.toHaveBeenCalled();
});

it.each(operations)(
	'readable generation-matched %s recovery keeps the operation after reload',
	async (operation) => {
		const current = fixture();
		current.status = 'failed';
		current.apply_generation = 1;
		current.failed = 1;
		current.completed = 0;
		current.results[filePath].status = 'failed';
		current.results[filePath].error_code = 'credit_conflict';
		localStorage.setItem(
			key,
			JSON.stringify({
				jobId: 'retry-job',
				operation: 'organize',
				preApplyGeneration: 0,
				destination: '/synthetic/output',
				skipNfo: true,
				skipDownload: true,
				failed: { [filePath]: 'Credit failure' },
				succeeded: [],
				organizeOperation: operation,
				eligibleFilePaths: [filePath],
			}),
		);
		vi.spyOn(apiClient, 'getBatchJob').mockResolvedValue(current);
		const apply = vi.spyOn(apiClient, 'organizeBatchJob').mockResolvedValue({} as never);
		const { state } = await mount();
		await waitFor(() => expect(state.organizing).toBe(false));
		state.organizeOperation = operation === 'move' ? 'copy' : 'move';
		await state.retryFailed();
		expect(apply).toHaveBeenCalledOnce();
		expect(apply.mock.calls[0][1]).toMatchObject({
			copy_only: operation !== 'move',
			link_mode: operation === 'hardlink' ? 'hard' : operation === 'softlink' ? 'soft' : undefined,
			retry_file_paths: [filePath],
		});
	},
);

it('unreadable recovery storage leaves a restored failure safely blocked', async () => {
	const current = fixture();
	current.status = 'failed';
	current.apply_generation = 1;
	current.failed = 1;
	current.completed = 0;
	current.results[filePath].status = 'failed';
	current.results[filePath].error_code = 'credit_conflict';
	const getItem = Storage.prototype.getItem;
	vi.spyOn(Storage.prototype, 'getItem').mockImplementation(function (this: Storage, k) {
		if (k === key) throw new DOMException('Storage blocked', 'SecurityError');
		return getItem.call(this, k);
	});
	const removeItem = Storage.prototype.removeItem;
	vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(function (this: Storage, k) {
		if (k === key) throw new DOMException('Storage blocked', 'SecurityError');
		return removeItem.call(this, k);
	});
	vi.spyOn(apiClient, 'getBatchJob').mockResolvedValue(current);
	const apply = vi.spyOn(apiClient, 'organizeBatchJob').mockResolvedValue({} as never);
	const { state } = await mount();
	await state.retryFailed();
	expect(apply).not.toHaveBeenCalled();
});

it('revokes same-session context when a newer external attempt replaces it', async () => {
	let current = fixture();
	vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
	const apply = vi.spyOn(apiClient, 'organizeBatchJob').mockRejectedValue(new Error('Response lost'));
	const { state } = await mount();
	state.organizeOperation = 'copy';
	await state.organizeAll();
	localStorage.removeItem(key);
	current = structuredClone(current);
	current.status = 'failed';
	current.apply_generation = 2;
	current.results[filePath].status = 'failed';
	current.results[filePath].error_code = 'credit_conflict';
	await state.retryLoad();
	state.fileStatuses.set(filePath, { status: 'failed', error: 'New attempt failed' });
	expect(state.canRetryFailed).toBe(false);
	await state.retryFailed();
	expect(apply).toHaveBeenCalledOnce();
});

it('does not retry a known operation when all failed files are excluded', async () => {
	const current = fixture();
	vi.spyOn(apiClient, 'getBatchJob').mockResolvedValue(current);
	const apply = vi.spyOn(apiClient, 'organizeBatchJob').mockRejectedValue(new Error('Response lost'));
	const { state } = await mount();
	await state.organizeAll();
	state.fileStatuses.set(filePath, { status: 'failed', error: 'Credit conflict' });
	state.job!.excluded = { [filePath]: true };
	await state.retryFailed();
	expect(apply).toHaveBeenCalledOnce();
});

it('retries authoritative failed paths with known options after a lost response and quota failure', async () => {
	let current = fixture();
	vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
	const setItem = Storage.prototype.setItem;
	vi.spyOn(Storage.prototype, 'setItem').mockImplementation(function (this: Storage, k, v) {
		if (k === key) throw new DOMException('Quota full', 'QuotaExceededError');
		return setItem.call(this, k, v);
	});
	const apply = vi.spyOn(apiClient, 'organizeBatchJob')
		.mockRejectedValueOnce(new Error('Response lost')).mockResolvedValue({} as never);
	const { state } = await mount();
	state.organizeOperation = 'hardlink';
	await state.organizeAll(true, true);
	current = structuredClone(current);
	current.status = 'failed'; current.apply_generation = 1;
	current.failed = 1; current.completed = 0;
	current.results[filePath].status = 'failed';
	current.results[filePath].error_code = 'credit_conflict';
	await state.retryLoad();
	expect(state.fileStatuses.size).toBe(0);
	expect(state.canRetryFailed).toBe(true);
	state.organizeOperation = 'move';
	await state.retryFailed();
	expect(apply).toHaveBeenCalledTimes(2);
	expect(apply.mock.calls[1][1]).toMatchObject({
		copy_only: true, link_mode: 'hard', skip_nfo: true, skip_download: true,
		retry_file_paths: [filePath],
	});
});
