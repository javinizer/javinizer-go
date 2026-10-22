import { cleanup, fireEvent, render, waitFor, within } from '@testing-library/svelte';
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

it('distinguishes form reset from search clear after the complete filter/search/sort paging flow', async () => {
	vi.mocked(apiClient.listActresses).mockImplementation(async (params) => {
		const offset = params?.offset ?? 0;
		const actresses = Array.from({ length: 20 }, (_, index) => ({
			id: offset + index + 1,
			first_name: `${params?.q ? 'SearchResult' : 'Catalog'} ${offset + index + 1}`,
			dmm_id: 1000 + offset + index,
			japanese_name: '名前',
			verified: true,
		}));
		return { actresses, count: 20, total: 60, limit: 20, offset };
	});
	// Match the application's default cache behavior (no staleTime override).
	const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	clients.push(client);
	const view = render(Page, {}, { wrapper: QueryClientWrapper, wrapperProps: { client } });
	const last = () => vi.mocked(apiClient.listActresses).mock.lastCall?.[0];
	await waitFor(() => expect(view.getByText('Catalog 1', { exact: true })).toBeTruthy());
	const filter = view.getByRole('combobox', { name: 'Filter actresses' });
	const sort = view.getByRole('combobox', { name: 'Sort actresses by' });
	const search = view.getByRole('textbox', {
		name: 'Search actresses',
	}) as HTMLInputElement;
	const next = view.getByRole('button', { name: 'Next' });
	for (const value of [
		'missing_dmm',
		'has_dmm',
		'missing_thumbnail',
		'missing_japanese_name',
		'japanese_name_only',
		'missing_metadata',
		'',
	]) {
		await fireEvent.click(next);
		await waitFor(() => expect(last()?.offset).toBe(20));
		await fireEvent.change(filter, { target: { value } });
		await waitFor(() => expect(last()).toMatchObject({ offset: 0, filter: value || undefined }));
		expect(view.getByText('Page 1 of 3', { exact: true })).toBeTruthy();
	}
	await fireEvent.change(filter, { target: { value: 'has_dmm' } });
	await fireEvent.click(next);
	await waitFor(() => expect(last()?.offset).toBe(20));
	await fireEvent.input(search, { target: { value: '  1001  ' } });
	await fireEvent.keyDown(search, { key: 'Enter' });
	await waitFor(() => expect(last()).toMatchObject({ q: '1001', offset: 0, filter: 'has_dmm' }));
	await fireEvent.click(next);
	await waitFor(() => expect(last()?.offset).toBe(20));
	await fireEvent.change(sort, { target: { value: 'dmm_id' } });
	await waitFor(() => expect(last()).toMatchObject({ sort_by: 'dmm_id', offset: 0 }));
	await fireEvent.click(next);
	await waitFor(() => expect(last()?.offset).toBe(20));
	await fireEvent.click(view.getByTitle('Toggle sort direction'));
	await waitFor(() => expect(last()).toMatchObject({ sort_order: 'desc', offset: 0 }));
	await waitFor(() => expect(view.getByText('SearchResult 1', { exact: true })).toBeTruthy());

	const firstName = view.getByRole('textbox', {
		name: 'First Name',
	}) as HTMLInputElement;
	await fireEvent.input(firstName, { target: { value: 'Unsaved form name' } });
	const requestsBeforeFormReset = vi.mocked(apiClient.listActresses).mock.calls.length;
	// The former global-first E2E selector targets this form reset, not search clear.
	await fireEvent.click(view.getAllByRole('button', { name: 'Clear' })[0]);
	expect(firstName.value).toBe('');
	expect(search.value).toBe('  1001  ');
	expect(last()?.q).toBe('1001');
	expect(vi.mocked(apiClient.listActresses).mock.calls).toHaveLength(requestsBeforeFormReset);
	expect(view.getByText('SearchResult 1', { exact: true })).toBeTruthy();

	const searchActions = view.getByRole('button', { name: 'Search' }).parentElement!;
	const searchClear = within(searchActions).getByRole('button', { name: 'Clear' });
	await fireEvent.click(searchClear);
	await waitFor(() =>
		expect(last()).toMatchObject({
			q: undefined,
			offset: 0,
			filter: 'has_dmm',
			sort_by: 'dmm_id',
			sort_order: 'desc',
		}),
	);
	expect(search.value).toBe('');
	expect((filter as HTMLSelectElement).value).toBe('has_dmm');
	expect(view.getByText('Page 1 of 3', { exact: true })).toBeTruthy();
	await waitFor(() => expect(view.getByText('Catalog 1', { exact: true })).toBeTruthy());
	expect(view.queryByText('SearchResult 1', { exact: true })).toBeNull();
});
