import type { BatchJobResponse, Movie, PosterCropResponse } from '$lib/api/types';
import { siblingResultFilePaths } from './poster-crop-sync';

/**
 * Reconcile a committed poster edit without mutating the job or pending edits.
 * The caller supplies the poster-specific, immutable movie transformation and
 * applies the returned job and overlay updates after its route-generation guard.
 */
export function reconcilePosterSuccess(
	job: BatchJobResponse,
	editedMovies: ReadonlyMap<string, Movie>,
	resultId: string,
	response: Pick<PosterCropResponse, 'revision' | 'revisions'>,
	transformMovie: (movie: Movie) => Movie,
): { job: BatchJobResponse; editedMovieUpdates: Map<string, Movie> } {
	// The backend writes every case-insensitive multipart sibling. Update both
	// server movies and existing overlays so a later save cannot restore stale posters.
	const partPaths = siblingResultFilePaths(job.results, resultId);
	const updatedJob: BatchJobResponse = { ...job, results: { ...job.results } };
	for (const filePath of partPaths) {
		const result = updatedJob.results[filePath];
		if (result?.movie) {
			updatedJob.results[filePath] = { ...result, movie: transformMovie(result.movie) };
		}
	}

	// A nonempty committed map is authoritative, even when it omits the target.
	// Older servers' scalar revision belongs only to the target, never its siblings.
	if (response.revisions && Object.keys(response.revisions).length > 0) {
		const resultToPath = new Map(
			Object.entries(updatedJob.results).map(([filePath, result]) => [result.result_id, filePath]),
		);
		for (const [id, revision] of Object.entries(response.revisions)) {
			const filePath = resultToPath.get(id);
			if (filePath) {
				updatedJob.results[filePath] = { ...updatedJob.results[filePath], revision };
			}
		}
	} else if (response.revision !== undefined) {
		for (const [filePath, result] of Object.entries(updatedJob.results)) {
			if (result?.result_id === resultId) {
				updatedJob.results[filePath] = { ...result, revision: response.revision };
			}
		}
	}

	// Return only existing family edits, not a replacement map: unrelated edits
	// retain their identity and unedited siblings must not gain new overlays.
	const editedMovieUpdates = new Map<string, Movie>();
	for (const filePath of partPaths) {
		const movie = editedMovies.get(filePath);
		if (movie) editedMovieUpdates.set(filePath, transformMovie(movie));
	}
	return { job: updatedJob, editedMovieUpdates };
}
