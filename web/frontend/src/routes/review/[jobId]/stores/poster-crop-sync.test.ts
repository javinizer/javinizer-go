import { describe, it, expect } from 'vitest';
import type { Movie } from '$lib/api/types';
import { buildMovieToSave } from './save-helpers';
import {
	applyCropEcho,
	clearCropGeometry,
	rescrapeClearedMovieKeys,
	siblingResultFilePaths,
} from './poster-crop-sync';

function makeMovie(overrides: Partial<Movie> = {}): Movie {
	return {
		id: 'ABC-001',
		code: 'ABC-001',
		title: 'Subject',
		poster_url: 'https://cdn.example/poster.jpg',
		cover_url: 'https://cdn.example/cover.jpg',
		should_crop_poster: true,
		cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=1',
		...overrides,
	};
}

const BOUNDS = { x: 0.1, y: 0.05, width: 0.4, height: 0.9, source_aspect: 1.667 };

describe('applyCropEcho', () => {
	it('writes the server-echoed geometry and resulting intent onto the movie', () => {
		const synced = applyCropEcho(makeMovie(), {
			cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=2',
			should_crop_poster: false,
			poster_crop_bounds: BOUNDS,
			poster_crop_source_full: true,
		});
		expect(synced.poster_crop_bounds).toEqual(BOUNDS);
		expect(synced.poster_crop_source_full).toBe(true);
		expect(synced.should_crop_poster).toBe(false);
		expect(synced.cropped_poster_url).toContain('v=2');
	});

	it('drops both geometry keys on a null (legacy fallback) echo', () => {
		const synced = applyCropEcho(
			makeMovie({ poster_crop_bounds: BOUNDS, poster_crop_source_full: true }),
			{
				cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=3',
				should_crop_poster: false,
				poster_crop_bounds: null,
				poster_crop_source_full: false,
			},
		);
		expect('poster_crop_bounds' in synced).toBe(false);
		expect('poster_crop_source_full' in synced).toBe(false);
		expect(synced.should_crop_poster).toBe(false);
	});

	it('saveAllEdits payload carries the post-crop state (amplifier regression)', () => {
		// The pre-organize saveAllEdits() PATCHes buildMovieToSave(overlayMovie);
		// the synced overlay must serialize the crop state so the save cannot
		// re-upload stale pre-crop intent.
		const overlayMovie = applyCropEcho(makeMovie(), {
			cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=2',
			should_crop_poster: false,
			poster_crop_bounds: BOUNDS,
			poster_crop_source_full: true,
		});
		const wire = JSON.parse(JSON.stringify(buildMovieToSave(overlayMovie)));
		expect(wire.poster_crop_bounds).toEqual(BOUNDS);
		// the apply gate refuses geometry whose full-source flag is lost in transit
		expect(wire.poster_crop_source_full).toBe(true);
		expect(wire.should_crop_poster).toBe(false);
	});

	it('adopts the server-echoed pre-crop baseline verbatim', () => {
		const synced = applyCropEcho(makeMovie(), {
			cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=2',
			should_crop_poster: false,
			poster_crop_bounds: BOUNDS,
			poster_crop_source_full: true,
			original_poster_url: 'https://cdn.example/scraped.jpg',
			original_cropped_poster_url: 'https://cdn.example/scraped-crop.jpg',
			original_should_crop_poster: true,
		});
		expect(synced.original_poster_url).toBe('https://cdn.example/scraped.jpg');
		expect(synced.original_cropped_poster_url).toContain('scraped-crop');
		expect(synced.original_should_crop_poster).toBe(true);
	});

	it('empty original_poster_url is still a baseline (cover-fallback movies)', () => {
		// Cover-fallback movie that originally required auto-crop: the server
		// echoes an empty poster URL with the pre-crop crop/intent baseline.
		const synced = applyCropEcho(makeMovie({ poster_url: '' }), {
			cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=2',
			should_crop_poster: false,
			poster_crop_bounds: BOUNDS,
			poster_crop_source_full: true,
			original_poster_url: '',
			original_cropped_poster_url: 'https://cdn.example/scraped-crop.jpg',
			original_should_crop_poster: true,
		});
		expect(synced.original_poster_url).toBe('');
		expect(synced.original_cropped_poster_url).toContain('scraped-crop');
		expect(synced.original_should_crop_poster).toBe(true);
	});

	it('never invents a baseline when the echo carries none', () => {
		const synced = applyCropEcho(makeMovie(), {
			cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=2',
			should_crop_poster: false,
			poster_crop_bounds: BOUNDS,
			poster_crop_source_full: true,
		});
		expect(synced.original_poster_url).toBeUndefined();
	});

	it('existing scraped baseline is never clobbered by the echo', () => {
		const synced = applyCropEcho(
			makeMovie({
				original_poster_url: 'https://cdn.example/original.jpg',
				original_cropped_poster_url: 'https://cdn.example/original-crop.jpg',
				original_should_crop_poster: true,
			}),
			{
				cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=2',
				should_crop_poster: false,
				poster_crop_bounds: BOUNDS,
				poster_crop_source_full: true,
			},
		);
		expect(synced.original_poster_url).toBe('https://cdn.example/original.jpg');
		expect(synced.original_cropped_poster_url).toContain('original-crop');
	});
});

describe('siblingResultFilePaths', () => {
	const results = {
		'/a/IPX-535.mp4': { result_id: 'r1', movie_id: 'IPX-535' },
		'/a/IPX-535-cd2.mp4': { result_id: 'r2', movie_id: 'IPX-535' },
		'/a/OTHER-1.mp4': { result_id: 'r3', movie_id: 'OTHER-1' },
	};

	it('returns every part of the same movie (multipart overlay sync)', () => {
		expect(siblingResultFilePaths(results, 'r1')).toEqual(['/a/IPX-535.mp4', '/a/IPX-535-cd2.mp4']);
	});

	it('is empty for an unknown resultId', () => {
		expect(siblingResultFilePaths(results, 'nope')).toEqual([]);
		expect(siblingResultFilePaths(undefined, 'r1')).toEqual([]);
	});
});

describe('rescrapeClearedMovieKeys', () => {
	it('keys successful rescrapes by requested ID and corrected content ID', () => {
		const keys = rescrapeClearedMovieKeys([
			{ movie_id: 'OLD-001', status: 'success', movie: { id: 'NEW-001' } },
			{ movie_id: 'KEEP-002', status: 'success', movie: { id: 'KEEP-002' } },
			{ movie_id: 'FAIL-003', status: 'failed' },
		]);
		expect(keys.has('OLD-001')).toBe(true);
		expect(keys.has('NEW-001')).toBe(true);
		expect(keys.has('KEEP-002')).toBe(true);
		expect(keys.has('FAIL-003')).toBe(false);
	});

	it('empty results clear nothing', () => {
		expect(rescrapeClearedMovieKeys([]).size).toBe(0);
	});
});

describe('clearCropGeometry', () => {
	it('marks geometry for explicit clear on the next save', () => {
		const cleared = clearCropGeometry(makeMovie({ poster_crop_bounds: BOUNDS }));
		expect(cleared.poster_crop_bounds).toBeNull();
		const wire = JSON.parse(JSON.stringify(buildMovieToSave(cleared)));
		expect(wire).toHaveProperty('poster_crop_bounds', null);
	});

	it('preserves unrelated pending edits', () => {
		const cleared = clearCropGeometry(makeMovie({ title: 'Edited', poster_crop_bounds: BOUNDS }));
		expect(cleared.title).toBe('Edited');
		expect(cleared.poster_url).toBe('https://cdn.example/poster.jpg');
	});
});