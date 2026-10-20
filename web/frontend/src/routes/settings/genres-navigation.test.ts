import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import { apiClient } from '$lib/api/client';
import type { Config } from '$lib/api/types';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import SettingsPage from './+page.svelte';
import GenresPage from '../genres/+page.svelte';

vi.mock('$lib/api/client', () => ({
	apiClient: {
		getConfig: vi.fn(),
		getAvailableScrapers: vi.fn(),
		getVersionStatus: vi.fn(),
		request: vi.fn(),
		r18dev: { getDumpStatus: vi.fn() },
		listGenreReplacements: vi.fn(),
		listIgnoredGenres: vi.fn(),
		listFavoriteGenres: vi.fn(),
	},
}));
// Animation timing is not part of the navigation contract, and jsdom has no Web Animations API.
vi.mock('svelte/transition', () => ({
	fly: () => ({ duration: 0 }),
	fade: () => ({ duration: 0 }),
	slide: () => ({ duration: 0 }),
	scale: () => ({ duration: 0 }),
}));

let client: QueryClient;
beforeEach(() => {
	vi.resetAllMocks();
	client = new QueryClient({
		defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
	});
	vi.mocked(apiClient.getConfig).mockResolvedValue({
		scrapers: {},
		metadata: { genre_replacement: { enabled: true, auto_add: false } },
	} as Config);
	vi.mocked(apiClient.getAvailableScrapers).mockResolvedValue({ scrapers: [] });
	vi.mocked(apiClient.getVersionStatus).mockResolvedValue({
		current: 'v1.6.4',
		update_available: false,
		source: 'disabled',
	} as Awaited<ReturnType<typeof apiClient.getVersionStatus>>);
	vi.mocked(apiClient.request).mockResolvedValue({ tokens: [] });
	vi.mocked(apiClient.r18dev.getDumpStatus).mockResolvedValue({
		present: false,
		running: false,
		enabled: false,
		path: '',
	});
});
afterEach(() => {
	cleanup();
	client.clear();
});

it('keeps inline genre switches and links to the dedicated manager without fetching rules', async () => {
	const view = render(SettingsPage, {}, { wrapper: QueryClientWrapper, wrapperProps: { client } });
	const section = await view.findByRole('button', { name: /^Genre Replacements/ });
	await fireEvent.click(section);

	const enabled = view.getByRole('checkbox', { name: 'Enable genre replacement' });
	const autoAdd = view.getByRole('checkbox', { name: 'Auto-add genres' });
	expect((enabled as HTMLInputElement).checked).toBe(true);
	expect((autoAdd as HTMLInputElement).checked).toBe(false);
	await fireEvent.click(enabled);
	await fireEvent.click(autoAdd);
	expect((enabled as HTMLInputElement).checked).toBe(false);
	expect((autoAdd as HTMLInputElement).checked).toBe(true);
	expect(view.getByRole('link', { name: 'Manage Genres' }).getAttribute('href')).toBe('/genres');
	expect(apiClient.listGenreReplacements).not.toHaveBeenCalled();
});

it('renders and filters replacement rules on the dedicated genre manager', async () => {
	vi.mocked(apiClient.listGenreReplacements).mockResolvedValue({
		replacements: [
			{
				id: 1,
				original: 'Sample original',
				replacement: 'Sample replacement',
				created_at: '2026-10-20T00:00:00Z',
				updated_at: '2026-10-20T00:00:00Z',
			},
		],
		count: 1,
		limit: 500,
		offset: 0,
		total: 1,
	});
	vi.mocked(apiClient.listIgnoredGenres).mockResolvedValue({
		ignored_genres: ['Sample ignored'],
		count: 1,
	});
	vi.mocked(apiClient.listFavoriteGenres).mockResolvedValue({
		favorites: ['Sample favorite'],
		count: 1,
	});
	const view = render(GenresPage, {}, { wrapper: QueryClientWrapper, wrapperProps: { client } });
	await view.findByText('Sample original', { exact: true });
	expect(view.getByText('Sample replacement', { exact: true })).toBeTruthy();
	expect(view.getByText('Sample ignored', { exact: true })).toBeTruthy();
	expect(view.getByText('Sample favorite', { exact: true })).toBeTruthy();
	expect(apiClient.listGenreReplacements).toHaveBeenCalledWith({ limit: 500 });

	const search = view.getByPlaceholderText('Search by original or replacement...');
	await fireEvent.input(search, { target: { value: 'not present' } });
	await waitFor(() => expect(view.queryByText('Sample original', { exact: true })).toBeNull());
	await fireEvent.input(search, { target: { value: '' } });
	await view.findByText('Sample original', { exact: true });
});
