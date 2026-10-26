import { cleanup, render, waitFor } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import { apiClient } from '$lib/api/client';
import type { BatchJobResponse, FileResult } from '$lib/api/types';
import { createReviewState } from './review-state.svelte';
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
				movie: { id, code: id, title: 'Original title', actresses: [], credits: [] },
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
it('opens the exact multipart credit-failed movie without discarding drafts or retrying Apply', async () => {
	const job = fixture('a');
	const original = job.results['/synthetic/movie.mp4'];
	job.results['/synthetic/other-part1.mp4'] = {
		...original,
		file_path: '/synthetic/other-part1.mp4',
		result_id: 'other-part1',
		movie_id: 'other',
		movie: { ...original.movie!, id: 'other', code: 'other', title: 'Other movie' },
	};
	job.results['/synthetic/other-part2.mp4'] = {
		...job.results['/synthetic/other-part1.mp4'],
		file_path: '/synthetic/other-part2.mp4',
		result_id: 'other-part2',
		status: 'failed',
		error_code: 'credit_conflict',
		error: 'Backend collision detail',
	};
	vi.mocked(apiClient.getBatchJob).mockResolvedValue(job);
	const apply = vi.spyOn(apiClient, 'organizeBatchJob').mockResolvedValue({} as never);
	const save = vi.spyOn(apiClient, 'updateBatchMovie').mockResolvedValue({} as never);
	const { state } = await mount();
	state.updateCurrentMovie({ ...state.currentMovie!, title: 'Keep user draft' });
	state.viewMode = 'grid-poster';
	const path = '/synthetic/other-part2.mp4';
	expect(state.creditGuidance.get(path)).toEqual({ kind: 'conflict', movieIndex: 1 });
	const previousRequest = state.creditGuidanceRequest;
	expect(state.openCreditGuidance(path)).toBe(true);
	expect(state.currentMovieIndex).toBe(1);
	expect(state.currentMovie?.id).toBe('other');
	expect(state.viewMode).toBe('detail');
	expect(state.creditGuidanceFilePath).toBe(path);
	expect(state.creditGuidanceRequest).toBeGreaterThan(previousRequest);
	expect(apply).not.toHaveBeenCalled();
	expect(save).not.toHaveBeenCalled();
	state.currentMovieIndex = 0;
	expect(state.currentMovie?.title).toBe('Keep user draft');
});

it.each(['credit_conflict', 'credit_status_unavailable'] as const)(
	'recovers structured guidance from a reloaded %s result',
	async (error_code) => {
		const job = fixture('a');
		Object.assign(job.results['/synthetic/movie.mp4'], {
			status: 'failed',
			error_code,
			error: 'Original backend error',
		});
		vi.mocked(apiClient.getBatchJob).mockResolvedValue(job);
		const { state } = await mount();
		expect(state.displayedFileStatuses.get('/synthetic/movie.mp4')).toMatchObject({
			status: 'failed',
			error: 'Original backend error',
			error_code,
		});
		expect(state.creditGuidance.get('/synthetic/movie.mp4')).toEqual({
			kind: error_code === 'credit_conflict' ? 'conflict' : 'unavailable',
			movieIndex: 0,
		});
	},
);

it('does not navigate unknown, excluded, or generic failures to the wrong movie', async () => {
	const job = fixture('a');
	const original = job.results['/synthetic/movie.mp4'];
	job.results['/synthetic/excluded.mp4'] = {
		...original,
		file_path: '/synthetic/excluded.mp4',
		result_id: 'excluded',
		movie_id: 'excluded',
		status: 'failed',
		error_code: 'credit_conflict',
		error: 'Excluded conflict',
	};
	job.excluded = { '/synthetic/excluded.mp4': true };
	vi.mocked(apiClient.getBatchJob).mockResolvedValue(job);
	const { state } = await mount();
	state.fileStatuses.set('/synthetic/excluded.mp4', {
		status: 'failed',
		error_code: 'credit_conflict',
		error: 'Excluded conflict',
	});
	state.fileStatuses.set('/synthetic/missing.mp4', {
		status: 'failed',
		error_code: 'credit_conflict',
		error: 'Historical conflict',
	});
	state.fileStatuses.set('/synthetic/movie.mp4', { status: 'failed', error: 'Permission denied' });
	for (const path of [
		'/synthetic/excluded.mp4',
		'/synthetic/missing.mp4',
		'/synthetic/movie.mp4',
	]) {
		expect(state.openCreditGuidance(path)).toBe(false);
	}
	expect(state.currentMovieIndex).toBe(0);
	expect(state.creditGuidance.get('/synthetic/movie.mp4')).toBeUndefined();
});

it('reissues explicit focus requests and invalidates them when the route changes', async () => {
	const job = fixture('a');
	Object.assign(job.results['/synthetic/movie.mp4'], {
		status: 'failed',
		error_code: 'credit_conflict',
	});
	vi.mocked(apiClient.getBatchJob).mockImplementation(async (id) =>
		id === 'a' ? job : fixture(id),
	);
	const { state, view } = await mount();
	const path = '/synthetic/movie.mp4';
	expect(state.openCreditGuidance(path)).toBe(true);
	const request = state.creditGuidanceRequest;
	expect(state.openCreditGuidance(path)).toBe(true);
	expect(state.creditGuidanceRequest).toBe(request + 1);
	await view.rerender({ jobId: 'b' });
	await waitFor(() => expect(state.job?.id).toBe('b'));
	expect(state.creditGuidanceFilePath).toBeNull();
	expect(state.creditGuidanceRequest).toBeGreaterThan(request + 1);
	expect(state.openCreditGuidance(path)).toBe(false);
});

it('keeps the recorded failure guidance after authoritative refresh reports success', async () => {
	const { state } = await mount();
	const path = '/synthetic/movie.mp4';
	state.fileStatuses.set(path, {
		status: 'failed',
		error_code: 'credit_conflict',
		error: 'Previous attempt conflict',
	});
	await state.retryLoad();
	expect(state.job?.results[path].status).toBe('completed');
	expect(state.displayedFileStatuses.get(path)?.error).toBe('Previous attempt conflict');
	expect(state.creditGuidance.get(path)).toEqual({ kind: 'conflict', movieIndex: 0 });
	expect(state.openCreditGuidance(path)).toBe(true);
});

it('rejects direct guidance navigation during a pending credit mutation', async () => {
	const job = fixture('a');
	Object.assign(job.results['/synthetic/movie.mp4'], {
		status: 'failed',
		error_code: 'credit_conflict',
	});
	vi.mocked(apiClient.getBatchJob).mockResolvedValue(job);
	const { state } = await mount();
	const operation = state.beginCreditMutation('credit:41', 'a')!;
	const request = state.creditGuidanceRequest;
	expect(state.openCreditGuidance('/synthetic/movie.mp4')).toBe(false);
	expect(state.creditGuidanceRequest).toBe(request);
	expect(state.creditGuidanceFilePath).toBeNull();
	await operation.finish();
	expect(state.openCreditGuidance('/synthetic/movie.mp4')).toBe(true);
});

it.each(['completed', 'failed'])('restores a committed cleanup warning over %s on reload without offering retry', async (priorStatus) => {
	const job = fixture('a');
	job.status = 'organized';
	const path = '/synthetic/movie.mp4';
	const warning = 'Output committed; backup cleanup pending. Do not re-apply.';
	job.results[path].status = priorStatus;
	job.results[path].recovery_warning = warning;
	vi.mocked(apiClient.getBatchJob).mockResolvedValue(job);
	const apply = vi.spyOn(apiClient, 'organizeBatchJob').mockResolvedValue({} as never);
	const { state } = await mount();
	expect(state.organizeStatus).toBe('completed');
	expect(state.displayedFileStatuses.get(path)).toEqual({ status: 'success', recovery_warning: warning });
	expect(state.canRetryFailed).toBe(false);
	expect(apply).not.toHaveBeenCalled();
});

it('keeps ordinary failed siblings distinct from committed cleanup warnings on reload', async () => {
	const job = fixture('a');
	job.status = 'organized';
	const path = '/synthetic/movie.mp4';
	job.results[path].recovery_warning = 'Output committed; cleanup pending.';
	job.results['/synthetic/failed.mp4'] = {
		...job.results[path], file_path: '/synthetic/failed.mp4',
		status: 'failed', error: 'Permission denied', recovery_warning: undefined,
	};
	vi.mocked(apiClient.getBatchJob).mockResolvedValue(job);
	const { state } = await mount();
	expect(state.organizeStatus).toBe('failed');
	expect(state.displayedFileStatuses.get(path)?.status).toBe('success');
	expect(state.displayedFileStatuses.get('/synthetic/failed.mp4')?.status).toBe('failed');
});
