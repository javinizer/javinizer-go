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

const modes = ['move', 'copy', 'hardlink', 'softlink', 'update'] as const;
function markForeignFailure(job: BatchJobResponse, generation = 1) {
	const next = structuredClone(job);
	next.apply_generation = generation;
	next.status = 'failed';
	next.failed = 1;
	next.completed = 0;
	next.results[filePath].status = 'failed';
	next.results[filePath].error_code = 'credit_conflict';
	return next;
}
const definitive = [400, 403, 404, 409, 'APPLY_NOT_STARTED'] as const;
it.each(modes.flatMap((mode) => definitive.map((status) => ({ mode, status }))))(
	'reviewer: definitive $status rejects $mode ownership before another client claims N+1',
	async ({ mode, status }) => {
		let current = fixture();
		current.update = mode === 'update';
		vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
		const rejection = Object.assign(
			new Error('Request never acquired generation'),
			typeof status === 'number' ? { status } : { code: status },
		);
		const apply =
			mode === 'update'
				? vi
						.spyOn(apiClient, 'updateBatchJob')
						.mockRejectedValueOnce(rejection)
						.mockResolvedValue({ message: 'unexpected' })
				: vi
						.spyOn(apiClient, 'organizeBatchJob')
						.mockRejectedValueOnce(rejection)
						.mockResolvedValue({ message: 'unexpected' });
		const { state } = await mount();
		if (mode === 'update')
			await state.updateAll({ force_overwrite: true, preserve_nfo: false, skip_nfo: false });
		else {
			state.organizeOperation = mode;
			await state.organizeAll(true, true);
		}
		expect(apply).toHaveBeenCalledOnce();
		current = markForeignFailure(current);
		await state.retryLoad();
		await state.retryFailed();
		expect(apply).toHaveBeenCalledOnce();
		expect(state.canRetryFailed).toBe(false);
	},
);
const ambiguous = ['network', 'timeout', '500', '502'] as const;
it.each(
	modes.flatMap((mode) =>
		ambiguous.flatMap((kind) => [false, true].map((reload) => ({ mode, kind, reload }))),
	),
)(
	'reviewer: ambiguous $kind $mode launch cannot own foreign N+1 (reload=$reload)',
	async ({ mode, kind, reload }) => {
		let current = fixture();
		current.update = mode === 'update';
		vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
		const rejection =
			kind === 'network'
				? new TypeError('Network error')
				: kind === 'timeout'
					? new DOMException('Timed out', 'AbortError')
					: Object.assign(new Error('Proxy error'), { status: Number(kind) });
		const apply =
			mode === 'update'
				? vi
						.spyOn(apiClient, 'updateBatchJob')
						.mockRejectedValueOnce(rejection)
						.mockResolvedValue({ message: 'unexpected' })
				: vi
						.spyOn(apiClient, 'organizeBatchJob')
						.mockRejectedValueOnce(rejection)
						.mockResolvedValue({ message: 'unexpected' });
		const first = await mount();
		if (mode === 'update')
			await first.state.updateAll({
				preserve_nfo: true,
				force_overwrite: false,
				skip_download: true,
			});
		else {
			first.state.organizeOperation = mode;
			await first.state.organizeAll(true, true);
		}
		current = markForeignFailure(current);
		let state = first.state;
		if (reload) {
			first.view.unmount();
			first.client.clear();
			state = (await mount()).state;
			await waitFor(() => expect(state.organizing).toBe(false), { timeout: 2500 });
		} else await state.retryLoad();
		await state.retryFailed();
		expect(apply).toHaveBeenCalledOnce();
		expect(state.canRetryFailed).toBe(false);
	},
);
it.each(['skip', 'replace'] as const)(
	'retries only late-collision failures using fresh %s review decisions',
	async (policy) => {
		let current = fixture();
		const succeeded = '/synthetic/already-succeeded.mp4';
		current.results[succeeded] = {
			...structuredClone(current.results[filePath]),
			file_path: succeeded,
			result_id: 'succeeded',
		};
		current.total_files = 2;
		vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
		const item = {
			source: filePath,
			destination: '/synthetic/output/movie.mp4',
			kind: 'file',
			source_size: 5,
			destination_size: 7,
			review_token: 'fresh-generation-one',
		};
		const review = vi
			.spyOn(apiClient, 'reviewVideoCollisions')
			.mockImplementation(async (_id, request) => {
				const decision = request.overrides?.video_collision_decisions?.[filePath];
				const resolved = decision?.review_token === item.review_token && decision.policy === policy;
				return {
					items: [item],
					new: 0,
					replaced: resolved && policy === 'replace' ? 1 : 0,
					skipped: resolved && policy === 'skip' ? 1 : 0,
					unresolved: resolved ? 0 : 1,
					same_file: 0,
				};
			});
		let attempts = 0;
		const apply = vi
			.spyOn(apiClient, 'organizeBatchJob')
			.mockImplementation(async (_id, request) => {
				attempts++;
				if (attempts === 1) {
					current = markForeignFailure(current);
					delete current.results[filePath].error_code;
					current.results[filePath].video_collision = {
						code: 'VIDEO_COLLISION_REFRESHED',
						collision: item,
					};
					current.results[succeeded].video_outcome = 'installed';
					return { message: 'started', apply_generation: 1 };
				}
				expect(request.copy_only).toBe(true);
				expect(request.destination).toBe('/synthetic/output');
				expect(request.retry_file_paths).toEqual([filePath]);
				expect(request.expected_apply_generation).toBe(1);
				expect(request.overrides?.video_collision_decisions).toEqual({
					[filePath]: {
						source: filePath,
						destination: item.destination,
						policy,
						review_token: item.review_token,
					},
				});
				current.apply_generation = 2;
				current.status = 'organized';
				current.failed = 0;
				current.results[filePath].status = 'completed';
				current.results[filePath].video_outcome = policy === 'skip' ? 'skipped' : 'replaced';
				return { message: 'started', apply_generation: 2 };
			});
		const { state } = await mount();
		state.organizeOperation = 'copy';
		await state.organizeAll(true, true);
		await waitFor(() => expect(state.organizing).toBe(false), { timeout: 5000 });
		await state.reviewVideoCollisions();
		await state.decideVideoCollision(item, policy);
		expect(state.videoCollisionBlocked).toBe(false);
		state.organizeOperation = 'move';
		state.destinationPath = '/must-not-change-original-settings';
		await state.retryFailed();
		expect(apply).toHaveBeenCalledTimes(2);
		expect(review.mock.calls.at(-1)?.[1].retry_file_paths).toEqual([filePath]);
		await waitFor(() => expect(state.organizing).toBe(false), { timeout: 5000 });
		expect(state.fileStatuses.get(filePath)?.status).toBe(
			policy === 'skip' ? 'skipped' : 'replaced',
		);
	},
);

const updateFlags = [
	{ force_overwrite: true, preserve_nfo: false },
	{ force_overwrite: false, preserve_nfo: true },
	{ force_overwrite: false, preserve_nfo: false },
];
it.each(
	updateFlags.flatMap((flags) =>
		['missing', 'corrupt', 'unreadable'].map((storage) => ({ flags, storage })),
	),
)(
	'reviewer: restored Update $flags with $storage recovery cannot invent original flags',
	async ({ flags, storage }) => {
		let current = fixture();
		current.update = true;
		vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
		const apply = vi
			.spyOn(apiClient, 'updateBatchJob')
			.mockImplementationOnce(async () => {
				current = markForeignFailure(current);
				throw new Error('Original response lost');
			})
			.mockResolvedValue({ message: 'unexpected' });
		const first = await mount();
		await first.state.updateAll(flags);
		first.view.unmount();
		first.client.clear();
		if (storage === 'missing') sessionStorage.removeItem(key);
		if (storage === 'corrupt') sessionStorage.setItem(key, '{bad');
		if (storage === 'unreadable') {
			const get = Storage.prototype.getItem;
			vi.spyOn(Storage.prototype, 'getItem').mockImplementation(function (this: Storage, k) {
				if (k === key) throw new DOMException('Read denied', 'SecurityError');
				return get.call(this, k);
			});
		}
		const { state } = await mount();
		expect(state.editedMovies.size).toBe(0);
		await state.retryFailed();
		expect(apply).toHaveBeenCalledOnce();
		expect(state.canRetryFailed).toBe(false);
	},
);
