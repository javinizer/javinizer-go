import { describe, it, expect } from 'vitest';
import { formatScraperName } from './scraperNames';

describe('formatScraperName', () => {
	it('maps known scraper keys to their display labels', () => {
		expect(formatScraperName('dmm')).toBe('DMM/Fanza');
		expect(formatScraperName('libredmm')).toBe('LibreDMM (Fanza, MGStage, SOD, FC2)');
		expect(formatScraperName('r18dev')).toBe('R18.dev');
		expect(formatScraperName('javlibrary')).toBe('JavLibrary');
		expect(formatScraperName('javdb')).toBe('JavDB');
		expect(formatScraperName('javbus')).toBe('JavBus');
		expect(formatScraperName('jav321')).toBe('Jav321');
		expect(formatScraperName('tokyohot')).toBe('Tokyo-Hot');
		expect(formatScraperName('aventertainment')).toBe('AV Entertainment');
		expect(formatScraperName('dlgetchu')).toBe('DLGetchu');
		expect(formatScraperName('caribbeancom')).toBe('Caribbeancom');
	});

	// Regression guard for issue #105: mgstage, fc2, and javstage fell through
	// to the raw key (or a naive capitalize) because the mapping table omitted
	// them. They must render as proper labels everywhere the function is used.
	it('maps previously-missing scraper keys instead of returning the raw key', () => {
		expect(formatScraperName('mgstage')).toBe('MGStage');
		expect(formatScraperName('fc2')).toBe('FC2');
		expect(formatScraperName('javstash')).toBe('JAVStash');
	});

	it('falls back to the raw key for unknown scrapers', () => {
		expect(formatScraperName('unknownscraper')).toBe('unknownscraper');
	});
});
