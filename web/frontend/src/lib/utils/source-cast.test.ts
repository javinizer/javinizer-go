import { describe, expect, it } from 'vitest';
import {
	canCorrectSourceName,
	normalizeNamePart,
	sourceCastNames,
	splitReconstructsSource,
} from './source-cast';
import type { CastNameProvenance, ScraperResult } from '$lib/api/types';

const provenance = (
	display_name: string,
	state: CastNameProvenance['state'] = 'unknown_order',
): CastNameProvenance => ({
	source: 'javlibrary',
	source_id: 'abc123',
	display_name,
	state,
});

describe('source cast evidence', () => {
	it('uses exact once-decoded evidence, independent of output order and native preference', () => {
		const result: ScraperResult = {
			source: 'javlibrary',
			actresses: [
				{ first_name: 'Lee', last_name: 'Mary &amp; Jane', japanese_name: '古い' },
				{ japanese_name: '青木りん' },
			],
			cast_name_provenance: [
				provenance('Lee Mary &amp; Jane'),
				provenance('青木りん', 'native_unsplit'),
			],
		};
		const before = structuredClone(result);
		expect(
			sourceCastNames(result, { firstNameOrder: true, japaneseNames: true }).map(
				(a) => a.displayName,
			),
		).toEqual(['Lee Mary &amp; Jane', '青木りん']);
		expect(result).toEqual(before);
	});

	it('preserves positional alignment through blank or legacy names and never invents provenance', () => {
		const result: ScraperResult = {
			source: 'javlibrary',
			actresses: [{}, { first_name: 'Legacy' }, { first_name: 'SUMIRE' }],
			cast_name_provenance: [null, null, provenance('SUMIRE', 'single_name')],
		};
		expect(sourceCastNames(result)).toEqual([
			{ displayName: 'Legacy', provenance: undefined, index: 1 },
			{ displayName: 'SUMIRE', provenance: provenance('SUMIRE', 'single_name'), index: 2 },
		]);
	});

	it('rejects misaligned or foreign-source evidence rather than binding the wrong identity', () => {
		const short: ScraperResult = {
			source: 'javlibrary',
			actresses: [{ first_name: 'First' }, { first_name: 'Second' }],
			cast_name_provenance: [provenance('Wrong Person')],
		};
		expect(sourceCastNames(short).map((entry) => entry.provenance)).toEqual([undefined, undefined]);
		const foreign: ScraperResult = {
			source: 'javbus',
			actresses: [{ first_name: 'First' }],
			cast_name_provenance: [provenance('Wrong Person')],
		};
		expect(sourceCastNames(foreign)[0]).toEqual({
			displayName: 'First',
			provenance: undefined,
			index: 0,
		});
	});

	it('rejects same-length swapped sidecars and names that do not match their stated shape', () => {
		const swapped: ScraperResult = {
			source: 'javlibrary',
			actresses: [
				{ first_name: 'Mary', last_name: 'Lee' },
				{ first_name: 'Jane', last_name: 'Doe' },
			],
			cast_name_provenance: [provenance('Doe Jane'), provenance('Lee Mary')],
		};
		expect(sourceCastNames(swapped).map((entry) => entry.provenance)).toEqual([
			undefined,
			undefined,
		]);
		for (const [actress, evidence] of [
			[{}, provenance('Lee Mary')],
			[{ first_name: 'RIN', last_name: 'Other' }, provenance('RIN', 'single_name')],
			[{ japanese_name: '別名' }, provenance('青木りん', 'native_unsplit')],
		] as const) {
			const result: ScraperResult = {
				source: 'javlibrary',
				actresses: [actress],
				cast_name_provenance: [evidence],
			};
			expect(sourceCastNames(result).every((entry) => entry.provenance === undefined)).toBe(true);
		}
	});

	it('allows only bounded JAVLibrary IDs and uncertain/corrected names', () => {
		expect(canCorrectSourceName(provenance('Lee Mary Jane'))).toBe(true);
		expect(canCorrectSourceName(provenance('Lee Mary Jane', 'user_corrected'))).toBe(true);
		for (const item of [
			undefined,
			null,
			provenance('SUMIRE', 'single_name'),
			provenance('青木りん', 'native_unsplit'),
			{ ...provenance('Lee Mary'), source: 'other' },
			{ ...provenance('Lee Mary'), source_id: '../x' },
			{ ...provenance('Lee Mary'), source_id: 'a'.repeat(65) },
			provenance(' '),
		]) {
			expect(canCorrectSourceName(item)).toBe(false);
		}
	});

	it('requires lossless whole-label reconstruction and permits multiword parts in either order', () => {
		expect(normalizeNamePart(' Mary\u3000 Jane  ')).toBe('Mary Jane');
		expect(normalizeNamePart('\u0085Mary\u0085Jane\u0085')).toBe('Mary Jane');
		expect(normalizeNamePart('Mary\uFEFFJane')).toBe('Mary\uFEFFJane');
		expect(splitReconstructsSource('Lee\u0085Mary', 'Mary', 'Lee')).toBe(true);
		expect(splitReconstructsSource('Lee\uFEFFMary', 'Mary', 'Lee')).toBe(false);
		expect(splitReconstructsSource('Lee Mary Jane', ' Mary  Jane ', 'Lee')).toBe(true);
		expect(splitReconstructsSource('Lee Mary Jane', 'Lee', 'Mary Jane')).toBe(true);
		expect(splitReconstructsSource('Lee Mary Jane', 'Mary', 'Lee')).toBe(false);
		expect(splitReconstructsSource('Lee Mary Jane', 'Mary Jane', 'lee')).toBe(false);
		expect(splitReconstructsSource('Lee Mary Jane', '', 'Lee Mary Jane')).toBe(false);
		expect(splitReconstructsSource('Lee &amp; Jane', '& Jane', 'Lee')).toBe(false);
		expect(splitReconstructsSource('Lee &amp; Jane', '&amp; Jane', 'Lee')).toBe(true);
	});
});
