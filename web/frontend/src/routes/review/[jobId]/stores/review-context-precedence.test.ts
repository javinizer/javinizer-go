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

it.each(['copy', 'hardlink', 'softlink'] as const)(
	'newer in-memory %s context wins over an externally replaced valid move record',
	async (operation) => {
		let current = fixture();
		vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
		const apply = vi
			.spyOn(apiClient, 'organizeBatchJob')
			.mockImplementationOnce(async () => {
				current = structuredClone(current);
				current.status = 'failed';
				current.apply_generation = 1;
				current.failed = 1;
				current.completed = 0;
				current.results[filePath].status = 'failed';
				current.results[filePath].error_code = 'credit_conflict';
				throw new Error('Original non-move failed');
			})
			.mockResolvedValue({} as never);
		const { state } = await mount();
		state.organizeOperation = operation;
		await state.organizeAll(true, true);
		await state.retryLoad();
		localStorage.setItem(
			key,
			JSON.stringify({
				jobId: 'retry-job',
				operation: 'organize',
				preApplyGeneration: 0,
				destination: '/synthetic/output',
				skipNfo: false,
				skipDownload: false,
				organizeOperation: 'move',
				failed: { [filePath]: 'Older move failure' },
				succeeded: [],
				eligibleFilePaths: [filePath],
			}),
		);
		state.organizeOperation = 'hardlink';
		await state.retryLoad();
		expect(state.organizeOperation).toBe('hardlink');
		await state.retryFailed();
		expect(apply).toHaveBeenCalledTimes(2);
		expect(apply.mock.calls[1][1]).toMatchObject({
			copy_only: true,
			link_mode: operation === 'hardlink' ? 'hard' : operation === 'softlink' ? 'soft' : undefined,
			skip_nfo: true,
			skip_download: true,
			retry_file_paths: [filePath],
		});
	},
);

it.each(operations)(
	'known %s Retry persists new generation authority for the next reload',
	async (operation) => {
		let current = fixture();
		vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
		let launches = 0;
		const apply = vi.spyOn(apiClient, 'organizeBatchJob').mockImplementation(async () => {
			launches += 1;
			current = structuredClone(current);
			current.status = 'failed';
			current.apply_generation = launches;
			current.failed = 1;
			current.completed = 0;
			current.results[filePath].status = 'failed';
			current.results[filePath].error_code = 'credit_conflict';
			throw new Error('Synthetic generation failed');
		});
		const first = await mount();
		first.state.organizeOperation = operation;
		await first.state.organizeAll(true, true);
		await first.state.retryLoad();
		first.state.organizeOperation = operation === 'move' ? 'copy' : 'move';
		await first.state.retryFailed();
		await first.state.retryLoad();
		expect(apply).toHaveBeenCalledTimes(2);
		expect(first.state.job?.apply_generation).toBe(2);
		const record = JSON.parse(localStorage.getItem(key)!);
		expect(record).toMatchObject({
			preApplyGeneration: 1,
			organizeOperation: operation,
			skipNfo: true,
			skipDownload: true,
		});
		first.view.unmount();
		first.client.clear();
		const restored = await mount();
		await waitFor(() => expect(restored.state.organizing).toBe(false));
		expect(restored.state.canRetryFailed).toBe(true);
		await restored.state.retryFailed();
		expect(apply).toHaveBeenCalledTimes(3);
		expect(apply.mock.calls[2][1]).toMatchObject({
			copy_only: operation !== 'move',
			link_mode: operation === 'hardlink' ? 'hard' : operation === 'softlink' ? 'soft' : undefined,
			skip_nfo: true,
			skip_download: true,
			retry_file_paths: [filePath],
		});
	},
);
