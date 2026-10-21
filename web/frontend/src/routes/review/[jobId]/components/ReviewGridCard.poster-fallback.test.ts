import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/svelte';
import type { Movie, FileResult } from '$lib/api/types';
import ReviewGridCard from './ReviewGridCard.svelte';

const auto = {
	id: 'TEST',
	poster_url: '/full.jpg',
	cropped_poster_url: '/auto.jpg',
	should_crop_poster: true,
} as Movie;
function props(movie: Movie, mode = 'center', imageType: 'poster' | 'cover' = 'poster') {
	const result = { movie } as FileResult;
	return {
		movieGroup: { movieId: 'TEST', results: [result], primaryResult: result },
		isSelected: false,
		isEdited: false,
		isBulkSelected: false,
		selectionMode: false,
		effectiveMovie: movie,
		displayPosterUrl: movie.cropped_poster_url,
		displayCoverUrl: '/cover.jpg',
		posterFallbackMode: mode,
		displayImageType: imageType,
		previewImageURL: (url?: string) => url || '',
		onclick: () => {},
	};
}
describe('grid poster source preview', () => {
	it.each(['center', 'fit'])('shows uncropped source for %s', (mode) => {
		const view = render(ReviewGridCard, { props: props(auto, mode) });
		expect(view.getByText(/Source preview;/)).toBeTruthy();
		expect(view.getByAltText('TEST').getAttribute('src')).toBe('/full.jpg');
		expect(view.getByAltText('TEST').className).toContain('object-contain');
	});
	it('tracks automatic to manual to cover-only changes', async () => {
		const view = render(ReviewGridCard, { props: props(auto) });
		const manual = {
			...auto,
			cropped_poster_url: '/manual.jpg',
			poster_crop_bounds: { x: 0, y: 0, width: 200, height: 300 },
			poster_crop_source_full: true,
		};
		await view.rerender(props(manual));
		expect(view.queryByText(/Source preview;/)).toBeNull();
		expect(view.getByAltText('TEST').getAttribute('src')).toBe('/manual.jpg');
		await view.rerender(
			props({ ...auto, poster_url: '', cover_url: '/cover.jpg', should_crop_poster: false }),
		);
		expect(view.getByText(/Source preview;/)).toBeTruthy();
		expect(view.getByAltText('TEST').getAttribute('src')).toBe('/cover.jpg');
	});
	it('preserves legacy auto crop', () => {
		const view = render(ReviewGridCard, { props: props(auto, 'legacy') });
		expect(view.queryByText(/Source preview;/)).toBeNull();
		expect(view.getByAltText('TEST').getAttribute('src')).toBe('/auto.jpg');
	});
	it('does not relabel cover view', () => {
		const view = render(ReviewGridCard, { props: props(auto, 'center', 'cover') });
		expect(view.queryByText(/Source preview;/)).toBeNull();
		expect(view.getByAltText('TEST').getAttribute('src')).toBe('/cover.jpg');
	});
});
