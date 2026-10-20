import { cleanup, render, waitFor } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import { SvelteMap } from 'svelte/reactivity';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from '$lib/api/client';
import type { BatchJobResponse, CompletenessConfig, Movie } from '$lib/api/types';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import { confirmDialog } from '$lib/stores/dialog.svelte';
import type { createReviewState } from './review-state.svelte';
import Harness from '../../../../../test/harness/ReviewStateHarness.svelte';

vi.mock('$app/environment', () => ({ browser: true }));
vi.mock('$lib/stores/dialog.svelte', () => ({ confirmDialog: vi.fn(async () => true) }));
vi.mock('$lib/stores/websocket', () => ({
	websocketStore: { subscribe: () => () => {}, clearMessages: vi.fn() },
}));
const completeness = new SvelteMap<string, CompletenessConfig>();
vi.mock('$lib/query/queries', () => ({
	createConfigQuery: () => ({
		get data() {
			return { metadata: { completeness: completeness.get('config') } };
		},
	}),
}));

function movie(id: string, tier: 'incomplete' | 'partial' | 'complete'): Movie {
	const result: Movie = { id, title: id };
	if (tier !== 'incomplete') {
		Object.assign(result, {
			poster_url: '/poster.jpg',
			cover_url: '/cover.jpg',
			actresses: [{ id: 1, first_name: 'Synthetic' }],
			genres: [{ id: 1, name: 'Drama' }],
		});
	}
	if (tier === 'complete') {
		Object.assign(result, {
			description: 'Synthetic description',
			maker: 'Studio',
			release_date: '2026-01-01',
			director: 'Director',
			runtime: 120,
			trailer_url: '/trailer.mp4',
			screenshot_urls: ['/1.jpg', '/2.jpg', '/3.jpg'],
		});
	}
	return result;
}
function fixture(id = 'selection-job'): BatchJobResponse {
	return {
		id,
		status: 'completed',
		apply_generation: 1,
		total_files: 5,
		completed: 5,
		failed: 0,
		progress: 100,
		excluded: {},
		results: Object.fromEntries(
			[
				['A', 'incomplete'],
				['B', 'partial'],
				['C', 'complete'],
				['D', 'incomplete'],
				['C-part2', 'complete'],
			].map(([resultId, tier]) => {
				const movieId = resultId === 'C-part2' ? 'C' : resultId;
				const filePath = `/synthetic/${resultId}.mkv`;
				return [
					filePath,
					{
						file_path: filePath,
						result_id: resultId,
						movie_id: movieId,
						status: 'completed',
						revision: 1,
						movie: movie(movieId, tier as 'incomplete' | 'partial' | 'complete'),
					},
				];
			}),
		),
	} as BatchJobResponse;
}
const clients: QueryClient[] = [];
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
			jobId: 'selection-job',
			capture: (value) => {
				state = value;
			},
		},
		{ wrapper: QueryClientWrapper, wrapperProps: { client } },
	);
	await waitFor(() => expect(state.movieGroups).toHaveLength(4));
	return { state, view };
}
function onlyComplete(state: ReturnType<typeof createReviewState>) {
	state.toggleCompletenessTier('incomplete');
	state.toggleCompletenessTier('partial');
}
function customConfig(field: string, enabled = true): CompletenessConfig {
	return {
		enabled,
		tiers: {
			essential: { weight: 100, fields: [field] },
			important: { weight: 0, fields: [] },
			nice_to_have: { weight: 0, fields: [] },
		},
	};
}
beforeEach(() => {
	completeness.clear();
	sessionStorage.clear();
	localStorage.clear();
	vi.mocked(confirmDialog).mockResolvedValue(true);
	vi.spyOn(apiClient, 'getBatchJob').mockImplementation(async (id) => fixture(id));
	vi.spyOn(apiClient, 'previewOrganize').mockResolvedValue({} as never);
	vi.spyOn(apiClient, 'getExistingNFO').mockResolvedValue({} as never);
	vi.spyOn(apiClient, 'previewDisplayTitle').mockResolvedValue({ display_title: 'Synthetic' });
	vi.spyOn(apiClient, 'listCollisions').mockResolvedValue({ collisions: [] });
});
afterEach(() => {
	cleanup();
	clients.splice(0).forEach((client) => client.clear());
	vi.restoreAllMocks();
});

describe('real review selection characterization', () => {
	it('uses visible order for additive reverse shift ranges without moving the anchor', async () => {
		const { state } = await mount();
		state.toggleMovieSelection('D', false);
		expect(state.selectedCount).toBe(0);
		state.toggleSelectionMode();
		state.toggleMovieSelection('D', false);
		state.toggleMovieSelection('B', true);
		expect([...state.selectedMovieIds]).toEqual(['D', 'B', 'C']);
		state.toggleMovieSelection('D', false);
		state.toggleMovieSelection('C', true);
		expect([...state.selectedMovieIds]).toEqual(['B', 'C', 'D']);
	});

	it('keeps hidden selections and makes shift-click a no-op for either missing endpoint', async () => {
		const { state } = await mount();
		state.toggleSelectionMode();
		state.toggleMovieSelection('A', false);
		onlyComplete(state);
		state.toggleMovieSelection('C', true);
		expect([...state.selectedMovieIds]).toEqual(['A']);
		state.toggleMovieSelection('C', false);
		state.toggleMovieSelection('missing', true);
		expect([...state.selectedMovieIds]).toEqual(['A', 'C']);
		expect(state.selectedCount).toBe(2);
		expect(state.allSelected).toBe(true);
		expect(state.tierCounts).toEqual({ incomplete: 2, partial: 1, complete: 1 });
		state.toggleCompletenessTier('complete');
		expect(state.filteredMovieGroups).toEqual([]);
		expect(state.allSelected).toBe(false);
		expect(state.selectedCount).toBe(2);
	});

	it('reacts to overlays, overlay reset, configured tiers and enabled changes', async () => {
		const { state } = await mount();
		onlyComplete(state);
		expect(state.filteredMovieGroups.map((g) => g.movieId)).toEqual(['C']);
		state.updateCurrentMovie(movie('A', 'complete'));
		expect(state.filteredMovieGroups.map((g) => g.movieId)).toEqual(['A', 'C']);
		expect(state.tierCounts).toEqual({ incomplete: 1, partial: 1, complete: 2 });
		state.resetCurrentMovie();
		expect(state.filteredMovieGroups.map((g) => g.movieId)).toEqual(['C']);
		completeness.set('config', customConfig('title'));
		await waitFor(() => expect(state.tierCounts.complete).toBe(4));
		expect(state.filteredMovieGroups).toHaveLength(4);
		completeness.set('config', customConfig('cover_url'));
		await waitFor(() => expect(state.tierCounts.complete).toBe(2));
		expect(state.filteredMovieGroups.map((g) => g.movieId)).toEqual(['B', 'C']);
		completeness.set('config', customConfig('title', false));
		await waitFor(() =>
			expect(state.tierCounts).toEqual({ incomplete: 2, partial: 1, complete: 1 }),
		);
	});

	it('retains IDs removed by reload until explicitly deselected', async () => {
		const { state } = await mount();
		state.toggleSelectionMode();
		state.toggleMovieSelection('A', false);
		const next = fixture();
		delete next.results['/synthetic/A.mkv'];
		vi.mocked(apiClient.getBatchJob).mockResolvedValue(next);
		await state.retryLoad();
		expect(state.movieGroups.map((g) => g.movieId)).toEqual(['B', 'C', 'D']);
		expect([...state.selectedMovieIds]).toEqual(['A']);
		state.toggleMovieSelection('C', true);
		expect([...state.selectedMovieIds]).toEqual(['A']);
		state.deselectAllMovies();
		state.toggleMovieSelection('C', true);
		expect([...state.selectedMovieIds]).toEqual(['C']);
	});

	it('resets selection, anchor, mode and filters on route changes', async () => {
		const { state, view } = await mount();
		state.toggleSelectionMode();
		state.toggleMovieSelection('D', false);
		onlyComplete(state);
		await view.rerender({ jobId: 'next-job' });
		await waitFor(() => expect(state.job?.id).toBe('next-job'));
		expect(state.selectedCount).toBe(0);
		expect(state.selectionMode).toBe(false);
		expect([...state.completenessFilter]).toEqual(['incomplete', 'partial', 'complete']);
		state.toggleSelectionMode();
		state.toggleMovieSelection('B', true);
		expect([...state.selectedMovieIds]).toEqual(['B']);
	});

	it('single exclusion deletes only its selected ID and resets an excluded anchor', async () => {
		vi.spyOn(apiClient, 'excludeBatchMovie').mockResolvedValue({} as never);
		const { state } = await mount();
		state.toggleSelectionMode();
		state.toggleMovieSelection('C', false);
		state.toggleMovieSelection('A', false);
		state.reviewPageController.excludeCurrentMovie();
		await waitFor(() => expect([...state.selectedMovieIds]).toEqual(['C']));
		state.toggleMovieSelection('D', true);
		expect([...state.selectedMovieIds]).toEqual(['C', 'D']);
	});

	it('bulk exclude includes only selected visible multipart results and clears hidden selections', async () => {
		const exclude = vi.spyOn(apiClient, 'batchExcludeMovies').mockResolvedValue({
			excluded: ['C', 'C-part2'],
			failed: [],
		} as never);
		const { state } = await mount();
		state.toggleSelectionMode();
		state.selectAllMovies();
		onlyComplete(state);
		await state.bulkExcludeMovies();
		await waitFor(() =>
			expect(exclude).toHaveBeenCalledWith('selection-job', { result_ids: ['C', 'C-part2'] }),
		);
		await waitFor(() => expect(state.selectedCount).toBe(0));
		expect(state.selectionMode).toBe(true);
		state.toggleMovieSelection('C', true);
		expect([...state.selectedMovieIds]).toEqual(['C']);
	});

	it('bulk rescrape snapshots all selected IDs, including hidden ones, and keeps selection', async () => {
		vi.spyOn(apiClient, 'getScrapers').mockResolvedValue([
			{ name: 'synthetic', enabled: true },
		] as never);
		const rescrape = vi.spyOn(apiClient, 'bulkRescrapeMovies').mockResolvedValue({
			results: [],
			succeeded: 2,
			failed: 0,
		} as never);
		const { state } = await mount();
		state.toggleSelectionMode();
		state.toggleMovieSelection('A', false);
		state.toggleMovieSelection('C', false);
		onlyComplete(state);
		await state.openBulkRescrapeModal();
		expect(state.bulkRescrapeMovieIds).toEqual(['A', 'C']);
		state.toggleMovieSelection('C', false);
		await state.executeBulkRescrape();
		expect(rescrape).toHaveBeenCalledWith(
			'selection-job',
			expect.objectContaining({ movie_ids: ['A', 'C'], selected_scrapers: ['synthetic'] }),
		);
		expect([...state.selectedMovieIds]).toEqual(['A']);
	});
});
