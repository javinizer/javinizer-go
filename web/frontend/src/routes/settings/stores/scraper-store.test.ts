import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Config, ScraperInfo } from '$lib/api/types';

const { getAvailableScrapers } = vi.hoisted(() => ({
	getAvailableScrapers: vi.fn(),
}));

vi.mock('$lib/api/client', () => ({
	apiClient: { getAvailableScrapers },
}));

import { createScraperStore } from './scraper-store.svelte';

function makeConfig(): Config {
	return {
		scrapers: {
			priority: ['minnanoav', 'dmm'],
			minnanoav: { enabled: true },
			dmm: { enabled: true },
		},
		metadata: {
			priority: {
				actress: ['minnanoav', 'dmm'],
			},
		},
	} as unknown as Config;
}

function scraper(name: string, supportsMovieSearch: boolean): ScraperInfo {
	return {
		name,
		display_title: name,
		enabled: true,
		supports_movie_search: supportsMovieSearch,
		supports_actress_metadata: !supportsMovieSearch,
		options: [],
	};
}

describe('createScraperStore scraper priority', () => {
	beforeEach(() => {
		getAvailableScrapers.mockReset();
	});

	it('preserves actress-only resolvers in configured priority order', async () => {
		const config = makeConfig();
		getAvailableScrapers.mockResolvedValue({
			scrapers: [scraper('minnanoav', false), scraper('dmm', true)],
		});
		const store = createScraperStore({
			getConfig: () => config,
			setConfig: vi.fn(),
			getProxyProfileNames: () => [],
			refreshLocalProxyProfileChoices: (scrapers) => scrapers,
		});

		await store.buildScraperList();

		expect(config.scrapers?.priority).toEqual(['minnanoav', 'dmm']);
		expect(config.metadata?.priority?.actress).toEqual(['minnanoav', 'dmm']);
		expect(store.scrapers.map((item) => item.name)).toEqual(['minnanoav', 'dmm']);

		store.moveScraperDown(0);
		expect(config.scrapers?.priority).toEqual(['dmm', 'minnanoav']);
	});
});
