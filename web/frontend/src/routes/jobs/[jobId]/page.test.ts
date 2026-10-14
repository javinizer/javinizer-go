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
	const { writable } = await import('svelte/store');
	return { page: writable({ params: { jobId: 'undo-job' }, url: { pathname: '/jobs/undo-job' } }) };
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
const pageStore = (await import('$app/stores')).page as unknown as {
	set: (value: { params: { jobId: string }; url: { pathname: string } }) => void;
};
let client: QueryClient;
let currentStatus: string;

beforeEach(() => {
	vi.resetAllMocks();
	toastStore.clear();
	pageStore.set({ params: { jobId: 'undo-job' }, url: { pathname: '/jobs/undo-job' } });
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
		expect(invalidate).toHaveBeenCalledWith({ queryKey: ['job', 'undo-job'] });
		expect(invalidate).toHaveBeenCalledWith({ queryKey: ['batch-jobs'] });
		vi.mocked(apiClient.revertJobOperation).mockImplementationOnce(async () => {
			currentStatus = 'reverted';
			return success;
		});
		await confirmOperation(view);
		await waitFor(() =>
			expect(get(toastStore).at(-1)).toMatchObject({
				type: 'success',
				message: 'Reverted TEST-001',
			}),
		);
		await waitFor(() => expect(view.queryByRole('alert')).toBeNull());
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

async function retryMovie(view: ReturnType<typeof showPage>, movieId: string) {
	const label = await view.findByText(movieId, { selector: 'span.font-semibold.text-sm' });
	const rowHeader = label.parentElement!.parentElement!;
	await fireEvent.click(within(rowHeader).getByRole('button', { name: 'Revert File' }));
	await fireEvent.click(
		within(view.getByRole('dialog')).getByRole('button', { name: 'Revert file' }),
	);
	await waitFor(() => expect(view.queryByRole('dialog')).toBeNull());
}

async function undoBatch(view: ReturnType<typeof showPage>, count: number) {
	await fireEvent.click(await view.findByRole('button', { name: /Revert Batch/ }));
	await fireEvent.click(
		within(view.getByRole('dialog')).getByRole('button', {
			name: `Revert ${count} ${count === 1 ? 'file' : 'files'}`,
		}),
	);
	await waitFor(() => expect(view.queryByRole('dialog')).toBeNull());
}

describe('unresolved Undo details across scoped retries', () => {
	it('keeps another movie’s skipped paths through a failed retry and a successful retry', async () => {
		const other = {
			...operation,
			id: 2,
			movie_id: 'TEST-002',
			original_path: '/incoming/TEST-002.mp4',
			new_path: '/library/TEST-002.mp4',
		};
		let rows = [{ ...operation }, other];
		vi.mocked(apiClient.getJobOperations).mockImplementation(async () => ({
			job_id: 'undo-job',
			job_status: 'organized',
			total: rows.length,
			operations: rows,
		}));
		const skipped = {
			...failure.errors![0],
			operation_id: 2,
			movie_id: other.movie_id,
			original_path: other.original_path,
			new_path: other.new_path,
			error: '',
			reason: 'anchor_missing',
			outcome: 'skipped',
		};
		vi.mocked(apiClient.revertBatchJob).mockImplementation(async () => {
			rows = rows.map((row) => ({ ...row, revert_status: row.id === 1 ? 'failed' : 'applied' }));
			return { ...failure, total: 2, skipped: 1, errors: [...failure.errors!, skipped] };
		});
		const view = showPage();
		await undoBatch(view, 2);
		await waitFor(() =>
			expect(within(view.getByRole('alert')).getAllByRole('listitem')).toHaveLength(2),
		);
		vi.mocked(apiClient.revertJobOperation).mockResolvedValueOnce({
			...failure,
			errors: [{ ...failure.errors![0], error: 'destination conflict after retry' }],
		});
		await retryMovie(view, 'TEST-001');
		expect(view.getByRole('alert').textContent).toContain('destination conflict after retry');
		expect(view.getByRole('alert').textContent).toContain('anchor_missing');
		expect(view.getByRole('alert').textContent).toContain(other.new_path);
		expect(view.getByRole('alert').textContent).not.toContain('invalid cross-device link');
		vi.mocked(apiClient.revertJobOperation).mockImplementationOnce(async () => {
			rows = rows.map((row) => (row.id === 1 ? { ...row, revert_status: 'reverted' } : row));
			return success;
		});
		await retryMovie(view, 'TEST-001');
		await waitFor(() =>
			expect(within(view.getByRole('alert')).getAllByRole('listitem')).toHaveLength(1),
		);
		expect(view.getByRole('alert').textContent).toContain('anchor_missing');
		expect(view.getByRole('alert').textContent).toContain(other.original_path);
		expect(view.getByRole('alert').textContent).not.toContain('destination conflict');
		vi.mocked(apiClient.revertJobOperation).mockImplementationOnce(async () => {
			rows = rows.map((row) => ({ ...row, revert_status: 'reverted' }));
			return success;
		});
		await retryMovie(view, 'TEST-002');
		await waitFor(() => expect(view.queryByRole('alert')).toBeNull());
	});

	it('does not erase excluded errors when a subsequent batch only attempts another row', async () => {
		const other = { ...operation, id: 2, movie_id: 'TEST-002' };
		let rows = [{ ...operation }, other];
		vi.mocked(apiClient.getJobOperations).mockImplementation(async () => ({
			job_id: 'undo-job',
			job_status: 'organized',
			total: 2,
			operations: rows,
		}));
		vi.mocked(apiClient.revertBatchJob).mockResolvedValueOnce({
			...failure,
			total: 2,
			failed: 2,
			errors: [
				...failure.errors!,
				{
					...failure.errors![0],
					operation_id: 2,
					movie_id: 'TEST-002',
					error: 'excluded row still needs attention',
				},
			],
		});
		const view = showPage();
		await undoBatch(view, 2);
		rows = [{ ...operation }, { ...other, revert_status: 'pending' }];
		await client.invalidateQueries({ queryKey: ['job', 'undo-job'] });
		vi.mocked(apiClient.revertBatchJob).mockImplementationOnce(async () => {
			rows = [
				{ ...operation, revert_status: 'reverted' },
				{ ...other, revert_status: 'pending' },
			];
			return success;
		});
		await undoBatch(view, 1);
		await waitFor(() =>
			expect(within(view.getByRole('alert')).getAllByRole('listitem')).toHaveLength(1),
		);
		expect(view.getByRole('alert').textContent).toContain('excluded row still needs attention');
	});

	it('clears a settled noop skip only after fresh rows while retaining a retryable failure', async () => {
		const other = { ...operation, id: 2, movie_id: 'TEST-002' };
		const snapshot = (firstStatus: string) => ({
			job_id: 'undo-job',
			job_status: 'organized',
			total: 2,
			operations: [
				{ ...operation, revert_status: firstStatus },
				{ ...other, revert_status: 'failed' },
			],
		});
		vi.mocked(apiClient.getJobOperations).mockResolvedValue(snapshot('applied'));
		const skipped = {
			...failure.errors![0],
			error: '',
			outcome: 'skipped',
			reason: 'anchor_missing',
		};
		let refresh!: (value: ReturnType<typeof snapshot>) => void;
		vi.mocked(apiClient.revertBatchJob).mockImplementationOnce(async () => {
			vi.mocked(apiClient.getJobOperations).mockImplementationOnce(
				() =>
					new Promise((resolve) => {
						refresh = resolve;
					}),
			);
			return {
				...failure,
				total: 2,
				skipped: 1,
				errors: [
					skipped,
					{
						...failure.errors![0],
						operation_id: 2,
						movie_id: 'TEST-002',
						error: 'other row still blocked',
					},
				],
			};
		});
		const view = showPage();
		await undoBatch(view, 2);
		await waitFor(() =>
			expect(within(view.getByRole('alert')).getAllByRole('listitem')).toHaveLength(2),
		);
		expect(view.getByRole('alert').textContent).toContain('anchor_missing');
		refresh(snapshot('noop'));
		await waitFor(() =>
			expect(within(view.getByRole('alert')).getAllByRole('listitem')).toHaveLength(1),
		);
		expect(view.getByRole('alert').textContent).toContain('other row still blocked');
		expect(view.getByRole('alert').textContent).not.toContain('anchor_missing');
		expect(
			(view.getByRole('button', { name: 'Nothing to revert' }) as HTMLButtonElement).disabled,
		).toBe(true);
		await waitFor(() =>
			expect(
				(view.getByRole('button', { name: 'Revert File' }) as HTMLButtonElement).disabled,
			).toBe(false),
		);
	});

	it('reconciles a successful retry after a failed immediate refresh later recovers', async () => {
		vi.mocked(apiClient.revertJobOperation).mockResolvedValueOnce(failure);
		const view = showPage();
		await retryMovie(view, 'TEST-001');
		expect(view.getByRole('alert').textContent).toContain('invalid cross-device link');
		vi.mocked(apiClient.revertJobOperation).mockImplementationOnce(async () => {
			currentStatus = 'reverted';
			vi.mocked(apiClient.getJobOperations).mockRejectedValueOnce(new Error('refresh interrupted'));
			return success;
		});
		await retryMovie(view, 'TEST-001');
		await view.findByText('refresh interrupted');
		await client.invalidateQueries({ queryKey: ['job', 'undo-job'] });
		await waitFor(() => expect(view.queryByRole('alert')).toBeNull());
		expect(view.getByRole('button', { name: 'Reverted ✓' })).toBeTruthy();
	});

	it('keeps a newer failure authoritative over an older deferred successful retry', async () => {
		vi.mocked(apiClient.revertJobOperation).mockResolvedValueOnce(failure);
		const view = showPage();
		await retryMovie(view, 'TEST-001');
		vi.mocked(apiClient.revertJobOperation).mockImplementationOnce(async () => {
			vi.mocked(apiClient.getJobOperations).mockRejectedValueOnce(new Error('refresh interrupted'));
			return success;
		});
		await retryMovie(view, 'TEST-001');
		await view.findByText('refresh interrupted');
		await client.invalidateQueries({ queryKey: ['job', 'undo-job'] });
		await waitFor(() => expect(view.getByRole('button', { name: 'Revert File' })).toBeTruthy());
		vi.mocked(apiClient.revertJobOperation).mockResolvedValueOnce({
			...failure,
			errors: [{ ...failure.errors![0], error: 'newer retry failed' }],
		});
		await retryMovie(view, 'TEST-001');
		currentStatus = 'reverted';
		await client.invalidateQueries({ queryKey: ['job', 'undo-job'] });
		expect(view.getByRole('alert').textContent).toContain('newer retry failed');
	});

	it('ignores a result after leaving the detail view entirely', async () => {
		let resolve!: (result: RevertResultResponse) => void;
		vi.mocked(apiClient.revertJobOperation).mockImplementationOnce(
			() =>
				new Promise((done) => {
					resolve = done;
				}),
		);
		const view = showPage();
		await confirmOperation(view);
		await waitFor(() => expect(apiClient.revertJobOperation).toHaveBeenCalledTimes(1));
		view.unmount();
		resolve(failure);
		await new Promise((done) => setTimeout(done, 0));
		expect(get(toastStore)).toEqual([]);
	});

	it.each(['response', 'error'])(
		'clears on job switch and ignores a late old-job %s',
		async (kind) => {
			vi.mocked(apiClient.getJob).mockImplementation(
				async (id) => ({ id, status: 'organized', started_at: '2026-01-01T00:00:00Z' }) as never,
			);
			vi.mocked(apiClient.getJobOperations).mockImplementation(async (id) => ({
				job_id: id,
				job_status: 'organized',
				total: 1,
				operations: [{ ...operation }],
			}));
			vi.mocked(apiClient.revertJobOperation).mockResolvedValueOnce(failure);
			const view = showPage();
			await confirmOperation(view);
			await waitFor(() => expect(view.getByRole('alert')).toBeTruthy());
			let resolve!: (result: RevertResultResponse) => void;
			let reject!: (error: Error) => void;
			vi.mocked(apiClient.revertJobOperation).mockImplementationOnce(
				() =>
					new Promise((done, fail) => {
						resolve = done;
						reject = fail;
					}),
			);
			await confirmOperation(view);
			await waitFor(() => expect(apiClient.revertJobOperation).toHaveBeenCalledTimes(2));
			pageStore.set({ params: { jobId: 'another-job' }, url: { pathname: '/jobs/another-job' } });
			await waitFor(() => expect(apiClient.getJobOperations).toHaveBeenCalledWith('another-job'));
			await waitFor(() => expect(view.queryByRole('alert')).toBeNull());
			await waitFor(() => expect(view.queryByRole('dialog')).toBeNull());
			await fireEvent.click(view.getByRole('button', { name: 'Revert File' }));
			const newConfirm = within(view.getByRole('dialog')).getByRole('button', {
				name: 'Revert file',
			}) as HTMLButtonElement;
			expect(newConfirm.disabled).toBe(false);
			toastStore.clear();
			if (kind === 'response') resolve(failure);
			else reject(new Error('old request failed'));
			await new Promise((resolve) => setTimeout(resolve, 0));
			expect(get(toastStore)).toEqual([]);
			expect(view.queryByRole('alert')).toBeNull();
			expect(newConfirm.disabled).toBe(false);
			expect(view.getByRole('dialog')).toBeTruthy();
		},
	);
});
