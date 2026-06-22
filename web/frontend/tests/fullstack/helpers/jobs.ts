/**
 * Read-side helpers: wait for the real job to reach a terminal state and
 * extract structured results.
 *
 * Separated from ./api.ts (the write-side POST helpers) so specs that only
 * need to poll job state don't pull in the submit-side bundle — keeps specs
 * that lean on data created by global-setup free of scrape/preview/organize
 * imports.
 */
import type { APIRequestContext } from '@playwright/test';
import { expect } from '@playwright/test';
import { BACKEND_BASE } from './api';
import {
	type BatchJobResponse,
	type FileResult,
	type JobStatus,
	type WaitForJobOptions,
} from './types';

/**
 * Terminal job statuses. The full list mirrors `internal/worker` job-state
 * transitions — anything outside this set is considered in-flight.
 */
export const TERMINAL_JOB_STATUSES: readonly JobStatus[] = [
	'completed',
	'failed',
	'cancelled',
	'organized',
	'reverted',
];

/**
 * Poll `GET /api/v1/batch/:id?include_data=true` until the job reaches a
 * terminal status, then return the full job response (including every
 * FileResult's Movie payload).
 *
 * `expectStatus` option throws if the terminal status doesn't match —
 * useful for specs that specifically drive the success or failure path.
 */
export async function waitForJobCompletion(
	api: APIRequestContext,
	jobId: string,
	opts: WaitForJobOptions = {},
): Promise<BatchJobResponse> {
	const timeoutMs = opts.timeoutMs ?? 30_000;
	const expected = opts.expectStatus;
	const deadline = Date.now() + timeoutMs;
	let lastStatus: JobStatus | string = 'pending';

	while (Date.now() < deadline) {
		const resp = await api.get(`${BACKEND_BASE}/api/v1/batch/${jobId}?include_data=true`);
		expect(resp.ok(), `GET /batch/${jobId} failed: ${resp.status()}`).toBeTruthy();
		const job = (await resp.json()) as BatchJobResponse;
		lastStatus = job.status;
		if (TERMINAL_JOB_STATUSES.includes(job.status)) {
			if (expected && job.status !== expected) {
				throw new Error(
					`expected job status ${expected}, got ${job.status} — job: ${JSON.stringify(job, null, 2)}`,
				);
			}
			return job;
		}
		await new Promise((resolve) => setTimeout(resolve, 500));
	}
	throw new Error(
		`job ${jobId} did not reach terminal status within ${timeoutMs}ms (last status: ${lastStatus})`,
	);
}

/**
 * Poll the single FileResult of a single-file job until its per-file `status`
 * reaches `expectedStatus` (or the job itself goes terminal in a non-`expected`
 * state, in which case the actual per-file status is returned).
 *
 * Use after `waitForJobCompletion` when a spec asserts on per-file result fields
 * (status, error, ended_at) — there's a known race where the job-level status
 * flips terminal fractionally before the per-file tracker row is updated, so
 * reading the result immediately can observe a stale pre-terminal per-file
 * status. This helper refetches `GET /batch/:id?include_data=true` until the
 * per-file row settles, eliminating the race.
 */
export async function waitForFileResultStatus(
	api: APIRequestContext,
	jobId: string,
	expectedStatus: string,
	opts: WaitForJobOptions = {},
): Promise<FileResult> {
	const timeoutMs = opts.timeoutMs ?? 30_000;
	const deadline = Date.now() + timeoutMs;
	let lastResult: FileResult | undefined;
	while (Date.now() < deadline) {
		const resp = await api.get(`${BACKEND_BASE}/api/v1/batch/${jobId}?include_data=true`);
		expect(resp.ok(), `GET /batch/${jobId} failed: ${resp.status()}`).toBeTruthy();
		const job = (await resp.json()) as BatchJobResponse;
		try {
			lastResult = soleResult(job).result;
		} catch {
			throw new Error(`job ${jobId} has no file results to poll`);
		}
		if (lastResult.status === expectedStatus) {
			return lastResult;
		}
		// Bail early if the job has gone terminal but the per-file row is not
		// the expected status — that's a real failure, not settling lag.
		if (TERMINAL_JOB_STATUSES.includes(job.status) && lastResult.status !== expectedStatus) {
			return lastResult;
		}
		await new Promise((resolve) => setTimeout(resolve, 200));
	}
	throw new Error(`
		job ${jobId} per-file result did not reach status ${expectedStatus} within ${timeoutMs}ms
		(last per-file status: ${lastResult?.status ?? 'unknown'})
	`);
}

/**
 * Throw if the job has anything other than exactly one FileResult. Returns
 * the { filePath, result } pair on success.
 *
 * For specs that submit multiple files but want a structured single-result
 * helper, write the iteration inline — this helper's contract is the
 * single-input-file invariant.
 */
export function soleResult(job: BatchJobResponse): { filePath: string; result: FileResult } {
	const entries = Object.entries(job.results);
	if (entries.length !== 1) {
		throw new Error(
			`expected exactly 1 result, got ${entries.length}: ${JSON.stringify(job.results, null, 2)}`,
		);
	}
	const [filePath, result] = entries[0];
	return { filePath, result };
}
