import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';
import { afterEach, expect, it, vi } from 'vitest';
import Modal from './RescrapeModal.svelte';
vi.mock('svelte/transition', () => ({
	fade: () => ({ duration: 0 }),
	scale: () => ({ duration: 0 }),
}));
afterEach(cleanup);
it.each([undefined, 2])(
	'single/bulk modal requires fresh explicit opt-in (%s)',
	async (bulkMovieCount) => {
		const execute = vi.fn();
		const view = render(Modal, {
			show: true,
			rescraping: false,
			rescrapeMovieId: 'ABC-001',
			bulkMovieCount,
			availableScrapers: [{ name: 'r18dev', display_title: 'R18.dev', enabled: true }],
			selectedScrapers: ['r18dev'],
			manualSearchMode: false,
			manualSearchInput: '',
			rescrapeScalarStrategy: 'prefer-nfo',
			onApplyPreset: vi.fn(),
			onExecute: execute,
		});
		const option = view.getByRole('checkbox', { name: /Show offline R18/ }) as HTMLInputElement;
		expect(option.checked).toBe(false);
		await fireEvent.click(option);
		await fireEvent.click(
			view.getByRole('button', { name: bulkMovieCount ? 'Rescrape 2 movies' : 'Rescrape' }),
		);
		expect(execute).toHaveBeenCalledWith(expect.objectContaining({ showOfflineEvidence: true }));
		await view.rerender({ show: false });
		await waitFor(() =>
			expect(view.queryByRole('checkbox', { name: /Show offline R18/ })).toBeNull(),
		);
		await view.rerender({ show: true });
		expect(
			(view.getByRole('checkbox', { name: /Show offline R18/ }) as HTMLInputElement).checked,
		).toBe(false);
	},
);
