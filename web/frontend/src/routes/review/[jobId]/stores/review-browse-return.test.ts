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
import OrganizeStatusCard from '../components/OrganizeStatusCard.svelte';
vi.mock('$app/environment', () => ({ browser: true }));
vi.mock('$lib/query/queries', () => ({ createConfigQuery: () => ({ data: null }) }));
const socket = vi.hoisted(() => ({ listener: null as null | ((state: { messages: unknown[] }) => void) }));
vi.mock('$lib/stores/websocket', () => ({
	websocketStore: { subscribe: (listener: NonNullable<typeof socket.listener>) => { socket.listener = listener; return () => { socket.listener = null; }; }, clearMessages: vi.fn() },
}));
// Capture only the real controller's validated snapshot ingress; do not replace its behavior.
const controllerIngress = vi.hoisted(() => ({ setJob: null as null | ((job: BatchJobResponse) => void) }));
vi.mock('../logic/organize-controller', async (importOriginal) => {
	const actual = await importOriginal<typeof import('../logic/organize-controller')>();
	return { ...actual, createOrganizeController: (deps: Parameters<typeof actual.createOrganizeController>[0]) => {
		controllerIngress.setJob = deps.setJob;
		return actual.createOrganizeController(deps);
	} };
});
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
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update', abandon: 'warning-before-redirect' },
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update', abandon: 'warning-job-preflight' },
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update', abandon: 'warning-live-preflight' },
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update', abandon: 'warning-unrelated' },
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update', abandon: 'warning-excluded' },
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update', staleWarning: true },
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update', abandon: 'generation-before-redirect' },
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update', abandon: 'warning-display-old-failed' },
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update', abandon: 'warning-display-stale-failure' },
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update', abandon: 'warning-display-warningless-rest' },
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update', abandon: 'warning-display-new-generation' },
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update', abandon: 'warning-display-success' },
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update', abandon: 'warning-display-stale-skip' },
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update', abandon: 'warning-display-reload' },
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update', abandon: 'warning-display-same-tick' },
	{ fromJobs: true, source: '/synthetic', failure: false, operation: 'update', abandon: 'warning-display-new-apply' },
])('actual completion handoff %j', async ({ fromJobs, source, failure, operation, unavailable = false, abandon, staleWarning = false }) => {
	if (unavailable) vi.mocked(apiClient.browse).mockRejectedValue(new Error('source unavailable'));
	let release: (() => void) | undefined;
	if (abandon) vi.mocked(apiClient.browse).mockImplementation(() => new Promise((resolve) => { release = () => resolve({ current_path: source!, items: [] }); }));
	let current = fixture(); current.update = operation === 'update'; current.browse_source_path = source;
	vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
	const accept = async () => {
		current = structuredClone(current); current.apply_generation = (current.apply_generation ?? 0) + 1;
		current.status = failure ? 'failed' : staleWarning || (abandon === 'warning-display-new-apply' && current.apply_generation === 2) ? 'organizing' : 'completed';
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
	if (staleWarning) {
		await vi.advanceTimersByTimeAsync(1500); await tick();
		expect(state.organizeStatus).toBe('organizing');
		socket.listener!({ messages: [{ job_id: 'retry-job', status: 'pending', apply_generation: 1, progress: 42 }] });
		expect(state.organizeProgress).toBe(42); // Prove current WS lane is active, not terminal-ignored.
		socket.listener!({ messages: [{ job_id: 'retry-job', file_path: filePath, status: 'updated', apply_generation: 0, recovery_warning: 'old-generation warning' }] });
		expect(state.fileStatuses.get(filePath)?.recovery_warning).toBeUndefined();
		current.status = 'completed';
	}
	await vi.advanceTimersByTimeAsync(1500); await tick();
	await vi.advanceTimersByTimeAsync(800); await tick();
	if (abandon === 'before-redirect') leaveWhileMounted!();
	if (abandon === 'generation-before-redirect') {
		current = structuredClone(current); current.apply_generation = 2;
		client.setQueryData(['batch-job', 'retry-job'], current);
		await vi.advanceTimersByTimeAsync(0); await tick();
		expect(state.job?.apply_generation).toBe(2);
	}
	if (abandon === 'warning-before-redirect') state.fileStatuses.set(filePath, { status: 'success', recovery_warning: 'synthetic cleanup warning' });
	await vi.advanceTimersByTimeAsync(5000); await tick();
	if (abandon === 'before-redirect' || abandon === 'warning-before-redirect' || abandon === 'generation-before-redirect') expect(apiClient.browse).not.toHaveBeenCalled();
	if (abandon && abandon !== 'before-redirect' && abandon !== 'warning-before-redirect' && abandon !== 'generation-before-redirect') {
		expect(apiClient.browse).toHaveBeenCalledExactlyOnceWith({ path: source, scope: 'operation' }, { timeoutMs: 5000 });
		if (abandon === 'unmount') view.unmount();
		else if (abandon === 'new-apply') await state.updateAll();
		else if (abandon === 'pending-navigation') leaveWhileMounted!();
		else if (abandon === 'view-query') leaveWhileMounted!('/review/retry-job?view=detail');
		else if (abandon === 'warning-live-preflight') state.fileStatuses.set(filePath, { status: 'success', recovery_warning: 'synthetic cleanup warning' });
		else if (abandon === 'warning-job-preflight' || abandon === 'warning-unrelated' || abandon === 'warning-excluded' || abandon?.startsWith('warning-display-')) {
			current = structuredClone(current);
			if (abandon === 'warning-display-old-failed' || abandon === 'warning-display-same-tick') {
				state.fileStatuses.set(filePath, { status: 'failed', error: 'prior failed receipt' });
				current.results[filePath].status = 'failed';
			}
			const liveBeforeWarning = state.fileStatuses.get(filePath);
			const warningPath = abandon === 'warning-unrelated' ? '/historical/other.mp4' : filePath;
			current.results[warningPath] = { ...current.results[filePath], file_path: warningPath, recovery_warning: 'synthetic cleanup warning' };
			if (abandon === 'warning-unrelated') state.fileStatuses.set(warningPath, { status: 'success', recovery_warning: 'historical warning' });
			if (abandon === 'warning-excluded') current.excluded[filePath] = true;
			if (abandon === 'warning-display-same-tick') {
				controllerIngress.setJob!(structuredClone(current));
				const stale = structuredClone(current);
				delete stale.results[filePath].recovery_warning;
				stale.results[filePath].status = 'failed';
				controllerIngress.setJob!(stale); // No tick/render between accepted snapshots.
			} else client.setQueryData(['batch-job', 'retry-job'], current);
			await vi.advanceTimersByTimeAsync(0); await tick();
			if (abandon === 'warning-display-same-tick') expect(state.job?.results[filePath].recovery_warning).toBeUndefined();
			else expect(state.job?.results[warningPath].recovery_warning).toBe('synthetic cleanup warning');
			if (abandon === 'warning-job-preflight' || abandon?.startsWith('warning-display-')) {
				expect(state.displayedFileStatuses.get(filePath)?.recovery_warning).toBe('synthetic cleanup warning');
				expect(state.fileStatuses.get(filePath)).toBe(liveBeforeWarning); // View-only, including prior Failed.
				expect(state.displayedFileStatuses.get(filePath)?.status).toBe('success');
				const card = render(OrganizeStatusCard, { organizeStatus: state.organizeStatus, organizeProgress: 100, fileStatuses: state.displayedFileStatuses, expectedOrganizeFilePaths: [filePath], isUpdateMode: true, onRetryFailed: vi.fn(), onContinue: vi.fn() });
				expect(card.getByRole('status').textContent).toContain('synthetic cleanup warning');
				expect(card.queryByRole('button', { name: /Retry Failed/i })).toBeNull();
				card.unmount();
			}
			if (abandon === 'warning-display-stale-failure' || abandon === 'warning-display-stale-skip') {
				const outcome = abandon === 'warning-display-stale-failure' ? 'failed' : 'skipped';
				state.fileStatuses.set(filePath, { status: outcome, error: outcome === 'failed' ? 'new failure' : undefined });
				// Same-generation failure/skip has no ordering authority over committed output.
				await tick();
				// Cloning the same-generation snapshot is not ordering or cleanup proof.
				current = structuredClone(current); client.setQueryData(['batch-job', 'retry-job'], current);
				await vi.advanceTimersByTimeAsync(0); await tick();
				expect(state.displayedFileStatuses.get(filePath)?.status).toBe('success');
				expect(state.displayedFileStatuses.get(filePath)?.recovery_warning).toBe('synthetic cleanup warning');
			}
			if (abandon === 'warning-display-warningless-rest' || abandon === 'warning-display-new-generation' || abandon === 'warning-display-stale-failure') {
				current = structuredClone(current);
				if (abandon === 'warning-display-new-generation') {
					current.apply_generation = 2; current.status = 'failed';
					delete current.results[filePath].recovery_warning; current.results[filePath].status = 'failed';
					state.fileStatuses.set(filePath, { status: 'failed', error: 'new-generation failure' });
					state.organizeStatus = 'failed';
				}
				else delete current.results[filePath].recovery_warning;
				if (abandon === 'warning-display-stale-failure') current.results[filePath].status = 'failed';
				client.setQueryData(['batch-job', 'retry-job'], current);
				await vi.advanceTimersByTimeAsync(0); await tick();
				if (abandon === 'warning-display-new-generation') {
					expect(state.displayedFileStatuses.get(filePath)?.recovery_warning).toBeUndefined();
					expect(state.displayedFileStatuses.get(filePath)?.status).toBe('failed');
				} else {
					expect(state.displayedFileStatuses.get(filePath)?.recovery_warning).toBe('synthetic cleanup warning');
				}
			}
			if (abandon === 'warning-display-success') {
				state.fileStatuses.set(filePath, { status: 'replaced' }); await tick();
				expect(state.displayedFileStatuses.get(filePath)).toEqual({ status: 'replaced', recovery_warning: 'synthetic cleanup warning' });
			}
			if (abandon === 'warning-display-stale-failure' || abandon === 'warning-display-warningless-rest') {
				const applyCalls = vi.mocked(apiClient.updateBatchJob).mock.calls.length;
				await state.retryFailed();
				expect(vi.mocked(apiClient.updateBatchJob).mock.calls.length).toBe(applyCalls);
			}
			if (abandon === 'warning-display-new-apply') {
				// Backend exposes generation/running before each old result is processed.
				await state.updateAll(); await vi.advanceTimersByTimeAsync(1500); await tick();
				expect(state.job?.apply_generation).toBe(2);
				expect(state.job?.results[filePath].recovery_warning).toBe('synthetic cleanup warning');
				expect(state.displayedFileStatuses.get(filePath)?.recovery_warning).toBeUndefined();
				delete current.results[filePath].recovery_warning;
				current.status = 'completed';
				await vi.advanceTimersByTimeAsync(1500); await tick();
				expect(state.displayedFileStatuses.get(filePath)?.recovery_warning).toBeUndefined();
			}
			if (abandon === 'warning-display-reload') {
				view.unmount();
				vi.useRealTimers();
				const freshClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } }); clients.push(freshClient);
				render(Harness, { jobId: 'retry-job', fromJobs, capture: (value) => { state = value; } }, { wrapper: QueryClientWrapper, wrapperProps: { client: freshClient } });
				await waitFor(() => expect(state.job?.results[filePath].recovery_warning).toBe('synthetic cleanup warning'));
				expect(state.displayedFileStatuses.get(filePath)).toEqual({ status: 'success', recovery_warning: 'synthetic cleanup warning' });
				expect(state.canRetryFailed).toBe(false);
				const card = render(OrganizeStatusCard, { organizeStatus: state.organizeStatus, organizeProgress: 100, fileStatuses: state.displayedFileStatuses, expectedOrganizeFilePaths: [filePath], isUpdateMode: true, onRetryFailed: vi.fn(), onContinue: vi.fn() });
				expect(card.getByRole('status').textContent).toContain('synthetic cleanup warning');
				expect(card.queryByRole('button', { name: /Retry Failed/i })).toBeNull();
			}

		}
		else { await view.rerender({ jobId: 'new-job' }); await tick(); }
		release!(); await tick(); await Promise.resolve();
	}
	if (failure || (abandon && !['view-query', 'warning-unrelated', 'warning-excluded'].includes(abandon))) expect(goto).not.toHaveBeenCalled();
	else if (fromJobs && source && !unavailable) expect(goto).toHaveBeenCalledExactlyOnceWith('/browse', { state: { browseReturnPath: source } });
	else expect(goto).toHaveBeenCalledExactlyOnceWith('/browse');
	// Only successful Browse listing owns persistence, never Review entry/apply.
	expect(localStorage.getItem('javinizer_input_path')).toBe('/other-browse');
});
