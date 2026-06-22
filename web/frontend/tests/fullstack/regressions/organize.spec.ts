/**
 * Organize (apply) endpoint spec.
 *
 * Real stack: browser → Vite proxy → real POST /api/v1/batch/:id/organize
 * → real apply phase → real organizer workflow → real filesystem write
 * to the destination directory (the Go binary's AllowedDirectories list
 * includes JAVINIZER_E2E_OUTPUT_DIR).
 *
 * Pins:
 * - The organize endpoint accepts the request + returns 200 with the
 *   canonical "Organization started" message — proving the route is
 *   registered + the worker-pool enqueue succeeded.
 * - The destination directory + the template-resolved subfolder are
 *   ACTUALLY created on disk by the real organizer workflow — proving
 *   the apply phase ran (not just enqueued).
 * - The job's stored destination field matches what the caller passed
 *   (the field-drop bug class: job.destination dropped on enqueue,
 *   leaving the frontend to fall back to the config default).
 *
 * Note: we do NOT assert per-file `status=organized` here because the
 * 1-byte fixture files don't satisfy the organizer's expected file-size
 * + sibling-group invariants; the file-move step may report "failed"
 * for fixture-content reasons unrelated to the apply pipeline. The
 * folder-creation assertion is the durable end-to-end signal.
 */
import { test, expect, type APIRequestContext } from '@playwright/test';
import { existsSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

import {
	BACKEND_BASE,
	loginAgainstRealBackend,
	submitScrape,
	submitOrganize,
	waitForJobCompletion,
	soleResult,
	DEFAULT_INPUT_DIR,
	DEFAULT_OUTPUT_DIR,
} from '../helpers';

test.describe('Organize: real apply phase creates destination + subfolder on disk', () => {
	test('POST /batch/:id/organize returns 200 + "Organization started" message', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		await waitForJobCompletion(request, job_id);

		// Issue organize + assert the canonical response. failOnStatusCode
		// is false so we can inspect non-2xx bodies for diagnostics.
		const resp = await request.post(`${BACKEND_BASE}/api/v1/batch/${job_id}/organize`, {
			data: {
				destination: `${DEFAULT_OUTPUT_DIR}/organize-api-${Date.now()}`,
				operation_mode: 'organize',
				copy_only: false,
			},
			failOnStatusCode: false,
		});
		expect(resp.status(), 'organize endpoint must return 200 on enqueue').toBe(200);
		const body = await resp.json();
		expect(
			body.message,
			'organize response must carry the canonical "Organization started" message',
		).toBe('Organization started');
	});

	test('organize creates the requested destination + the template-resolved subfolder on disk', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		await waitForJobCompletion(request, job_id);

		const destination = `${DEFAULT_OUTPUT_DIR}/organize-disk-${Date.now()}`;
		expect(existsSync(destination), 'precondition: destination must not pre-exist').toBeFalsy();

		// Drive the real apply phase — the organizer runs the FileFormat
		// template (`<ID>` per cmd/javinizer-e2e config) + creates
		// destination/<ID>/<ID>/ on disk.
		await submitOrganize(request, job_id, destination);

		// Wait for the organize job to reach a terminal status — the
		// apply phase runs async via the worker pool.
		await waitForJobCompletion(request, job_id, { timeoutMs: 30_000 });

		// The destination directory must exist + contain a subfolder named
		// after the movie ID (the FolderFormat template = "<ID>").
		expect(existsSync(destination), 'destination directory must exist after organize').toBeTruthy();
		const entries = readdirSync(destination);
		expect(entries, 'destination must contain a per-movie subfolder').toContain('GOOD-001');

		// The subfolder is a directory (not a stray file) — the organizer
		// uses MkdirAll, so this holds for the happy path.
		const subfolder = join(destination, 'GOOD-001');
		expect(
			statSync(subfolder).isDirectory(),
			'per-movie subfolder must be a directory',
		).toBeTruthy();
	});

	test('job.destination persists the caller-supplied destination through the organize enqueue', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// Bug class field-drop test: when the organize endpoint enqueues
		// the apply work, it must persist the caller-supplied destination
		// on the job row. A regression that overwrote destination with ""
		// (or with the config default) would leave the frontend rendering
		// the wrong destination in the organize-progress UI.
		await loginAgainstRealBackend(request);

		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		await waitForJobCompletion(request, job_id);

		const destination = `${DEFAULT_OUTPUT_DIR}/organize-dest-${Date.now()}`;
		await submitOrganize(request, job_id, destination);

		// Re-fetch the job + assert destination persisted.
		const resp = await request.get(`${BACKEND_BASE}/api/v1/batch/${job_id}?include_data=true`);
		expect(resp.ok()).toBeTruthy();
		const job = await resp.json();
		expect(job.destination, 'job.destination must match the caller-supplied value').toBe(
			destination,
		);
	});
});
