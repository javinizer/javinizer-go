/**
 * Bulk-rescrape endpoint spec.
 *
 * Real stack: browser → Vite proxy → real POST
 * /api/v1/batch/:id/movies/batch-rescrape → real BatchRescrapeRequest
 * binding → real batch-rescrape handler → real per-movie rescrape
 * pipeline using selected_scrapers.
 *
 * Endpoint contract (verified against the live backend before writing
 * this spec):
 *   Request  body:  { movie_ids: string[], selected_scrapers?: string[],
 *                     force?: bool, manual_search_input?: string }
 *   Response body:  { results: Array<{ movie_id, status, movie? }>,
 *                     succeeded: number, failed: number,
 *                     job: BatchJobResponse }   // refreshed source job
 *
 * The endpoint runs the rescrape SYNCHRONOUSLY — it returns the
 * rescraped Movies inline + the refreshed source-job state under `job`.
 * There is no separate rescrape job_id (unlike the per-result rescrape
 * endpoint at /results/:resultId/rescrape, which uses BatchScrapeResponse
 * shape with a new job_id).
 *
 * Bulk-rescrape is the "Rescrape all selected" action on the review page.
 * It requires `movie_ids` (binding:"required") AND either
 * `selected_scrapers` or `manual_search_input` (use-case-layer validation
 * — the use-case rejects requests that omit both).
 *
 * Pins:
 * - Validation: missing movie_ids → 400 (binding error).
 * - Validation: movie_ids present but no scrapers + no manual input → 400
 *   (use-case error "either selected_scrapers or manual_search_input
 *   must be provided").
 * - Bad path: rescrape on a non-existent job (with valid body) → 404.
 * - Happy path: rescrape on a completed job with a successful result →
 *   200, response.results includes the rescraped Movie with matching
 *   movie_id, succeeded count > 0.
 * - The response's `job` field carries the refreshed source-job state.
 */
import { test, expect, type APIRequestContext } from '@playwright/test';

import {
	BACKEND_BASE,
	loginAgainstRealBackend,
	submitScrape,
	waitForJobCompletion,
	DEFAULT_INPUT_DIR,
} from '../helpers';

test.describe('Bulk-rescrape: real batch-rescrape handler happy + bad paths', () => {
	test('POST /batch/:id/movies/batch-rescrape without movie_ids returns 400 (binding error)', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// The BindingError is from gin's ShouldBindJSON validator — the
		// `required` tag on MovieIDs rejects `{}` bodies. A regression that
		// removed the `binding:"required"` tag would let empty movie_ids
		// propagate to the use-case layer (which would happily no-op
		// instead of erroring). This test pins the at-bind-time rejection.
		await loginAgainstRealBackend(request);

		const resp = await request.post(
			`${BACKEND_BASE}/api/v1/batch/00000000-0000-0000-0000-000000000000/movies/batch-rescrape`,
			{ data: { selected_scrapers: ['e2emock'], force: false }, failOnStatusCode: false },
		);
		expect(resp.status(), 'missing movie_ids must return 400').toBe(400);
		const body = await resp.json();
		expect(body.error, 'binding error must mention MovieIDs').toMatch(/MovieIDs/);
	});

	test('POST /batch/:id/movies/batch-rescrape without scrapers AND without manual_search_input returns 400 (use-case validation)', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// The use-case rejects requests that provide neither scrapers nor a
		// manual_search_input — without at least one, there's nothing to
		// scrape against. This validation happens AFTER binding (the
		// request body has valid movie_ids) but BEFORE any job lookup.
		//
		// A regression that dropped this validation would proceed to
		// scrape every movie_id with NO scrapers registered — returning
		// every per-movie rescrape as "failed" silently + a misleading
		// "succeeded: 0, failed: N" response.
		await loginAgainstRealBackend(request);

		const resp = await request.post(
			`${BACKEND_BASE}/api/v1/batch/00000000-0000-0000-0000-000000000000/movies/batch-rescrape`,
			{ data: { movie_ids: ['GOOD-001'], force: false }, failOnStatusCode: false },
		);
		expect(resp.status(), 'use-case layer must reject missing scrapers + manual input').toBe(400);
		const body = await resp.json();
		expect(body.error, 'error must mention the missing-field rule').toContain('selected_scrapers');
		expect(body.error).toContain('manual_search_input');
	});

	test('POST /batch/:id/movies/batch-rescrape on a non-existent job (valid body) returns 404', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// After binding + use-case validation pass, the handler resolves
		// the job — an unknown job_id must 404 with the canonical "Job not
		// found" message. A regression that returned 200 with an empty
		// results array (silently treating unknown jobs as no-ops) would
		// surface here.
		await loginAgainstRealBackend(request);

		const resp = await request.post(
			`${BACKEND_BASE}/api/v1/batch/00000000-0000-0000-0000-000000000000/movies/batch-rescrape`,
			{
				data: { movie_ids: ['GOOD-001'], selected_scrapers: ['e2emock'], force: false },
				failOnStatusCode: false,
			},
		);
		expect(resp.status(), 'rescrape on unknown job must return 404').toBe(404);
		const body = await resp.json();
		expect(body.error, '404 must use canonical "Job not found" wording').toBe('Job not found');
	});

	test('POST /batch/:id/movies/batch-rescrape on a real completed job returns 200 + inline Movie + refreshed source job state', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// Happy path — exercises the real rescrape pipeline synchronously
		// and verifies the response shape the FRONTEND depends on:
		//   - body.results: every requested movie_id's freshly-rescraped
		//     Movie payload (so the frontend can update the per-card state
		//     without a follow-up GET).
		//   - body.succeeded + body.failed: counts the frontend uses to
		//     render a toast ("Rescraped 3 / 5 movies").
		//   - body.job: the refreshed source-job state, so the frontend
		//     can update the per-job card's status badge without an extra
		//     GET /batch/:id call.
		//
		// A regression that returned a new job_id (instead of running
		// synchronously) would surface here — body.results would be
		// missing + the frontend's bulk-rescrape UI would need to be
		// rewritten.
		await loginAgainstRealBackend(request);

		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		await waitForJobCompletion(request, job_id);

		const resp = await request.post(
			`${BACKEND_BASE}/api/v1/batch/${job_id}/movies/batch-rescrape`,
			{ data: { movie_ids: ['GOOD-001'], selected_scrapers: ['e2emock'], force: false } },
		);
		expect(resp.ok(), `bulk-rescrape must return 200, got ${resp.status()}`).toBeTruthy();
		const body = await resp.json();

		// results must include the requested movie_id with a real Movie
		// payload + success status.
		expect(body.results, 'response must include a results array').toBeInstanceOf(Array);
		expect(body.results.length, 'results array must have at least one entry').toBeGreaterThan(0);
		const good = body.results.find((r: { movie_id: string }) => r.movie_id === 'GOOD-001');
		expect(good, 'requested movie_id must appear in results').toBeTruthy();
		expect(good.status, 'rescrape result status must be success').toBe('success');
		expect(good.movie, 'rescrape result must include the inlined Movie payload').toBeTruthy();
		expect(good.movie.id, 'rescraped Movie.id must match the request').toBe('GOOD-001');
		expect(good.movie.poster_url, 'rescraped Movie must carry the e2emock poster_url').toBeTruthy();

		// succeeded + failed counts must reconcile with the results array.
		expect(body.succeeded, 'succeeded count must be at least 1').toBeGreaterThanOrEqual(1);
		expect(body.succeeded + body.failed, 'succeeded + failed must equal results.length').toBe(
			body.results.length,
		);

		// job field must carry the refreshed source-job state.
		expect(body.job, 'response must include the refreshed source job').toBeTruthy();
		expect(body.job.id, 'response job.id must match the source job_id').toBe(job_id);
		expect(body.job.status, 'refreshed job status must be completed').toBe('completed');
	});

	test('POST /batch/:id/movies/batch-rescrape with force=true returns 200 + bypasses cache', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// The force flag bypasses the per-MovieID cache + re-fetches from
		// the scraper — the "Rescrape with cache bypass" UX. Pin the
		// contract: force=true is accepted (not 400-rejected) + the
		// response is the same shape as the non-force happy path.
		await loginAgainstRealBackend(request);

		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		await waitForJobCompletion(request, job_id);

		const resp = await request.post(
			`${BACKEND_BASE}/api/v1/batch/${job_id}/movies/batch-rescrape`,
			{ data: { movie_ids: ['GOOD-001'], selected_scrapers: ['e2emock'], force: true } },
		);
		expect(
			resp.ok(),
			`force=true bulk-rescrape must return 200, got ${resp.status()}`,
		).toBeTruthy();
		const body = await resp.json();
		expect(body.results, 'force=true response must include results array').toBeInstanceOf(Array);
		expect(body.results.length, 'force=true response must have at least one entry').toBeGreaterThan(
			0,
		);
		expect(body.job.id, 'force=true response must carry the refreshed source job').toBe(job_id);
	});
});
