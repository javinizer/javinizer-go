/**
 * Rescrape endpoint spec.
 *
 * Real stack: browser → Vite proxy → real POST
 * /api/v1/batch/:id/results/:resultId/rescrape → real
 * RescrapeOrchestrator → real workflow factory + delegate→execute
 * pipeline → real e2emock scraper (cache bypass via Force=true).
 *
 * Pins:
 * - The rescrape endpoint accepts a previously-completed job's result_id
 *   + returns 200 with the Movie payload — proving the route is
 *   registered, the orchestrator wired, and the result tracker holds
 *   the FileMatchInfo.MovieID needed to drive the rescrape.
 * - The returned Movie carries the same ID as the original scrape's
 *   MovieID — proving the rescrape path resolves the resultId + filePath
 *   + MovieID correctly (regression class: a result tracker that lost
 *   the FileMatchInfo would 404 on the rescrape call).
 * - Rescrape on a NON-existent result_id returns 404 with the canonical
 *   "Result ... not found in job" message.
 * - Rescrape with force=true bypasses the cache + returns the same Movie
 *   from the fresh scraper invocation (the e2emock returns deterministic
 *   data per MovieID, so rescraper == cached Movie payload — but the
 *   exercise is the real Force path through the orchestrator).
 */
import { test, expect, type APIRequestContext } from '@playwright/test';

import {
	BACKEND_BASE,
	loginAgainstRealBackend,
	submitScrape,
	waitForJobCompletion,
	soleResult,
	DEFAULT_INPUT_DIR,
} from '../helpers';

test.describe('Rescrape: real RescrapeOrchestrator re-invokes the scraper for an existing result', () => {
	test('POST /batch/:id/results/:resultId/rescrape returns 200 + the Movie payload', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// Seed — submit + complete a real scrape to get a result_id.
		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		const job = await waitForJobCompletion(request, job_id);
		const { result } = soleResult(job);
		expect(result.result_id, 'precondition: scrape must produce a result_id').toBeTruthy();

		// Drive the real rescrape endpoint. failOnStatusCode:false so we
		// can inspect non-2xx bodies for diagnostics on a regression.
		const resp = await request.post(
			`${BACKEND_BASE}/api/v1/batch/${job_id}/results/${result.result_id}/rescrape`,
			{
				data: { selected_scrapers: ['e2emock'], force: true },
				failOnStatusCode: false,
			},
		);
		expect(
			resp.status(),
			`rescrape must return 200, got ${resp.status()} ${await resp.text()}`,
		).toBe(200);
		const body = await resp.json();
		// The response carries the Movie payload (BatchRescrapeResponse.Movie).
		expect(body.movie, 'rescrape response must carry the Movie payload').toBeTruthy();
		expect(body.movie.id, 'rescrape Movie.id must match the original MovieID').toBe('GOOD-001');
	});

	test('POST /batch/:id/results/:resultId/rescrape with a non-existent result_id returns 404', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		await waitForJobCompletion(request, job_id);

		// Use a syntactically-valid but non-existent UUID — the resolver
		// path must return 404, not 500 or 200 with empty body.
		const fakeResultId = '00000000-0000-0000-0000-000000000000';
		const resp = await request.post(
			`${BACKEND_BASE}/api/v1/batch/${job_id}/results/${fakeResultId}/rescrape`,
			{
				data: { selected_scrapers: ['e2emock'] },
				failOnStatusCode: false,
			},
		);
		expect(resp.status(), 'rescrape of non-existent result_id must return 404').toBe(404);
		const body = await resp.json();
		expect(body.error, '404 error message must mention the result_id').toContain(fakeResultId);
		expect(
			body.error,
			'404 error message must use the canonical "not found in job" wording',
		).toContain('not found in job');
	});

	test('POST /batch/:id/results/:resultId/rescrape with sequential scrapes re-invokes the source + returns the Movie', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// Rescrape right after a successful scrape — the e2emock is
		// deterministic per MovieID, so the rescrape returns the SAME Movie
		// payload (same id, title, poster_url). The value of this test is
		// the exercise of the real RescrapeOrchestrator + workflow factory +
		// the FileMatchInfo.MovieID lookup on the previous result (not the
		// metadata change, which is impossible with a deterministic scraper).
		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		const job = await waitForJobCompletion(request, job_id);
		const { result } = soleResult(job);
		expect(result.result_id, 'precondition: scrape must produce a result_id').toBeTruthy();

		// Capture the original Movie payload first.
		const originalMovie = result.movie;
		expect(originalMovie, 'precondition: scrape Movie must be populated').toBeTruthy();

		const resp = await request.post(
			`${BACKEND_BASE}/api/v1/batch/${job_id}/results/${result.result_id}/rescrape`,
			{
				data: { selected_scrapers: ['e2emock'], force: true },
				failOnStatusCode: false,
			},
		);
		expect(
			resp.status(),
			`rescrape must return 200, got ${resp.status()} ${await resp.text()}`,
		).toBe(200);
		const body = await resp.json();
		expect(body.movie, 'rescrape response must carry the Movie payload').toBeTruthy();
		expect(body.movie.id, 'rescrape Movie.id must match the original MovieID').toBe('GOOD-001');
		expect(body.movie.title, 'rescrape Movie.title must match the original').toBe(
			originalMovie?.title,
		);
		expect(body.movie.poster_url, 'rescrape Movie.poster_url must match the original').toBe(
			originalMovie?.poster_url,
		);
	});
});
