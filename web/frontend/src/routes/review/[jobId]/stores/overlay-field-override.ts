import type { FieldOverrideResponse, FileResult, Movie, PosterCropBounds, PosterCropResponse } from '$lib/api/types';

export interface PosterEditOverlay {
	poster_url?: string;
	cropped_poster_url: string;
	should_crop_poster: boolean;
	poster_crop_bounds: PosterCropBounds | null;
}

// posterEditTargetFilePaths resolves every file path belonging to the movie of
// the given result — the server applies poster edits to all files of the movie
// (multi-part), so the client overlay must fan out the same way or sibling
// entries keep stale poster state.
export function posterEditTargetFilePaths(
	results: Record<string, { result_id?: string; movie_id?: string }>,
	resultId: string,
): string[] {
	const entries = Object.entries(results);
	const hit = entries.find(([, r]) => r.result_id === resultId);
	if (!hit) return [];
	if (!hit[1].movie_id) return [hit[0]];
	const movieId = hit[1].movie_id;
	return entries.filter(([, r]) => r.movie_id === movieId).map(([path]) => path);
}

// posterCropOverlayFromResponse maps the crop endpoint response to the poster
// overlay. The bounds come from the server (effective stored state), NOT from
// the request: for legacy-source crops the server deliberately drops them, and
// re-uploading request bounds via a later whole-movie save would miscrop.
export function posterCropOverlayFromResponse(response: PosterCropResponse): PosterEditOverlay {
	return {
		cropped_poster_url: response.cropped_poster_url,
		should_crop_poster: false,
		poster_crop_bounds: response.poster_crop_bounds ?? null,
	};
}

// overlayPosterEdit returns a copy of movie with the poster-edit fields applied.
// Used by the poster-crop and poster-from-url mutations so a subsequent
// whole-movie Save (buildMovieToSave sends every field) doesn't clobber the
// server-side crop state with a stale pre-edit snapshot.
export function overlayPosterEdit(movie: Movie, edit: PosterEditOverlay): Movie {
	return {
		...movie,
		...(edit.poster_url !== undefined ? { poster_url: edit.poster_url } : {}),
		cropped_poster_url: edit.cropped_poster_url,
		should_crop_poster: edit.should_crop_poster,
		poster_crop_bounds: edit.poster_crop_bounds,
	};
}

// The field-override endpoint applies the override to EVERY part of a
// multipart movie server-side (ApplyFieldOverride iterates
// FindFilePathsForMovieID), but the response carries only the edited part's
// movie — which includes per-part identity fields such as original_filename.
// applyFieldOverrideToResults therefore mirrors the backend fan-out
// (mergeOverrideOntoPart in internal/worker/field_override.go): the SELECTED
// result takes response.movie wholesale, while each SIBLING result keeps its
// own movie and receives ONLY the overridden field's new value plus the
// synchronized poster state. Cloning response.movie onto siblings would
// corrupt their per-part fields (e.g. original_filename); the next
// whole-movie Save would then resubmit that corrupted snapshot as a
// deliberate change that updateBatchMovie fans across every part.
export function applyFieldOverrideToResults(
	results: Record<string, FileResult>,
	resultId: string,
	field: string,
	response: FieldOverrideResponse,
): Record<string, FileResult> {
	if (!response.movie) return results;
	const src = response.movie;
	const targets = new Set(posterEditTargetFilePaths(results, resultId));
	const updated: Record<string, FileResult> = { ...results };
	for (const [filePath, result] of Object.entries(updated)) {
		if (!targets.has(filePath)) continue;
		let movie = src;
		if (result.result_id !== resultId && result.movie) {
			const merged: Movie = { ...result.movie };
			overlayFieldOverride(merged, field, src);
			overlayPosterState(merged, src);
			movie = merged;
		}
		updated[filePath] = {
			...result,
			movie,
			field_sources: response.field_sources ?? result.field_sources,
			actress_sources: response.actress_sources ?? result.actress_sources,
		};
	}
	return updated;
}

// overlayPosterState mirrors the backend's wholesale Poster struct clone:
// the cached poster assets an override refreshes are movie-wide (every part
// shares {movie.ID}-full.jpg), so poster identity — unlike file identity —
// must stay identical across parts. An absent poster_crop_bounds key means
// "cleared" (omitempty), so it falls back to null rather than surviving stale.
function overlayPosterState(target: Movie, src: Movie): void {
	target.poster_url = src.poster_url;
	target.cover_url = src.cover_url;
	target.cropped_poster_url = src.cropped_poster_url;
	target.should_crop_poster = src.should_crop_poster;
	target.poster_crop_bounds = src.poster_crop_bounds ?? null;
}

// applyFieldOverrideToEditedMovies overlays ONLY the overridden field (plus
// its synchronized poster state) onto pending edits for every part of the
// movie — unrelated pending edits on sibling parts survive untouched.
export function applyFieldOverrideToEditedMovies(
	editedMovies: Map<string, Movie>,
	results: Record<string, FileResult>,
	resultId: string,
	field: string,
	src: Movie,
): void {
	const targets = new Set(posterEditTargetFilePaths(results, resultId));
	for (const [filePath, movie] of editedMovies) {
		if (!targets.has(filePath)) continue;
		const merged: Movie = { ...movie };
		overlayFieldOverride(merged, field, src);
		editedMovies.set(filePath, merged);
	}
}

export function overlayFieldOverride(target: Movie, field: string, src: Movie): void {
	switch (field) {
		case 'title':
		case 'display_title':
			target.title = src.title;
			target.display_title = src.display_title;
			break;
		case 'content_id':
			target.code = src.code;
			break;
		case 'release_date':
			target.release_date = src.release_date;
			target.release_year = src.release_year;
			break;
		case 'poster_url':
		case 'cover_url': {
			target.poster_url = src.poster_url;
			if (src.cover_url !== undefined) target.cover_url = src.cover_url;
			if (src.cropped_poster_url !== undefined) target.cropped_poster_url = src.cropped_poster_url;
			if (src.should_crop_poster !== undefined) target.should_crop_poster = src.should_crop_poster;
			// NOT guarded on !== undefined: the server omits poster_crop_bounds
			// when CLEARED (omitempty on *CropBounds), so an absent key means "the
			// source changed; old bounds are obsolete" and must clear to null —
			// leaving the stale pending bounds would resubmit them on Save.
			target.poster_crop_bounds = src.poster_crop_bounds ?? null;
			break;
		}
		case 'should_crop_poster': {
			const old = target.should_crop_poster;
			target.should_crop_poster = src.should_crop_poster;
			if (old !== src.should_crop_poster) target.poster_crop_bounds = null;
			break;
		}
		case 'id': {
			// An id override RE-KEYS the movie server-side (ApplyFieldOverride's
			// id-rekey: MigratePosterCacheAssets moves the cached poster files to
			// the new {movie.ID} key and RewritePosterIDInPreviewURL rewrites BOTH
			// persisted preview URLs), but the response only carries the one
			// edited part's movie. Overlaying just the new id onto a pending edit
			// would leave the next whole-movie Save (buildMovieToSave sends every
			// field) resubmitting the STALE cropped_poster_url /
			// original_cropped_poster_url pointing at the deleted old cache key —
			// the server performs no second migration — so the synchronized poster
			// state the server returned must come along with the id. Unrelated
			// pending fields (title, maker, ...) stay untouched.
			target.id = src.id;
			target.cropped_poster_url = src.cropped_poster_url;
			target.original_cropped_poster_url = src.original_cropped_poster_url;
			target.should_crop_poster = src.should_crop_poster;
			// NOT guarded on !== undefined: the server omits poster_crop_bounds
			// when CLEARED (omitempty on *CropBounds), so an absent key means
			// "cleared" and must land as null — same absent-key-vs-explicit-null
			// convention as the poster_url/cover_url case above.
			target.poster_crop_bounds = src.poster_crop_bounds ?? null;
			break;
		}
		default:
			(target as unknown as Record<string, unknown>)[field] = (
				src as unknown as Record<string, unknown>
			)[field];
			break;
	}
}
