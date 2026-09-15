import { describe, it, expect } from 'vitest';
import type { BatchJobResponse, Movie } from '$lib/api/types';
import {
	buildMovieToSave,
	buildMovieOverride,
	mergePersistedMovieIntoBatchJob,
	rebaseOverlayOntoMovie,
} from './save-helpers';

function makeMovie(overrides: Partial<Movie> = {}): Movie {
	return {
		id: 'orig-id',
		code: 'orig-code',
		title: 'Orig Title',
		display_title: 'Orig Title',
		maker: 'Orig Maker',
		director: 'Orig Director',
		release_date: '2020-01-01',
		release_year: 2020,
		...overrides,
	} as Movie;
}

describe('buildMovieToSave / buildMovieOverride', () => {
	it('clones the movie', () => {
		const m = makeMovie();
		const out = buildMovieToSave(m);
		expect(out).toEqual(m);
		expect(out).not.toBe(m);
	});
	it('override returns undefined for undefined and clones otherwise', () => {
		expect(buildMovieOverride(undefined)).toBeUndefined();
		const m = makeMovie();
		const out = buildMovieOverride(m);
		expect(out).toEqual(m);
		expect(out).not.toBe(m);
	});
});

describe('mergePersistedMovieIntoBatchJob', () => {
	it('replaces every matching family result with independent persisted movies', () => {
		const stale = makeMovie({ id: 'MOV-1', actresses: [{ id: 1 }] });
		const otherMovie = makeMovie({ id: 'MOV-2' });
		const job = {
			results: {
				first: { movie_id: 'mov-1', movie: stale },
				second: { movie_id: 'MOV-1', movie: makeMovie({ id: 'MOV-1' }) },
				other: { movie_id: 'MOV-2', movie: otherMovie },
			},
		} as unknown as BatchJobResponse;
		const persisted = makeMovie({ id: 'MOV-1', actresses: [{ id: 2 }] });

		const refreshed = mergePersistedMovieIntoBatchJob(job, [' MOV-1 '], persisted);

		expect(refreshed).toBe(job);
		expect(job.results.first.movie?.actresses?.[0].id).toBe(2);
		expect(job.results.second.movie?.actresses?.[0].id).toBe(2);
		expect(job.results.first.movie).not.toBe(job.results.second.movie);
		expect(job.results.other.movie).toBe(otherMovie);
		expect(job.results.other.movie?.id).toBe('MOV-2');
	});
});

describe('rebaseOverlayOntoMovie (codex P1)', () => {
	it('untouched fields follow the fresh server value', () => {
		const baseline = makeMovie();
		const overlay = makeMovie({ maker: 'User Maker' }); // user edited maker only
		const fresh = makeMovie({ title: 'Server Title', display_title: 'Server Title' });
		const out = rebaseOverlayOntoMovie(baseline, overlay, fresh);
		expect(out.title).toBe('Server Title');
		expect(out.maker).toBe('User Maker');
	});

	it('nothing rebased when the overlay carries no deltas', () => {
		const baseline = makeMovie();
		const overlay = makeMovie();
		const fresh = makeMovie({ director: 'Server Director' });
		const out = rebaseOverlayOntoMovie(baseline, overlay, fresh);
		expect(out).toEqual(fresh);
	});

	it('array/nested user edits survive; untouched nested fields follow fresh', () => {
		const baseline = makeMovie({
			actresses: [{ first_name: 'A' }] as never,
			poster_url: 'https://orig/p.jpg',
		});
		const overlay = makeMovie({
			actresses: [{ first_name: 'B' }] as never,
			poster_url: 'https://orig/p.jpg',
		});
		const fresh = makeMovie({
			actresses: [{ first_name: 'A' }] as never,
			poster_url: 'https://server/p.jpg',
		});
		const out = rebaseOverlayOntoMovie(baseline, overlay, fresh);
		expect((out.actresses as Array<{ first_name: string }>)[0].first_name).toBe('B');
		expect(out.poster_url).toBe('https://server/p.jpg');
	});

	it('a changed id rides along as user rekey intent', () => {
		const baseline = makeMovie({ id: 'OLD-1' });
		const overlay = makeMovie({ id: 'NEW-1' });
		const fresh = makeMovie({ id: 'OLD-1', title: 'Server Title' });
		const out = rebaseOverlayOntoMovie(baseline, overlay, fresh);
		expect(out.id).toBe('NEW-1');
		expect(out.title).toBe('Server Title');
	});

	it('codex P2: an explicit clear of a previously-seen field SURVIVES the rebase', () => {
		const baseline = makeMovie(); // maker present at the baseline
		const overlay = makeMovie();
		(overlay as unknown as Record<string, unknown>).maker = undefined; // user cleared it
		const fresh = makeMovie({ maker: 'Server Maker' });
		const out = rebaseOverlayOntoMovie(baseline, overlay, fresh);
		expect(out.maker).toBeUndefined();
	});

	it('a clear-key whose baseline was absent stays skipped', () => {
		const baseline = makeMovie();
		delete (baseline as unknown as Record<string, unknown>).content_id; // never seen
		const overlay = makeMovie();
		(overlay as unknown as Record<string, unknown>).content_id = undefined;
		const fresh = makeMovie();
		(fresh as unknown as Record<string, unknown>).content_id = 'cid-x';
		const out = rebaseOverlayOntoMovie(baseline, overlay, fresh);
		expect((out as unknown as Record<string, unknown>).content_id).toBe('cid-x');
	});
});
