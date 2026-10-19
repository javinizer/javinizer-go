import { cleanup, fireEvent, render, waitFor, within } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import { apiClient } from '$lib/api/client';
import type { BatchJobResponse, FileResult } from '$lib/api/types';
import type { createReviewState } from './review-state.svelte';
import Harness from '../../../../../test/harness/ReviewStateHarness.svelte';
import Page from '../+page.svelte';

vi.setConfig({ testTimeout: 20000 });
// Disable visual fade timing only; Page, grouping and navigation remain real.
vi.mock('svelte/transition', async (importOriginal) => ({
	...(await importOriginal<typeof import('svelte/transition')>()),
	fade: () => ({ duration: 0 }),
}));

const observed = vi.hoisted(() => ({ state: null as ReturnType<typeof createReviewState> | null }));
vi.mock('./review-state.svelte', async (importOriginal) => {
	const actual = await importOriginal<typeof import('./review-state.svelte')>();
	return {
		...actual,
		createReviewState: (...args: Parameters<typeof createReviewState>) => {
			const state = actual.createReviewState(...args);
			observed.state = state;
			return state;
		},
	};
});
vi.mock('$app/environment', () => ({ browser: true }));
// These editors do not participate in grouping or tab/navigation behavior.
vi.mock('$lib/components/ActressEditor.svelte', async () => ({
	default: (await import('$lib/components/ui/Card.svelte')).default,
}));
vi.mock('../components/MovieMetadataCard.svelte', async () => ({
	default: (await import('$lib/components/ui/Card.svelte')).default,
}));
vi.mock('../components/ReviewMediaSidebar.svelte', async () => ({
	default: (await import('$lib/components/ui/Card.svelte')).default,
}));

vi.mock('$app/stores', async () => {
	const { readable } = await import('svelte/store');
	return {
		page: readable({
			params: { jobId: 'selection-job' },
			url: new URL('http://localhost/review/selection-job?view=detail'),
		}),
	};
});
vi.mock('$lib/query/queries', () => ({
	createConfigQuery: () => ({
		data: {
			output: { operation_mode: 'organize' },
			metadata: {},
			webui: { default_review_view: 'detail' },
		},
	}),
	createFavoriteGenresQuery: () => ({ data: { favorites: [] }, isError: false }),
}));
vi.mock('$lib/stores/websocket', () => ({
	websocketStore: { subscribe: () => () => {}, clearMessages: vi.fn() },
}));

function fixture(ids = ['A', 'B', 'C']): BatchJobResponse {
	return {
		id: 'selection-job',
		status: 'completed',
		apply_generation: 1,
		destination: '/synthetic/output',
		results: Object.fromEntries(
			ids.map((id, index) => [
				`/synthetic/${id}.mkv`,
				{
					file_path: `/synthetic/${id}.mkv`,
					result_id: id,
					movie_id: id,
					status: ids.length > 1 && index === ids.length - 1 ? 'failed' : 'completed',
					revision: 1,
					error_code: ids.length > 1 && index === ids.length - 1 ? 'credit_conflict' : undefined,
					error:
						ids.length > 1 && index === ids.length - 1
							? 'apply blocked by an open credit collision'
							: '',
					ended_at: '2026-10-18T15:00:00Z',
					movie: {
						id,
						code: id.toLowerCase(),
						title: `Synthetic ${id}`,
						actresses: [],
						credits: [],
						genres: [],
						screenshot_urls: [],
					},
				},
			]),
		),
		total_files: ids.length,
		completed: ids.length > 1 ? ids.length - 1 : ids.length,
		failed: ids.length > 1 ? 1 : 0,
		progress: 100,
		excluded: {},
	} as unknown as BatchJobResponse;
}
function ordinaryFailure(job: BatchJobResponse, id: string) {
	const row = job.results[`/synthetic/${id}.mkv`];
	row.status = 'failed';
	row.error_code = undefined;
	row.error = 'Synthetic NFO write failure';
	job.failed = Object.values(job.results).filter((result) => result.status === 'failed').length;
	job.completed = Object.values(job.results).filter(
		(result) => result.status === 'completed',
	).length;
}
const clients: QueryClient[] = [];
function options() {
	const client = new QueryClient({
		defaultOptions: {
			queries: { retry: false, gcTime: 0 },
			mutations: { retry: false, gcTime: 0 },
		},
	});
	clients.push(client);
	return { wrapper: QueryClientWrapper, wrapperProps: { client } };
}
async function mount() {
	let state!: ReturnType<typeof createReviewState>;
	render(
		Harness,
		{
			jobId: 'selection-job',
			capture: (s) => {
				state = s;
			},
		},
		options(),
	);
	await waitFor(() => expect(state.job?.id).toBe('selection-job'));
	return state;
}
beforeEach(() => {
	sessionStorage.clear();
	localStorage.clear();
	window.history.replaceState({}, '', '/review/selection-job?view=detail');
	// jsdom has no Web Animations API; animation timing is not under test.
	Object.defineProperty(Element.prototype, 'animate', {
		configurable: true,
		value: () => {
			const animation = { cancel() {}, onfinish: null as (() => void) | null };
			setTimeout(() => animation.onfinish?.(), 0);
			return animation;
		},
	});
	Object.defineProperty(Element.prototype, 'getAnimations', {
		configurable: true,
		value: () => [],
	});
	vi.spyOn(apiClient, 'getBatchJob').mockResolvedValue(fixture());
	vi.spyOn(apiClient, 'previewOrganize').mockResolvedValue({} as never);
	vi.spyOn(apiClient, 'getExistingNFO').mockResolvedValue({} as never);
	vi.spyOn(apiClient, 'previewDisplayTitle').mockResolvedValue({
		display_title: 'Synthetic title',
	});
	vi.spyOn(apiClient, 'listCollisions').mockResolvedValue({ collisions: [] });
});
afterEach(() => {
	cleanup();
	clients.splice(0).forEach((client) => client.clear());
	vi.restoreAllMocks();
});

it('selects a remaining neighbour when the selected last credit row leaves Movies after Apply', async () => {
	let current = fixture();
	vi.mocked(apiClient.getBatchJob).mockImplementation(async () => structuredClone(current));
	vi.spyOn(apiClient, 'organizeBatchJob').mockImplementation(async () => {
		ordinaryFailure(current, 'C');
		current.apply_generation = 2;
		return { apply_generation: 2 } as never;
	});
	const state = await mount();
	state.currentMovieIndex = 2;
	expect(state.currentMovie?.id).toBe('C');
	await state.organizeAll();
	await waitFor(() => expect(state.organizing).toBe(false), { timeout: 5000 });
	expect(state.currentMovieIndex).toBe(1);
	expect(state.currentMovie?.id).toBe('B');
});
it('preserves a selected group when an earlier group leaves Movies during reload', async () => {
	const state = await mount();
	state.currentMovieIndex = 1;
	await waitFor(() => expect(state.currentMovie?.id).toBe('B'));
	const next = fixture();
	ordinaryFailure(next, 'A');
	vi.mocked(apiClient.getBatchJob).mockResolvedValue(next);
	await state.retryLoad();
	expect(state.currentMovieIndex).toBe(0);
	expect(state.currentMovie?.id).toBe('B');
});
it('keeps an explicit selection when grouping has not changed', async () => {
	const state = await mount();
	state.currentMovieIndex = 2;
	await waitFor(() => expect(state.currentMovie?.id).toBe('C'));
	vi.mocked(apiClient.getBatchJob).mockResolvedValue(fixture());
	await state.retryLoad();
	expect(state.currentMovieIndex).toBe(2);
	expect(state.currentMovie?.id).toBe('C');
});
it('resets selection for an empty Movies list and recovers when movies return', async () => {
	const state = await mount();
	state.currentMovieIndex = 2;
	const next = fixture();
	for (const id of ['A', 'B', 'C']) ordinaryFailure(next, id);
	vi.mocked(apiClient.getBatchJob).mockResolvedValue(next);
	await state.retryLoad();
	expect(state.movieGroups).toHaveLength(0);
	expect(state.currentMovieIndex).toBe(0);
	expect(state.currentResult).toBeUndefined();
	vi.mocked(apiClient.getBatchJob).mockResolvedValue(fixture(['B']));
	await state.retryLoad();
	expect(state.currentMovie?.id).toBe('B');
});
it('keeps remaining Movies navigation reachable through Failed and Movies tabs', async () => {
	let current = fixture(['A', 'B']);
	vi.mocked(apiClient.getBatchJob).mockImplementation(async () => structuredClone(current));
	vi.spyOn(apiClient, 'organizeBatchJob').mockImplementation(async () => {
		ordinaryFailure(current, 'B');
		current.apply_generation = 2;
		return { apply_generation: 2 } as never;
	});
	const view = render(Page, {}, options());
	await view.findByTestId('movie-navigation-card');
	expect(observed.state?.currentMovie?.id).toBe('A');
	await observed.state!.organizeAll();
	await waitFor(() => expect(observed.state?.organizing).toBe(false), { timeout: 5000 });
	expect(view.getByTestId('movie-navigation-card')).toBeTruthy();
	await fireEvent.click(view.getByRole('tab', { name: /Failed/ }));
	expect(
		within(view.container.querySelector('#failed-panel') as HTMLElement).getByText('B.mkv'),
	).toBeTruthy();
	await fireEvent.click(view.getByRole('tab', { name: /Movies/ }));
	expect(view.getByTestId('movie-navigation-card')).toBeTruthy();
});
it('switches to Failed when no movie groups remain', async () => {
	let current = fixture(['A']);
	vi.mocked(apiClient.getBatchJob).mockImplementation(async () => structuredClone(current));
	const view = render(Page, {}, options());
	await view.findByTestId('movie-navigation-card');
	ordinaryFailure(current, 'A');
	await observed.state!.retryLoad();
	await waitFor(() =>
		expect(view.getByRole('tab', { name: /Failed/ }).getAttribute('aria-selected')).toBe('true'),
	);
	expect(view.queryByTestId('movie-navigation-card')).toBeNull();
});
