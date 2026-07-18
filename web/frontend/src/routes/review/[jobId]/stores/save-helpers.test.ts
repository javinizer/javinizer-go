import { describe, it, expect } from 'vitest';
import type { Movie } from '$lib/api/types';
import { buildMovieToSave, buildMovieOverride } from './save-helpers';

function makeMovie(overrides: Partial<Movie> = {}): Movie {
	return {
		id: 'MKMP-094',
		code: 'mkmp094',
		title: 'Ayaka Tomoda',
		display_title: '[MKMP-094] Ayaka Tomoda',
		...overrides,
	};
}

describe('buildMovieToSave', () => {
	it('returns a shallow copy of the movie', () => {
		const movie = makeMovie();
		const out = buildMovieToSave(movie);
		expect(out).toEqual(movie);
		expect(out).not.toBe(movie);
	});

	it('does NOT overwrite title with display_title (regression for title doubling)', () => {
		const movie = makeMovie({ title: 'Ayaka Tomoda', display_title: '[MKMP-094] Ayaka Tomoda' });
		const out = buildMovieToSave(movie);
		expect(out.title).toBe('Ayaka Tomoda');
		expect(out.display_title).toBe('[MKMP-094] Ayaka Tomoda');
	});

	it('keeps a user-edited title intact when display_title is code-prefixed', () => {
		const movie = makeMovie({ title: 'My Better Title', display_title: '[MKMP-094] Ayaka Tomoda' });
		const out = buildMovieToSave(movie);
		expect(out.title).toBe('My Better Title');
	});
});

describe('buildMovieOverride', () => {
	it('returns a shallow copy when given a movie', () => {
		const movie = makeMovie();
		const out = buildMovieOverride(movie);
		expect(out).toEqual(movie);
		expect(out).not.toBe(movie);
	});

	it('returns undefined when given undefined', () => {
		expect(buildMovieOverride(undefined)).toBeUndefined();
	});

	it('does NOT overwrite title with display_title (regression for title doubling)', () => {
		const movie = makeMovie({ title: 'Ayaka Tomoda', display_title: '[MKMP-094] Ayaka Tomoda' });
		const out = buildMovieOverride(movie);
		expect(out?.title).toBe('Ayaka Tomoda');
		expect(out?.display_title).toBe('[MKMP-094] Ayaka Tomoda');
	});
});
