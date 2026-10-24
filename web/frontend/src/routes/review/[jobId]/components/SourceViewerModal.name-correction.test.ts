import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from '$lib/api/client';
import type { ScraperResult } from '$lib/api/types';
import SourceViewerModal from './SourceViewerModal.svelte';

vi.mock('$lib/query/queries', () => ({
	createConfigQuery: () => ({
		data: {
			output: { first_name_order: true, actress_language_ja: true },
			metadata: {},
		},
	}),
}));
const results: ScraperResult[] = [
	{
		source: 'javlibrary',
		actresses: [
			{ first_name: 'Lee', last_name: 'Mary &amp; Jane' },
			{ japanese_name: '青木りん' },
			{ first_name: 'SUMIRE' },
			{ first_name: 'Old', last_name: 'Entry' },
		],
		cast_name_provenance: [
			{
				source: 'javlibrary',
				source_id: 'ab12',
				display_name: 'Lee Mary &amp; Jane',
				state: 'unknown_order',
			},
			{
				source: 'javlibrary',
				source_id: 'cd34',
				display_name: '青木りん',
				state: 'native_unsplit',
			},
			{ source: 'javlibrary', source_id: 'ef56', display_name: 'SUMIRE', state: 'single_name' },
			null,
		],
	},
];
beforeEach(() => {
	Object.defineProperty(Element.prototype, 'animate', {
		configurable: true,
		value: () => ({ cancel: vi.fn(), finished: Promise.resolve(), onfinish: null }),
	});
});
afterEach(() => {
	cleanup();
	vi.restoreAllMocks();
});
async function mount() {
	const onApply = vi.fn();
	const input = structuredClone(results);
	const view = render(SourceViewerModal, {
		show: true,
		loading: false,
		results: input,
		fieldSources: { actresses: 'other' },
		pendingField: null,
		onLoad: vi.fn(),
		onApply,
	});
	await fireEvent.click(view.getByRole('button', { name: 'Actresses' }));
	return { view, input, onApply };
}

describe('source viewer name correction integration', () => {
	it('renders exact raw evidence, keeps native/single names unsplit and explains unannotated entries', async () => {
		const { view, input } = await mount();
		expect(view.getByText('Lee Mary &amp; Jane')).toBeTruthy();
		expect(view.queryByText('Lee Mary & Jane')).toBeNull();
		expect(view.getByText('青木りん')).toBeTruthy();
		expect(view.getByText('SUMIRE')).toBeTruthy();
		expect(view.getByText('Old Entry')).toBeTruthy();
		expect(view.getByText('Name order unverified · original source label')).toBeTruthy();
		expect(view.getByText('Native name · kept unsplit')).toBeTruthy();
		expect(view.getByText('Single name · kept unsplit')).toBeTruthy();
		expect(view.getByText(/Name-order provenance unavailable. Re-scrape/)).toBeTruthy();
		expect(view.getAllByRole('button', { name: /^Correct name for/ })).toHaveLength(1);
		expect(input).toEqual(results);
	});

	it('saving only a future rule never applies the source or rewrites current data', async () => {
		vi.spyOn(apiClient.scrapers, 'getNameOverride').mockResolvedValue({ rule: null });
		const save = vi.spyOn(apiClient.scrapers, 'saveNameOverride').mockResolvedValue({
			rule: {
				source_name: 'Lee Mary &amp; Jane',
				first_name: 'Mary &amp; Jane',
				last_name: 'Lee',
			},
		});
		const { view, input, onApply } = await mount();
		await fireEvent.click(
			view.getByRole('button', { name: 'Correct name for Lee Mary &amp; Jane' }),
		);
		await waitFor(() =>
			expect((view.getByLabelText('Given name') as HTMLInputElement).disabled).toBe(false),
		);
		await fireEvent.input(view.getByLabelText('Given name'), {
			target: { value: 'Mary &amp; Jane' },
		});
		await fireEvent.input(view.getByLabelText('Family name'), { target: { value: 'Lee' } });
		await fireEvent.click(view.getByRole('checkbox'));
		await fireEvent.click(view.getByRole('button', { name: 'Save for future scrapes' }));
		await view.findByText('Saved for future scrapes.');
		await fireEvent.click(view.getByRole('button', { name: 'Done' }));
		expect(save).toHaveBeenCalledOnce();
		expect(onApply).not.toHaveBeenCalled();
		expect(input).toEqual(results);
		expect(view.getByText('Name order unverified · original source label')).toBeTruthy();
		expect(
			view.queryByText('Saved correction used for this scrape · original source label'),
		).toBeNull();
	});

	it('Escape dismisses only correction, leaving the source resolver open', async () => {
		vi.spyOn(apiClient.scrapers, 'getNameOverride').mockResolvedValue({ rule: null });
		const { view, onApply } = await mount();
		await fireEvent.click(
			view.getByRole('button', { name: 'Correct name for Lee Mary &amp; Jane' }),
		);
		await view.findByRole('dialog', { name: 'Correct source name' });
		await fireEvent.keyDown(window, { key: 'Escape' });
		await waitFor(() =>
			expect(view.queryByRole('dialog', { name: 'Correct source name' })).toBeNull(),
		);
		expect(view.getByRole('dialog', { name: 'Source Merge Resolver' })).toBeTruthy();
		expect(onApply).not.toHaveBeenCalled();
	});
});
