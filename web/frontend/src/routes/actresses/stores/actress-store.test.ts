import { cleanup, render, waitFor } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { flushSync } from 'svelte';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import { apiClient } from '$lib/api/client';
import type { ActressFilter } from '$lib/api/types';
import { createActressStore } from './actress-store.svelte';
import Harness from '../../../../test/harness/ActressStoreHarness.svelte';
import { ACTRESS_SORT_STORAGE_KEY } from '../sort-preferences';
const clients: QueryClient[] = [];
beforeEach(() => {
	localStorage.clear();
	vi.spyOn(apiClient, 'listActresses').mockImplementation(async (params) => ({
		actresses: [{ id: (params?.offset ?? 0) + 1, first_name: 'Sample' }],
		count: 1,
		total: 60,
		limit: params?.limit ?? 20,
		offset: params?.offset ?? 0,
	}));
});
afterEach(() => {
	cleanup();
	clients.splice(0).forEach((c) => c.clear());
	vi.restoreAllMocks();
	localStorage.clear();
});
async function mount() {
	const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
	clients.push(client);
	let store!: ReturnType<typeof createActressStore>;
	render(
		Harness,
		{
			capture: (s) => {
				store = s;
			},
		},
		{ wrapper: QueryClientWrapper, wrapperProps: { client } },
	);
	await waitFor(() => expect(store.total).toBe(60));
	return store;
}
async function next(store: ReturnType<typeof createActressStore>) {
	flushSync(() => store.nextPage());
	await waitFor(() =>
		expect(vi.mocked(apiClient.listActresses).mock.lastCall?.[0]?.offset).toBe(20),
	);
}
it.each<ActressFilter>([
	'missing_dmm',
	'has_dmm',
	'missing_thumbnail',
	'missing_japanese_name',
	'japanese_name_only',
	'missing_metadata',
])('resets pagination and combines %s with search and sort', async (filter) => {
	const store = await mount();
	flushSync(() => {
		store.queryInput = '  123  ';
		store.applySearch();
		store.sortBy = 'dmm_id';
		store.toggleSortOrder();
	});
	await next(store);
	flushSync(() => {
		store.filter = filter;
	});
	await waitFor(() =>
		expect(apiClient.listActresses).toHaveBeenLastCalledWith({
			limit: 20,
			offset: 0,
			q: '123',
			sort_by: 'dmm_id',
			sort_order: 'desc',
			filter,
		}),
	);
	expect(store.filter).toBe(filter);
	await next(store);
	flushSync(() => {
		store.filter = '';
	});
	await waitFor(() =>
		expect(apiClient.listActresses).toHaveBeenLastCalledWith({
			limit: 20,
			offset: 0,
			q: '123',
			sort_by: 'dmm_id',
			sort_order: 'desc',
			filter: undefined,
		}),
	);
});
it('search, clear, sort field and sort direction reset the page without clearing the filter', async () => {
	const store = await mount();
	flushSync(() => {
		store.filter = 'has_dmm';
	});
	await next(store);
	flushSync(() => {
		store.queryInput = ' Alice ';
		store.applySearch();
	});
	expect(store.offset).toBe(0);
	expect(store.activeQuery).toBe('Alice');
	await next(store);
	flushSync(() => store.clearSearch());
	expect(store.offset).toBe(0);
	expect(store.queryInput).toBe('');
	expect(store.activeQuery).toBe('');
	await next(store);
	flushSync(() => {
		store.sortBy = 'updated_at';
	});
	expect(store.offset).toBe(0);
	await next(store);
	flushSync(() => store.toggleSortOrder());
	expect(store.offset).toBe(0);
	expect(store.filter).toBe('has_dmm');
	expect(JSON.parse(localStorage.getItem(ACTRESS_SORT_STORAGE_KEY)!)).toEqual({
		sortBy: 'updated_at',
		sortOrder: 'desc',
	});
});
it('hydrates sort preferences and preserves page-local selection behavior', async () => {
	localStorage.setItem(
		ACTRESS_SORT_STORAGE_KEY,
		JSON.stringify({ sortBy: 'id', sortOrder: 'desc' }),
	);
	const store = await mount();
	flushSync(() => store.hydrateSortPreferences());
	expect(store.sortBy).toBe('id');
	expect(store.sortOrder).toBe('desc');
	flushSync(() => store.selectCurrentPage());
	expect(store.selectedIds).toEqual([1]);
	await next(store);
	await waitFor(() => expect(store.selectedIds).toEqual([]));
});
it('exposes filtered request errors rather than replacing the active filter', async () => {
	const store = await mount();
	vi.mocked(apiClient.listActresses).mockRejectedValue(new Error('Filter request failed'));
	flushSync(() => {
		store.filter = 'missing_metadata';
	});
	await waitFor(() => expect(store.error).toBe('Filter request failed'));
	expect(store.filter).toBe('missing_metadata');
});
