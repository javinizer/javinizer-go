import { describe, it, expect } from 'vitest';
import { isFallbackSourcePreview, resolveFallbackPreviewUrl } from './poster-fallback';

const automatic = {
	should_crop_poster: true,
	poster_url: '/full.jpg',
	cropped_poster_url: '/legacy-auto.jpg',
};
const geometry = { x: 0, y: 0, width: 200, height: 300 };

describe('automatic poster fallback preview', () => {
	it.each([undefined, '', 'legacy'])('preserves legacy preview for %s', (mode) => {
		expect(isFallbackSourcePreview(automatic, mode)).toBe(false);
		expect(resolveFallbackPreviewUrl(automatic, mode, '/legacy-auto.jpg?v=1')).toBe(
			'/legacy-auto.jpg?v=1',
		);
	});
	it.each(['center', 'fit'])('uses full source despite legacy temporary crop for %s', (mode) => {
		expect(isFallbackSourcePreview(automatic, mode)).toBe(true);
		expect(resolveFallbackPreviewUrl(automatic, mode, '/legacy-auto.jpg?v=1')).toBe('/full.jpg');
		const coverOnly = { should_crop_poster: false, poster_url: '', cover_url: '/cover.jpg' };
		expect(isFallbackSourcePreview(coverOnly, mode)).toBe(true);
		expect(resolveFallbackPreviewUrl(coverOnly, mode)).toBe('/cover.jpg');
		expect(isFallbackSourcePreview({ ...automatic, should_crop_poster: false }, mode)).toBe(false);
		const manual = { ...automatic, poster_crop_bounds: geometry, poster_crop_source_full: true };
		expect(isFallbackSourcePreview(manual, mode)).toBe(false);
		expect(resolveFallbackPreviewUrl(manual, mode, '/manual.jpg?v=2')).toBe('/manual.jpg?v=2');
		expect(
			isFallbackSourcePreview(
				{ ...automatic, poster_crop_bounds: geometry, poster_crop_source_full: false },
				mode,
			),
		).toBe(true);
	});
});
it('handles missing source and keeps native/manual URL fallbacks', () => {
	expect(
		resolveFallbackPreviewUrl({ should_crop_poster: true }, 'center', '/auto.jpg'),
	).toBeUndefined();
	expect(resolveFallbackPreviewUrl({}, 'fit')).toBeUndefined();
	expect(resolveFallbackPreviewUrl({ poster_url: '/native.jpg' }, 'fit')).toBe('/native.jpg');
	expect(
		resolveFallbackPreviewUrl({ should_crop_poster: true, cover_url: '/cover.jpg' }, 'center'),
	).toBe('/cover.jpg');
	expect(isFallbackSourcePreview({ should_crop_poster: false }, 'center')).toBe(false);
	expect(
		isFallbackSourcePreview({ poster_crop_bounds: geometry, poster_crop_source_full: true }, 'fit'),
	).toBe(false);
});
