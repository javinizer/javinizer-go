import type { Movie, PosterCropBounds, PosterCropResponse } from '$lib/api/types';

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
		case 'should_crop_poster': {
			const old = (target as unknown as Record<string, unknown>)[field];
			const next = (src as unknown as Record<string, unknown>)[field];
			(target as unknown as Record<string, unknown>)[field] = next;
			// Only invalidate when the source actually changed — re-selecting an
			// identical value must keep a still-valid manual crop.
			if (old !== next) {
				target.poster_crop_bounds = null;
			}
			break;
		}
		case 'cover_url': {
			// Cover only feeds the poster pipeline when no poster URL is set.
			const coverIsPosterSource = !target.poster_url;
			const oldCover = target.cover_url;
			target.cover_url = src.cover_url;
			if (coverIsPosterSource && oldCover !== src.cover_url) {
				target.poster_crop_bounds = null;
			}
			break;
		}
		default:
			(target as unknown as Record<string, unknown>)[field] =
				(src as unknown as Record<string, unknown>)[field];
			break;
	}
}
