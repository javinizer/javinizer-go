import { describe, it, expect } from 'vitest';
import type { Movie } from '$lib/api/types';
import { overlayPosterEdit, posterCropOverlayFromResponse, posterEditTargetFilePaths } from './overlay-field-override';

function makeMovie(overrides: Partial<Movie> = {}): Movie {
	return {
		id: 'orig-id',
		title: 'Orig Title',
		poster_url: 'https://example.com/cover.jpg',
		cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=1',
		should_crop_poster: true,
		...overrides,
	};
}

describe('posterEditTargetFilePaths', () => {
	const results = {
		'/a/ABC-001-pt1.mp4': { result_id: 'r1', movie_id: 'ABC-001' },
		'/a/ABC-001-pt2.mp4': { result_id: 'r2', movie_id: 'ABC-001' },
		'/a/XYZ-999.mp4': { result_id: 'r3', movie_id: 'XYZ-999' },
	};

	it('targets every part of the movie, matching the server-side per-movie fan-out', () => {
		const targets = posterEditTargetFilePaths(results, 'r1');
		expect(new Set(targets)).toEqual(new Set(['/a/ABC-001-pt1.mp4', '/a/ABC-001-pt2.mp4']));
	});

	it('falls back to the single matching result when movie_id linkage is absent', () => {
		const loose = { '/only.mp4': { result_id: 'r9' } };
		expect(posterEditTargetFilePaths(loose, 'r9')).toEqual(['/only.mp4']);
	});

	it('returns empty for an unknown result id', () => {
		expect(posterEditTargetFilePaths(results, 'nope')).toEqual([]);
	});
});

describe('posterCropOverlayFromResponse', () => {
	it('overlays the server-echoed bounds for normal crops', () => {
		const overlay = posterCropOverlayFromResponse({
			cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=2',
			poster_crop_bounds: { x: 10, y: 20, width: 300, height: 450 },
		});
		expect(overlay).toEqual({
			cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=2',
			should_crop_poster: false,
			poster_crop_bounds: { x: 10, y: 20, width: 300, height: 450 },
		});
	});

	it('overlays null bounds when the server dropped them (legacy-source crop)', () => {
		expect(posterCropOverlayFromResponse({ cropped_poster_url: '/x.jpg' }).poster_crop_bounds).toBeNull();
		expect(
			posterCropOverlayFromResponse({ cropped_poster_url: '/x.jpg', poster_crop_bounds: null })
				.poster_crop_bounds,
		).toBeNull();
	});
});

describe('overlayPosterEdit', () => {
	it('applies a manual poster crop onto pending edits without losing them', () => {
		const target = makeMovie({ title: 'User Edited Title' });
		const result = overlayPosterEdit(target, {
			cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=2',
			should_crop_poster: false,
			poster_crop_bounds: { x: 0, y: 0, width: 400, height: 600 },
		});

		expect(result.should_crop_poster).toBe(false);
		expect(result.cropped_poster_url).toBe('/api/v1/temp/posters/job/ABC-001.jpg?v=2');
		expect(result.poster_crop_bounds).toEqual({ x: 0, y: 0, width: 400, height: 600 });
		expect(result.title).toBe('User Edited Title');
		expect(result.poster_url).toBe('https://example.com/cover.jpg');
	});

	it('clears crop bounds when the edit carries null (poster replaced / reset)', () => {
		const target = makeMovie({
			poster_crop_bounds: { x: 1, y: 2, width: 3, height: 4 },
		});
		const result = overlayPosterEdit(target, {
			poster_url: 'https://example.com/new.jpg',
			cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=3',
			should_crop_poster: false,
			poster_crop_bounds: null,
		});

		expect(result.poster_crop_bounds).toBeNull();
		expect(result.poster_url).toBe('https://example.com/new.jpg');
	});

	it('keeps the current poster_url when the edit omits it', () => {
		const target = makeMovie();
		const result = overlayPosterEdit(target, {
			cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=2',
			should_crop_poster: false,
			poster_crop_bounds: { x: 0, y: 0, width: 400, height: 600 },
		});
		expect(result.poster_url).toBe('https://example.com/cover.jpg');
	});

	it('does not mutate the input movie', () => {
		const target = makeMovie();
		overlayPosterEdit(target, {
			cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=2',
			should_crop_poster: false,
			poster_crop_bounds: { x: 0, y: 0, width: 400, height: 600 },
		});
		expect(target.should_crop_poster).toBe(true);
		expect(target.poster_crop_bounds).toBeUndefined();
	});
});