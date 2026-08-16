import { describe, it, expect } from 'vitest';
import { movieSearchScraperNames, movieSearchScrapers } from '$lib/scraper-capabilities';
import type { Scraper } from '$lib/api/types';

function makeScraper(overrides: Partial<Scraper> = {}): Scraper {
	return {
		name: 'r18dev',
		display_title: 'R18.dev',
		enabled: true,
		supports_movie_search: true,
		supports_actress_metadata: false,
		options: {},
		...overrides,
	};
}

describe('movieSearchScrapers', () => {
	it('includes movie-capable scrapers', () => {
		const scrapers = [makeScraper({ name: 'r18dev' })];
		expect(movieSearchScrapers(scrapers)).toHaveLength(1);
	});

	it('excludes actress-metadata-only scrapers', () => {
		const scrapers = [
			makeScraper({ name: 'r18dev' }),
			makeScraper({
				name: 'minnanoav',
				supports_movie_search: false,
				supports_actress_metadata: true,
			}),
		];
		const result = movieSearchScrapers(scrapers);
		expect(result).toHaveLength(1);
		expect(result[0].name).toBe('r18dev');
	});

	it('treats undefined supports_movie_search as movie-capable (default)', () => {
		const scrapers = [{ name: 'dmm', display_title: 'DMM', enabled: true, options: {} } as Scraper];
		expect(movieSearchScrapers(scrapers)).toHaveLength(1);
	});
});

describe('movieSearchScraperNames', () => {
	it('removes actress-only and unknown names from persisted selections', () => {
		const scrapers = [
			makeScraper({ name: 'r18dev', enabled: true }),
			makeScraper({
				name: 'minnanoav',
				enabled: true,
				supports_movie_search: false,
				supports_actress_metadata: true,
			}),
		];
		expect(movieSearchScraperNames(scrapers, ['minnanoav', 'r18dev', 'missing'])).toEqual([
			'r18dev',
		]);
	});

	it('preserves order of kept names', () => {
		const scrapers = [
			makeScraper({ name: 'r18dev', enabled: true }),
			makeScraper({ name: 'dmm', enabled: true }),
		];
		expect(movieSearchScraperNames(scrapers, ['dmm', 'r18dev'])).toEqual(['dmm', 'r18dev']);
	});
});
