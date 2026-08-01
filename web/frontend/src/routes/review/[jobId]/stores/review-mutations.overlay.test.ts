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
		'%s override clears stale poster_crop_bounds when the value changes',
		(field) => {
			const target = makeMovie({
				poster_crop_bounds: { x: 0, y: 0, width: 400, height: 600 },
				...(field === 'should_crop_poster' ? { should_crop_poster: false } : {}),
			});
			const src = makeMovie({
				poster_url: 'new-poster',
				cover_url: 'new-cover',
				poster_crop_bounds: null,
				...(field === 'should_crop_poster' ? { should_crop_poster: true } : {}),
			});
			overlayFieldOverride(target, field, src);
			expect(target.poster_crop_bounds).toBeNull();
		},
	);

	it('poster_url override whose response OMITS poster_crop_bounds clears stale bounds to null', () => {
		// The server omits poster_crop_bounds when it cleared them (omitempty
		// on *CropBounds in internal/api/contracts/movie_view.go). An absent
		// key on a source override therefore means "source changed; old bounds
		// obsolete", and the pending entry must end up with null — not keep
		// the stale pre-override bounds, or Save would resubmit them.
		const target = makeMovie({
			poster_url: 'https://example.com/old-poster.jpg',
			poster_crop_bounds: { x: 10, y: 10, width: 200, height: 300 },
		});
		const src = makeMovie({
			poster_url: 'https://dmm.example/new-poster.jpg',
			cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=2',
			should_crop_poster: false,
		});
		delete src.poster_crop_bounds;
		overlayFieldOverride(target, 'poster_url', src);
		expect(target.poster_crop_bounds).toBeNull();
	});

	it('poster_url override with explicit poster_crop_bounds:null in the response clears stale bounds', () => {
		const target = makeMovie({
			poster_url: 'https://example.com/old-poster.jpg',
			poster_crop_bounds: { x: 10, y: 10, width: 200, height: 300 },
		});
		const src = makeMovie({
			poster_url: 'https://dmm.example/new-poster.jpg',
			poster_crop_bounds: null,
		});
		overlayFieldOverride(target, 'poster_url', src);
		expect(target.poster_crop_bounds).toBeNull();
	});

	it('poster_url re-select of the identical URL carries the server-kept bounds', () => {
		// The server keeps a still-valid manual crop on an identical-source
		// re-select; src (the field-override response movie) is authoritative,
		// so the kept bounds arrive on the response and must land on the
		// pending edit intact.
		const kept = { x: 0, y: 0, width: 400, height: 600 };
		const target = makeMovie({
			poster_url: 'same-url',
			poster_crop_bounds: { ...kept },
		});
		overlayFieldOverride(
			target,
			'poster_url',
			makeMovie({ poster_url: 'same-url', poster_crop_bounds: { ...kept } }),
		);
		expect(target.poster_crop_bounds).toEqual(kept);
	});

	it('cover_url override behind an explicit poster carries the server-kept bounds', () => {
		// Cover is unused by the poster pipeline when poster_url is set, so the
		// server keeps the existing crop; the kept bounds arrive on the response
		// movie and must survive onto the pending edit.
		const kept = { x: 0, y: 0, width: 400, height: 600 };
		const target = makeMovie({
			poster_url: 'https://example.com/poster.jpg',
			cover_url: 'old-cover',
			poster_crop_bounds: { ...kept },
		});
		overlayFieldOverride(
			target,
			'cover_url',
			makeMovie({
				poster_url: 'https://example.com/poster.jpg',
				cover_url: 'new-cover',
				poster_crop_bounds: { ...kept },
			}),
		);
		expect(target.cover_url).toBe('new-cover');
		expect(target.poster_crop_bounds).toEqual(kept);
	});

	it('cover_url override clears bounds when cover is the poster source (no poster_url)', () => {
		const target = makeMovie({
			poster_url: undefined,
			cover_url: 'old-cover',
			poster_crop_bounds: { x: 0, y: 0, width: 400, height: 600 },
		});
		overlayFieldOverride(
			target,
			'cover_url',
			makeMovie({ cover_url: 'new-cover', poster_crop_bounds: null }),
		);
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

	// Finding A: a poster_url field override on a movie that already has a
	// pending edit must overlay the COMPLETE server-returned poster state onto
	// the edit entry. The backend regenerates cropped_poster_url and re-syncs
	// should_crop_poster for the newly selected source; a Save that resends
	// only the picked URL with the stale preview/intent organizes a landscape
	// scraper poster uncropped (updateBatchMovie does not re-sync when the
	// source URL is unchanged server-side).
	it('poster_url override onto a pending edit carries the full server poster state', () => {
		const target = makeMovie({
			// Unrelated pending edits the user already made:
			title: 'User Edited Title',
			maker: 'User Edited Maker',
			// Stale pre-override poster state:
			poster_url: 'https://example.com/old-poster.jpg',
			cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=1',
			should_crop_poster: true,
			poster_crop_bounds: { x: 10, y: 10, width: 200, height: 300 },
		});
		const src = makeMovie({
			// Server-synchronized poster state from the field-override response:
			poster_url: 'https://dmm.example/new-poster.jpg',
			cover_url: 'https://dmm.example/cover.jpg',
			cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=2',
			should_crop_poster: false,
			poster_crop_bounds: null,
		});
		overlayFieldOverride(target, 'poster_url', src);

		expect(target.poster_url).toBe('https://dmm.example/new-poster.jpg');
		expect(target.cover_url).toBe('https://dmm.example/cover.jpg');
		// The pending edit must carry the regenerated preview URL:
		expect(target.cropped_poster_url).toBe('/api/v1/temp/posters/job/ABC-001.jpg?v=2');
		// ...and the re-synced crop intent:
		expect(target.should_crop_poster).toBe(false);
		// The server cleared the stale bounds when the source changed:
		expect(target.poster_crop_bounds).toBeNull();
		// Unrelated pending edits survive untouched:
		expect(target.title).toBe('User Edited Title');
		expect(target.maker).toBe('User Edited Maker');
	});

	it('cover_url override leaves an unrelated cover edit alone when the response omits cover_url', () => {
		const target = makeMovie({
			poster_url: 'https://example.com/poster.jpg',
			cover_url: 'user-pending-cover',
		});
		const src = makeMovie({ poster_url: 'https://example.com/poster.jpg' });
		delete src.cover_url;
		overlayFieldOverride(target, 'cover_url', src);
		expect(target.cover_url).toBe('user-pending-cover');
	});
});
