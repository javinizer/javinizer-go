/**
 * Preview endpoint security + access spec.
 *
 * Real stack: browser → Vite proxy → real POST
 * /api/v1/batch/:jobId/results/:resultId/preview. The endpoint runs the
 * real security validator (isDirAllowed) against the destination path +
 * the API config's AllowedDirectories list (cmd/javinizer-e2e sets it to
 * JAVINIZER_E2E_INPUT_DIR + JAVINIZER_E2E_OUTPUT_DIR).
 *
 * Pins:
 * - Preview to a directory OUTSIDE AllowedDirectories returns 403 with the
 *   canonical "Access denied to requested directory" message — AND the
 *   error message MUST NOT include the actual path (security: don't leak
 *   directory paths in error messages).
 * - Preview to a directory INSIDE AllowedDirectories returns 200 + the
 *   full preview payload (folder_name, file_name, full_path, video_files...).
 */
import { test, expect, type APIRequestContext } from '@playwright/test';

import {
	BACKEND_BASE,
	loginAgainstRealBackend,
	submitScrape,
	submitPreview,
	waitForJobCompletion,
	soleResult,
	DEFAULT_INPUT_DIR,
	DEFAULT_OUTPUT_DIR,
} from '../helpers';

test.describe('Preview access: security validator gates preview by AllowedDirectories', () => {
	test('preview to a denied directory returns 403 + sanitized error message', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		const job = await waitForJobCompletion(request, job_id);
		const { result } = soleResult(job);

		// /etc is never in AllowedDirectories — security validator must reject.
		// failOnStatusCode:false so Playwright doesn't throw on the 403.
		const resp = await request.post(
			`${BACKEND_BASE}/api/v1/batch/${job_id}/results/${result.result_id}/preview`,
			{
				data: { destination: '/etc', operation_mode: 'organize' },
				failOnStatusCode: false,
			},
		);
		expect(resp.status(), 'preview to /etc must return 403, not 200 or 500').toBe(403);
		const body = await resp.json();
		expect(body.error, 'error message must be the canonical sanitized "Access denied" string').toBe(
			'Access denied to requested directory',
		);
		// Security: the denial message must NOT echo back the requested path —
		// a regression that includes the path would leak server-side directory
		// structure to a potential attacker probing the endpoint.
		expect(body.error, 'denial message must not leak the requested path').not.toContain('/etc');
	});

	test('preview to an allowed directory returns 200 + the full organize-preview payload', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		const job = await waitForJobCompletion(request, job_id);
		const { result } = soleResult(job);

		// /tmp/javinizer-e2e-output is in AllowedDirectories via
		// JAVINIZER_E2E_OUTPUT_DIR — preview must succeed + return the full
		// payload the frontend renders in the preview tree.
		const preview = await submitPreview(request, job_id, result.result_id, DEFAULT_OUTPUT_DIR);

		// Full payload contract — every field the frontend's preview tree
		// component reads must be present. A regression that drops a field
		// (the field-drop bug class) surfaces here.
		expect(preview.folder_name, 'preview.folder_name must be populated').toBeTruthy();
		expect(preview.file_name, 'preview.file_name must be populated').toBeTruthy();
		expect(preview.full_path, 'preview.full_path must be populated').toBeTruthy();
		expect(preview.poster_path, 'preview.poster_path must be populated').toBeTruthy();
		expect(preview.fanart_path, 'preview.fanart_path must be populated').toBeTruthy();
		expect(preview.nfo_path, 'preview.nfo_path must be populated').toBeTruthy();

		// The destination the caller requested must actually appear in
		// full_path — proves the organizer used the caller's destination
		// rather than a stale config-derived one.
		expect(
			preview.full_path,
			'preview.full_path must be rooted at the requested destination',
		).toContain(DEFAULT_OUTPUT_DIR);
	});

	test('preview to an allowed directory whose parent is denied but sibling is allowed behaves per allowed list', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// /tmp/javinizer-e2e-output is allowed; /tmp is NOT explicitly
		// listed (only the full /tmp/javinizer-e2e-output path is). The
		// security validator uses path-prefix containment, so a request for
		// /tmp/javinizer-e2e-output/sub must succeed (sub-of-allowed).
		await loginAgainstRealBackend(request);

		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		const job = await waitForJobCompletion(request, job_id);
		const { result } = soleResult(job);

		const subDestination = `${DEFAULT_OUTPUT_DIR}/subdir-${Date.now()}`;
		const preview = await submitPreview(request, job_id, result.result_id, subDestination);
		expect(
			preview.full_path,
			'preview.full_path must be rooted under the sub-allowed destination',
		).toContain(subDestination);
	});
});
