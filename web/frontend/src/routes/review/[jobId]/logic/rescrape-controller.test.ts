import { describe, expect, it, vi } from 'vitest';
import { createRescrapeController } from './rescrape-controller';

function scraper(name: string, supportsMovieSearch: boolean) {
	return {
		name,
		display_title: name,
		enabled: true,
		supports_movie_search: supportsMovieSearch,
		supports_actress_metadata: !supportsMovieSearch,
		options: {},
	};
}

describe('createRescrapeController', () => {
	it('removes actress-only and unknown scraper names before submission', async () => {
		let selected = ['minnanoav', 'r18dev', 'missing'];
		const rescrapeBatchMovie = vi.fn().mockResolvedValue({ movie: {} });
		const controller = createRescrapeController({
			getJobId: () => 'job-1',
			getCurrentResult: () => ({ result_id: 'result-1', file_path: '/movie.mp4', movie: {} }),
			getJob: () => null,
			setJob: vi.fn(),
			getEditedMovies: () => new Map(),
			getAvailableScrapers: () => [scraper('r18dev', true), scraper('minnanoav', false)],
			setAvailableScrapers: vi.fn(),
			getRescrapeResultId: () => 'result-1',
			setRescrapeResultId: vi.fn(),
			getSelectedScrapers: () => selected,
			setSelectedScrapers: (value: string[]) => {
				selected = value;
			},
			getManualSearchMode: () => false,
			setManualSearchMode: vi.fn(),
			getManualSearchInput: () => '',
			setManualSearchInput: vi.fn(),
			setShowRescrapeModal: vi.fn(),
			getRescrapePreset: () => undefined,
			setRescrapePreset: vi.fn(),
			getRescrapeScalarStrategy: () => '',
			setRescrapeScalarStrategy: vi.fn(),
			getRescrapeArrayStrategy: () => '',
			setRescrapeArrayStrategy: vi.fn(),
			getRescrapingStates: () => new Map(),
			toastSuccess: vi.fn(),
			toastError: vi.fn(),
			api: { getScrapers: vi.fn(), rescrapeBatchMovie },
		} as never);

		await controller.executeRescrape();

		expect(selected).toEqual(['r18dev']);
		expect(rescrapeBatchMovie).toHaveBeenCalledWith(
			'job-1',
			'result-1',
			expect.objectContaining({ selected_scrapers: ['r18dev'] }),
		);
	});
});
