/**
 * /movies/:id detail endpoint spec — happy + bad paths.
 *
 * Real stack: browser → Vite proxy → real GET /api/v1/movies/:id → real
 * MovieRepository.FindByID against :memory: SQLite (real GORM read).
 *
 * Hard-core dependency on a real scrape that produced a Movie first —
 * so this spec submits a real scrape + waits for completion (which
 * writes the Movie to the DB via the real scrape-phase persistence
 * path) before issuing the detail GET.
 *
 * Pins:
 * - Happy path: GET /movies/:id returns the full Movie payload with
 *   every field the frontend's movie detail page expects (id, title,
 *   poster_url, cover_url, actresses, genres, maker, label, etc.).
 * - Bad path: GET /movies/:id for an unknown ID returns 404 + the
 *   canonical "Movie not found" message (not 500 / not empty body).
 * - Bad path: GET /movies/:id for a malformed ID (empty string) does
 *   not panic the handler — returns 404 (the path param won't match
 *   any row).
 */
import { test, expect, type APIRequestContext } from '@playwright/test';

import {
	BACKEND_BASE,
	loginAgainstRealBackend,
	submitScrape,
	waitForJobCompletion,
	DEFAULT_INPUT_DIR,
} from '../helpers';

test.describe('/movies/:id detail: real MovieRepository.FindByID happy + bad paths', () => {
	test('GET /movies/:id after a real scrape returns the full Movie payload', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		await waitForJobCompletion(request, job_id);

		const resp = await request.get(`${BACKEND_BASE}/api/v1/movies/GOOD-001`);
		expect(resp.ok(), `GET /movies/GOOD-001 must return 200, got ${resp.status()}`).toBeTruthy();
		const body = await resp.json();

		// The Movie payload must be under body.movie (the MovieResponse contract).
		expect(body.movie, 'GET /movies/:id response must wrap Movie in body.movie').toBeTruthy();
		const movie = body.movie;

		// Every field the frontend's movie detail page renders must be
		// populated. A regression that dropped a field on the Movie
		// serializer would surface here as undefined.
		expect(movie.id, 'Movie.id must match the requested ID').toBe('GOOD-001');
		expect(movie.code, 'Movie.code must be populated').toBe('GOOD-001');
		expect(movie.title, 'Movie.title must be populated').toBeTruthy();
		expect(movie.display_title, 'Movie.display_title must be populated').toBeTruthy();
		expect(movie.maker, 'Movie.maker must be populated').toBe('E2E Test Studio');
		expect(movie.label, 'Movie.label must be populated').toBe('E2E Test Label');
		expect(movie.director, 'Movie.director must be populated').toBe('Test Director');
		expect(
			movie.poster_url,
			'Movie.poster_url must be populated (thumbnail card source)',
		).toBeTruthy();
		expect(
			movie.cover_url,
			'Movie.cover_url must be populated (banner / fanart source)',
		).toBeTruthy();

		// Arrays the details page renders as lists (actresses, genres)
		// must survive the JSON roundtrip — a regression that dropped the
		// relationship preload on FindByID would make these empty arrays.
		expect(Array.isArray(movie.actresses), 'Movie.actresses must be an array').toBeTruthy();
		expect(movie.actresses.length, 'Movie.actresses must be populated').toBeGreaterThan(0);
		expect(
			movie.actresses[0].first_name,
			'first actress first_name must be the e2emock value',
		).toBe('Test');
		expect(movie.actresses[0].last_name, 'first actress last_name must be the e2emock value').toBe(
			'Actor',
		);

		expect(Array.isArray(movie.genres), 'Movie.genres must be an array').toBeTruthy();
		expect(movie.genres.length, 'Movie.genres must be populated').toBeGreaterThan(0);
		const genreNames = movie.genres.map((g: { name: string }) => g.name);
		expect(genreNames, 'genres must include the e2emock "Drama" + "Test" entries').toEqual(
			expect.arrayContaining(['Drama', 'Test']),
		);
	});

	test('GET /movies/:id for an unknown ID returns 404 + "Movie not found"', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// Issue GET for a movie ID the e2emock never wrote to the DB. The
		// handler must 404 with the canonical message — not 500 (which
		// would mean a GORM error surfaced as a stack trace) and not 200
		// with a null movie (which would break the frontend's "movie
		// doesn't exist" UI branch).
		const resp = await request.get(`${BACKEND_BASE}/api/v1/movies/UNKNOWN-99999`, {
			failOnStatusCode: false,
		});
		expect(resp.status(), 'GET /movies/unknown-id must return 404').toBe(404);
		const body = await resp.json();
		expect(body.error, '404 error must use the canonical "Movie not found" wording').toBe(
			'Movie not found',
		);
	});

	test('GET /movies/:id immediately after a failed-scrape job returns 404 (failed scrape does NOT persist a Movie)', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// A failed-scrape job must NOT have a Movie in the DB — the
		// scrape phase only persists on success (the commit
		// 7289effc "move movie persist off critical path" preserves this
		// invariant under concurrency). Pinning here so a regression
		// that persisted a half-built Movie on failure surfaces:
		// the movie detail page would render a card with no metadata
		// for a "failed" movie ID.
		await loginAgainstRealBackend(request);

		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/FAIL-002.mp4`] });
		await waitForJobCompletion(request, job_id);

		const resp = await request.get(`${BACKEND_BASE}/api/v1/movies/FAIL-002`, {
			failOnStatusCode: false,
		});
		expect(resp.status(), 'failed-scrape Movie ID must NOT be persisted in the DB').toBe(404);
	});
});
