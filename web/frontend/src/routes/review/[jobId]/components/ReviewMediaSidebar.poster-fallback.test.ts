import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent } from '@testing-library/svelte';
import type { Movie } from '$lib/api/types';
import ReviewMediaSidebar from './ReviewMediaSidebar.svelte';

const noop = () => {};
function preview(
	mode?: string,
	croppedURL?: string,
	automatic = true,
	overrides: Partial<Movie> = {},
	onOpenPosterViewer = noop,
) {
	return render(ReviewMediaSidebar, {
		props: {
			currentMovie: {
				id: 'TEST',
				poster_url: '/source.jpg',
				should_crop_poster: automatic,
				cropped_poster_url: croppedURL,
				...overrides,
			} as Movie,
			displayPosterUrl: croppedURL || '/source.jpg',
			posterFallbackMode: mode,
			showPosterPanel: true,
			showCoverPanel: false,
			showTrailerPanel: false,
			showScreenshotsPanel: false,
			onOpenImageManager: noop,
			onOpenTrailerModal: noop,
			onOpenPosterViewer,
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
		const view = preview('center', '/manual.jpg', true, {
			poster_crop_bounds: { x: 0, y: 0, width: 200, height: 300 },
			poster_crop_source_full: true,
		});
		expect(view.queryByText(/Source preview;/)).toBeNull();
		expect(view.getByAltText('Poster').getAttribute('src')).toBe('/manual.jpg');
	});
	it('preserves native posters', () => {
		const view = preview('fit', undefined, false);
		expect(view.queryByText(/Source preview;/)).toBeNull();
		expect(view.getByAltText('Poster').className).toContain('object-contain');
	});
});

it.each(['center', 'fit'])(
	'ignores generated legacy crop for %s and opens original source',
	async (mode) => {
		const open = vi.fn();
		const view = preview(mode, '/api/v1/temp/legacy-auto.jpg', true, {}, open);
		expect(view.getByText(/Source preview;/)).toBeTruthy();
		expect(view.getByAltText('Poster').getAttribute('src')).toBe('/source.jpg');
		await fireEvent.click(view.getByAltText('Poster').closest('button')!);
		expect(open).toHaveBeenCalledWith('/source.jpg');
	},
);
it.each(['center', 'fit'])('renders cover-only fallback for %s', (mode) => {
	const view = preview(mode, undefined, false, { poster_url: '', cover_url: '/cover.jpg' });
	expect(view.getByText(/Source preview;/)).toBeTruthy();
	expect(view.getByAltText('Poster').getAttribute('src')).toBe('/cover.jpg');
});
