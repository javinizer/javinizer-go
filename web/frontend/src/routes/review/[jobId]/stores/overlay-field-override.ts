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
		default:
			(target as unknown as Record<string, unknown>)[field] =
				(src as unknown as Record<string, unknown>)[field];
			break;
	}
}
