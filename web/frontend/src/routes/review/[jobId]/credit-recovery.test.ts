import { cleanup, configure, fireEvent, render, waitFor } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import { apiClient } from '$lib/api/client';
import type { BatchJobResponse } from '$lib/api/types';
import Page from './+page.svelte';
import type { createReviewState } from './stores/review-state.svelte';
const observedReview = vi.hoisted(() => ({
	state: null as ReturnType<typeof createReviewState> | null,
}));
vi.mock('./stores/review-state.svelte', async (importOriginal) => {
	const actual = await importOriginal<typeof import('./stores/review-state.svelte')>();
	return {
		...actual,
		createReviewState: (...args: Parameters<typeof createReviewState>) => {
			const state = actual.createReviewState(...args);
			observedReview.state = state;
			return state;
		},
	};
});
vi.mock('$app/stores', async () => {
	const { writable } = await import('svelte/store');
	return {
		page: writable({
			params: { jobId: 'credit-job' },
			url: new URL('http://localhost/review/credit-job?view=detail'),
		}),
	};
});
vi.mock('$lib/query/queries', () => ({
	createFavoriteGenresQuery: () => ({ data: { favorites: [] }, isError: false }),
	createConfigQuery: () => ({
		data: {
			output: { operation_mode: 'organize' },
			metadata: {},
			webui: { default_review_view: 'detail' },
		},
	}),
}));
vi.mock('$lib/stores/websocket', () => ({
	websocketStore: { subscribe: () => () => {}, clearMessages: vi.fn() },
}));
const filePath = '/synthetic/MOV-001.mp4';
const original = (): BatchJobResponse =>
	({
		id: 'credit-job',
		status: 'completed',
		destination: '/synthetic/output',
		apply_generation: 0,
		results: {
			[filePath]: {
				file_path: filePath,
				result_id: 'result-1',
				movie_id: 'MOV-001',
				status: 'completed',
				revision: 1,
				movie: {
					id: 'MOV-001',
					code: 'mov001',
					title: 'Synthetic movie',
					actresses: [{ id: 7, first_name: 'Catalog', last_name: 'Name' }],
					credits: [{ id: 41, actress_id: 7 }],
					genres: [],
					screenshots: [],
				},
			},
		},
		total_files: 1,
		completed: 1,
		failed: 0,
		progress: 100,
		excluded: {},
	}) as unknown as BatchJobResponse;
const clients: QueryClient[] = [];
vi.setConfig({ testTimeout: 30000 });
configure({ asyncUtilTimeout: 5000 });
beforeEach(async () => {
	const { page } = await import('$app/stores');
	(page as any).set({
		params: { jobId: 'credit-job' },
		url: new URL('http://localhost/review/credit-job?view=detail'),
	});
	localStorage.clear();
	sessionStorage.clear();
	Object.defineProperty(Element.prototype, 'animate', {
		configurable: true,
		value: () => ({ cancel: vi.fn(), finished: Promise.resolve(), onfinish: null }),
	});
	Object.defineProperty(Element.prototype, 'getAnimations', {
		configurable: true,
		value: () => [],
	});
	vi.spyOn(apiClient, 'previewOrganize').mockResolvedValue({} as never);
	vi.spyOn(apiClient, 'getExistingNFO').mockResolvedValue({} as never);
	vi.spyOn(apiClient, 'getScrapers').mockResolvedValue([
		{ name: 'synthetic', enabled: true },
	] as never);
	vi.spyOn(apiClient, 'listCollisions').mockResolvedValue({
		collisions: [
			{
				id: 1,
				credit_id: 41,
				movie_content_id: 'mov001',
				field: 'credited_name',
				reported_value: 'Scraped Name',
				canonical_value: 'Catalog Name',
				status: 'open',
				occurrences: 1,
				current_actress_id: 7,
				allowed_resolutions: ['keep_identity', 'adopt_canonical'],
			},
		],
	});
});
afterEach(() => {
	cleanup();
	clients.splice(0).forEach((c) => c.clear());
	vi.restoreAllMocks();
});
function mount() {
	const client = new QueryClient({
		defaultOptions: {
			queries: { retry: false, gcTime: 0 },
			mutations: { retry: false, gcTime: 0 },
		},
	});
	clients.push(client);
	return render(Page, {}, { wrapper: QueryClientWrapper, wrapperProps: { client } });
}
it.each(['credit_conflict', 'credit_status_unavailable'] as const)(
	'keeps resolution and retry available after Apply failure %s',
	async (errorCode) => {
		let current = original();
		vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
		const apply = vi.spyOn(apiClient, 'organizeBatchJob').mockImplementation(async () => {
			current = structuredClone(current);
			current.status = 'failed';
			current.apply_generation = 1;
			current.completed = 0;
			current.failed = 1;
			current.results[filePath].status = 'failed';
			current.results[filePath].error_code = errorCode;
			current.results[filePath].error =
				errorCode === 'credit_conflict'
					? 'apply blocked by an open credit collision'
					: 'apply blocked because credit collision status is unavailable';
			return {} as never;
		});
		const view = mount();
		await waitFor(() => expect(view.getByRole('button', { name: /Keep catalog/ })).toBeTruthy());
		const organize = view.getAllByRole('button', { name: /Organize.*1.*file/i });
		expect(organize.length).toBeGreaterThan(0);
		expect((organize[0] as HTMLButtonElement).disabled).toBe(false);
		await fireEvent.click(organize[0]);
		await waitFor(() => expect(apply).toHaveBeenCalledOnce());
		await waitFor(() => expect(view.getByRole('button', { name: /Retry Failed/i })).toBeTruthy());
		expect((view.getByRole('tab', { name: /Movies/ }) as HTMLButtonElement).disabled).toBe(false);
		expect(view.getByRole('button', { name: /Keep catalog/ })).toBeTruthy();
		expect(view.getByRole('button', { name: /Adopt as truth/ })).toBeTruthy();
		expect(view.getByRole('button', { name: /This movie only/i })).toBeTruthy();
		expect(view.queryByRole('button', { name: /Search Manually/i })).toBeNull();
		// Retrying never bypasses the live conflict gate.
		await fireEvent.click(view.getByRole('button', { name: /Retry Failed/i }));
		await waitFor(() => expect(apply).toHaveBeenCalledTimes(2));
		expect(apply.mock.calls[1][1].retry_file_paths).toEqual([filePath]);
		expect(apiClient.listCollisions).toHaveBeenCalledWith('mov001');
	},
);
it('blocks Organize throughout a movie-specific write and authoritative refresh', async () => {
	vi.spyOn(apiClient, 'getBatchJob').mockResolvedValue(original());
	vi.mocked(apiClient.listCollisions).mockResolvedValue({ collisions: [] });
	let finish!: () => void;
	const override = vi.spyOn(apiClient, 'updateCreditOverride').mockImplementation(
		() =>
			new Promise<void>((resolve) => {
				finish = resolve;
			}),
	);
	const apply = vi.spyOn(apiClient, 'organizeBatchJob').mockResolvedValue({} as never);
	const view = mount();
	await fireEvent.click(await view.findByRole('button', { name: 'This movie only' }));
	await fireEvent.input(view.getByLabelText('Name for this movie'), {
		target: { value: 'Movie-specific saved name' },
	});
	await fireEvent.click(view.getByRole('button', { name: 'Set name' }));
	await waitFor(() => expect(override).toHaveBeenCalledWith(41, 'Movie-specific saved name', true));
	expect((view.getByRole('button', { name: 'Set name' }) as HTMLButtonElement).disabled).toBe(true);
	const organize = view.getAllByRole('button', { name: /Organize.*1.*file/i });
	expect((organize[0] as HTMLButtonElement).disabled).toBe(true);
	await fireEvent.click(organize[0]);
	expect(apply).not.toHaveBeenCalled();
	expect(apiClient.getBatchJob).toHaveBeenCalledOnce();
	finish();
	await waitFor(() => expect(apiClient.getBatchJob).toHaveBeenCalledTimes(2));
	await waitFor(() => expect((organize[0] as HTMLButtonElement).disabled).toBe(false));
	await fireEvent.click(organize[0]);
	await waitFor(() => expect(apply).toHaveBeenCalledOnce());
});
it('blocks an expanded movie-specific editor during Organize', async () => {
	vi.spyOn(apiClient, 'getBatchJob').mockResolvedValue(original());
	vi.mocked(apiClient.listCollisions).mockResolvedValue({ collisions: [] });
	const override = vi.spyOn(apiClient, 'updateCreditOverride').mockResolvedValue();
	const apply = vi.spyOn(apiClient, 'organizeBatchJob').mockResolvedValue({} as never);
	const view = mount();
	await fireEvent.click(await view.findByRole('button', { name: 'This movie only' }));
	await fireEvent.input(view.getByLabelText('Name for this movie'), {
		target: { value: 'Changed during Organize' },
	});
	await fireEvent.click(view.getAllByRole('button', { name: /Organize.*1.*file/i })[0]);
	await waitFor(() => expect(apply).toHaveBeenCalledOnce());
	expect(
		(view.getByRole('button', { name: 'This movie only' }) as HTMLButtonElement).disabled,
	).toBe(true);
	expect((view.getByRole('button', { name: 'Set name' }) as HTMLButtonElement).disabled).toBe(true);
	await fireEvent.click(view.getByRole('button', { name: 'Set name' }));
	expect(override).not.toHaveBeenCalled();
});
it('removing an actor refreshes and clears its resolved collision panel', async () => {
	let current = original();
	const read = vi
		.spyOn(apiClient, 'getBatchJob')
		.mockImplementation(async () => structuredClone(current));
	const dialog = await import('$lib/stores/dialog.svelte');
	vi.spyOn(dialog, 'confirmDialog').mockResolvedValue(true);
	const save = vi.spyOn(apiClient, 'updateBatchMovie').mockImplementation(async () => {
		current = structuredClone(current);
		current.results[filePath].revision = 2;
		current.results[filePath].movie!.actresses = [];
		current.results[filePath].movie!.credits![0].suppressed = true;
		vi.mocked(apiClient.listCollisions).mockResolvedValue({ collisions: [] });
		return {} as never;
	});
	const view = mount();
	await waitFor(() => expect(view.getByRole('button', { name: /Keep catalog/ })).toBeTruthy());
	await fireEvent.click(view.getAllByRole('button', { name: 'Remove' }).at(-1)!);
	await waitFor(() => expect(save).toHaveBeenCalledOnce());
	await waitFor(() => expect(read).toHaveBeenCalledTimes(2));
	await waitFor(() => expect(view.queryByRole('button', { name: 'This movie only' })).toBeNull());
	expect(vi.mocked(apiClient.listCollisions).mock.calls.length).toBeGreaterThanOrEqual(2);
	await waitFor(() => expect(view.queryByRole('button', { name: /Keep catalog/ })).toBeNull());
	expect(view.queryByText(/Collisions.*organize is blocked until resolved/)).toBeNull();
});
it.each([true, false])(
	'ignores old override completion after job navigation (same file: %s)',
	async (sameFile) => {
		const a = original();
		const b = structuredClone(a);
		b.id = 'other-job';
		b.results[filePath].movie!.id = 'OTHER-001';
		b.results[filePath].movie!.code = 'other001';
		b.results[filePath].movie_id = 'OTHER-001';
		b.results[filePath].movie!.credits![0].id = 99;
		const otherPath = sameFile ? filePath : '/synthetic/OTHER-001.mp4';
		b.results[otherPath] = b.results[filePath];
		b.results[otherPath].file_path = otherPath;
		b.results[otherPath].result_id = 'result-b';
		if (!sameFile) delete b.results[filePath];
		let readsB = 0;
		let rejectBRefresh = false;
		const read = vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async (id) => {
			if (id === 'credit-job') return structuredClone(a);
			readsB++;
			if (rejectBRefresh) throw new Error('Job B refresh triggered by old A override');
			return structuredClone(b);
		});
		vi.mocked(apiClient.listCollisions).mockResolvedValue({ collisions: [] });
		let finish!: () => void;
		vi.spyOn(apiClient, 'updateCreditOverride').mockImplementation(
			() =>
				new Promise<void>((resolve) => {
					finish = resolve;
				}),
		);
		const apply = vi.spyOn(apiClient, 'organizeBatchJob').mockResolvedValue({} as never);
		const view = mount();
		await fireEvent.click(await view.findByRole('button', { name: 'This movie only' }));
		await fireEvent.input(view.getByLabelText('Name for this movie'), {
			target: { value: 'A saved name' },
		});
		await fireEvent.click(view.getByRole('button', { name: 'Set name' }));
		await waitFor(() => expect(finish).toBeTypeOf('function'));
		const { page } = await import('$app/stores');
		(page as any).set({
			params: { jobId: 'other-job' },
			url: new URL('http://localhost/review/other-job?view=detail'),
		});
		await waitFor(() => expect(apiClient.listCollisions).toHaveBeenCalledWith('other001'));
		const readsBeforeFinish = readsB;
		rejectBRefresh = true;
		finish();
		await new Promise((resolve) => setTimeout(resolve, 50));
		expect(readsB).toBe(readsBeforeFinish);
		expect(view.queryByText('Job B refresh triggered by old A override')).toBeNull();
		await fireEvent.click(view.getAllByRole('button', { name: /Organize.*1.*file/i })[0]);
		await waitFor(() => expect(apply).toHaveBeenCalledOnce());
		expect(apply.mock.calls[0][0]).toBe('other-job');
		// Reset the shared route store for later test files/reruns.
		(page as any).set({
			params: { jobId: 'credit-job' },
			url: new URL('http://localhost/review/credit-job?view=detail'),
		});
	},
);

it.each(['credit_conflict', 'credit_status_unavailable'] as const)(
	'restores %s recovery after reload while keeping scraper failures separate',
	async (errorCode) => {
		const current = original();
		current.results[filePath].status = 'failed';
		current.results[filePath].error_code = errorCode;
		current.results[filePath].error = 'Credit gate rejected Apply';
		current.status = 'failed';
		current.failed = 2;
		current.completed = 0;
		current.results['/synthetic/unidentified.mp4'] = {
			file_path: '/synthetic/unidentified.mp4',
			movie_id: 'unknown',
			result_id: 'raw-failure',
			status: 'failed',
			error: 'No scraper result',
		} as never;
		vi.spyOn(apiClient, 'getBatchJob').mockResolvedValue(current);
		const apply = vi.spyOn(apiClient, 'organizeBatchJob').mockResolvedValue({} as never);
		const resolve = vi.spyOn(apiClient, 'resolveCollision').mockImplementation(async () => {
			vi.mocked(apiClient.listCollisions).mockResolvedValue({ collisions: [] });
			return { remaining_open: 0 } as never;
		});
		const view = mount();
		await fireEvent.click(await view.findByRole('button', { name: /Keep catalog/ }));
		await waitFor(() => expect(resolve).toHaveBeenCalledOnce());
		await waitFor(() =>
			expect(
				(view.getByRole('button', { name: /Retry Failed/i }) as HTMLButtonElement).disabled,
			).toBe(false),
		);
		await fireEvent.click(view.getByRole('tab', { name: /Failed/ }));
		expect(view.getAllByRole('button', { name: /Search Manually/i })).toHaveLength(1);
		expect(view.getByText('unidentified.mp4')).toBeTruthy();
		await fireEvent.click(view.getByRole('button', { name: /Retry Failed/i }));
		await waitFor(() => expect(apply).toHaveBeenCalledOnce());
		expect(apply.mock.calls[0][1].retry_file_paths).toEqual([filePath]);
	},
);

it.each(['Set name', 'Clear override'])(
	'blocks Apply on failed %s, preserves the draft, and permits an explicit retry',
	async (action) => {
		const fixture = original();
		fixture.results[filePath].movie!.credits![0] = {
			id: 41,
			actress_id: 7,
			override_name: 'Old name',
			user_override: true,
		};
		vi.spyOn(apiClient, 'getBatchJob').mockResolvedValue(fixture);
		vi.mocked(apiClient.listCollisions).mockResolvedValue({ collisions: [] });
		const override = vi
			.spyOn(apiClient, 'updateCreditOverride')
			.mockRejectedValueOnce(new Error('Name write failed'));
		const apply = vi.spyOn(apiClient, 'organizeBatchJob').mockResolvedValue({} as never);
		const view = mount();
		await fireEvent.click(await view.findByRole('button', { name: 'This movie only' }));
		await fireEvent.input(view.getByLabelText('Name for this movie'), {
			target: { value: 'New draft' },
		});
		await fireEvent.click(view.getByRole('button', { name: action }));
		await waitFor(() => expect(view.getByText('Name write failed')).toBeTruthy());
		const organize = view.getAllByRole('button', {
			name: /Organize.*1.*file/i,
		})[0] as HTMLButtonElement;
		expect(organize.disabled).toBe(true);
		expect((view.getByLabelText('Name for this movie') as HTMLInputElement).value).toBe(
			'New draft',
		);
		await fireEvent.click(organize);
		expect(apply).not.toHaveBeenCalled();
		override.mockResolvedValueOnce();
		await fireEvent.click(view.getByRole('button', { name: action }));
		await waitFor(() => expect(organize.disabled).toBe(false));
		expect(override).toHaveBeenCalledTimes(2);
	},
);

it('keeps the expected-file success summary and Continue action wired outside the Movies tab', async () => {
	let current = original();
	vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async () => structuredClone(current));
	vi.mocked(apiClient.listCollisions).mockResolvedValue({ collisions: [] });
	vi.spyOn(apiClient, 'organizeBatchJob').mockImplementation(async () => {
		current = structuredClone(current);
		current.status = 'organized';
		current.apply_generation = 1;
		return {} as never;
	});
	const view = mount();
	await fireEvent.click((await view.findAllByRole('button', { name: /Organize.*1.*file/i }))[0]);
	await waitFor(() => expect(view.getByText('Organization Complete!')).toBeTruthy());
	// The status card supports a terminal notification without individual file
	// outcomes. Exercise that fallback using the real page's captured state.
	observedReview.state!.fileStatuses.clear();
	await waitFor(() => expect(view.getByText(/All 1 file\(s\) organized successfully/)).toBeTruthy());
	const { goto } = await import('$app/navigation');
	vi.mocked(goto).mockClear();
	await fireEvent.click(view.getByRole('button', { name: /Return to Browse now/i }));
	expect(goto).toHaveBeenCalledWith('/browse');
});
