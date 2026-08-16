import type { Scraper } from '$lib/api/types';

export function movieSearchScrapers(scrapers: Scraper[]): Scraper[] {
	return scrapers.filter((s) => s.supports_movie_search !== false);
}

export function movieSearchScraperNames(scrapers: Scraper[], selected: string[]): string[] {
	const allowed = new Set(movieSearchScrapers(scrapers).map((s) => s.name));
	return selected.filter((name) => allowed.has(name));
}
