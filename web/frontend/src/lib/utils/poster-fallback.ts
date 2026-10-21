import type { Movie } from '$lib/api/types';

// A source image is not a preview of the generated center/fit output.
export function isFallbackSourcePreview(
	movie: Pick<Movie, 'should_crop_poster' | 'cropped_poster_url'>,
	mode?: string,
): boolean {
	return (
		(mode === 'center' || mode === 'fit') && !!movie.should_crop_poster && !movie.cropped_poster_url
	);
}
