import { render, waitFor, cleanup } from '@testing-library/svelte';
import { onlineManager, QueryClient } from '@tanstack/svelte-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import { apiClient } from '$lib/api/client';
import { toastStore } from '$lib/stores/toast';
import type { BatchJobResponse } from '$lib/api/types';
import type { createReviewState } from './review-state.svelte';
import Harness from '../../../../../test/harness/ReviewStateHarness.svelte';
vi.mock('$lib/query/queries', () => ({ createConfigQuery: () => ({ data: null }) }));
vi.mock('$lib/stores/websocket', () => ({
	websocketStore: { subscribe: () => () => {}, clearMessages: vi.fn() },
}));
const job = (id: string): BatchJobResponse =>
	({
		id,
		status: 'completed',
		results: {
			[`/${id}.mp4`]: {
				file_path: `/${id}.mp4`,
				result_id: id,
				movie_id: id,
				status: 'completed',
				movie: { id, title: `Title ${id}`, actresses: [], credits: [] },
				revision: 1,
			},
		},
		total_files: 1,
		completed: 1,
		failed: 0,
		progress: 100,
		excluded: {},
	}) as unknown as BatchJobResponse;
afterEach(() => {
	cleanup();
	onlineManager.setOnline(true);
	toastStore.clear();
	vi.restoreAllMocks();
});
beforeEach(() => {
	vi.spyOn(toastStore, 'success');
	vi.spyOn(apiClient, 'previewOrganize').mockResolvedValue({} as never);
	vi.spyOn(apiClient, 'getExistingNFO').mockResolvedValue({} as never);
});
it('does not carry job A collision refresh error into a healthy job B route', async () => {
	const read = vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async (id) => job(id));
	const save = vi.spyOn(apiClient, 'updateBatchMovie').mockResolvedValue({} as never);
	const client = new QueryClient({
		defaultOptions: {
			queries: { retry: false, gcTime: 0 },
			mutations: { retry: false, gcTime: 0 },
		},
	});
	let state!: ReturnType<typeof createReviewState>;
	const view = render(
		Harness,
		{
			jobId: 'job-a',
			capture: (s) => {
				state = s;
			},
		},
		{ wrapper: QueryClientWrapper, wrapperProps: { client } },
	);
	await waitFor(() => expect(state.job?.id).toBe('job-a'));
	read.mockRejectedValueOnce(new Error('old job refresh failed'));
	await state.refreshAfterCollision('job-a');
	expect(state.refreshError).toBe('old job refresh failed');
	await view.rerender({ jobId: 'job-b' });
	await waitFor(() => expect(state.job?.id).toBe('job-b'));
	state.updateCurrentMovie({ ...state.currentMovie!, title: 'User job B title' });
	await state.saveAllEdits();
	expect({ error: state.refreshError, saves: save.mock.calls.length }).toEqual({
		error: null,
		saves: 1,
	});
});
it('retains latest local title but rebases authoritative credits if user types during refresh', async () => {
	const baseline = job('job-a');
	baseline.results['/job-a.mp4'].movie!.credits = [
		{ id: 41, actress_id: 7, override_name: '', user_override: false },
	];
	baseline.results['/job-a.mp4'].movie!.actresses = [{ id: 7, first_name: 'Canonical' }];
	const read = vi.spyOn(apiClient, 'getBatchJob').mockResolvedValue(baseline);
	const client = new QueryClient({
		defaultOptions: {
			queries: { retry: false, gcTime: 0 },
			mutations: { retry: false, gcTime: 0 },
		},
	});
	let state!: ReturnType<typeof createReviewState>;
	render(
		Harness,
		{
			jobId: 'job-a',
			capture: (s) => {
				state = s;
			},
		},
		{ wrapper: QueryClientWrapper, wrapperProps: { client } },
	);
	await waitFor(() => expect(state.job?.id).toBe('job-a'));
	state.updateCurrentMovie({ ...state.currentMovie!, title: 'First unsaved title' });
	let resolve!: (value: BatchJobResponse) => void;
	read.mockImplementationOnce(
		() =>
			new Promise((r) => {
				resolve = r;
			}),
	);
	const refreshing = state.refreshAfterCollision('job-a');
	await waitFor(() => expect(resolve).toBeTypeOf('function'));
	state.updateCurrentMovie({ ...state.currentMovie!, title: 'Latest unsaved title' });
	const fresh = structuredClone(baseline);
	fresh.results['/job-a.mp4'].movie!.credits![0].override_name = 'Saved movie-only name';
	fresh.results['/job-a.mp4'].movie!.credits![0].user_override = true;
	resolve(fresh);
	await refreshing;
	expect(state.currentMovie?.title).toBe('Latest unsaved title');
	expect(state.currentMovie?.credits?.[0].override_name).toBe('Saved movie-only name');
});
it.each(['organize', 'update'] as const)(
	'does not launch %s with unsaved edits while collision refresh is failed',
	async (operation) => {
		const read = vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async (id) => job(id));
		const apply = vi
			.spyOn(apiClient, operation === 'organize' ? 'organizeBatchJob' : 'updateBatchJob')
			.mockResolvedValue({} as never);
		const client = new QueryClient({
			defaultOptions: {
				queries: { retry: false, gcTime: 0 },
				mutations: { retry: false, gcTime: 0 },
			},
		});
		let state!: ReturnType<typeof createReviewState>;
		render(
			Harness,
			{
				jobId: 'job-a',
				capture: (s) => {
					state = s;
				},
			},
			{ wrapper: QueryClientWrapper, wrapperProps: { client } },
		);
		await waitFor(() => expect(state.job?.id).toBe('job-a'));
		state.destinationPath = '/synthetic/destination';
		state.updateCurrentMovie({ ...state.currentMovie!, title: 'Unsaved title' });
		read.mockRejectedValueOnce(new Error('refresh failed'));
		await state.refreshAfterCollision('job-a');
		await (operation === 'organize' ? state.organizeAll() : state.updateAll());
		expect(apply).not.toHaveBeenCalled();
	},
);
function deferred<T>() {
	let resolve!: (value: T) => void;
	let reject!: (reason: Error) => void;
	const promise = new Promise<T>((yes, no) => {
		resolve = yes;
		reject = no;
	});
	return { promise, resolve, reject };
}
it.each(['success', 'failure'] as const)(
	'ignores late job A refresh %s without unlocking a pending job B refresh',
	async (outcome) => {
		const read = vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async (id) => job(id));
		const save = vi.spyOn(apiClient, 'updateBatchMovie').mockResolvedValue({} as never);
		const client = new QueryClient({
			defaultOptions: {
				queries: { retry: false, gcTime: 0 },
				mutations: { retry: false, gcTime: 0 },
			},
		});
		let state!: ReturnType<typeof createReviewState>;
		const view = render(
			Harness,
			{
				jobId: 'job-a',
				capture: (s) => {
					state = s;
				},
			},
			{ wrapper: QueryClientWrapper, wrapperProps: { client } },
		);
		await waitFor(() => expect(state.job?.id).toBe('job-a'));
		const oldRequest = deferred<BatchJobResponse>();
		read.mockImplementationOnce(() => oldRequest.promise);
		const refreshA = state.refreshAfterCollision('job-a');
		await view.rerender({ jobId: 'job-b' });
		await waitFor(() => expect(state.job?.id).toBe('job-b'));
		state.updateCurrentMovie({ ...state.currentMovie!, title: 'User B title' });
		// A's pending flag must not prevent saves on the healthy new route.
		await state.saveAllEdits();
		expect(save).toHaveBeenCalledOnce();
		save.mockClear();
		state.updateCurrentMovie({ ...state.currentMovie!, title: 'Next B title' });
		const newRequest = deferred<BatchJobResponse>();
		read.mockImplementationOnce(() => newRequest.promise);
		const refreshB = state.refreshAfterCollision('job-b');
		if (outcome === 'success') oldRequest.resolve(job('job-a'));
		else oldRequest.reject(new Error('late A refresh failed'));
		await refreshA;
		await state.saveAllEdits();
		expect(save).not.toHaveBeenCalled();
		expect(state.refreshError).toBeNull();
		newRequest.resolve(job('job-b'));
		await refreshB;
		await state.saveAllEdits();
		expect(save).toHaveBeenCalledOnce();
	},
);
it('keeps saves blocked during a failed refresh retry and permits them after authoritative recovery', async () => {
	const read = vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async (id) => job(id));
	const save = vi.spyOn(apiClient, 'updateBatchMovie').mockResolvedValue({} as never);
	const client = new QueryClient({
		defaultOptions: {
			queries: { retry: false, gcTime: 0 },
			mutations: { retry: false, gcTime: 0 },
		},
	});
	let state!: ReturnType<typeof createReviewState>;
	render(
		Harness,
		{
			jobId: 'job-a',
			capture: (s) => {
				state = s;
			},
		},
		{ wrapper: QueryClientWrapper, wrapperProps: { client } },
	);
	await waitFor(() => expect(state.job?.id).toBe('job-a'));
	state.updateCurrentMovie({ ...state.currentMovie!, title: 'Retained title' });
	read.mockRejectedValueOnce(new Error('refresh failed'));
	await state.refreshAfterCollision('job-a');
	await state.saveAllEdits();
	expect(save).not.toHaveBeenCalled();
	const retry = deferred<BatchJobResponse>();
	read.mockImplementationOnce(() => retry.promise);
	const retrying = state.retryLoad();
	await state.saveAllEdits();
	expect(save).not.toHaveBeenCalled();
	retry.reject(new Error('retry failed'));
	await retrying;
	expect(state.refreshError).toBe('retry failed');
	await state.saveAllEdits();
	expect(save).not.toHaveBeenCalled();
	await state.retryLoad();
	expect(state.refreshError).toBeNull();
	expect(state.currentMovie?.title).toBe('Retained title');
	await state.saveAllEdits();
	expect(save).toHaveBeenCalledOnce();
});
it.each(['success', 'failure'] as const)(
	'late job A save refresh %s does not change job B refresh feedback',
	async (outcome) => {
		const read = vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async (id) => job(id));
		const saveRequest = deferred<void>();
		vi.spyOn(apiClient, 'updateBatchMovie').mockImplementationOnce(
			() => saveRequest.promise as never,
		);
		const client = new QueryClient({
			defaultOptions: {
				queries: { retry: false, gcTime: 0 },
				mutations: { retry: false, gcTime: 0 },
			},
		});
		let state!: ReturnType<typeof createReviewState>;
		const view = render(
			Harness,
			{
				jobId: 'job-a',
				capture: (s) => {
					state = s;
				},
			},
			{ wrapper: QueryClientWrapper, wrapperProps: { client } },
		);
		await waitFor(() => expect(state.job?.id).toBe('job-a'));
		state.updateCurrentMovie({ ...state.currentMovie!, title: 'A saved title' });
		const saving = state.saveAllEdits();
		await view.rerender({ jobId: 'job-b' });
		await waitFor(() => expect(state.job?.id).toBe('job-b'));
		if (outcome === 'success') {
			read.mockRejectedValueOnce(new Error('B refresh failed'));
			await state.refreshAfterCollision('job-b');
		} else {
			const invalidate = client.invalidateQueries.bind(client);
			vi.spyOn(client, 'invalidateQueries').mockImplementation((filters, ...args) =>
				filters?.queryKey?.[1] === 'job-a'
					? Promise.reject(new Error('late A refresh failed'))
					: invalidate(filters, ...args),
			);
		}
		saveRequest.resolve();
		await saving;
		expect(state.refreshError).toBe(outcome === 'success' ? 'B refresh failed' : null);
		expect(state.job?.id).toBe('job-b');
	},
);
it('does not treat a fulfilled query invalidation as recovery when the actual refetch failed', async () => {
	const read = vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async (id) => job(id));
	const saveRequest = deferred<void>();
	const save = vi
		.spyOn(apiClient, 'updateBatchMovie')
		.mockImplementationOnce(() => saveRequest.promise as never);
	const client = new QueryClient({
		defaultOptions: {
			queries: { retry: false, gcTime: 0 },
			mutations: { retry: false, gcTime: 0 },
		},
	});
	let state!: ReturnType<typeof createReviewState>;
	render(
		Harness,
		{
			jobId: 'job-a',
			capture: (s) => {
				state = s;
			},
		},
		{ wrapper: QueryClientWrapper, wrapperProps: { client } },
	);
	await waitFor(() => expect(state.job?.id).toBe('job-a'));
	state.updateCurrentMovie({ ...state.currentMovie!, title: 'First title' });
	const saving = state.saveAllEdits();
	read.mockRejectedValue(new Error('authoritative refetch failed'));
	await state.refreshAfterCollision('job-a');
	saveRequest.resolve();
	await saving;
	expect(state.refreshError).toBe('authoritative refetch failed');
	state.updateCurrentMovie({ ...state.currentMovie!, title: 'Next title' });
	await state.saveAllEdits();
	expect(save).toHaveBeenCalledOnce();
});
it.each(['organize', 'update'] as const)(
	'rechecks refresh safety after the pre-%s save completes',
	async (operation) => {
		const read = vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async (id) => job(id));
		const saveRequest = deferred<void>();
		vi.spyOn(apiClient, 'updateBatchMovie').mockImplementationOnce(
			() => saveRequest.promise as never,
		);
		const apply = vi
			.spyOn(apiClient, operation === 'organize' ? 'organizeBatchJob' : 'updateBatchJob')
			.mockResolvedValue({} as never);
		const client = new QueryClient({
			defaultOptions: {
				queries: { retry: false, gcTime: 0 },
				mutations: { retry: false, gcTime: 0 },
			},
		});
		let state!: ReturnType<typeof createReviewState>;
		render(
			Harness,
			{
				jobId: 'job-a',
				capture: (s) => {
					state = s;
				},
			},
			{ wrapper: QueryClientWrapper, wrapperProps: { client } },
		);
		await waitFor(() => expect(state.job?.id).toBe('job-a'));
		state.destinationPath = '/synthetic/destination';
		state.updateCurrentMovie({ ...state.currentMovie!, title: 'Unsaved title' });
		const applying = operation === 'organize' ? state.organizeAll() : state.updateAll();
		read.mockRejectedValue(new Error('authoritative refetch failed'));
		await state.refreshAfterCollision('job-a');
		saveRequest.resolve();
		await applying;
		expect(state.refreshError).toBe('authoritative refetch failed');
		expect(apply).not.toHaveBeenCalled();
		expect(state.organizing).toBe(false);
	},
);

it.each(['organize', 'update'] as const)(
	'stops %s after a rejected edit even when its authoritative refetch succeeds',
	async (operation) => {
		const baseline = job('job-a');
		const fresh = structuredClone(baseline);
		fresh.results['/job-a.mp4'].revision = 2;
		fresh.results['/job-a.mp4'].movie!.director = 'Concurrent director';
		const read = vi.spyOn(apiClient, 'getBatchJob').mockResolvedValue(baseline);
		const save = vi
			.spyOn(apiClient, 'updateBatchMovie')
			.mockRejectedValueOnce(new Error('CAS conflict'))
			.mockResolvedValue({} as never);
		const apply = vi
			.spyOn(apiClient, operation === 'organize' ? 'organizeBatchJob' : 'updateBatchJob')
			.mockResolvedValue({} as never);
		const client = new QueryClient({
			defaultOptions: {
				queries: { retry: false, gcTime: 0 },
				mutations: { retry: false, gcTime: 0 },
			},
		});
		let state!: ReturnType<typeof createReviewState>;
		render(
			Harness,
			{
				jobId: 'job-a',
				capture: (s) => {
					state = s;
				},
			},
			{ wrapper: QueryClientWrapper, wrapperProps: { client } },
		);
		await waitFor(() => expect(state.job?.id).toBe('job-a'));
		state.destinationPath = '/synthetic/destination';
		state.updateCurrentMovie({ ...state.currentMovie!, title: 'Unsaved title' });
		read.mockResolvedValue(fresh);
		await (operation === 'organize' ? state.organizeAll() : state.updateAll());
		expect(read).toHaveBeenCalledTimes(2);
		expect(state.refreshError).toBeNull();
		expect(apply).not.toHaveBeenCalled();
		expect(state.organizing).toBe(false);
		expect(state.editedMovies.get('/job-a.mp4')).toMatchObject({
			title: 'Unsaved title',
			director: 'Concurrent director',
		});
		expect(toastStore.success).not.toHaveBeenCalled();
		// A successful explicit retry can save the rebased edit and then Apply.
		await (operation === 'organize' ? state.organizeAll() : state.updateAll());
		expect(save).toHaveBeenCalledTimes(2);
		expect(save.mock.calls[1][3]).toBe(2);
		expect(toastStore.success).toHaveBeenCalledOnce();
		expect(apply).toHaveBeenCalledOnce();
		expect(state.editedMovies.size).toBe(0);
	},
);
it.each(['organize', 'update'] as const)(
	'stops %s after a partially rejected batch while clearing only saved families',
	async (operation) => {
		const baseline = job('job-a');
		Object.assign(baseline.results, job('job-b').results);
		baseline.total_files = 2;
		baseline.completed = 2;
		vi.spyOn(apiClient, 'getBatchJob').mockResolvedValue(baseline);
		const save = vi
			.spyOn(apiClient, 'updateBatchMovie')
			.mockImplementation(async (_id, resultId) => {
				if (resultId === 'job-b') throw new Error('CAS conflict');
				return {} as never;
			});
		const apply = vi
			.spyOn(apiClient, operation === 'organize' ? 'organizeBatchJob' : 'updateBatchJob')
			.mockResolvedValue({} as never);
		const client = new QueryClient({
			defaultOptions: {
				queries: { retry: false, gcTime: 0 },
				mutations: { retry: false, gcTime: 0 },
			},
		});
		let state!: ReturnType<typeof createReviewState>;
		render(
			Harness,
			{
				jobId: 'job-a',
				capture: (s) => {
					state = s;
				},
			},
			{ wrapper: QueryClientWrapper, wrapperProps: { client } },
		);
		await waitFor(() => expect(state.job?.id).toBe('job-a'));
		state.destinationPath = '/synthetic/destination';
		state.editedMovies.set('/job-a.mp4', {
			...baseline.results['/job-a.mp4'].movie!,
			title: 'Saved A',
		});
		state.editedMovies.set('/job-b.mp4', {
			...baseline.results['/job-b.mp4'].movie!,
			title: 'Rejected B',
		});
		await (operation === 'organize' ? state.organizeAll() : state.updateAll());
		expect(save).toHaveBeenCalledTimes(2);
		expect(apply).not.toHaveBeenCalled();
		expect(state.organizing).toBe(false);
		expect(state.editedMovies.has('/job-a.mp4')).toBe(false);
		expect(state.editedMovies.get('/job-b.mp4')?.title).toBe('Rejected B');
		expect(toastStore.success).not.toHaveBeenCalled();
	},
);
it('reports a rejected Save All outcome after reconciling the refreshed job', async () => {
	vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async (id) => job(id));
	vi.spyOn(apiClient, 'updateBatchMovie').mockRejectedValue(new Error('CAS conflict'));
	const client = new QueryClient({
		defaultOptions: {
			queries: { retry: false, gcTime: 0 },
			mutations: { retry: false, gcTime: 0 },
		},
	});
	let state!: ReturnType<typeof createReviewState>;
	render(
		Harness,
		{
			jobId: 'job-a',
			capture: (s) => {
				state = s;
			},
		},
		{ wrapper: QueryClientWrapper, wrapperProps: { client } },
	);
	await waitFor(() => expect(state.job?.id).toBe('job-a'));
	state.updateCurrentMovie({ ...state.currentMovie!, title: 'Unsaved title' });
	await expect(state.saveAllEdits()).rejects.toMatchObject({ code: 'APPLY_NOT_STARTED' });
	expect(state.refreshError).toBeNull();
	expect(state.editedMovies.get('/job-a.mp4')?.title).toBe('Unsaved title');
});
it.each(['organize', 'update'] as const)(
	'stops %s when going offline pauses the post-save authoritative refetch',
	async (operation) => {
		const read = vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async (id) => job(id));
		const saveRequest = deferred<void>();
		const save = vi
			.spyOn(apiClient, 'updateBatchMovie')
			.mockImplementationOnce(() => saveRequest.promise as never);
		const apply = vi
			.spyOn(apiClient, operation === 'organize' ? 'organizeBatchJob' : 'updateBatchJob')
			.mockResolvedValue({} as never);
		const client = new QueryClient({
			defaultOptions: {
				queries: { retry: false, gcTime: 0 },
				mutations: { retry: false, gcTime: 0 },
			},
		});
		let state!: ReturnType<typeof createReviewState>;
		render(
			Harness,
			{
				jobId: 'job-a',
				capture: (s) => {
					state = s;
				},
			},
			{ wrapper: QueryClientWrapper, wrapperProps: { client } },
		);
		await waitFor(() => expect(state.job?.id).toBe('job-a'));
		state.destinationPath = '/synthetic/destination';
		state.updateCurrentMovie({ ...state.currentMovie!, title: 'Saved title' });
		const applying = operation === 'organize' ? state.organizeAll() : state.updateAll();
		await waitFor(() => expect(save).toHaveBeenCalledOnce());
		const before = client.getQueryState(['batch-job', 'job-a'])!.dataUpdateCount;
		onlineManager.setOnline(false);
		saveRequest.resolve();
		await applying;
		expect(client.getQueryState(['batch-job', 'job-a'])).toMatchObject({
			status: 'success',
			fetchStatus: 'paused',
			dataUpdateCount: before,
			error: null,
		});
		expect(read).toHaveBeenCalledOnce();
		expect(state.refreshError).toBeTruthy();
		expect(apply).not.toHaveBeenCalled();
		expect(state.organizing).toBe(false);
		expect(toastStore.success).not.toHaveBeenCalled();
		// Reconnecting alone must not launch Apply; explicit authoritative recovery unlocks it.
		onlineManager.setOnline(true);
		await state.retryLoad();
		expect(state.refreshError).toBeNull();
		expect(apply).not.toHaveBeenCalled();
		await (operation === 'organize' ? state.organizeAll() : state.updateAll());
		expect(apply).toHaveBeenCalledOnce();
	},
);

it.each([false,true])('same-route query ingress cannot downgrade newer evidence presence=%s', async (present) => {
	const initial=job('job-a'); const path='/job-a.mp4'; vi.spyOn(apiClient,'getBatchJob').mockResolvedValue(initial);
	const client=new QueryClient({defaultOptions:{queries:{retry:false,gcTime:0},mutations:{retry:false,gcTime:0}}}); let state!:ReturnType<typeof createReviewState>;
	render(Harness,{jobId:'job-a',capture:(value)=>{state=value;}},{wrapper:QueryClientWrapper,wrapperProps:{client}});
	await waitFor(()=>expect(state.job?.id).toBe('job-a'));
	const report={outcome:'evidence_available',admitted_at:'stored',snapshot_digest:'A'}; const latest=structuredClone(initial);
	latest.results[path].revision=3; latest.results[path].offline_evidence=present?report:undefined; client.setQueryData(['batch-job','job-a'],latest);
	await waitFor(()=>expect(state.job?.results[path].revision).toBe(3));
	const stale=structuredClone(initial); stale.results[path].revision=2; stale.results[path].offline_evidence=present?undefined:report;
	client.setQueryData(['batch-job','job-a'],stale); await waitFor(()=>expect(client.getQueryData<BatchJobResponse>(['batch-job','job-a'])?.results[path].revision).toBe(2));
	await new Promise(resolve=>setTimeout(resolve,0)); expect(state.job?.results[path]).toEqual(latest.results[path]); client.clear();
});
