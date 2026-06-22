/**
 * Delete-job endpoint spec.
 *
 * Real stack: browser → Vite proxy → real DELETE /api/v1/batch/:id →
 * real JobStore removal.
 *
 * The DELETE endpoint removes a job from the in-memory JobStore + cleans
 * up its temp posters. The frontend uses this on the /jobs page's
 * "delete job" button — useful for clearing the list of stale jobs.
 *
 * Pins:
 * - Happy path: DELETE /batch/:id returns 200, subsequent GET /batch/:id
 *   returns 404 (the job is gone from the JobStore).
 * - Bad path: DELETE /batch/:id on a non-existent job returns 404.
 * - The list endpoint reflects the deletion (the deleted job no longer
 *   appears in GET /api/v1/batch).
 */
import { test, expect, type APIRequestContext } from '@playwright/test';

import {
	BACKEND_BASE,
	loginAgainstRealBackend,
	submitScrape,
	waitForJobCompletion,
	DEFAULT_INPUT_DIR,
} from '../helpers';

test.describe('Delete-job: real JobStore removal happy + bad paths', () => {
	test('DELETE /batch/:id on a real job returns 200 + subsequent GET returns 404', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// Seed a real job we can delete.
		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		await waitForJobCompletion(request, job_id);

		// Sanity: the job exists before we delete it.
		const before = await request.get(`${BACKEND_BASE}/api/v1/batch/${job_id}`);
		expect(before.ok(), 'precondition: GET before delete must 200').toBeTruthy();

		// Delete it.
		const del = await request.delete(`${BACKEND_BASE}/api/v1/batch/${job_id}`);
		expect(del.ok(), `DELETE must return 200, got ${del.status()}`).toBeTruthy();

		// After delete: GET /batch/:id must 404 — the JobStore no longer
		// has the job. A regression that only marked the job as deleted
		// but didn't remove it from the JobStore's lookup map would
		// surface here as a 200 (with the still-present job).
		const after = await request.get(`${BACKEND_BASE}/api/v1/batch/${job_id}`, {
			failOnStatusCode: false,
		});
		expect(after.status(), 'GET after delete must return 404').toBe(404);
		const body = await after.json();
		expect(body.error, '404 must use canonical "not found" wording').toMatch(/not found/i);
	});

	test('DELETE /batch/:id on a non-existent job returns 404', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		const fakeJobId = '00000000-0000-0000-0000-000000000000';
		const resp = await request.delete(`${BACKEND_BASE}/api/v1/batch/${fakeJobId}`, {
			failOnStatusCode: false,
		});
		expect(resp.status(), 'DELETE on unknown job must return 404').toBe(404);
	});

	test('after DELETE, the job no longer appears in GET /api/v1/batch', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// The /jobs list must reflect the deletion — the job's id must NOT
		// appear in any subsequent GET /api/v1/batch response. A regression
		// that removed the job from the by-id lookup but left it in the
		// list iteration would surface here.
		await loginAgainstRealBackend(request);

		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		await waitForJobCompletion(request, job_id);

		// Verify the job appears in the list before delete.
		const beforeList = await request.get(`${BACKEND_BASE}/api/v1/batch?limit=200`);
		const beforeBody = await beforeList.json();
		expect(
			beforeBody.jobs.some((j: { id: string }) => j.id === job_id),
			'precondition: deleted job must appear in list BEFORE delete',
		).toBe(true);

		// Delete + verify it's gone from the list.
		await request.delete(`${BACKEND_BASE}/api/v1/batch/${job_id}`);

		const afterList = await request.get(`${BACKEND_BASE}/api/v1/batch?limit=200`);
		const afterBody = await afterList.json();
		expect(
			afterBody.jobs.some((j: { id: string }) => j.id === job_id),
			'deleted job must NOT appear in the list after delete',
		).toBe(false);

		// NB: We intentionally do NOT assert on the global `total` decreasing —
		// other specs in concurrent worker runs can create jobs between the
		// before/after GET /batch calls, keeping the global total flat even
		// when our delete succeeded. The negative-existence assertion above is
		// the durable contract: the deleted job id must not appear in the
		// list. The global-total check is racy + redundant.
	});
});
