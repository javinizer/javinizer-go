import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import { apiClient } from '$lib/api/client';
import { toastStore } from '$lib/stores/toast';
import type { Config } from '$lib/api/types';
import Harness from './test/SettingsStoreHarness.svelte';

vi.mock('$lib/api/client', () => ({ apiClient: { getConfig: vi.fn(), request: vi.fn() } }));
vi.mock('$lib/stores/toast', () => ({ toastStore: { success: vi.fn(), error: vi.fn() } }));
vi.mock('$lib/i18n/locale', () => ({ applySavedLocale: vi.fn() }));

let client: QueryClient;
const config = { ui: { language: 'en' }, scrapers: {}, metadata: {} } as Config;
beforeEach(() => {
	vi.resetAllMocks();
	client = new QueryClient({
		defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
	});
	vi.mocked(apiClient.getConfig).mockResolvedValue(config);
});
afterEach(() => {
	cleanup();
	client.clear();
});

const renderStore = () =>
	render(Harness, {}, { wrapper: QueryClientWrapper, wrapperProps: { client } });
async function loaded() {
	const view = renderStore();
	await waitFor(() => expect(view.getByTestId('loaded').textContent).toBe('true'));
	expect(view.getByTestId('error').textContent).toBe('');
	return view;
}

it('surfaces an initial config API failure and clears it after successful reload', async () => {
	vi.mocked(apiClient.getConfig).mockRejectedValue(new Error('Configuration unavailable'));
	const view = renderStore();
	await waitFor(() => expect(view.getByTestId('loading').textContent).toBe('false'));
	expect(view.getByTestId('loaded').textContent).toBe('false');
	expect(view.getByTestId('query-error').textContent).toBe('Configuration unavailable');
	expect(view.getByTestId('error').textContent).toBe('Configuration unavailable');
	vi.mocked(apiClient.getConfig).mockResolvedValue(config);
	await fireEvent.click(view.getByRole('button', { name: 'Reload' }));
	await waitFor(() => expect(view.getByTestId('loaded').textContent).toBe('true'));
	expect(view.getByTestId('error').textContent).toBe('');
});

it('keeps the current query failure visible when reload also fails', async () => {
	vi.mocked(apiClient.getConfig)
		.mockRejectedValueOnce(new Error('Initial failure'))
		.mockRejectedValueOnce(new Error('Reload failure'));
	const view = renderStore();
	await waitFor(() => expect(view.getByTestId('error').textContent).toBe('Initial failure'));
	await fireEvent.click(view.getByRole('button', { name: 'Reload' }));
	await waitFor(() => expect(toastStore.error).toHaveBeenCalledWith('Reload failure', 5000));
	expect(view.getByTestId('error').textContent).toBe('Reload failure');
	expect(view.getByTestId('loaded').textContent).toBe('false');
});

it('clears an old save error after corrected successful resubmission', async () => {
	vi.mocked(apiClient.request)
		.mockRejectedValueOnce(new Error('Invalid timeout'))
		.mockResolvedValueOnce({});
	const view = await loaded();
	await fireEvent.click(view.getByRole('button', { name: 'Save' }));
	await waitFor(() => expect(view.getByTestId('error').textContent).toBe('Invalid timeout'));
	await waitFor(() =>
		expect((view.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(false),
	);
	await fireEvent.click(view.getByRole('button', { name: 'Save' }));
	await waitFor(() => expect(toastStore.success).toHaveBeenCalledTimes(1));
	expect(apiClient.request).toHaveBeenCalledTimes(2);
	expect(view.getByTestId('error').textContent).toBe('');
});

it('updates the visible error when the save retry fails again', async () => {
	vi.mocked(apiClient.request)
		.mockRejectedValueOnce(new Error('Invalid timeout'))
		.mockRejectedValueOnce(new Error('Still invalid'));
	const view = await loaded();
	await fireEvent.click(view.getByRole('button', { name: 'Save' }));
	await waitFor(() => expect(view.getByTestId('error').textContent).toBe('Invalid timeout'));
	await waitFor(() =>
		expect((view.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(false),
	);
	await fireEvent.click(view.getByRole('button', { name: 'Save' }));
	await waitFor(() => expect(view.getByTestId('error').textContent).toBe('Still invalid'));
	expect(toastStore.success).not.toHaveBeenCalled();
});

it('clears an old save error after a successful reload', async () => {
	vi.mocked(apiClient.request).mockRejectedValue(new Error('Save unavailable'));
	const view = await loaded();
	await fireEvent.click(view.getByRole('button', { name: 'Save' }));
	await waitFor(() => expect(view.getByTestId('error').textContent).toBe('Save unavailable'));
	await fireEvent.click(view.getByRole('button', { name: 'Reload' }));
	await waitFor(() => expect(toastStore.success).toHaveBeenCalledTimes(1));
	expect(view.getByTestId('error').textContent).toBe('');
});

it('surfaces refetch errors without dropping already loaded configuration', async () => {
	const view = await loaded();
	vi.mocked(apiClient.getConfig).mockRejectedValue(new Error('Refresh unavailable'));
	await fireEvent.click(view.getByRole('button', { name: 'Reload' }));
	await waitFor(() => expect(toastStore.error).toHaveBeenCalledWith('Refresh unavailable', 5000));
	expect(view.getByTestId('error').textContent).toBe('Refresh unavailable');
	expect(view.getByTestId('loaded').textContent).toBe('true');
});
