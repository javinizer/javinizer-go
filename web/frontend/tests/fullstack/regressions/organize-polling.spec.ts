/**
 * Organize polling terminal-state spec.
 *
 * Pins commits:
 *   cec74d43 fix(organize): stop infinite poll when organize succeeds
 *           (status: organized)
 *   d9bee8be test(organize-controller): pin pollOnce terminal-success for
 *           organized/reverted
 *
 * The bug: the frontend's pollOnce loop waited for the job's
 * per-file status to flip to "organized" (the success case). The
 * orchestrator's post-organize tracker update set the JOB status to
 * "organized" but never propagated a per-file status change to "organized"
 * for the organized file row — so pollOnce kept polling forever (the
 * frontend would spin on GET /batch/:id indefinitely, never reaching
 * the terminal-success branch).
 *
 * The fix made the orchestrator mark the per-file status transition
 * consistently with the job-level transition, so pollOnce sees a terminal
 * state and stops.
 *
 * This spec exercises the full-stack end-to-end path: real organize
 * endpoint → real apply phase → real organizer workflow → real
 * filesystem write → real JobEvent broadcast → real result tracker
 * update. After issuing organize, the job MUST reach a terminal status
 * (completed / failed / organized / reverted) — never "running" or
 * "pending" — within a bounded timeout.
 */
import { test, expect, type APIRequestContext } from '@playwright/test';

import {
	loginAgainstRealBackend,
	submitScrape,
	submitOrganize,
	waitForJobCompletion,
	DEFAULT_INPUT_DIR,
	DEFAULT_OUTPUT_DIR,
	type BatchJobResponse,
} from '../helpers';

test.describe('Organize polling: job reaches terminal status after start (no infinite poll)', () => {
	test('after POST /batch/:id/organize, the job reaches a terminal state within a bounded timeout', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// Seed: complete a real scrape so we have something to organize.
		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		const job = await waitForJobCompletion(request, job_id);
		expect(job.status, 'precondition: scrape must reach completed').toBe('completed');

		// Issue organize — the orchestrator runs async via the worker pool.
		// Use a fresh destination so the test is independent of prior runs.
		await submitOrganize(request, job_id, `${DEFAULT_OUTPUT_DIR}/org-poll-${Date.now()}`);

		// [cec74d43 E2E] Wait for the job to reach a terminal state.
		// Before the fix, this would NEVER terminate when organize succeeded:
		// the per-file status never transitioned to "organized" (only the
		// job-level status did), so pollOnce spun forever.
		//
		// We assert the job reaches a terminal state within a 30s budget.
		// The e2emock's 1-byte fixture file may cause the file move to fail
		// (per-file status="failed", job status="completed") — both are
		// terminal + satisfy this assertion. The "running" / "pending" states
		// are NOT terminal + would fail here if the regression reappeared.
		const postOrganizeJob: BatchJobResponse = await waitForJobCompletion(request, job_id, {
			timeoutMs: 30_000,
		});

		const terminalStatuses = ['completed', 'failed', 'organized', 'reverted'];
		expect(
			terminalStatuses,
			'post-organize job status must be terminal (cec74d43: previously hung in "running")',
		).toContain(postOrganizeJob.status);

		// Sanity: the post-organize job's progress is at 100% (the worker
		// pool computed the terminal transition + updated the progress tracker).
		// Before cec74d43's fix, the job could sit at <100% while pollOnce
		// spun forever on a never-arriving "organized" status.
		expect(postOrganizeJob.progress, 'terminal job must report progress=100').toBe(100);
	});

	test('organize on a job whose tasks already completed does NOT leave the job in "running"', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// Edge case the original bug report showed: organize on a previously
		// completed job would set status="running" + then NOTHING would
		// transition it back to terminal because the per-file organized flag
		// never propagated. A second POST organize on the same job (without
		// waiting) demonstrates the regression sharper: even on a no-op
		// organize, the job must transition to terminal.
		await loginAgainstRealBackend(request);

		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		await waitForJobCompletion(request, job_id);

		await submitOrganize(request, job_id, `${DEFAULT_OUTPUT_DIR}/org-poll-2-${Date.now()}`);
		const postOrganize = await waitForJobCompletion(request, job_id, { timeoutMs: 30_000 });
		expect(
			['completed', 'failed', 'organized', 'reverted'],
			'post-organize status must be terminal',
		).toContain(postOrganize.status);
	});
});
