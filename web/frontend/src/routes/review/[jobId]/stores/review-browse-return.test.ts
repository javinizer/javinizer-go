import { cleanup, render, waitFor } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import { tick } from 'svelte';
import { beforeNavigate, goto } from '$app/navigation';
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
let leaveWhileMounted: ((target?: string) => void) | undefined;
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

beforeEach(() => {
	vi.clearAllMocks(); localStorage.clear(); sessionStorage.clear();
	vi.mocked(beforeNavigate).mockImplementation((callback) => {
		leaveWhileMounted = (target = '/jobs') => callback({ from: { url: new URL('http://localhost/review/retry-job') }, to: { url: new URL(target, 'http://localhost') } } as never);
	});
	localStorage.setItem('javinizer_input_path', '/other-browse');
	vi.spyOn(apiClient, 'browse').mockImplementation(async ({ path }) => ({ current_path: path, items: [] }) as never);
	vi.spyOn(apiClient, 'previewOrganize').mockResolvedValue({} as never);
	vi.spyOn(apiClient, 'getExistingNFO').mockResolvedValue({} as never);
});
afterEach(() => {
	cleanup(); vi.useRealTimers(); clients.splice(0).forEach((c) => c.clear());
	vi.restoreAllMocks(); localStorage.clear(); sessionStorage.clear();
});

it.each([
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update' },
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'organize' },
	{ fromJobs: false, source: '/synthetic', failure: false, operation: 'update' },
	{ fromJobs: true, source: undefined, failure: false, operation: 'update' },
	{ fromJobs: true, source: '/synthetic', failure: true, operation: 'update' },
	{ fromJobs: true, source: '/missing', failure: false, operation: 'update', unavailable: true },
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update', abandon: 'unmount' },
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update', abandon: 'new-job' },
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update', abandon: 'new-apply' },
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update', abandon: 'pending-navigation' },
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update', abandon: 'view-query' },
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update', abandon: 'before-redirect' },
])('actual completion handoff %j', async ({ fromJobs, source, failure, operation, unavailable = false, abandon }) => {
	if (unavailable) vi.mocked(apiClient.browse).mockRejectedValue(new Error('source unavailable'));
	let release: (() => void) | undefined;
	if (abandon) vi.mocked(apiClient.browse).mockImplementation(() => new Promise((resolve) => { release = () => resolve({ current_path: source!, items: [] }); }));
	let current = fixture(); current.update = operation === 'update'; current.browse_source_path = source;
	vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
	const accept = async () => {
		current = structuredClone(current); current.apply_generation = (current.apply_generation ?? 0) + 1;
		current.status = failure ? 'failed' : 'completed';
		current.results[filePath].status = failure ? 'failed' : 'completed';
		if (failure) { current.failed = 1; current.completed = 0; current.results[filePath].error = 'synthetic failure'; }
		return { message: 'accepted', apply_generation: current.apply_generation };
	};
	vi.spyOn(apiClient, 'updateBatchJob').mockImplementation(accept);
	vi.spyOn(apiClient, 'organizeBatchJob').mockImplementation(accept);
	const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 }, mutations: { retry: false, gcTime: 0 } } });
	clients.push(client);
	let state!: ReturnType<typeof createReviewState>;
	const view = render(Harness, { jobId: 'retry-job', fromJobs, capture: (value) => { state = value; } }, { wrapper: QueryClientWrapper, wrapperProps: { client } });
	await waitFor(() => expect(state.job?.id).toBe('retry-job'));
	state.destinationPath = '/synthetic/output';
	expect(goto).not.toHaveBeenCalled();
	expect(localStorage.getItem('javinizer_input_path')).toBe('/other-browse');
	vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
	if (operation === 'update') await state.updateAll(); else await state.organizeAll(true, true);
	await vi.advanceTimersByTimeAsync(1500); await tick();
	await vi.advanceTimersByTimeAsync(800); await tick();
	if (abandon === 'before-redirect') leaveWhileMounted!();
	await vi.advanceTimersByTimeAsync(5000); await tick();
	if (abandon === 'before-redirect') expect(apiClient.browse).not.toHaveBeenCalled();
	if (abandon && abandon !== 'before-redirect') {
		expect(apiClient.browse).toHaveBeenCalledExactlyOnceWith({ path: source, scope: 'operation' }, { timeoutMs: 5000 });
		if (abandon === 'unmount') view.unmount();
		else if (abandon === 'new-apply') await state.updateAll();
		else if (abandon === 'pending-navigation') leaveWhileMounted!();
		else if (abandon === 'view-query') leaveWhileMounted!('/review/retry-job?view=detail');
		else { await view.rerender({ jobId: 'new-job' }); await tick(); }
		release!(); await tick(); await Promise.resolve();
	}
	if (failure || (abandon && abandon !== 'view-query')) expect(goto).not.toHaveBeenCalled();
	else if (fromJobs && source && !unavailable) expect(goto).toHaveBeenCalledExactlyOnceWith('/browse', { state: { browseReturnPath: source } });
	else expect(goto).toHaveBeenCalledExactlyOnceWith('/browse');
	// Only successful Browse listing owns persistence, never Review entry/apply.
	expect(localStorage.getItem('javinizer_input_path')).toBe('/other-browse');
});
