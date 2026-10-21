import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/svelte';
import type { Movie } from '$lib/api/types';
import ReviewMediaSidebar from './ReviewMediaSidebar.svelte';

const noop = () => {};
function preview(mode?: string, croppedURL?: string, automatic = true) {
	return render(ReviewMediaSidebar, {
		props: {
			currentMovie: {
				id: 'TEST',
				should_crop_poster: automatic,
				cropped_poster_url: croppedURL,
			} as Movie,
			displayPosterUrl: croppedURL || '/source.jpg',
			posterFallbackMode: mode,
			showPosterPanel: true,
			showCoverPanel: false,
			showTrailerPanel: false,
			showScreenshotsPanel: false,
			onOpenImageManager: noop,
			onOpenTrailerModal: noop,
			onOpenPosterViewer: noop,
			onOpenCoverViewer: noop,
			onOpenScreenshotViewer: noop,
			previewImageURL: (url?: string) => url || '',
		},
	});
}

describe('review automatic fallback preview rendering', () => {
	it.each([undefined, '', 'legacy'])('keeps legacy visual preview for %s', (mode) => {
		const view = preview(mode);
		expect(view.getByAltText('Poster').getAttribute('style')).toContain('211.8%');
		expect(view.queryByText(/Source preview;/)).toBeNull();
	});
	it.each(['center', 'fit'])('shows full source and accurate label for %s', (mode) => {
		const view = preview(mode);
		expect(view.getByText(/Source preview;/)).toBeTruthy();
		expect(view.getByAltText('Poster').className).toContain('object-contain');
	});
	it('preserves rendered manual crops', () => {
		const view = preview('center', '/manual.jpg');
		expect(view.queryByText(/Source preview;/)).toBeNull();
		expect(view.getByAltText('Poster').getAttribute('src')).toBe('/manual.jpg');
	});
	it('preserves native posters', () => {
		const view = preview('fit', undefined, false);
		expect(view.queryByText(/Source preview;/)).toBeNull();
		expect(view.getByAltText('Poster').className).toContain('object-contain');
	});
});
