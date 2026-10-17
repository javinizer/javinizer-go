import { cleanup, render, waitFor } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { apiClient } from '$lib/api/client';
import type { BatchJobResponse } from '$lib/api/types';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import type { createOrganizeController } from '../logic/organize-controller';
import type { createReviewState } from './review-state.svelte';
import Harness from '../../../../../test/harness/ReviewStateHarness.svelte';

type RefreshResult = { data?: BatchJobResponse; error?: Error };
const boundary = vi.hoisted(() => ({
	refetch: null as ((options: { cancelRefetch: boolean }) => Promise<RefreshResult>) | null,
	organizeDeps: null as Parameters<typeof createOrganizeController>[0] | null,
}));

vi.mock('$app/environment', () => ({ browser: true }));
vi.mock('$lib/query/queries', () => ({ createConfigQuery: () => ({ data: null }) }));
vi.mock('$lib/stores/websocket', () => ({
	websocketStore: { subscribe: () => () => {}, clearMessages: vi.fn() },
}));
vi.mock('@tanstack/svelte-query', async (importOriginal) => {
	const actual = await importOriginal<typeof import('@tanstack/svelte-query')>();
	return {
		...actual,
		createQuery: (...args: Parameters<typeof actual.createQuery>) => {
			const query = actual.createQuery(...args);
			if (args[0]().queryKey[0] !== 'batch-job') return query;
			// Keep the real reactive query for loading/navigation. Override only the
			// refetch boundary to deliver exceptions and reentrant result objects
			// that the store must defend against independently of query internals.
			return new Proxy(query, {
				get(target, property, receiver) {
					if (property === 'refetch' && boundary.refetch) return boundary.refetch;
					return Reflect.get(target, property, receiver);
				},
			});
		},
	};
});
vi.mock('../logic/organize-controller', async (importOriginal) => {
	const actual = await importOriginal<typeof import('../logic/organize-controller')>();
	return {
		...actual,
		createOrganizeController: (deps: Parameters<typeof createOrganizeController>[0]) => {
			boundary.organizeDeps = deps;
			return actual.createOrganizeController(deps);
		},
	};
});

const clients: QueryClient[] = [];
const filePath = '/synthetic/movie.mp4';
const recoveryKey = 'javinizer.review.applyRecovery.a';

function fixture(id = 'a', title = 'Initial movie'): BatchJobResponse {
	return {
		id,
		status: 'completed',
		apply_generation: 0,
		destination: '/synthetic/output',
		results: {
			[filePath]: {
				file_path: filePath,
				result_id: id,
				movie_id: id,
				status: 'completed',
				revision: 1,
				movie: { id, title, actresses: [], credits: [] },
			},
		},
		total_files: 1,
		completed: 1,
		failed: 0,
		progress: 100,
		excluded: {},
	} as unknown as BatchJobResponse;
}

function deferred<T>() {
	let resolve!: (value: T) => void;
	let reject!: (reason: unknown) => void;
	const promise = new Promise<T>((done, fail) => {
		resolve = done;
		reject = fail;
	});
	return { promise, resolve, reject };
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
		{ jobId: 'a', capture: (value) => (state = value) },
		{ wrapper: QueryClientWrapper, wrapperProps: { client } },
	);
	await waitFor(() => expect(state.job?.id).toBe('a'));
	return { state, view };
}

beforeEach(() => {
	boundary.refetch = null;
	boundary.organizeDeps = null;
	localStorage.clear();
	sessionStorage.clear();
	vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async (id) => fixture(id));
	vi.spyOn(apiClient, 'previewOrganize').mockResolvedValue({} as never);
	vi.spyOn(apiClient, 'getExistingNFO').mockResolvedValue({} as never);
});

afterEach(() => {
	cleanup();
	clients.splice(0).forEach((client) => client.clear());
	vi.restoreAllMocks();
	localStorage.clear();
	sessionStorage.clear();
});

it('ignores an accepted-Apply callback after its recovery owner was cleared', async () => {
	const { state } = await mount();
	const request = deferred<never>();
	vi.spyOn(apiClient, 'organizeBatchJob').mockReturnValueOnce(request.promise);
	const applying = state.organizeAll();
	await waitFor(() => expect(apiClient.organizeBatchJob).toHaveBeenCalledOnce());
	expect(sessionStorage.getItem(recoveryKey)).not.toBeNull();
	const deps = boundary.organizeDeps!;
	deps.clearApplyRecovery!();
	deps.setOrganizing(false);
	deps.setOrganizeStatus('idle');
	const newerRecord = JSON.stringify({
		jobId: 'a',
		operation: 'organize',
		preApplyGeneration: 8,
		acceptedApplyGeneration: 9,
		operationMode: 'organize',
		destination: '/synthetic/newer-output',
		skipNfo: false,
		skipDownload: true,
		organizeOperation: 'copy',
		eligibleFilePaths: [filePath],
		failed: { [filePath]: 'Newer owner failure' },
		succeeded: [],
	});
	sessionStorage.setItem(recoveryKey, newerRecord);
	const writes = vi.spyOn(Storage.prototype, 'setItem');
	const removals = vi.spyOn(Storage.prototype, 'removeItem');
	// Invoke the captured callback directly: the state boundary must remain safe
	// even if a delayed controller callback outlives its recovery ownership.
	deps.recordApplyAccepted!(1);
	expect(sessionStorage.getItem(recoveryKey)).toBe(newerRecord);
	expect(writes).not.toHaveBeenCalled();
	expect(removals).not.toHaveBeenCalled();
	expect(state.job?.apply_generation).toBe(0);
	expect(state.organizing).toBe(false);
	request.resolve({ apply_generation: 1 } as never);
	await applying;
	expect(sessionStorage.getItem(recoveryKey)).toBe(newerRecord);
});

it('does not install a cloned refresh that starts a newer refresh while being serialized', async () => {
	const { state } = await mount();
	const newer = deferred<RefreshResult>();
	let currentRefresh!: Promise<void>;
	const stale = fixture('a', 'Stale refresh');
	const onClone = vi.fn(() => {
		currentRefresh = state.refreshAfterCollision('a');
		return stale;
	});
	const refetch = vi
		.fn()
		.mockResolvedValueOnce({ data: { ...stale, toJSON: onClone } })
		.mockReturnValueOnce(newer.promise);
	boundary.refetch = refetch;
	await state.refreshAfterCollision('a');
	expect(onClone).toHaveBeenCalledOnce();
	expect(refetch).toHaveBeenCalledTimes(2);
	expect(refetch).toHaveBeenNthCalledWith(2, { cancelRefetch: true });
	expect(state.currentMovie?.title).toBe('Initial movie');
	expect(state.refreshError).toBeNull();
	expect(state.creditWritesBlocked).toBe(true);
	expect(state.beginCreditMutation('credit:new', 'a')).toBeNull();
	newer.resolve({ data: fixture('a', 'Newest confirmed movie') });
	await currentRefresh;
	expect(state.currentMovie?.title).toBe('Newest confirmed movie');
	expect(state.creditWritesBlocked).toBe(false);
});

it('keeps the latest refresh blocked when a superseded same-job refetch throws', async () => {
	const { state } = await mount();
	const stale = deferred<RefreshResult>();
	const newer = deferred<RefreshResult>();
	boundary.refetch = vi.fn().mockReturnValueOnce(stale.promise).mockReturnValueOnce(newer.promise);
	const staleRefresh = state.refreshAfterCollision('a');
	const currentRefresh = state.refreshAfterCollision('a');
	stale.reject(new Error('Obsolete refresh exception'));
	await staleRefresh;
	expect(state.refreshError).toBeNull();
	expect(state.currentMovie?.title).toBe('Initial movie');
	expect(state.creditWritesBlocked).toBe(true);
	expect(state.beginCreditMutation('credit:new', 'a')).toBeNull();
	newer.resolve({ data: fixture('a', 'Newest confirmed movie') });
	await currentRefresh;
	expect(state.currentMovie?.title).toBe('Newest confirmed movie');
	expect(state.creditWritesBlocked).toBe(false);
});

it.each([false, true])(
	'ignores an old-route thrown refresh after navigation (returned to the same job: %s)',
	async (returnToSameJob) => {
		const { state, view } = await mount();
		const stale = deferred<RefreshResult>();
		boundary.refetch = vi.fn().mockReturnValueOnce(stale.promise);
		const staleRefresh = state.refreshAfterCollision('a');
		await view.rerender({ jobId: 'b' });
		await waitFor(() => expect(state.job?.id).toBe('b'));
		if (returnToSameJob) {
			await view.rerender({ jobId: 'a' });
			boundary.refetch = vi.fn().mockResolvedValueOnce({ data: fixture('a') });
			await state.retryLoad();
			await waitFor(() => expect(state.job?.id).toBe('a'));
		}
		const currentJobId = returnToSameJob ? 'a' : 'b';
		boundary.refetch = vi.fn().mockRejectedValueOnce(new Error('Current refresh exception'));
		await state.refreshAfterCollision(currentJobId);
		expect(state.refreshError).toBe('Current refresh exception');
		stale.reject(new Error('Obsolete route exception'));
		await staleRefresh;
		expect(state.job?.id).toBe(currentJobId);
		expect(state.refreshError).toBe('Current refresh exception');
		expect(state.creditWritesBlocked).toBe(true);
		boundary.refetch = vi.fn().mockResolvedValueOnce({ data: fixture(currentJobId, 'Recovered') });
		await state.retryLoad();
		expect(state.refreshError).toBeNull();
		expect(state.currentMovie?.title).toBe('Recovered');
		expect(state.creditWritesBlocked).toBe(false);
	},
);
