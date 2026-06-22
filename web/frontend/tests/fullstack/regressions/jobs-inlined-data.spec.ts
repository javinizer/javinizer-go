/**
 * /jobs list inlined-data spec.
 *
 * Pins commit:
 *   44a84e34 fix(batch): inline job results in list response to restore
 *           /jobs thumbnails
 *
 * The bug: the /jobs list endpoint (GET /api/v1/batch) was returning every
 * job WITHOUT its inlined result data. The frontend's per-job card renders
 * a thumbnail from the first result's Movie.poster_url — without inlined
 * data, the card rendered a blank thumbnail + blank metadata for every
 * job. The fix built the full BatchJobResponse (with results) for each
 * list entry.
 *
 * This spec exercises the full-stack end-to-end path: scrape a real file →
 * wait for completion (real Movie populated on the result tracker) →
 * GET /api/v1/batch → assert the returned job carries an inlined
 * results.*.movie.poster_url so the frontend's /jobs card can render the
 * thumbnail.
 *
 * A future regression that drops the inlined movie payload from the list
 * response (e.g. an adapter that returns BatchJobResponseSlim by
 * accident) would surface here as a missing `results[fieldPath].movie`
 * field on the just-scraped job.
 */
import { test, expect, type APIRequestContext } from '@playwright/test';

import {
	BACKEND_BASE,
	loginAgainstRealBackend,
	submitScrape,
	waitForJobCompletion,
	DEFAULT_INPUT_DIR,
	type BatchJobResponse,
} from '../helpers';

test.describe('/jobs list: each job entry inlines its results + movie thumbnail data (commit 44a84e34)', () => {
	test('GET /api/v1/batch after a real scrape returns the job with inlined results + movie.poster_url', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// Seed: real scrape produces a Movie whose poster_url is the
		// canonical "e2e.invalid/poster-..." value the e2emock returns.
		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		await waitForJobCompletion(request, job_id);

		// Fetch the list — the e2emock's deterministic data makes the
		// just-scraped job findable by id.
		const resp = await request.get(`${BACKEND_BASE}/api/v1/batch?limit=50`);
		expect(resp.ok()).toBeTruthy();
		const list = await resp.json();
		expect(list.jobs, 'list response must include a jobs array').toBeInstanceOf(Array);
		expect(list.total, 'list response must include a total count').toBeGreaterThan(0);

		const found = list.jobs.find((j: BatchJobResponse) => j.id === job_id);
		expect(found, 'just-scraped job must appear in the list').toBeTruthy();

		// [44a84e34 E2E] The list entry MUST carry inlined results so the
		// frontend's per-job card can render the thumbnail — that's the
		// fix's whole point. Before the fix, results was an empty map /
		// missing field for every list entry.
		expect(found.results, 'list entry must inline the results map').toBeTruthy();
		expect(
			Object.keys(found.results).length,
			'list entry results map must be non-empty for a completed job',
		).toBeGreaterThan(0);

		// The inlined Movie must carry the poster_url so the frontend
		// thumbnail renders — the user-visible signal for this bug class.
		const firstResult = Object.values(found.results)[0] as { movie?: { poster_url?: string } };
		expect(firstResult.movie, 'inlined result must carry the Movie payload').toBeTruthy();
		expect(
			firstResult.movie!.poster_url,
			'inlined Movie.poster_url must be populated',
		).toBeTruthy();
		expect(firstResult.movie!.poster_url).toContain('GOOD-001');
	});

	test('GET /api/v1/batch list paginates via limit + offset', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// Pagination contracts: limit controls page size, offset skips.
		// A regression that ignored `offset` would make second-page
		// queries return the same first-page jobs — pinning here so the
		// frontend's job-list pagination UI stays correct end-to-end.
		//
		// We assert against the IDs of the two jobs THIS test created
		// (not global position) so concurrent tests in other files can't
		// shift the global newest-first ordering out from under us. With
		// workers=1 + the backend's `started_at DESC, id DESC` tiebreaker,
		// pagination over our own two jobs is fully deterministic.
		await loginAgainstRealBackend(request);

		// Create exactly 2 jobs + capture their IDs. A tight creation loop +
		// the id DESC tiebreaker guarantee page1 = the second-created +
		// page2 = the first-created (newest-first).
		const createdIds: string[] = [];
		for (let i = 0; i < 2; i++) {
			const id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
			await waitForJobCompletion(request, id);
			createdIds.push(id);
		}
		const newestId = createdIds[1];
		const olderId = createdIds[0];

		// Page 1: 1 job per page
		const page1Resp = await request.get(`${BACKEND_BASE}/api/v1/batch?limit=1&offset=0`);
		expect(page1Resp.ok()).toBeTruthy();
		const page1 = await page1Resp.json();
		expect(page1.jobs, 'page 1 must return 1 job').toHaveLength(1);
		expect(page1.total, 'page 1 must report total >= 2').toBeGreaterThanOrEqual(2);
		expect(page1.jobs[0].id, 'page 1 must be the newest of our two jobs').toBe(newestId);

		// Page 2: same `limit=1` but `offset=1` — must return the other
		// of our two jobs (offset honored). Asserts against our own IDs,
		// so concurrent test runs can't break it.
		const page2Resp = await request.get(`${BACKEND_BASE}/api/v1/batch?limit=1&offset=1`);
		expect(page2Resp.ok()).toBeTruthy();
		const page2 = await page2Resp.json();
		expect(page2.jobs, 'page 2 must return 1 job').toHaveLength(1);
		expect(
			page2.jobs[0].id,
			'page 2 must be a DIFFERENT job than page 1 (offset honored)',
		).not.toBe(page1.jobs[0].id);
		expect(page2.jobs[0].id, 'page 2 must be the older of our two jobs').toBe(olderId);
	});

	test('GET /api/v1/batch/:id without include_data=true returns slim response without inlined Movie payloads', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// Companion to the inlined-data test: the slim (default) endpoint
		// returns `results` but each FileResult OMITITS the heavy `movie`
		// payload. The 44a84e34 fix only added inline Movie to the FULL
		// response shape — the slim shape deliberately skips it so /jobs
		// list queries don't pay the cost of serializing every result's
		// movie payload.
		//
		// A regression that always returned the heavy `movie` payload on
		// the slim path would balloon list responses proportionally to
		// total_files + tank the /jobs page's first paint.
		await loginAgainstRealBackend(request);

		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		const completed = await waitForJobCompletion(request, job_id);

		// Full — with include_data=true: results values carry the `movie` field
		expect(
			Object.keys(completed.results).length,
			'include_data=true must populate results',
		).toBeGreaterThan(0);
		const fullResult = Object.values(completed.results)[0];
		expect(fullResult.movie, 'full result must carry the inlined Movie payload').toBeTruthy();
		expect(fullResult.movie!.poster_url, 'full result Movie must carry poster_url').toBeTruthy();

		// Slim — no include_data: results values OMIT the `movie` field
		const slim = await request.get(`${BACKEND_BASE}/api/v1/batch/${job_id}`);
		expect(slim.ok()).toBeTruthy();
		const slimBody = await slim.json();
		expect(
			Object.keys(slimBody.results ?? {}).length,
			'slim endpoint must populate results',
		).toBeGreaterThan(0);
		const slimResult = Object.values(slimBody.results)[0] as { movie?: unknown };
		expect(
			slimResult.movie,
			'slim result must NOT carry the inlined Movie payload',
		).toBeUndefined();
	});
});
