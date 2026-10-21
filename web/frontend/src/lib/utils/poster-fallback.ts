import type { Movie } from '$lib/api/types';

type PosterPreviewMovie = Pick<
	Movie,
	| 'should_crop_poster'
	| 'poster_url'
	| 'cover_url'
	| 'cropped_poster_url'
	| 'poster_crop_bounds'
	| 'poster_crop_source_full'
>;

// Scraping can create a legacy automatic cropped_poster_url. Only explicit
// crop geometry with full-source provenance establishes manual crop intent,
// matching the downloader; the presence of a rendered URL alone does not.
export function isFallbackSourcePreview(movie: PosterPreviewMovie, mode?: string): boolean {
	const manualIntent = movie.poster_crop_bounds != null && movie.poster_crop_source_full === true;
	const automaticIntent = !!movie.should_crop_poster || (!movie.poster_url && !!movie.cover_url);
	return (mode === 'center' || mode === 'fit') && automaticIntent && !manualIntent;
}

// Use the same original source as the automatic renderer, never an existing
// legacy temporary crop. Keep manual/native/legacy URL and cache handling intact.
export function resolveFallbackPreviewUrl(
	movie: PosterPreviewMovie,
	mode?: string,
	displayPosterUrl?: string,
): string | undefined {
	return isFallbackSourcePreview(movie, mode)
		? movie.poster_url || movie.cover_url || undefined
		: displayPosterUrl || movie.poster_url || undefined;
}
