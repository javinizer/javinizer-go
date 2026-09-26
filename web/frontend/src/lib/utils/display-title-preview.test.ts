import { describe, it, expect } from 'vitest';
import { buildDisplayTitlePreviewSignature } from './display-title-preview';
import { rebaseOverlayOntoMovie } from '../../routes/review/[jobId]/stores/save-helpers';
import type { Actress, Movie } from '$lib/api/types';

function makeActress(over: Partial<Actress>): Actress {
	return {
		id: 7,
		dmm_id: 12,
		first_name: 'Yui',
		last_name: 'Hatano',
		japanese_name: '波多野結衣',
		...over,
	};
}

function makeMovie(actresses: Actress[]): Movie {
	return {
		id: 'ABC-123',
		title: 'Some Title',
		actresses,
		genres: [],
		poster_url: 'https://cdn.example/p.jpg',
	} as Movie;
}

describe('buildDisplayTitlePreviewSignature', () => {
	it('is stable when only volatile actress fields churn between polls', () => {
		// v1.6.0 regression: during organizing, media snapshots transiently
		// clear/restore ThumbURL and identity resolution flips verified/origin/
		// name_key/aliases. None of these affect the rendered display title, so
		// the preview must NOT refire for them.
		const before = makeMovie([
			makeActress({ thumb_url: '', origin: 'scrape', name_key: '', aliases: '' }),
		]);
		const after = makeMovie([
			makeActress({
				thumb_url: 'https://cdn.example/t.jpg',
				origin: 'identity',
				name_key: 'hatano yui',
				aliases: 'Y.H.',
			}),
		]);
		expect(buildDisplayTitlePreviewSignature(after)).toBe(buildDisplayTitlePreviewSignature(before));
	});

	it('rebase after cast_version churn does not alter the preview signature', () => {
		// Full chain: poll replaces movie (thumb resolved), cast_version changes,
		// rebaseOverlayOntoMovie drops the overlay actresses and installs fresh ones.
		const baseline = { ...makeMovie([makeActress({ thumb_url: '' })]), cast_version: 'vA' };
		const overlay = { ...baseline };
		const fresh = {
			...makeMovie([makeActress({ thumb_url: 'https://cdn.example/t.jpg' })]),
			cast_version: 'vB',
		};
		const rebased = rebaseOverlayOntoMovie(baseline, overlay, fresh);
		expect(rebased.actresses?.[0]?.thumb_url).toBe('https://cdn.example/t.jpg');
		expect(buildDisplayTitlePreviewSignature(rebased)).toBe(
			buildDisplayTitlePreviewSignature(overlay),
		);
	});

	it('changes when verification transitions (render eligibility)', () => {
		// codex PR269 P2: the template context drops credits whose actress is
		// unverified + ambiguity-quarantined, so verification alone can change
		// the rendered title — the signature must track it.
		const a = makeMovie([makeActress({ verified: false })]);
		const b = makeMovie([makeActress({ verified: true })]);
		expect(buildDisplayTitlePreviewSignature(b)).not.toBe(buildDisplayTitlePreviewSignature(a));
	});

	it('changes when a credit render token changes', () => {
		const credit = {
			actress_id: 7,
			credited_name: 'Yui Hatano',
			suppressed: false,
			order_index: 0,
		};
		const a = { ...makeMovie([makeActress({})]), credits: [credit] };
		const b = { ...makeMovie([makeActress({})]), credits: [{ ...credit, suppressed: true }] };
		expect(buildDisplayTitlePreviewSignature(b)).not.toBe(buildDisplayTitlePreviewSignature(a));
	});

	it('changes when an actress display name changes', () => {
		const a = makeMovie([makeActress({})]);
		const b = makeMovie([makeActress({ first_name: 'Aoi' })]);
		expect(buildDisplayTitlePreviewSignature(b)).not.toBe(buildDisplayTitlePreviewSignature(a));
	});

	it('changes when actress order changes', () => {
		const other = makeActress({ id: 9, first_name: 'Aoi', last_name: 'Sora', japanese_name: '蒼井そら' });
		const a = makeMovie([makeActress({}), other]);
		const b = makeMovie([other, makeActress({})]);
		expect(buildDisplayTitlePreviewSignature(b)).not.toBe(buildDisplayTitlePreviewSignature(a));
	});

	it('changes when scalar template inputs change', () => {
		const a = makeMovie([makeActress({})]);
		const b = { ...makeMovie([makeActress({})]), title: 'Different Title' };
		expect(buildDisplayTitlePreviewSignature(b)).not.toBe(buildDisplayTitlePreviewSignature(a));
	});
});
