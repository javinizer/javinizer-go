import { describe, it, expect, vi } from 'vitest';
import { render } from '@testing-library/svelte';
import type { Movie } from '$lib/api/types';
import ScreenshotManager from './ScreenshotManager.svelte';

vi.mock('$lib/api/client', () => ({
	apiClient: { getPreviewImageURL: (url: string) => url, withSessionParam: (url: string) => url },
}));
const geometry = { x: 0, y: 0, width: 200, height: 300 };
function preview(mode: string, overrides: Partial<Movie> = {}) {
	return render(ScreenshotManager, {
		props: {
			movie: {
				id: 'TEST',
				poster_url: '/source.jpg',
				cropped_poster_url: '/legacy-auto.jpg',
				should_crop_poster: true,
				...overrides,
			} as Movie,
			displayPosterUrl: '/legacy-auto.jpg',
			posterFallbackMode: mode,
			onUpdate: () => {},
		},
	});
}
describe('image manager automatic fallback preview', () => {
	it.each(['center', 'fit'])('ignores legacy temporary crop for %s', (mode) => {
		const view = preview(mode);
		expect(view.getByText(/Source preview;/)).toBeTruthy();
		expect(view.getByAltText('Poster').getAttribute('src')).toBe('/source.jpg');
	});
	it('uses cover-only automatic source', () => {
		const view = preview('fit', {
			poster_url: '',
			cover_url: '/cover.jpg',
			should_crop_poster: false,
		});
		expect(view.getByAltText('Poster').getAttribute('src')).toBe('/cover.jpg');
	});
	it('keeps explicit manual preview', () => {
		const view = preview('center', { poster_crop_bounds: geometry, poster_crop_source_full: true });
		expect(view.queryByText(/Source preview;/)).toBeNull();
		expect(view.getByAltText('Poster').getAttribute('src')).toBe('/legacy-auto.jpg');
	});
	it('keeps legacy preview', () => {
		const view = preview('legacy');
		expect(view.queryByText(/Source preview;/)).toBeNull();
		expect(view.getByAltText('Poster').getAttribute('src')).toBe('/legacy-auto.jpg');
	});
});
