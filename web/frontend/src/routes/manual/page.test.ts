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
		const { getByText, getByLabelText, getByRole } = renderPage();

		expect(getByText('Manual Scrape')).toBeTruthy();
		expect(getByText('Scrape & Organize')).toBeTruthy();
		expect(getByText('/out')).toBeTruthy();
		expect(getByText('javdb')).toBeTruthy();
		expect(getByText('/library/a.mp4')).toBeTruthy();

		const input = getByLabelText('Manual input for /library/a.mp4') as HTMLInputElement;
		await fireEvent.input(input, { target: { value: 'IPX-123' } });
		expect(getByText('Manual · ID')).toBeTruthy();

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

	it('badge flips to Manual · URL for an http(s) input', async () => {
		setPendingScrape(snapshot());
		const { getByLabelText, getByText } = renderPage();
		const input = getByLabelText('Manual input for /library/a.mp4') as HTMLInputElement;
		await fireEvent.input(input, { target: { value: 'https://example.com/v/123' } });
		expect(getByText('Manual · URL (scraper auto-filtered)')).toBeTruthy();
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
		expect(updateRender.getByText('gap-fill')).toBeTruthy();
	});
});
