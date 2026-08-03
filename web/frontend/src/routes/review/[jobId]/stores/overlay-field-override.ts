import type { Movie } from '$lib/api/types';

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
		case 'should_crop_poster':
			(target as unknown as Record<string, unknown>)[field] =
				(src as unknown as Record<string, unknown>)[field];
			// Poster source/intent overrides clear stored manual crop geometry
			// server-side — mirror that in the pending-edit overlay so the next
			// save cannot re-upload geometry measured against the superseded image.
			target.poster_crop_bounds = src.poster_crop_bounds ?? null;
			target.poster_crop_source_full = src.poster_crop_source_full ?? false;
			break;
		case 'cover_url':
			(target as unknown as Record<string, unknown>)[field] =
				(src as unknown as Record<string, unknown>)[field];
			// Cover is only the effective poster source when poster_url is
			// empty — fanart churn under an explicit poster keeps the crop.
			if (!target.poster_url) {
				target.poster_crop_bounds = src.poster_crop_bounds ?? null;
				target.poster_crop_source_full = src.poster_crop_source_full ?? false;
			}
			break;
		default:
			(target as unknown as Record<string, unknown>)[field] =
				(src as unknown as Record<string, unknown>)[field];
			break;
	}
}
