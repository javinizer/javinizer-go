import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import { setPendingScrape, clearPendingScrape } from '$lib/stores/pending-scrape.svelte';
import type { PendingScrape } from '$lib/stores/pending-scrape.svelte';
import ManualPage from './+page.svelte';
import { goto as mockGoto } from '$app/navigation';

vi.mock('$lib/api/client', () => ({
	apiClient: { batchScrape: vi.fn() }
}));
vi.mock('$lib/stores/background-job.svelte', () => ({
	startJob: vi.fn()
}));

const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
const mod = await import('$lib/api/client');
const bg = await import('$lib/stores/background-job.svelte');
const mockBatchScrape = vi.mocked(mod.apiClient.batchScrape);
const mockStartJob = vi.mocked(bg.startJob);
const invalidateSpy = vi.spyOn(client, 'invalidateQueries');

function snapshot(overrides: Partial<PendingScrape> = {}): PendingScrape {
	return {
		files: ['/library/a.mp4'],
		browseMode: 'scrape',
		update: false,
		effectiveOperationMode: 'organize',
		isInPlaceImplied: false,
		showScraperSelector: true,
		destination: '/out',
		selectedScrapers: ['javdb'],
		force: false,
		...overrides
	};
}

function renderPage() {
	return render(ManualPage, {}, {
		wrapper: QueryClientWrapper,
		wrapperProps: { client }
	});
}

beforeEach(() => {
	vi.clearAllMocks();
	mockBatchScrape.mockResolvedValue({ job_id: 'job-1' } as never);
	clearPendingScrape();
	if (typeof sessionStorage !== 'undefined') sessionStorage.clear();
});

describe('/manual tracer', () => {
	it('renders the read-only summary + rows and submits a valid BatchScrapeRequest (#1)', async () => {
		setPendingScrape(snapshot());
		const { getByText, getByLabelText, getByRole, getByDisplayValue } = renderPage();

		expect(getByText('Manual Scrape')).toBeTruthy();
		expect(getByText('Scrape & Organize')).toBeTruthy();
		expect(getByDisplayValue('/out')).toBeTruthy();
		expect(getByText('javdb')).toBeTruthy();
		expect(getByText('a.mp4')).toBeTruthy();
		expect(getByText('/library')).toBeTruthy();

		const input = getByLabelText('Manual input for /library/a.mp4') as HTMLInputElement;
		await fireEvent.input(input, { target: { value: 'IPX-123' } });
		expect(getByText('ID')).toBeTruthy();

		const submitBtn = getByRole('button', { name: 'Start manual scrape' });
		await fireEvent.click(submitBtn);

		await waitFor(() => expect(mockBatchScrape).toHaveBeenCalledTimes(1));
		expect(mockBatchScrape).toHaveBeenCalledWith({
			files: ['/library/a.mp4'],
			strict: false,
			force: false,
			destination: '/out',
			update: false,
			selected_scrapers: ['javdb'],
			operation_mode: 'organize',
			manual_inputs: { '/library/a.mp4': 'IPX-123' }
		});
		expect(mockStartJob).toHaveBeenCalledWith('job-1');
		expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['batch-jobs'] });
		expect(mockGoto).not.toHaveBeenCalled();
	});

	it('redirects to /browse on direct nav with no pending store (#2)', async () => {
		renderPage();
		await waitFor(() =>
			expect(mockGoto).toHaveBeenCalledWith('/browse', expect.objectContaining({ replaceState: true }))
		);
	});

	it('badge flips to URL for an http(s) input', async () => {
		setPendingScrape(snapshot());
		const { getByLabelText, getByText } = renderPage();
		const input = getByLabelText('Manual input for /library/a.mp4') as HTMLInputElement;
		await fireEvent.input(input, { target: { value: 'https://example.com/v/123' } });
		expect(getByText('URL')).toBeTruthy();
	});

	it('hides preset/strategies in scrape mode and shows them in update mode', async () => {
		setPendingScrape(snapshot());
		const scrapeRender = renderPage();
		expect(scrapeRender.queryByText('Preset')).toBeNull();

		clearPendingScrape();
		setPendingScrape(
			snapshot({
				browseMode: 'update',
				update: true,
				effectiveOperationMode: 'in-place',
				preset: 'gap-fill',
				scalarStrategy: 'prefer-scraper',
				arrayStrategy: 'replace'
			})
		);
		const updateRender = renderPage();
		expect(updateRender.getByDisplayValue('Gap Fill')).toBeTruthy();
	});

	it('removes the row from the batch and the request when "Remove from batch" is clicked (#4.8a)', async () => {
		setPendingScrape(snapshot({ files: ['/library/a.mp4', '/library/b.mp4'] }));
		const { getByRole, getByText, queryByText, queryByLabelText, getByLabelText } = renderPage();

		expect(getByText('a.mp4')).toBeTruthy();
		expect(getByText('b.mp4')).toBeTruthy();
		expect(getByText('Files (2)')).toBeTruthy();

		// Give the surviving row a manual override so we can assert it is preserved
		// in the request while the removed row is dropped entirely.
		const inputB = getByLabelText('Manual input for /library/b.mp4') as HTMLInputElement;
		await fireEvent.input(inputB, { target: { value: 'IPX-999' } });

		const removeBtn = getByRole('button', { name: 'Remove /library/a.mp4 from batch' });
		await fireEvent.click(removeBtn);

		// Row count drops (batch-removal, not a no-op); removed file + its input are
		// gone, the other row stays.
		expect(queryByText('a.mp4')).toBeNull();
		expect(queryByLabelText('Manual input for /library/a.mp4')).toBeNull();
		expect(getByText('b.mp4')).toBeTruthy();
		expect(getByLabelText('Manual input for /library/b.mp4')).toBeTruthy();
		expect(getByText('Files (1)')).toBeTruthy();

		// Submit builds the request from the remaining rows only — the removed file
		// is absent from both files[] and manual_inputs.
		await fireEvent.click(getByRole('button', { name: 'Start manual scrape' }));
		await waitFor(() => expect(mockBatchScrape).toHaveBeenCalledTimes(1));
		expect(mockBatchScrape).toHaveBeenCalledWith({
			files: ['/library/b.mp4'],
			strict: false,
			force: false,
			destination: '/out',
			update: false,
			selected_scrapers: ['javdb'],
			operation_mode: 'organize',
			manual_inputs: { '/library/b.mp4': 'IPX-999' }
		});
	});

	it('empties every row input but keeps the rows when "Clear all overrides" is clicked (#4.8b)', async () => {
		setPendingScrape(snapshot({ files: ['/library/a.mp4', '/library/b.mp4'] }));
		const { getByRole, getByLabelText, getByText, getAllByText, queryByRole } = renderPage();

		const inputA = getByLabelText('Manual input for /library/a.mp4') as HTMLInputElement;
		const inputB = getByLabelText('Manual input for /library/b.mp4') as HTMLInputElement;
		await fireEvent.input(inputA, { target: { value: 'IPX-123' } });
		await fireEvent.input(inputB, { target: { value: 'https://example.com/v/1' } });
		expect(inputA.value).toBe('IPX-123');
		expect(inputB.value).toBe('https://example.com/v/1');

		// The clear button only renders once at least one row has a non-empty override.
		const clearBtn = getByRole('button', { name: 'Clear all overrides' });
		await fireEvent.click(clearBtn);

		// Inputs cleared to empty (Auto), rows themselves remain (files[] unchanged).
		expect((getByLabelText('Manual input for /library/a.mp4') as HTMLInputElement).value).toBe('');
		expect((getByLabelText('Manual input for /library/b.mp4') as HTMLInputElement).value).toBe('');
		expect(getByText('a.mp4')).toBeTruthy();
		expect(getByText('b.mp4')).toBeTruthy();
		expect(getByText('Files (2)')).toBeTruthy();
		// Badge reverts to Auto for both rows.
		expect(getAllByText('Auto')).toHaveLength(2);
		// The clear button hides again now that no overrides remain.
		expect(queryByRole('button', { name: 'Clear all overrides' })).toBeNull();

		// Submit still carries every file, with manual_inputs dropped (no overrides).
		await fireEvent.click(getByRole('button', { name: 'Start manual scrape' }));
		await waitFor(() => expect(mockBatchScrape).toHaveBeenCalledTimes(1));
		expect(mockBatchScrape).toHaveBeenCalledWith({
			files: ['/library/a.mp4', '/library/b.mp4'],
			strict: false,
			force: false,
			destination: '/out',
			update: false,
			selected_scrapers: ['javdb'],
			operation_mode: 'organize',
			manual_inputs: undefined
		});
	});
});
