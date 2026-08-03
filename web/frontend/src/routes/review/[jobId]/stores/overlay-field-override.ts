import type { FieldOverrideResponse, FileResult, Movie, PosterCropBounds, PosterCropResponse } from '$lib/api/types';

export interface PosterEditOverlay {
	poster_url?: string;
	cropped_poster_url: string;
	should_crop_poster: boolean;
	poster_crop_bounds: PosterCropBounds | null;
	// Revert-baseline fields the server echoes from the post-edit state: the
	// crop / poster-from-URL handlers may have lazily STAMPED them
	// (backupPosterOriginals) on a legacy result that lacked a baseline.
	// Undefined means the response predates the fields — leave the target
	// untouched; a present value (even "" or null) is authoritative server
	// state and must overwrite, or a whole-movie Save issued before the async
	// refetch applies resubmits empty originals through UpdateMovie and
	// destroys the reset target the edit just created.
	original_poster_url?: string;
	original_cropped_poster_url?: string;
	original_should_crop_poster?: boolean | null;
}

// sameMovieIdentity mirrors the backend's case-insensitive movie-ID
// identity (resultstore indexKey lowercases; every FindFilePathsForMovieID
// / fan-out / lock key folds case). Case-variant siblings ("ABC-1" vs
// "abc-1") are one family server-side: fan-out helpers must match them or
// the folded sibling keeps stale state in the UI until refetch — and a
// later whole-movie Save would resubmit that stale snapshot over the
// server's fan-out.
export function sameMovieIdentity(a: string | undefined, b: string | undefined): boolean {
	return a !== undefined && b !== undefined && a.toLowerCase() === b.toLowerCase();
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
	return entries.filter(([, r]) => sameMovieIdentity(r.movie_id, movieId)).map(([path]) => path);
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
		original_poster_url: response.original_poster_url,
		original_cropped_poster_url: response.original_cropped_poster_url,
		original_should_crop_poster: response.original_should_crop_poster,
	};
}

export type PosterResetPlan = 'noop' | 'restore-url' | 'local-revert';

// planPosterReset decides how a poster Reset proceeds against the scraped
// baseline (original_poster_url group): a baseline URL the movie no longer
// references must be restored through the poster-from-URL server round-trip
// (it replaces the cached {movieID}-full.jpg — that mutation's onSuccess
// already clears the locally persisted crop geometry), while a URL-unchanged
// reset is a purely LOCAL revert of the crop preview/intent with NO server
// round-trip — so nothing else invalidates crop geometry measured in the
// pre-reset crop session. Callers receiving 'local-revert' MUST drop the
// stored crop states for posterEditTargetFilePaths(results, resultId),
// or reopening the crop editor restores the old box and a blind Apply
// silently undoes the reset.
export function planPosterReset(
	current: { poster_url?: string; cropped_poster_url?: string; should_crop_poster?: boolean },
	baseline: { poster_url: string; cropped_poster_url: string; should_crop_poster: boolean } | undefined,
): PosterResetPlan {
	if (!baseline || !baseline.poster_url) return 'noop';
	if ((current.poster_url ?? '') !== baseline.poster_url) return 'restore-url';
	if (
		(current.cropped_poster_url ?? '') !== baseline.cropped_poster_url ||
		(current.should_crop_poster ?? false) !== baseline.should_crop_poster
	) {
		return 'local-revert';
	}
	return 'noop';
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
		// Baseline fields follow the poster_url convention: applied only when
		// the server actually echoed them, so a response predating the fields
		// cannot wipe an existing baseline with undefined.
		...(edit.original_poster_url !== undefined
			? { original_poster_url: edit.original_poster_url }
			: {}),
		...(edit.original_cropped_poster_url !== undefined
			? { original_cropped_poster_url: edit.original_cropped_poster_url }
			: {}),
		...(edit.original_should_crop_poster !== undefined
			? { original_should_crop_poster: edit.original_should_crop_poster }
			: {}),
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
			// An id override re-keys the movie server-side: UpdateMovie sets
			// FileMatchInfo.MovieID = movie.ID for EVERY fanned-out part and the
			// cached poster assets move to the new {movie.ID} key. Mirror that
			// here or FileResult.movie_id stays on the old key until the async
			// refetch, and the crop modal requests the moved
			// {oldID}-full.jpg/{oldID}.jpg paths (poster-crop-controller builds
			// them from currentResult.movie_id).
			...(field === 'id' && src.id ? { movie_id: src.id } : {}),
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
// Exported: the single-rescrape controller (rescrape-controller.ts P1-B)
// reuses exactly this poster overlay for sibling pending-edit
// reconciliation — the backend fans the rescraped movie's wholesale Poster
// clone out to same-ID siblings (rescrape_phase.go I7), the exact state
// shape this overlay mirrors.
export function overlayPosterState(target: Movie, src: Movie): void {
	target.poster_url = src.poster_url;
	target.cover_url = src.cover_url;
	target.cropped_poster_url = src.cropped_poster_url;
	target.should_crop_poster = src.should_crop_poster;
	target.poster_crop_bounds = src.poster_crop_bounds ?? null;
	// The revert baseline travels with the wholesale Poster clone, too: the
	// backend rewrite-pointed OriginalCroppedPosterURL on id rekeys, and a
	// sibling left with the pre-override originals would Reset against a
	// stale/moved asset (review-state posterBaseline) or resubmit the stale
	// baseline through a pre-refetch whole-movie Save. The Go Poster fields
	// carry NO omitempty, so a present movie always ships authoritative values
	// ("" / null) — assign directly rather than the ??-undefined convention.
	target.original_poster_url = src.original_poster_url;
	target.original_cropped_poster_url = src.original_cropped_poster_url;
	target.original_should_crop_poster = src.original_should_crop_poster ?? null;
	target.original_cover_url = src.original_cover_url;
}

// applyFieldOverrideToEditedMovies overlays ONLY the overridden field (plus
// its synchronized poster state) onto pending edits for every part of the
// movie — unrelated pending edits on sibling parts survive untouched.
//
// The poster state overlay is the COMPLETE baseline (overlayPosterState,
// parity with applyFieldOverrideToResults): the backend synchronizes the
// whole Poster struct across parts on every override, and overlayFieldOverride
// alone carries at most a subset (its 'id' case omits original_poster_url /
// original_cover_url / original_should_crop_poster; scalar cases carry
// nothing). A pending edit left with the pre-override originals would resubmit
// those stale reset URLs through the next whole-movie Save (buildMovieToSave
// sends every field), overwriting the synchronized baseline server-side.
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
		overlayPosterState(merged, src);
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
