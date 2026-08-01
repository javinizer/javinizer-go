import { describe, it, expect } from 'vitest';
import type { Movie } from '$lib/api/types';
import { overlayFieldOverride } from './overlay-field-override';

function makeMovie(overrides: Partial<Movie> = {}): Movie {
	return {
		id: 'orig-id',
		code: 'orig-code',
		title: 'Orig Title',
		display_title: 'Orig Title',
		maker: 'Orig Maker',
		director: 'Orig Director',
		release_date: '2020-01-01',
		release_year: 2020,
		...overrides,
	};
}

describe('overlayFieldOverride', () => {
	it('title sets both target.title and target.display_title', () => {
		const target = makeMovie();
		const src = makeMovie({ title: 'New Title', display_title: 'New Title' });
		overlayFieldOverride(target, 'title', src);
		expect(target.title).toBe('New Title');
		expect(target.display_title).toBe('New Title');
	});

	it('content_id sets target.code (NOT target.content_id)', () => {
		const target = makeMovie({ code: 'old-code' });
		const src = makeMovie({ code: 'new-code' });
		overlayFieldOverride(target, 'content_id', src);
		expect(target.code).toBe('new-code');
	});

	it('release_date sets both target.release_date and target.release_year', () => {
		const target = makeMovie({ release_date: '2020-01-01', release_year: 2020 });
		const src = makeMovie({ release_date: '2023-06-15', release_year: 2023 });
		overlayFieldOverride(target, 'release_date', src);
		expect(target.release_date).toBe('2023-06-15');
		expect(target.release_year).toBe(2023);
	});

	it('default branch (e.g. maker) copies src.maker to target.maker', () => {
		const target = makeMovie({ maker: 'Orig Maker' });
		const src = makeMovie({ maker: 'New Maker' });
		overlayFieldOverride(target, 'maker', src);
		expect(target.maker).toBe('New Maker');
	});

	it.each(['poster_url', 'should_crop_poster'])(
		'%s override clears stale poster_crop_bounds',
		(field) => {
			const target = makeMovie({
				poster_crop_bounds: { x: 0, y: 0, width: 400, height: 600 },
			});
			const src = makeMovie({ poster_url: 'new-poster', cover_url: 'new-cover' });
			overlayFieldOverride(target, field, src);
			expect(target.poster_crop_bounds).toBeNull();
		},
	);

	it('cover_url override keeps bounds when poster_url is set (cover unused by poster pipeline)', () => {
		const target = makeMovie({
			poster_url: 'https://example.com/poster.jpg',
			cover_url: 'old-cover',
			poster_crop_bounds: { x: 0, y: 0, width: 400, height: 600 },
		});
		overlayFieldOverride(target, 'cover_url', makeMovie({ cover_url: 'new-cover' }));
		expect(target.cover_url).toBe('new-cover');
		expect(target.poster_crop_bounds).toEqual({ x: 0, y: 0, width: 400, height: 600 });
	});

	it('cover_url override clears bounds when cover is the poster source (no poster_url)', () => {
		const target = makeMovie({
			poster_url: undefined,
			cover_url: 'old-cover',
			poster_crop_bounds: { x: 0, y: 0, width: 400, height: 600 },
		});
		overlayFieldOverride(target, 'cover_url', makeMovie({ cover_url: 'new-cover' }));
		expect(target.poster_crop_bounds).toBeNull();
	});

	it('title override preserves poster_crop_bounds', () => {
		const target = makeMovie({
			poster_crop_bounds: { x: 0, y: 0, width: 400, height: 600 },
		});
		const src = makeMovie({ title: 'New Title', display_title: 'New Title' });
		overlayFieldOverride(target, 'title', src);
		expect(target.poster_crop_bounds).toEqual({ x: 0, y: 0, width: 400, height: 600 });
	});

	it('unrelated fields on target are preserved when overriding maker', () => {
		const target = makeMovie({ director: 'Orig Director', maker: 'Orig Maker' });
		const src = makeMovie({ maker: 'New Maker', director: 'Src Director' });
		overlayFieldOverride(target, 'maker', src);
		expect(target.director).toBe('Orig Director');
	});
});
