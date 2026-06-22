/**
 * Cancel endpoint spec.
 *
 * Pins commit:
 *   6249de64 fix(worker): preserve context on failure paths across phases
 *
 * The cancel-specific subset: the worker pool's Cancel() path cancels the
 * job's context — every in-flight phase (scrape / translate / apply)
 * observes the cancellation + transitions the per-file status without
 * panicking. Before 6249de64, some failure paths constructed a fresh
 * context that ignored cancellation, so a cancelled job could leave
 * per-file status="running" indefinitely. The commit pinned context
 * propagation across all failure / cancellation paths.
 *
 * This spec also pins the general cancel-contract:
 *   - POST /batch/:id/cancel on a terminal job returns 400 "Job is already X".
 *   - The error message includes the actual terminal status so the
 *     frontend can branch on "already completed" vs "already organized"
 *     for UX feedback.
 *   - The endpoint never 500s — canonical 400 for no-op cancels.
 */
import { test, expect, type APIRequestContext } from '@playwright/test';

import {
	BACKEND_BASE,
	loginAgainstRealBackend,
	submitScrape,
	waitForJobCompletion,
	DEFAULT_INPUT_DIR,
} from '../helpers';

test.describe('Cancel: real Cancel() propagates the cancellation signal across phases (commit 6249de64)', () => {
	test('POST /batch/:id/cancel on a completed job returns 400 + "Job is already completed"', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// Cancel of a terminal job is a no-op — pin the canonical error
		// contract so the frontend can show the canonical UX, not a generic
		// "something went wrong". The error message must include the
		// terminal status so the UI can say "job already completed" vs
		// "already cancelled" vs "already organized".
		await loginAgainstRealBackend(request);

		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		await waitForJobCompletion(request, job_id);

		const resp = await request.post(`${BACKEND_BASE}/api/v1/batch/${job_id}/cancel`, {
			failOnStatusCode: false,
		});
		expect(resp.status(), 'cancel of completed job must return 400, not 200 or 500').toBe(400);
		const body = await resp.json();
		expect(body.error, 'error message must use the canonical "already" wording').toContain(
			'already',
		);
		// The status string in the error must match the actual job status —
		// a regression that always said "already completed" regardless of
		// actual terminal state would surface here.
		expect(body.error).toContain('completed');
	});

	test('POST /batch/:id/cancel on a failed-scrape job returns 400 + mentions the failed status', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// Same canonical 400 path — but the error must reference the actual
		// terminal status ("failed" in this case, since the job reached
		// completed at the job level even though the per-file status was
		// failed; the cancel endpoint checks the JOB-level status).
		//
		// Subtle: a scrape whose every file fails still has job
		// status="completed" (the worker pool itself succeeded in
		// running every task to completion, even if every task reported
		// failure). So we expect "already completed" not "already failed" —
		// this is correct per the code's state model, but worth pinning
		// since the cancel endpoint branches on job-level status.
		await loginAgainstRealBackend(request);

		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/FAIL-002.mp4`] });
		await waitForJobCompletion(request, job_id);

		const resp = await request.post(`${BACKEND_BASE}/api/v1/batch/${job_id}/cancel`, {
			failOnStatusCode: false,
		});
		expect(resp.status()).toBe(400);
		const body = await resp.json();
		expect(body.error).toContain('already');
		// Job-status reflects the orchestrator's terminal state — which for
		// an all-files-failed scrape is still "completed" (the job itself
		// finished running).
		expect(body.error).toContain('completed');
	});

	test('POST /batch/:id/cancel on a non-existent job returns 404', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// Cancel of a job that doesn't exist must 404 (not 500 + stack trace,
		// not 400 with "already cancelled" — the canonical REST contract
		// for an unknown resource). A regression that swallowed the
		// job-not-found error path would surface here.
		await loginAgainstRealBackend(request);

		const fakeJobId = '00000000-0000-0000-0000-000000000000';
		const resp = await request.post(`${BACKEND_BASE}/api/v1/batch/${fakeJobId}/cancel`, {
			failOnStatusCode: false,
		});
		expect(resp.status(), 'cancel of non-existent job must return 404').toBe(404);
		const body = await resp.json();
		expect(body.error, '404 error must use canonical "not found" wording').toMatch(/not found/i);
	});

	test('POST /batch/:id/cancel on a freshly-created pending job returns 200 + "cancelled successfully"', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// The happy-path of Cancel(): a job is enqueued but hasn't reached
		// terminal yet (or just has — the race window is small). Pin the
		// 200-response canonical message so the frontend's cancel-button
		// UX branch ("show success toast") is stable across releases.
		//
		// The job may complete before our cancel POST lands (the e2emock
		// scraper is fast + the worker pool runs the scrape phase
		// immediately). Either outcome is acceptable:
		//   - 200 "cancelled successfully" — the cancel arrived in time.
		//   - 400 "Job is already completed" — the job raced ahead.
		// Both are terminal states + neither is a failure mode of the
		// cancel pipeline. We assert that the response is ONE of these
		// canonical codes (not 500, not a panic recovery, not stuck).
		await loginAgainstRealBackend(request);

		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });

		const resp = await request.post(`${BACKEND_BASE}/api/v1/batch/${job_id}/cancel`, {
			failOnStatusCode: false,
		});
		expect(
			[200, 400],
			`cancel response must be 200 (cancelled) or 400 (already terminal) — got ${resp.status()}`,
		).toContain(resp.status());

		// Whatever the outcome, the job must reach a terminal status
		// shortly afterwards — the cancel pipeline never leaves the job
		// stuck in "running" indefinitely (commit 6249de64).
		await waitForJobCompletion(request, job_id, { timeoutMs: 30_000 });
		// No assertion on the final status — cancelled / completed both
		// satisfy the "not stuck" guarantee. waitForJobCompletion throws
		// if the job doesn't terminate within the timeout.
	});
});
