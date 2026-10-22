import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import { apiClient } from '$lib/api/client';
import Page from './+page.svelte';
import { ACTRESS_SORT_STORAGE_KEY } from './sort-preferences';
vi.mock('$lib/query/queries', () => ({
	createConfigQuery: () => ({ data: { output: {}, metadata: {} } }),
}));
const clients: QueryClient[] = [];
beforeEach(() => {
	localStorage.clear();
	Object.defineProperty(Element.prototype, 'animate', {
		configurable: true,
		value: () => ({ cancel: vi.fn(), finished: Promise.resolve(), onfinish: null }),
	});
	Object.defineProperty(Element.prototype, 'getAnimations', {
		configurable: true,
		value: () => [],
	});
	vi.spyOn(apiClient, 'listActresses').mockResolvedValue({
		actresses: [],
		total: 0,
		count: 0,
		limit: 20,
		offset: 0,
	});
	vi.spyOn(apiClient.actresses, 'listCandidates').mockResolvedValue({ candidates: [], total: 0 });
});
afterEach(() => {
	cleanup();
	clients.splice(0).forEach((c) => c.clear());
	vi.restoreAllMocks();
	localStorage.clear();
});
it('mounts persisted sorting and wires all catalog filters through the rendered toolbar', async () => {
	localStorage.setItem(
		ACTRESS_SORT_STORAGE_KEY,
		JSON.stringify({ sortBy: 'dmm_id', sortOrder: 'desc' }),
	);
	const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
	clients.push(client);
	const view = render(Page, {}, { wrapper: QueryClientWrapper, wrapperProps: { client } });
	await waitFor(() =>
		expect(apiClient.listActresses).toHaveBeenLastCalledWith({
			limit: 20,
			offset: 0,
			q: undefined,
			sort_by: 'dmm_id',
			sort_order: 'desc',
			filter: undefined,
		}),
	);
	const filter = view.getByRole('combobox', { name: 'Filter actresses' });
	expect(filter.querySelectorAll('option')).toHaveLength(7);
	for (const value of [
		'missing_dmm',
		'has_dmm',
		'missing_thumbnail',
		'missing_japanese_name',
		'japanese_name_only',
		'missing_metadata',
		'',
	]) {
		await fireEvent.change(filter, { target: { value } });
		await waitFor(() =>
			expect(vi.mocked(apiClient.listActresses).mock.lastCall?.[0]?.filter).toBe(
				value || undefined,
			),
		);
	}
	const search = view.getByRole('textbox', { name: 'Search actresses' });
	expect(search.getAttribute('placeholder')).toContain('DMM ID');
	await fireEvent.input(search, { target: { value: ' 1234 ' } });
	await fireEvent.keyDown(search, { key: 'Enter' });
	await waitFor(() =>
		expect(vi.mocked(apiClient.listActresses).mock.lastCall?.[0]?.q).toBe('1234'),
	);
	await fireEvent.change(view.getByRole('combobox', { name: 'Sort actresses by' }), {
		target: { value: 'name' },
	});
	await waitFor(() =>
		expect(vi.mocked(apiClient.listActresses).mock.lastCall?.[0]?.sort_by).toBe('name'),
	);
});
