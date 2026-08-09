import type { CropBounds, Movie } from '$lib/api/types';

// CropEcho is the server-echoed crop state returned by the poster-crop
// endpoint after a successful manual crop.
export interface CropEcho {
	cropped_poster_url: string;
	should_crop_poster: boolean;
	poster_crop_bounds: CropBounds | null;
	poster_crop_source_full: boolean;
	// server-side pre-edit poster baseline (backupPosterOriginals); echoed so
	// the client reset baseline is authoritative, never guessed.
	original_poster_url?: string;
	original_cropped_poster_url?: string;
	original_should_crop_poster?: boolean | null;
}

// applyCropEcho merges the server-echoed crop state into a movie — used for
// both the visible job-result state and the pending editedMovies overlay — so
// a pre-organize saveAllEdits() uploads crop-consistent data instead of
// clobbering the crop with stale pre-crop intent.
//
// A null bounds echo (legacy already-cropped source — nothing applyable is
// stored server-side) drops the key entirely so the overlay round-trips as
// "no geometry", matching server state and avoiding phantom dirty diffs.
export function applyCropEcho(movie: Movie, echo: CropEcho): Movie {
	// Adopt the server-side pre-edit baseline verbatim (set on the first
	// poster edit via backupPosterOriginals). Never derive it from the current
	// movie: by the time the echo arrives the crop fields are already
	// post-edit, and a partially-populated legacy baseline would guess wrong.
	const base: Movie = {
		...movie,
		cropped_poster_url: echo.cropped_poster_url,
		should_crop_poster: echo.should_crop_poster,
		// Presence, not truthiness: a cover-fallback movie legitimately echoes
		// an EMPTY original_poster_url with the pre-crop crop/intent baseline,
		// and that baseline is exactly what Reset must restore.
		...(echo.original_poster_url !== undefined
			? {
					original_poster_url: echo.original_poster_url,
					original_cropped_poster_url: echo.original_cropped_poster_url,
					original_should_crop_poster: echo.original_should_crop_poster ?? null,
				}
			: {}),
	};
	delete base.poster_crop_bounds;
	delete base.poster_crop_source_full;
	if (echo.poster_crop_bounds == null) {
		return base;
	}
	return {
		...base,
		poster_crop_bounds: echo.poster_crop_bounds,
		// must round-trip with the bounds: the apply gate refuses geometry
		// whose full-source flag did not survive the overlay save
		poster_crop_source_full: echo.poster_crop_source_full,
	};
}

// siblingResultFilePaths returns every result file path belonging to the
// same movie as resultId. The crop endpoint applies poster state to ALL parts
// of a multipart movie (same geometry per part), so the job-state/overlay
// sync must cover siblings too — a stale sibling overlay would re-upload
// pre-crop intent on the next save and wipe the stored geometry.
export function siblingResultFilePaths(
	results: Record<string, { result_id?: string; movie_id?: string }> | undefined,
	resultId: string,
): string[] {
	if (!results) return [];
	const entry = Object.values(results).find((r) => r?.result_id === resultId);
	const movieId = entry?.movie_id;
	if (!movieId) return [];
	// audit F6: fold case like the save path's sameFamily predicate and the
	// backend resultstore family index — case-variant multipart siblings must
	// receive the post-crop echo too, or their stale overlay re-uploads
	// pre-crop geometry on the next save.
	const folded = movieId.toLowerCase();
	return Object.entries(results)
		.filter(([, r]) => (r?.movie_id ?? '').toLowerCase() === folded)
		.map(([filePath]) => filePath);
}

// rescrapeClearedMovieKeys returns the movie IDs whose stored crop state a
// bulk rescrape actually cleared (success only). A rescrape that corrects
// the content ID reports the requested (old) ID on the result row while the
// job results and the echoed movie carry the corrected ID — so both the old
// request ID and the echoed new ID count as cleared.
export function rescrapeClearedMovieKeys(
	results: ReadonlyArray<{ movie_id: string; status: string; movie?: { id?: string } }>,
): Set<string> {
	const keys = new Set<string>();
	for (const r of results) {
		if (r.status !== 'success') continue;
		// codex cloud P1: keys fold case like family resolution (and the
		// review-mutations cleanup compare) — a rescrape that corrects the ID's
		// spelling must clear the overlay persisted under the OLD spelling.
		if (r.movie_id) keys.add(r.movie_id.toLowerCase());
		if (r.movie?.id) keys.add(r.movie.id.toLowerCase());
	}
	return keys;
}

// clearCropGeometry marks pending crop geometry for server-side clearing on
// the next save: an explicit null rides the movie PATCH as "clear", whereas
// an omitted key would preserve stored geometry. Used when the poster source
// is replaced (poster-from-URL) or the poster is reset to its baseline.
export function clearCropGeometry(movie: Movie): Movie {
	return { ...movie, poster_crop_bounds: null };
}