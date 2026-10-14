import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor, within } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import { get } from 'svelte/store';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import { apiClient } from '$lib/api/client';
import { toastStore } from '$lib/stores/toast';
import type { OperationItem, RevertResultResponse } from '$lib/api/types';
import JobPage from './+page.svelte';

// Animation timing is covered by the real-browser fixture; jsdom has no Web Animations API.
vi.mock('svelte/transition', () => ({
	fade: () => ({ duration: 0 }),
	fly: () => ({ duration: 0 }),
	scale: () => ({ duration: 0 }),
}));

vi.mock('$app/stores', async () => {
	const { readable } = await import('svelte/store');
	return { page: readable({ params: { jobId: 'undo-job' }, url: { pathname: '/jobs/undo-job' } }) };
});
vi.mock('$lib/api/client', () => ({
	apiClient: {
		getJob: vi.fn(),
		getJobOperations: vi.fn(),
		getConfig: vi.fn(),
		revertBatchJob: vi.fn(),
		revertJobOperation: vi.fn(),
	},
}));

const operation: OperationItem = {
	id: 1,
	movie_id: 'TEST-001',
	original_path: '/incoming/TEST-001.mp4',
	new_path: '/library/TEST-001.mp4',
	operation_type: 'move',
	revert_status: 'applied',
	in_place_renamed: false,
	created_at: '2026-01-01T00:00:00Z',
};
const success: RevertResultResponse = {
	job_id: 'undo-job',
	status: 'reverted',
	total: 1,
	succeeded: 1,
	failed: 0,
	skipped: 0,
};
const failure: RevertResultResponse = {
	...success,
	status: 'organized',
	succeeded: 0,
	failed: 1,
	errors: [
		{
			operation_id: 1,
			movie_id: 'TEST-001',
			original_path: operation.original_path,
			new_path: operation.new_path,
			outcome: 'failed',
			reason: 'unexpected_path_state',
			error: 'failed to revert move: invalid cross-device link',
		},
	],
};
let client: QueryClient;
let currentStatus: string;

beforeEach(() => {
	vi.clearAllMocks();
	toastStore.clear();
	currentStatus = 'applied';
	client = new QueryClient({
		defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
	});
	vi.mocked(apiClient.getJob).mockResolvedValue({
		id: 'undo-job',
		status: 'organized',
		started_at: '2026-01-01T00:00:00Z',
	} as never);
	vi.mocked(apiClient.getConfig).mockResolvedValue({ output: { allow_revert: true } } as never);
	vi.mocked(apiClient.getJobOperations).mockImplementation(async () => ({
		job_id: 'undo-job',
		job_status: 'organized',
		total: 1,
		operations: [{ ...operation, revert_status: currentStatus }],
	}));
});
afterEach(() => {
	cleanup();
	client.clear();
});

function showPage() {
	return render(JobPage, {}, { wrapper: QueryClientWrapper, wrapperProps: { client } });
}
async function confirmOperation(view: ReturnType<typeof showPage>) {
	await fireEvent.click(await view.findByRole('button', { name: 'Revert File' }));
	const modal = await view.findByRole('dialog');
	await fireEvent.click(within(modal).getByRole('button', { name: 'Revert file' }));
}

describe('job history Undo outcomes', () => {
	it('shows HTTP-success failures, retains details after refresh, and permits retry', async () => {
		vi.mocked(apiClient.revertJobOperation).mockImplementation(async () => {
			currentStatus = 'failed';
			return failure;
		});
		const invalidate = vi.spyOn(client, 'invalidateQueries');
		const view = showPage();
		await confirmOperation(view);
		await waitFor(() =>
			expect(get(toastStore)).toEqual([
				expect.objectContaining({
					type: 'warning',
					message: 'Reverted 0 of 1. 1 failed, 0 skipped.',
				}),
			]),
		);
		expect(view.getByRole('alert').textContent).toContain(failure.errors![0].error);
		expect(view.getByRole('alert').textContent).toContain(operation.original_path);
		expect(view.getByRole('alert').textContent).toContain(operation.new_path);
		await waitFor(() => expect(view.queryByRole('dialog')).toBeNull());
		await waitFor(() => expect(view.getByRole('button', { name: 'Revert File' })).toBeTruthy());
		expect(invalidate).toHaveBeenCalledWith({ queryKey: ['job', 'undo-job', 'operations'] });
		expect(invalidate).toHaveBeenCalledWith({ queryKey: ['batch-jobs'] });
		vi.mocked(apiClient.revertJobOperation).mockResolvedValueOnce(success);
		await confirmOperation(view);
		await waitFor(() =>
			expect(get(toastStore).at(-1)).toMatchObject({
				type: 'success',
				message: 'Reverted TEST-001',
			}),
		);
		expect(view.queryByRole('alert')).toBeNull();
		expect(apiClient.revertJobOperation).toHaveBeenCalledTimes(2);
	});

	it('does not claim success for skipped-only results and uses reason when error is empty', async () => {
		vi.mocked(apiClient.revertJobOperation).mockResolvedValue({
			...failure,
			failed: 0,
			skipped: 1,
			errors: [{ ...failure.errors![0], error: '', outcome: 'skipped', reason: 'anchor_missing' }],
		});
		const view = showPage();
		await confirmOperation(view);
		await waitFor(() =>
			expect(get(toastStore)[0]).toMatchObject({
				type: 'warning',
				message: 'Reverted 0 of 1. 0 failed, 1 skipped.',
			}),
		);
		expect(view.getByRole('alert').textContent).toContain('anchor_missing');
	});

	it('reports mixed multipart outcomes without losing any failure details', async () => {
		vi.mocked(apiClient.revertJobOperation).mockResolvedValue({
			...failure,
			total: 3,
			succeeded: 1,
			skipped: 1,
			errors: [
				...failure.errors!,
				{
					...failure.errors![0],
					operation_id: 2,
					error: '',
					reason: 'anchor_missing',
					outcome: 'skipped',
				},
			],
		});
		const view = showPage();
		await confirmOperation(view);
		await waitFor(() =>
			expect(get(toastStore)[0]).toMatchObject({
				type: 'warning',
				message: 'Reverted 1 of 3. 1 failed, 1 skipped.',
			}),
		);
		expect(within(view.getByRole('alert')).getAllByRole('listitem')).toHaveLength(2);
	});

	it('shows success only for a completed operation', async () => {
		vi.mocked(apiClient.revertJobOperation).mockResolvedValue(success);
		const view = showPage();
		await confirmOperation(view);
		await waitFor(() =>
			expect(get(toastStore)[0]).toMatchObject({ type: 'success', message: 'Reverted TEST-001' }),
		);
		expect(view.queryByRole('alert')).toBeNull();
	});

	it('keeps confirmation pending until the request finishes, then handles transport failure', async () => {
		let reject!: (error: Error) => void;
		vi.mocked(apiClient.revertJobOperation).mockImplementation(
			() =>
				new Promise((_, fail) => {
					reject = fail;
				}),
		);
		const view = showPage();
		await confirmOperation(view);
		await waitFor(() => expect(apiClient.revertJobOperation).toHaveBeenCalledTimes(1));
		const confirm = within(view.getByRole('dialog')).getByRole('button', {
			name: 'Revert file',
		}) as HTMLButtonElement;
		expect(confirm.disabled).toBe(true);
		reject(new Error('Connection interrupted'));
		await waitFor(() =>
			expect(get(toastStore)[0]).toMatchObject({
				type: 'error',
				message: 'Revert failed: Connection interrupted',
			}),
		);
		await waitFor(() => expect(view.queryByRole('dialog')).toBeNull());
		expect(view.getByRole('button', { name: 'Revert File' })).toBeTruthy();
	});

	it('also reports skipped batch outcomes consistently on the detail page', async () => {
		vi.mocked(apiClient.revertBatchJob).mockResolvedValue({ ...failure, failed: 0, skipped: 1 });
		const view = showPage();
		await fireEvent.click(await view.findByRole('button', { name: /Revert Batch/ }));
		await fireEvent.click(
			within(view.getByRole('dialog')).getByRole('button', { name: 'Revert 1 file' }),
		);
		await waitFor(() => expect(get(toastStore)[0]).toMatchObject({ type: 'warning' }));
	});
});
