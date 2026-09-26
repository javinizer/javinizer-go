import type { Movie } from '$lib/api/types';

/**
 * Change signature for the live display-title preview in MovieEditor.
 * A change in the signature refires the (debounced) display-title-preview POST.
 *
 * Volatile actress fields (thumb_url, verified, origin, name_key, aliases)
 * are intentionally excluded: media snapshots and identity resolution churn
 * them between job polls while the rendered title (names only) is unchanged.
 * Including them made the preview refire on every poll during organize
 * (v1.6.0 flap between "Preview unavailable" and "Rendering preview…").
 */
export function buildDisplayTitlePreviewSignature(movie: Movie): string {
	return JSON.stringify({
		id: movie.id,
		code: movie.code,
		title: movie.title,
		original_title: movie.original_title,
		description: movie.description,
		actresses: (movie.actresses ?? []).map((a) => [
			a.id ?? 0,
			a.dmm_id ?? 0,
			a.first_name ?? '',
			a.last_name ?? '',
			a.japanese_name ?? '',
		]),
		genres: movie.genres,
		runtime: movie.runtime,
		release_year: movie.release_year,
		release_date: movie.release_date,
		director: movie.director,
		maker: movie.maker,
		label: movie.label,
		series: movie.series,
		rating_score: movie.rating_score,
		poster_url: movie.poster_url,
		cover_url: movie.cover_url,
		trailer_url: movie.trailer_url,
		original_filename: movie.original_filename,
	});
}
