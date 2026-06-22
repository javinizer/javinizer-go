/**
 * Job operations recording spec (regression guard).
 *
 * Pins the fix for the regression where completed/organized jobs rendered
 * "No operations recorded for this job" in the job-detail UI even though
 * organize had run to a terminal state.
 *
 * Root cause: `internal/workflow/revert_log.go::NewRevertLogFromConfig`
 * returned `noOpRevertLog` (writes nothing) whenever
 * `config.Output.Operation.AllowRevert == false`. Because AllowRevert
 * defaults to false, NO `BatchFileOperation` records were ever written in
 * the default configuration. But the operations list
 * (`GET /api/v1/jobs/:id/operations` → `BatchFileOpRepo.FindByBatchJobID`)
 * reads exactly those records, so the list was always empty for the
 * default-config user.
 *
 * The fix decoupled recording from AllowRevert: records are always written
 * when an apply phase runs (when a repository is available), and AllowRevert
 * continues to gate only the revert *action* (enforced separately by the
 * revert/revert-check HTTP handlers, which return 403 when disabled).
 *
 * This spec runs against the real `cmd/javinizer-e2e` backend, whose config
 * is `config.DefaultConfig(nil, nil)` — i.e. AllowRevert=false, the exact
 * configuration that triggered the regression. After a real scrape+organize
 * cycle, the operations endpoint MUST return a non-empty list — proving
 * records were persisted even though the user has not opted in to revert.
 *
 * Full stack: browser → Vite proxy → real Gin API → real worker pool →
 * real workflow.Apply orchestrator → real dbRevertLog.Begin/Complete →
 * real in-memory SQLite → real BatchFileOperation row → real
 * `GET /api/v1/jobs/:id/operations` query.
 */
import { test, expect, type APIRequestContext } from '@playwright/test';

import {
	loginAgainstRealBackend,
	submitScrape,
	submitOrganize,
	waitForJobCompletion,
	fetchJobOperations,
	DEFAULT_INPUT_DIR,
	DEFAULT_OUTPUT_DIR,
} from '../helpers';

test.describe('Job operations recording: organize persist BatchFileOperation records even when AllowRevert=false', () => {
	test('after organize reaches a terminal state, GET /jobs/:id/operations returns a non-empty list', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// Seed: complete a real scrape so the apply phase has a movie to organize.
		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		await waitForJobCompletion(request, job_id);

		// Drive the real apply phase. The e2emock's 1-byte fixture file may
		// cause the per-file file-move step to fail, but Begin runs BEFORE
		// organize mutates the filesystem — so a BatchFileOperation record is
		// always created for each apply Execute call regardless of the
		// file-move outcome.
		await submitOrganize(request, job_id, `${DEFAULT_OUTPUT_DIR}/ops-recorded-${Date.now()}`);

		// Wait for the organize job to reach a terminal state — the records
		// are written synchronously through the apply orchestrator, so by
		// the time the job is terminal they must be queryable.
		await waitForJobCompletion(request, job_id, { timeoutMs: 30_000 });

		// The regression: with AllowRevert=false (the e2e default), the
		// RevertLog seam returned noOpRevertLog, so no rows were ever
		// written + this endpoint returned operations: []. The fix makes
		// recording unconditional, so this assertion now passes.
		const ops = await fetchJobOperations(request, job_id);
		expect(
			ops.operations.length,
			'operations list must be non-empty for an organized job (regression: was 0 when AllowRevert=false)',
		).toBeGreaterThan(0);
		expect(ops.total, 'total must match operations.length').toBe(ops.operations.length);
		expect(ops.job_id, 'operations envelope must echo the requested job_id').toBe(job_id);

		// The recorded operation must reference the movie we scraped + the
		// source fixture path. This pins that Begin captured the canonical
		// pre-mutation state (movie ID + original path) — the fields the
		// job-detail UI renders as a file row.
		const op = ops.operations.find((o) => o.movie_id === 'GOOD-001');
		expect(op, 'must have a recorded operation for the scraped movie GOOD-001').toBeTruthy();
		expect(op!.original_path, 'operation.original_path must point at the source fixture').toContain(
			'GOOD-001.mp4',
		);
	});

	test('operations are recorded even when the per-file file-move fails (Begin runs before organize)', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// The 1-byte e2e fixture files do not satisfy the organizer's
		// file-size / sibling-group invariants, so the file-move step
		// frequently reports per-file failure. This is orthogonal to the
		// regression: Begin is called at the top of the apply orchestrator's
		// Execute, BEFORE the organize step mutates the filesystem. So even
		// when the per-file status ends as "failed", a BatchFileOperation
		// record MUST exist. This spec pins that invariant so a future
		// refactor that moves Begin after organize (or skips it on failure)
		// does not silently empty the operations list again.
		await loginAgainstRealBackend(request);

		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		await waitForJobCompletion(request, job_id);

		await submitOrganize(request, job_id, `${DEFAULT_OUTPUT_DIR}/ops-fail-${Date.now()}`);
		const postOrganize = await waitForJobCompletion(request, job_id, { timeoutMs: 30_000 });

		// Whatever the per-file outcome was, the operations list must be
		// non-empty — Begin does not depend on organize success.
		const ops = await fetchJobOperations(request, job_id);
		expect(
			ops.operations.length,
			'operations list must be non-empty regardless of per-file move outcome',
		).toBeGreaterThan(0);

		// The job_status field in the operations envelope must reflect the
		// real post-organize job state — pinning that listOperations reads
		// the live job row rather than a stale snapshot.
		const terminalStatuses = ['completed', 'failed', 'organized', 'reverted'];
		expect(
			terminalStatuses,
			'operations envelope job_status must be the real terminal job status',
		).toContain(postOrganize.status);
		expect(ops.job_status, 'operations envelope job_status must match the polled job status').toBe(
			postOrganize.status,
		);
	});
});
