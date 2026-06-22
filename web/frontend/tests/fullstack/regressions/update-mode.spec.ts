/**
 * Update-mode scrape spec.
 *
 * Real stack: browser → Vite proxy → real POST /api/v1/batch/scrape with
 * `update=true` → real BatchScrapeRequest binding → real StartScrapeUseCase
 * → real worker pool execution.
 *
 * The "Update" flag tells the scrape pipeline to overwrite existing Movie
 * rows in the DB instead of skipping them when a Movie with the same ID
 * already exists. The frontend uses this on the "Rescrape with overwrite"
 * button — it's distinct from a plain rescrape (which skips already-persisted
 * Movies).
 *
 * Pins:
 * - Happy path: POST /batch/scrape with update=true returns 200 + a job_id
 *   (the request is accepted, not 400-rejected for the unknown flag).
 * - The job response carries `update=true` so the frontend's per-job
 *   metadata can surface "update mode" UI affordances.
 * - Update-mode scrape does NOT duplicate Movies in the DB — re-scraping
 *   the same MovieID with update=true results in one row, not two.
 */
import { test, expect, type APIRequestContext } from '@playwright/test';

import {
	BACKEND_BASE,
	loginAgainstRealBackend,
	submitScrape,
	waitForJobCompletion,
	DEFAULT_INPUT_DIR,
	type SubmitScrapeOptions,
	type BatchJobResponse,
} from '../helpers';

test.describe('Update-mode scrape: real pipeline accepts + propagates the Update flag', () => {
	// Helper: submit a scrape with the Update flag set.
	async function submitScrapeWithUpdate(
		request: APIRequestContext,
		files: string[],
	): Promise<string> {
		const opts: SubmitScrapeOptions = { files, update: true };
		return submitScrape(request, opts);
	}

	test('POST /batch/scrape with update=true returns 200 + job_id', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// The endpoint must accept the request — a binding regression that
		// rejected update=true (or treated it as unknown) would 400.
		const job_id = await submitScrapeWithUpdate(request, [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`]);
		expect(job_id, 'update-mode scrape must return a job_id').toBeTruthy();

		// The job must reach terminal status — the Update flag is a
		// scrape-mode toggle, NOT a different code path that could
		// bypass the worker pool + the apply phase.
		const job = await waitForJobCompletion(request, job_id);
		expect(['completed', 'failed'], 'update-mode job must reach terminal status').toContain(
			job.status,
		);
	});

	test('GET /batch/:id after update-mode scrape shows update=true on the job', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// The job response must carry `update=true` so the frontend can
		// show "Update mode" badge on the per-job card. A regression that
		// accepted the flag but didn't persist it (or didn't propagate it
		// to the BatchJobStatus tracker) would surface here.
		await loginAgainstRealBackend(request);

		const job_id = await submitScrapeWithUpdate(request, [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`]);
		const job = await waitForJobCompletion(request, job_id);
		expect(job.update, 'update-mode scrape job must carry update=true on its response').toBe(true);
	});

	test('re-scraping the same MovieID with update=true does not duplicate Movies in the DB', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// Update mode is specifically for modifying existing rows, not
		// creating duplicates. After two update-mode scrapes of the same
		// MovieID, GET /movies must return exactly ONE Movie with that ID
		// (not two rows). A regression that treat update=true as INSERT
		// instead of UPSERT would create duplicate Movie rows + corrupt
		// the /movies list pagination.
		await loginAgainstRealBackend(request);

		// First update-mode scrape.
		const job1 = await submitScrapeWithUpdate(request, [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`]);
		await waitForJobCompletion(request, job1);

		// Second update-mode scrape of the same MovieID.
		const job2 = await submitScrapeWithUpdate(request, [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`]);
		await waitForJobCompletion(request, job2);

		// DB must have exactly one GOOD-001 row.
		const resp = await request.get(`${BACKEND_BASE}/api/v1/movies?limit=100`);
		expect(resp.ok()).toBeTruthy();
		const body = await resp.json();
		const goodRows = body.movies.filter((m: { id: string }) => m.id === 'GOOD-001');
		expect(
			goodRows,
			'update-mode re-scrape must UPSERT, not duplicate — exactly 1 GOOD-001 row expected',
		).toHaveLength(1);
	});

	test('POST /batch/scrape with update=false (default) also works + carries update=false on the job', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// Companion: the default (non-update) scrape must still work + the
		// `update` field must be present on the response (false). A
		// regression that only set update=true when the flag was passed
		// but left it undefined otherwise would surface here as undefined.
		await loginAgainstRealBackend(request);

		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		const job: BatchJobResponse = await waitForJobCompletion(request, job_id);
		expect(job.update, 'default scrape must carry update=false, not undefined').toBe(false);
	});
});
