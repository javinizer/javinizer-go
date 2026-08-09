import type { Movie } from '$lib/api/types';

export function buildMovieToSave(movie: Movie): Movie {
	return { ...movie };
}

export function buildMovieOverride(movie: Movie | undefined): Movie | undefined {
	return movie ? { ...movie } : undefined;
}

const jsonEq = (a: unknown, b: unknown): boolean => JSON.stringify(a) === JSON.stringify(b);

// rebaseOverlayOntoMovie keeps a REJECTED save's user intent without letting
// it clobber the concurrent edit that caused the rejection (codex P1): the
// post-reject refetch installs newer revisions, so retaining the whole
// pre-conflict overlay would let the next save pass CAS and overwrite the
// concurrent operation's changes. Rebase ONLY the user's field-level deltas
// (fields whose value differs from the pre-save server baseline) onto the
// fresh server movie; every untouched field follows the server. A changed id
// is user rekey intent and rides along like any other field.
export function rebaseOverlayOntoMovie(baseline: Movie, overlay: Movie, fresh: Movie): Movie {
	const rebased: Movie = { ...fresh };
	const baseRec = baseline as unknown as Record<string, unknown>;
	const overRec = overlay as unknown as Record<string, unknown>;
	for (const key of Object.keys(overRec)) {
		const value = overRec[key];
		// codex cloud P2: a key PRESENT with undefined is an explicit user
		// clear (e.g. date removal). Skipping it resurrects the server value
		// on rebase; compare-with-baseline preserves the clear on retry.
		if (value === undefined) {
			if (baseRec[key] !== undefined) {
				(rebased as unknown as Record<string, unknown>)[key] = undefined;
			}
			continue;
		}
		if (!jsonEq(baseRec[key], value)) {
			(rebased as unknown as Record<string, unknown>)[key] = value;
		}
	}
	return rebased;
}
