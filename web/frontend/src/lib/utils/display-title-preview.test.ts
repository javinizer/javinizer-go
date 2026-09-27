import { describe, it, expect } from 'vitest';
import { buildDisplayTitlePreviewSignature } from './display-title-preview';
import { rebaseOverlayOntoMovie } from '../../routes/review/[jobId]/stores/save-helpers';
import type { Actress, Movie, MovieCredit } from '$lib/api/types';

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

function makeCredit(over: Partial<MovieCredit>): MovieCredit {
	return {
		actress_id: 7,
		credited_name: 'Yui Hatano',
		suppressed: false,
		order_index: 0,
		render_visible: true,
		...over,
	};
}

function makeMovie(actresses: Actress[], credits?: MovieCredit[]): Movie {
	return {
		id: 'ABC-123',
		title: 'Some Title',
		actresses,
		credits,
		genres: [],
		poster_url: 'https://cdn.example/p.jpg',
	} as Movie;
}

describe('buildDisplayTitlePreviewSignature', () => {
	it('is stable when only volatile actress fields churn between polls', () => {
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
		expect(buildDisplayTitlePreviewSignature(after)).toBe(
			buildDisplayTitlePreviewSignature(before),
		);
	});

	it('rebase after cast_version churn does not alter the preview signature', () => {
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

	it('is stable when a raw verified flag flips (render gate is quarantine-based)', () => {
		// v1.6.1 follow-up: #265 gates rendering on verified || !quarantined, so a
		// bare verified transition does not change the rendered title.
		const a = makeMovie([makeActress({ verified: false })]);
		const b = makeMovie([makeActress({ verified: true })]);
		expect(buildDisplayTitlePreviewSignature(b)).toBe(buildDisplayTitlePreviewSignature(a));
	});

	it('is stable when dmm_id is assigned mid-job', () => {
		const a = makeMovie([makeActress({ dmm_id: 0 })]);
		const b = makeMovie([makeActress({ dmm_id: 999 })]);
		expect(buildDisplayTitlePreviewSignature(b)).toBe(buildDisplayTitlePreviewSignature(a));
	});

	it('is stable when poster_url churns (not a display-title template input)', () => {
		const a = makeMovie([makeActress({})]);
		const b = { ...makeMovie([makeActress({})]), poster_url: 'https://cdn.example/new.jpg' };
		expect(buildDisplayTitlePreviewSignature(b)).toBe(buildDisplayTitlePreviewSignature(a));
	});

	it('ignores movie.actresses churn while credits drive the render', () => {
		const credits = [makeCredit({})];
		const a = makeMovie([makeActress({})], credits);
		const b = makeMovie(
			[makeActress({}), { ...makeActress({}), id: 9, first_name: 'Appended' }],
			credits,
		);
		expect(buildDisplayTitlePreviewSignature(b)).toBe(buildDisplayTitlePreviewSignature(a));
	});

	it('changes when credit render visibility transitions', () => {
		const a = makeMovie([], [makeCredit({ render_visible: false })]);
		const b = makeMovie([], [makeCredit({ render_visible: true })]);
		expect(buildDisplayTitlePreviewSignature(b)).not.toBe(buildDisplayTitlePreviewSignature(a));
	});

	it('is stable when an invisible credit churns names or order', () => {
		const a = makeMovie([], [makeCredit({ render_visible: false })]);
		const b = makeMovie(
			[],
			[makeCredit({ render_visible: false, credited_name: 'Renamed', order_index: 5 })],
		);
		expect(buildDisplayTitlePreviewSignature(b)).toBe(buildDisplayTitlePreviewSignature(a));
	});

	it('treats a locally suppressed credit as invisible', () => {
		const a = makeMovie([], [makeCredit({})]);
		const b = makeMovie([], [makeCredit({ suppressed: true })]);
		expect(buildDisplayTitlePreviewSignature(b)).not.toBe(buildDisplayTitlePreviewSignature(a));
	});

	it('changes when a visible credit render token changes', () => {
		const a = makeMovie([], [makeCredit({})]);
		const b = makeMovie([], [makeCredit({ override_name: 'Alias' })]);
		expect(buildDisplayTitlePreviewSignature(b)).not.toBe(buildDisplayTitlePreviewSignature(a));
	});

	it('changes when an actress display name changes (creditless render)', () => {
		const a = makeMovie([makeActress({})]);
		const b = makeMovie([makeActress({ first_name: 'Aoi' })]);
		expect(buildDisplayTitlePreviewSignature(b)).not.toBe(buildDisplayTitlePreviewSignature(a));
	});

	it('changes when actress order changes (creditless render)', () => {
		const other = makeActress({
			id: 9,
			first_name: 'Aoi',
			last_name: 'Sora',
			japanese_name: '蒼井そら',
		});
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
