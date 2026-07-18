import type { Movie } from '$lib/api/types';

export function buildMovieToSave(movie: Movie): Movie {
	return { ...movie };
}

export function buildMovieOverride(movie: Movie | undefined): Movie | undefined {
	return movie ? { ...movie } : undefined;
}
