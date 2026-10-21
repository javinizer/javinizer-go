import { describe, it, expect } from 'vitest';
import { isFallbackSourcePreview } from './poster-fallback';

describe('automatic poster fallback preview', () => {
	it.each([undefined, '', 'legacy'])('preserves legacy preview for %s', (mode) => {
		expect(isFallbackSourcePreview({ should_crop_poster: true }, mode)).toBe(false);
	});
	it.each(['center', 'fit'])('labels only unrendered automatic %s fallback as source', (mode) => {
		expect(isFallbackSourcePreview({ should_crop_poster: true }, mode)).toBe(true);
		expect(isFallbackSourcePreview({ should_crop_poster: false }, mode)).toBe(false);
		expect(
			isFallbackSourcePreview(
				{ should_crop_poster: true, cropped_poster_url: '/manual.jpg' },
				mode,
			),
		).toBe(false);
	});
});
