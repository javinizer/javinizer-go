import { cleanup, render, waitFor } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { apiClient } from '$lib/api/client';
import type { BatchJobResponse } from '$lib/api/types';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import { toastStore } from '$lib/stores/toast';
import type { createReviewState } from './review-state.svelte';
import Harness from '../../../../../test/harness/ReviewStateHarness.svelte';

vi.mock('$app/environment', () => ({ browser: true }));
vi.mock('$lib/query/queries', () => ({ createConfigQuery: () => ({ data: null }) }));
vi.mock('$lib/stores/websocket', () => ({
	websocketStore: { subscribe: () => () => {}, clearMessages: vi.fn() },
}));

const filePath = '/synthetic/movie.mp4';
const recoveryKey = 'javinizer.review.applyRecovery.retry-job';
const clients: QueryClient[] = [];
function job(update: boolean): BatchJobResponse {
	return {
		id: 'retry-job',
		update,
		status: 'completed',
		results: {
			[filePath]: {
				file_path: filePath,
				result_id: 'result',
				movie_id: 'movie',
				status: 'completed',
				movie: { id: 'movie', title: 'Synthetic movie', actresses: [], credits: [] },
				revision: 1,
			},
		},
		total_files: 1,
		completed: 1,
		failed: 0,
		progress: 100,
		excluded: {},
	} as unknown as BatchJobResponse;
}

beforeEach(() => {
	sessionStorage.clear();
	localStorage.clear();
	vi.spyOn(apiClient, 'previewOrganize').mockResolvedValue({} as never);
	vi.spyOn(apiClient, 'getExistingNFO').mockResolvedValue({} as never);
});
afterEach(() => {
	cleanup();
	clients.splice(0).forEach((client) => client.clear());
	vi.restoreAllMocks();
	sessionStorage.clear();
	localStorage.clear();
	toastStore.clear();
});

const cases = (['organize', 'update'] as const).flatMap((operation) =>
	(['missing', 'write-failed'] as const).flatMap((recovery) =>
		(['failed', 'pending'] as const).map((refresh) => ({ operation, recovery, refresh })),
	),
);
it.each(cases)(
	'blocks $operation retry with $recovery recovery and $refresh refresh, without edited overlays',
	async ({ operation, recovery, refresh }) => {
		const fixture = job(operation === 'update');
		const read = vi.spyOn(apiClient, 'getBatchJob').mockResolvedValue(fixture);
		const apply = vi
			.spyOn(apiClient, operation === 'organize' ? 'organizeBatchJob' : 'updateBatchJob')
			.mockResolvedValue({} as never);
		const save = vi.spyOn(apiClient, 'updateBatchMovie').mockResolvedValue({} as never);
		const warning = vi.spyOn(toastStore, 'warning');
		const client = new QueryClient({
			defaultOptions: {
				queries: { retry: false, gcTime: 0 },
				mutations: { retry: false, gcTime: 0 },
			},
		});
		clients.push(client);
		let state!: ReturnType<typeof createReviewState>;
		render(
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
		state.destinationPath = '/synthetic/destination';

		if (recovery === 'write-failed') {
			const originalSetItem = Storage.prototype.setItem;
			const storage = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(function (
				this: Storage,
				key,
				value,
			) {
				if (key === recoveryKey)
					throw new DOMException('Synthetic quota failure', 'QuotaExceededError');
				return originalSetItem.call(this, key, value);
			});
			apply.mockRejectedValueOnce(new Error('Synthetic initial Apply failure'));
			await (operation === 'organize' ? state.organizeAll() : state.updateAll());
			expect(storage.mock.calls.some(([key]) => key === recoveryKey)).toBe(true);
			expect(apply).toHaveBeenCalledOnce();
			apply.mockClear();
		}
		expect(localStorage.getItem(recoveryKey)).toBeNull();
		expect(state.editedMovies.size).toBe(0);
		state.fileStatuses.set(filePath, { status: 'failed', error: 'Synthetic failed file' });

		let finishRefresh!: (value: BatchJobResponse) => void;
		if (refresh === 'failed') read.mockRejectedValueOnce(new Error('Authoritative refresh failed'));
		else
			read.mockImplementationOnce(
				() =>
					new Promise((resolve) => {
						finishRefresh = resolve;
					}),
			);
		const refreshing = state.refreshAfterCollision('movie');
		if (refresh === 'failed') await refreshing;
		else await waitFor(() => expect(finishRefresh).toBeTypeOf('function'));

		// Repeated retry clicks must neither dispatch Apply nor mutate failed paths.
		await state.retryFailed();
		await state.retryFailed();
		expect(apply).not.toHaveBeenCalled();
		expect(save).not.toHaveBeenCalled();
		expect(warning).toHaveBeenCalledTimes(2);
		expect(state.organizing).toBe(false);
		expect(state.fileStatuses.get(filePath)?.status).toBe('failed');
		expect(localStorage.getItem(recoveryKey)).toBeNull();

		if (refresh === 'pending') {
			finishRefresh(fixture);
			await refreshing;
		} else await state.retryLoad();
		expect(state.refreshError).toBeNull();
		await state.retryFailed();
		expect(apply).toHaveBeenCalledOnce();
		expect(apply.mock.calls[0][1]).toMatchObject({ retry_file_paths: [filePath] });
		expect(save).not.toHaveBeenCalled();
	},
);
