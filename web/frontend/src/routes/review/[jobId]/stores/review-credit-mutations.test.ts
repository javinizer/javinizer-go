import { cleanup, render, waitFor } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import { apiClient } from '$lib/api/client';
import type { BatchJobResponse, FileResult } from '$lib/api/types';
import { createReviewState, isCreditBlockedResult } from './review-state.svelte';
import Harness from '../../../../../test/harness/ReviewStateHarness.svelte';
vi.mock('$lib/query/queries', () => ({ createConfigQuery: () => ({ data: null }) }));
vi.mock('$lib/stores/websocket', () => ({
	websocketStore: { subscribe: () => () => {}, clearMessages: vi.fn() },
}));
const clients: QueryClient[] = [];
const fixture = (id: string): BatchJobResponse =>
	({
		id,
		status: 'completed',
		apply_generation: 0,
		destination: '/synthetic/output',
		results: {
			'/synthetic/movie.mp4': {
				file_path: '/synthetic/movie.mp4',
				result_id: id,
				movie_id: id,
				status: 'completed',
				revision: 1,
				movie: { id, title: 'Original title', actresses: [], credits: [] },
			},
		},
		total_files: 1,
		completed: 1,
		failed: 0,
		progress: 100,
		excluded: {},
	}) as unknown as BatchJobResponse;
beforeEach(() => {
	vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async (id) => fixture(id));
	vi.spyOn(apiClient, 'previewOrganize').mockResolvedValue({} as never);
	vi.spyOn(apiClient, 'getExistingNFO').mockResolvedValue({} as never);
});
afterEach(() => {
	cleanup();
	clients.splice(0).forEach((client) => client.clear());
	vi.restoreAllMocks();
	vi.useRealTimers();
});
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
			jobId: 'a',
			capture: (s) => {
				state = s;
			},
		},
		{ wrapper: QueryClientWrapper, wrapperProps: { client } },
	);
	await waitFor(() => expect(state.job?.id).toBe('a'));
	state.destinationPath = '/synthetic/output';
	return { state, view };
}
function deferred<T>() {
	let resolve!: (value: T) => void;
	const promise = new Promise<T>((done) => {
		resolve = done;
	});
	return { promise, resolve };
}
it('keeps only metadata-bearing credit failures in movie recovery', () => {
	for (const error_code of ['credit_conflict', 'credit_status_unavailable']) {
		expect(isCreditBlockedResult({ status: 'failed', error_code, movie: {} } as FileResult)).toBe(
			true,
		);
		expect(isCreditBlockedResult({ status: 'failed', error_code } as FileResult)).toBe(false);
	}
	expect(
		isCreditBlockedResult({
			status: 'completed',
			error_code: 'credit_conflict',
			movie: {},
		} as FileResult),
	).toBe(false);
	expect(
		isCreditBlockedResult({
			status: 'failed',
			error_code: undefined,
			movie: {},
		} as FileResult),
	).toBe(false);
});
it('blocks every save/Apply/retry entry point from mutation start through authoritative refresh', async () => {
	const organize = vi.spyOn(apiClient, 'organizeBatchJob').mockResolvedValue({} as never);
	const update = vi.spyOn(apiClient, 'updateBatchJob').mockResolvedValue({} as never);
	const save = vi.spyOn(apiClient, 'updateBatchMovie').mockResolvedValue({} as never);
	const { state } = await mount();
	state.updateCurrentMovie({ ...state.currentMovie!, title: 'User draft' });
	const operation = state.beginCreditMutation('credit:41', 'a')!;
	expect(operation.isCurrent()).toBe(true);
	expect(state.isSavingEdits).toBe(true);
	expect(state.beginCreditMutation('credit:42', 'a')).toBeNull();
	await state.organizeAll();
	await state.updateAll();
	await state.retryFailed();
	await state.saveAllEdits();
	expect(organize).not.toHaveBeenCalled();
	expect(update).not.toHaveBeenCalled();
	expect(save).not.toHaveBeenCalled();
	const refresh = deferred<BatchJobResponse>();
	vi.mocked(apiClient.getBatchJob).mockReturnValueOnce(refresh.promise);
	const finishing = operation.finish();
	await waitFor(() => expect(apiClient.getBatchJob).toHaveBeenCalledTimes(2));
	await operation.finish();
	expect(state.beginCreditMutation('credit:42', 'a')).toBeNull();
	await state.organizeAll();
	await state.updateAll();
	await state.retryFailed();
	await state.saveAllEdits();
	expect(organize).not.toHaveBeenCalled();
	expect(update).not.toHaveBeenCalled();
	expect(save).not.toHaveBeenCalled();
	refresh.resolve(fixture('a'));
	await finishing;
	expect(state.creditWritesBlocked).toBe(false);
	expect(state.currentMovie?.title).toBe('User draft');
	await state.saveAllEdits();
	expect(save).toHaveBeenCalledOnce();
});
it.each([new Error('Write rejected'), 'Write rejected'])(
	'keeps a failed mutation blocked until its own retry succeeds (%s)',
	async (error) => {
		const { state } = await mount();
		await state.beginCreditMutation('credit:41', 'a')!.finish(error);
		expect(state.creditMutationError).toBeTruthy();
		expect(state.creditWritesBlocked).toBe(true);
		await state.beginCreditMutation('credit:42', 'a')!.finish();
		expect(state.creditWritesBlocked).toBe(true);
		await state.beginCreditMutation('credit:41', 'a')!.finish();
		expect(state.creditWritesBlocked).toBe(false);
	},
);
it('keeps a persisted write blocked when refresh fails and recovers through Retry Load', async () => {
	const { state } = await mount();
	const operation = state.beginCreditMutation('credit:41', 'a')!;
	vi.mocked(apiClient.getBatchJob).mockRejectedValueOnce(new Error('Refresh failed'));
	await operation.finish();
	expect(state.isSavingEdits).toBe(false);
	expect(state.creditWritesBlocked).toBe(true);
	expect(state.refreshError).toBe('Refresh failed');
	await state.retryLoad();
	expect(state.creditWritesBlocked).toBe(false);
});
it.each([undefined, new Error('Old write rejected')])(
	'ignores old-job completion without releasing a new-job mutation (%s)',
	async (error) => {
		const { state, view } = await mount();
		const old = state.beginCreditMutation('credit:41', 'a')!;
		await view.rerender({ jobId: 'b' });
		await waitFor(() => expect(state.job?.id).toBe('b'));
		const current = state.beginCreditMutation('credit:41', 'b')!;
		const reads = vi.mocked(apiClient.getBatchJob).mock.calls.length;
		await old.finish(error);
		expect(old.isCurrent()).toBe(false);
		expect(apiClient.getBatchJob).toHaveBeenCalledTimes(reads);
		expect(state.creditMutationError).toBeNull();
		expect(state.creditWritesBlocked).toBe(true);
		await current.finish();
		expect(state.creditWritesBlocked).toBe(false);
	},
);
it('rejects old completion after returning to the same job and after unmount', async () => {
	const { state, view } = await mount();
	const old = state.beginCreditMutation('credit:41', 'a')!;
	await view.rerender({ jobId: 'b' });
	await waitFor(() => expect(state.job?.id).toBe('b'));
	await view.rerender({ jobId: 'a' });
	await state.retryLoad();
	await waitFor(() => expect(state.job?.id).toBe('a'));
	const reads = vi.mocked(apiClient.getBatchJob).mock.calls.length;
	await old.finish();
	expect(apiClient.getBatchJob).toHaveBeenCalledTimes(reads);
	const current = state.beginCreditMutation('credit:41', 'a')!;
	view.unmount();
	await current.finish();
	expect(current.isCurrent()).toBe(false);
	expect(apiClient.getBatchJob).toHaveBeenCalledTimes(reads);
	expect(state.beginCreditMutation('credit:42', 'a')).toBeNull();
});
it('refuses credit mutations during an ordinary save or Apply', async () => {
	const { state } = await mount();
	const saving = deferred<never>();
	vi.spyOn(apiClient, 'updateBatchMovie').mockReturnValueOnce(saving.promise);
	state.updateCurrentMovie({ ...state.currentMovie!, title: 'Saved title' });
	const save = state.saveAllEdits();
	await waitFor(() => expect(state.isSavingEdits).toBe(true));
	expect(state.beginCreditMutation('credit:41', 'a')).toBeNull();
	const apply = vi.spyOn(apiClient, 'organizeBatchJob').mockResolvedValue({ apply_generation: 1 } as never);
	await state.organizeAll();
	expect(apply).not.toHaveBeenCalled();
	saving.resolve({} as never);
	await save;
	await waitFor(() => expect(state.isSavingEdits).toBe(false));
	await state.organizeAll();
	expect(apply).toHaveBeenCalledOnce();
	expect(state.beginCreditMutation('credit:41', 'a')).toBeNull();
	await state.organizeAll();
	expect(apply).toHaveBeenCalledOnce();
});
it('permits explicit authoritative reload after an ambiguous failure without losing drafts', async () => {
	const { state } = await mount();
	state.updateCurrentMovie({ ...state.currentMovie!, title: 'Keep user draft' });
	const operation = state.beginCreditMutation('credit:41', 'a')!;
	await state.retryLoad();
	expect(apiClient.getBatchJob).toHaveBeenCalledOnce();
	await operation.finish(new Error('Response lost'));
	expect(state.refreshError).toBe('Response lost');
	await state.retryLoad();
	expect(state.creditWritesBlocked).toBe(false);
	expect(state.currentMovie?.title).toBe('Keep user draft');
});
it.each([false, true])(
	'main Apply includes completed and credit-blocked files in a mixed job (update: %s)',
	async (update) => {
		const job = fixture('a');
		job.update = update;
		job.results['/synthetic/movie.mp4'].status = 'failed';
		job.results['/synthetic/movie.mp4'].error_code = 'credit_conflict';
		job.results['/synthetic/other.mp4'] = {
			...job.results['/synthetic/movie.mp4'],
			file_path: '/synthetic/other.mp4',
			result_id: 'other',
			status: 'completed',
			error_code: undefined,
		};
		vi.mocked(apiClient.getBatchJob).mockResolvedValue(job);
		const apply = vi
			.spyOn(apiClient, update ? 'updateBatchJob' : 'organizeBatchJob')
			.mockResolvedValue({} as never);
		const { state } = await mount();
		await (update ? state.updateAll() : state.organizeAll());
		expect(apply).toHaveBeenCalledOnce();
		expect(apply.mock.calls[0][1]?.retry_file_paths).toEqual([
			'/synthetic/other.mp4',
			'/synthetic/movie.mp4',
		]);
	},
);
it('does not retry excluded files or start Apply when no failures remain', async () => {
	const job = fixture('a');
	job.results['/synthetic/movie.mp4'].status = 'failed';
	job.results['/synthetic/movie.mp4'].error_code = 'credit_conflict';
	job.excluded = { '/synthetic/movie.mp4': true };
	vi.mocked(apiClient.getBatchJob).mockResolvedValue(job);
	const apply = vi.spyOn(apiClient, 'organizeBatchJob').mockResolvedValue({} as never);
	const { state } = await mount();
	await state.retryFailed();
	expect(apply).not.toHaveBeenCalled();
	expect(state.movieResults).toHaveLength(0);
	expect(state.failedResults).toHaveLength(0);
});
it('preserves the controller retry options when persistent recovery is unavailable', async () => {
	const job = fixture('a');
	vi.mocked(apiClient.getBatchJob).mockResolvedValue(job);
	const { state } = await mount();
	vi.useFakeTimers();
	const apply = vi
		.spyOn(apiClient, 'organizeBatchJob')
		.mockResolvedValueOnce({ apply_generation: 1 } as never)
		.mockResolvedValue({} as never);
	await state.organizeAll(true, true, {
		operation_mode: 'metadata-artwork',
		destination: '',
		skip_nfo: true,
		skip_download: true,
	});
	job.apply_generation = 1;
	job.status = 'failed';
	job.results['/synthetic/movie.mp4'].status = 'failed';
	await vi.advanceTimersByTimeAsync(1500);
	vi.useRealTimers();
	state.fileStatuses.set('/synthetic/movie.mp4', { status: 'failed', error: 'Retry me' });
	await state.retryFailed();
	expect(apply).toHaveBeenCalledTimes(2);
	expect(apply.mock.calls[1][1]).toMatchObject({
		skip_nfo: true,
		skip_download: true,
		retry_file_paths: ['/synthetic/movie.mp4'],
		overrides: { operation_mode: 'metadata-artwork' },
	});
});
it('blocks a reloaded update-mode credit failure without a trusted request snapshot', async () => {
	const job = fixture('a');
	job.update = true;
	job.results['/synthetic/movie.mp4'].status = 'failed';
	job.results['/synthetic/movie.mp4'].error_code = 'credit_status_unavailable';
	vi.mocked(apiClient.getBatchJob).mockResolvedValue(job);
	const apply = vi.spyOn(apiClient, 'updateBatchJob').mockResolvedValue({} as never);
	const { state } = await mount();
	await state.retryFailed();
	expect(apply).not.toHaveBeenCalled();
	expect(state.canRetryFailed).toBe(false);
});
it('blocks new credit writes during a standalone authoritative refresh', async () => {
	const { state } = await mount();
	const refresh = deferred<BatchJobResponse>();
	vi.mocked(apiClient.getBatchJob).mockReturnValueOnce(refresh.promise);
	const loading = state.retryLoad();
	expect(state.beginCreditMutation('credit:41', 'a')).toBeNull();
	refresh.resolve(fixture('a')); await loading;
	expect(state.creditWritesBlocked).toBe(false);
});
