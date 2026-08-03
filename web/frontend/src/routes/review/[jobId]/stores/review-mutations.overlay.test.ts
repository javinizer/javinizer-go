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

	it('poster source override clears pending crop geometry (mirrors backend)', () => {
		const target = makeMovie({
			poster_url: 'https://old/poster.jpg',
			poster_crop_bounds: { x: 0.1, y: 0.1, width: 0.4, height: 0.9 },
			poster_crop_source_full: true,
		});
		const src = makeMovie({ poster_url: 'https://new/poster.jpg' });
		overlayFieldOverride(target, 'poster_url', src);
		expect(target.poster_url).toBe('https://new/poster.jpg');
		expect(target.poster_crop_bounds).toBeNull();
		expect(target.poster_crop_source_full).toBe(false);
	});

	it('should_crop_poster override clears pending crop geometry', () => {
		const target = makeMovie({
			poster_crop_bounds: { x: 0.1, y: 0.1, width: 0.4, height: 0.9 },
			poster_crop_source_full: true,
		});
		const src = makeMovie({ should_crop_poster: true });
		overlayFieldOverride(target, 'should_crop_poster', src);
		expect(target.should_crop_poster).toBe(true);
		expect(target.poster_crop_bounds).toBeNull();
	});

	it('cover_url override keeps geometry when an explicit poster exists', () => {
		const target = makeMovie({
			poster_url: 'https://cdn.example/explicit.jpg',
			poster_crop_bounds: { x: 0.1, y: 0.1, width: 0.4, height: 0.9 },
			poster_crop_source_full: true,
		});
		const src = makeMovie({ cover_url: 'https://cdn.example/new-cover.jpg' });
		overlayFieldOverride(target, 'cover_url', src);
		expect(target.cover_url).toBe('https://cdn.example/new-cover.jpg');
		expect(target.poster_crop_bounds).toEqual({ x: 0.1, y: 0.1, width: 0.4, height: 0.9 });
	});

	it('cover_url override clears geometry when cover is the poster source', () => {
		const target = makeMovie({
			poster_url: '',
			poster_crop_bounds: { x: 0.1, y: 0.1, width: 0.4, height: 0.9 },
		});
		const src = makeMovie({ cover_url: 'https://cdn.example/new-cover.jpg' });
		overlayFieldOverride(target, 'cover_url', src);
		expect(target.poster_crop_bounds).toBeNull();
	});

	it('non-poster overrides keep pending crop geometry', () => {
		const target = makeMovie({
			poster_crop_bounds: { x: 0.1, y: 0.1, width: 0.4, height: 0.9 },
		});
		const src = makeMovie({ maker: 'New Maker' });
		overlayFieldOverride(target, 'maker', src);
		expect(target.poster_crop_bounds).toEqual({ x: 0.1, y: 0.1, width: 0.4, height: 0.9 });
	});

	it('unrelated fields on target are preserved when overriding maker', () => {
		const target = makeMovie({ director: 'Orig Director', maker: 'Orig Maker' });
		const src = makeMovie({ maker: 'New Maker', director: 'Src Director' });
		overlayFieldOverride(target, 'maker', src);
		expect(target.director).toBe('Orig Director');
	});
});
