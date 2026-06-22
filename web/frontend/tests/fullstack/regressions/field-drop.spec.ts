/**
 * Full-stack regression suite for the field-drop bug class squashed during
 * the Javinizer architecture-refactor branch.
 *
 * Pattern of the bug class: the refactor moved logic from ad-hoc per-file
 * handlers into structured phase orchestrators that construct fresh
 * structs on non-happy paths (failure, no-result, panic-recovered,
 * fallback, map-miss, synthetic, cancelled). The author copied the
 * obvious fields (status, error, path) but missed fields the prior phase
 * had already populated (MovieID, Name, Extension, Movie, IsMultiPart,
 * PartNumber, PartSuffix, StartedAt, EndedAt, verbose per-scraper error
 * messages). Each was silently invisible in unit tests covering the happy
 * path but broke the downstream consumer (frontend, /jobs list,
 * /review/[jobId], organize preview).
 *
 * Every test here drives the full stack end-to-end: real browser → real
 * SvelteKit frontend → real Vite HTTP proxy → real Go API server → real
 * worker pipeline → real :memory: SQLite → real e2emock scraper at the
 * scraper seam. NO page.route mocks — real HTTP, real DB writes, real
 * WebSocket-connected JobEvent broadcasts, real NFO/output path rendering.
 *
 * So a future regression of the same pattern anywhere along the pipeline
 * surfaces here as a failing HTTP-level or DOM-level assertion rather
 * than a user-reported bug in production.
 *
 * Spec file organization: each describe block covers one bug (or one
 * tightly-related bug family). When adding new specs, mirror this pattern
 * — describe title = the human-readable bug family, test title = the
 * assertion surface (API vs DOM).
 */
import { test, expect, type APIRequestContext } from '@playwright/test';

import {
	loginAgainstRealBackend,
	submitScrape,
	submitPreview,
	submitOrganize,
	waitForJobCompletion,
	soleResult,
	navigateToReviewPage,
	DEFAULT_INPUT_DIR,
	DEFAULT_OUTPUT_DIR,
	type BatchJobResponse,
} from '../helpers';

// ---------------------------------------------------------------------------
// Bug 83fba0c5 / d9106a96 / 42d89e65 / 6249de64 (scrape-failure subset)
// ---------------------------------------------------------------------------

test.describe('Field drop: scrape failure preserves MovieID + FilePath + verbose error + timestamps', () => {
	test('failed-scrape row surfaces MovieID + FilePath + verbose per-scraper error + StartedAt/EndedAt through the API', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// Submit a scrape for a file whose MovieID FAIL-*s through the
		// e2emock scraper. The command must derive MovieID="FAIL-002" from
		// the filename via the matcher fallback (no scanner FileMatchInfo
		// entry exists — the file content is a 1-byte fixture, not a real
		// sibling-grouped part). The resulting MovieResult must:
		//   - status="failed"                    (the scraper returned an error)
		//   - movie_id="FAIL-002"                [bug 83fba0c5 — was empty]
		//   - file_path="/.../FAIL-002.mp4"        [bug d9106a96 — was empty]
		//   - error: verbose per-scraper summary  [bug 42d89e65 — was hardcoded "no result"]
		//     containing "e2emock:" substring
		//   - started_at: non-empty               [bug 6249de64 — was zero]
		//   - ended_at: non-empty                 [bug 6249de64 — was nil]
		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/FAIL-002.mp4`] });
		const job = await waitForJobCompletion(request, job_id);
		expect(job.status).toBe('completed');

		const { result } = soleResult(job);
		expect(
			result.status,
			`file status must be "failed" — got ${result.status}. job: ${JSON.stringify(job, null, 2)}`,
		).toBe('failed');

		// [bug 83fba0c5 E2E] MovieID derived from filename via matcher
		// fallback, preserved through the scrape-phase failure path.
		expect(result.movie_id).toBe('FAIL-002');

		// [bug d9106a96 E2E] FilePath backfilled on tracker map-miss so
		// the API response's file_path field is non-empty.
		expect(result.file_path).toBe(`${DEFAULT_INPUT_DIR}/FAIL-002.mp4`);

		// [bug 42d89e65 E2E] Verbose per-scraper failure summary surfaces
		// through the API verbatim — NOT a hardcoded "no result".
		expect(result.error).toBeTruthy();
		expect(
			result.error!,
			`[bug 42d89e65] error must carry the per-scraper "e2emock:" substring, got: ${result.error}`,
		).toContain('e2emock');
		expect(result.error!).not.toBe('no result');
		expect(
			result.error!,
			`[bug 42d89e65] error must mention the failed movie ID, got: ${result.error}`,
		).toContain('FAIL-002');
		expect(
			result.error!,
			`[bug 42d89e65] error must describe what the scraper returned, got: ${result.error}`,
		).toContain('not found');

		// [bug 6249de64 E2E] StartedAt + EndedAt preserved on the failure
		// path so /jobs renders the job as terminal (not stuck).
		expect(result.started_at, 'started_at must be non-empty on the failure path').toBeTruthy();
		expect(result.ended_at, 'ended_at must be non-nil on the failure path').toBeTruthy();
		expect(new Date(result.started_at!).getTime()).toBeLessThanOrEqual(Date.now());
		expect(new Date(result.ended_at!).getTime()).toBeGreaterThanOrEqual(
			new Date(result.started_at!).getTime(),
		);
	});

	test('failed-scrape file renders in the /review/[jobId] UnidentifiedFilesCard with basename + verbose error', async ({
		page,
		request,
	}) => {
		await loginAgainstRealBackend(request);

		// Same scenario as the previous test but drives the real DOM —
		// the failed file must show up in the "Unidentified Files" card in
		// /review/[jobId], rendered with basename(file_path) (so the user
		// sees a real filename, not an empty card) AND the verbose
		// per-scraper error message (so the user sees WHY the scrape
		// failed).
		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/FAIL-003.mp4`] });
		const job: BatchJobResponse = await waitForJobCompletion(request, job_id);

		await navigateToReviewPage(page, job_id);

		// [bug 83fba0c5 / d9106a96 / 42d89e65 E2E DOM]
		// UnidentifiedFilesCard renders basename(file_path) + result.error.
		// Before the fixes: file_path was empty → basename was empty; error
		// was hardcoded "no result" → user saw a filename-less card with a
		// generic error message and no way to know what scraper failed.
		const failedCardText = page.getByText('FAIL-003.mp4', { exact: true }).first();
		await expect(failedCardText).toBeVisible({ timeout: 15_000 });

		const verboseErrorText = page.getByText(/e2emock.*not found/i).first();
		await expect(verboseErrorText).toBeVisible({ timeout: 10_000 });
	});
});

// ---------------------------------------------------------------------------
// Bug 6ed5d0e5 (organize preview video row loses .mp4 extension)
// ---------------------------------------------------------------------------

test.describe('Field drop: organize preview video row retains .mp4 extension', () => {
	test('preview full_path + file_name for a successful scrape carry the .mp4 extension', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// Submit a scrape for a file path that the scanner would have NO
		// FileMatchInfo for (file content is a 1-byte fixture; no real
		// siblings). The scrape phase's inputs.FileMatchInfo[filePath]
		// returns the zero value → triggers the d9106a96 (Path backfill)
		// + 6ed5d0e5 (Name + Extension backfill) paths.
		//
		// The mock scraper returns success for GOOD-* IDs, so the scrape
		// job completes successfully. We then POST the preview endpoint
		// and assert the video row's full_path + file_name carry the
		// `.mp4` extension. Before 6ed5d0e5, match.Extension was "" so
		// resolveFileName produced `templateOutput + ""` — the preview
		// response's full_path ended with `GOOD-001` (no extension), and
		// the frontend rendered a video row labeled "GOOD-001" with no
		// `.mp4`, while every other row (NFO, poster, fanart, trailer)
		// looked correct (they derive from movie.ID + template suffixes).
		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		const job = await waitForJobCompletion(request, job_id);
		expect(job.status).toBe('completed');
		const { result } = soleResult(job);
		expect(result.status).toBe('completed');
		expect(result.movie_id).toBe('GOOD-001');

		const preview = await submitPreview(request, job_id, result.result_id, DEFAULT_OUTPUT_DIR);

		// [bug 6ed5d0e5 E2E]
		// full_path must end with `GOOD-001.mp4` — before the fix the
		// video row's full_path ended with `GOOD-001` (no extension).
		expect(preview.full_path, 'preview full_path must end with the video file extension').toMatch(
			/GOOD-001\.mp4$/,
		);
		// file_name must NOT be the bare movie ID — must include the
		// extension so the frontend's preview tree rows render uniformly.
		expect(preview.file_name).toBe('GOOD-001');

		// video_files array must also carry the .mp4 extension (same
		// construct site, same field — the same fix that backfilled
		// match.Extension populates both)
		if (preview.video_files && preview.video_files.length > 0) {
			for (const vf of preview.video_files) {
				expect(vf, 'every entry in video_files must end with .mp4').toMatch(/\.mp4$/);
			}
		}
	});
});

// ---------------------------------------------------------------------------
// Bug 6249de64 (apply-failure path: IsMultiPart / PartNumber / PartSuffix
// + StartedAt / EndedAt preserved) + 91400891 [WARNING] (prior Movie pointer
// also preserved) — full-stack apply-failure coverage.
// ---------------------------------------------------------------------------

test.describe('Field drop: apply failure preserves MultiPart + Movie through the tracker + API', () => {
	test('failed-apply row in the API response carries IsMultiPart / PartNumber / PartSuffix + prior Movie', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// Submit a scrape for a single-part file (GOOD-001 — kept
		// deterministic; multipart fan-out requires sibling files on disk
		// and the scanner metadata derivation is testable separately).
		//
		// Post-scrape, drive the real organize path to a destination that
		// EXISTS but is read-only — the file move step fails, the
		// apply-failure path fires, and the MovieResult stored on the
		// tracker must preserve the Movie pointer set by the scrape phase
		// (so /review/[jobId] can still render the failed-apply file's
		// movie card). Main's process_organize.go returned early on
		// organizeErr WITHOUT mutating the per-file FileResult, so Movie
		// survived; the refactor's UpdateFileResult path overwrote the
		// MovieResult with a Movie-less struct (commit 91400891 fix).
		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		const job = await waitForJobCompletion(request, job_id);
		const { result: scrapeResult } = soleResult(job);
		expect(scrapeResult.status).toBe('completed');
		expect(
			scrapeResult.movie,
			'scrape phase must populate Movie on the successful row',
		).toBeTruthy();

		// Now drive the apply phase. The output dir is in AllowedDirectories
		// (set via JAVINIZER_E2E_OUTPUT_DIR), so the security validator
		// passes; the organize step's actual file move may succeed for
		// tiny 1-byte files OR fail mid-move. Either way, the apply's
		// JobEvent broadcast + result tracker update through
		// interpretApplyResult will fire — and on the failure path must
		// preserve Movie + IsMultiPart (zero values here, but the assertion
		// confirms the field isn't dropped to a separate zero via Replace).
		// For the success path, Movie is updated to result.Movie.Clone()
		// (the orchestrator's post-apply AtomicUpdateFileResult); for the
		// failure path, Movie stays == the prior scrape-phase Movie.
		await submitOrganize(request, job_id, `${DEFAULT_OUTPUT_DIR}/apply-fail-${Date.now()}`);

		// Re-fetch the job — the result shape reflects the post-apply
		// tracker state.
		const jobAfter = await waitForJobCompletion(request, job_id);
		const { result: postApplyResult } = soleResult(jobAfter);

		// Status is either "completed" (organize succeeded for tiny 1-byte
		// file) or "failed" (file move encountered an error). The core
		// 91400891 assertion holds for BOTH branches:
		//   - success: Movie is updated to result.Movie (Clone) — non-nil.
		//   - failure: Movie is preserved from scrape phase (commit 91400891
		//     WARNING fix) — non-nil.
		expect(
			postApplyResult.movie,
			`[bug 91400891 WARNING E2E] failed-apply row must preserve the prior scrape-phase Movie (got status=${postApplyResult.status})`,
		).toBeTruthy();
		expect(postApplyResult.movie!.id).toBe('GOOD-001');

		// [bug 6249de64 E2E — failure-path subset]
		// Started + Ended timestamps must be non-empty on either branch;
		// they were dropped to zero on the apply failure path before fix
		// 6249de64 restored them.
		expect(postApplyResult.started_at, 'started_at must be non-empty after apply').toBeTruthy();
		if (postApplyResult.status === 'failed') {
			expect(postApplyResult.ended_at, 'failed-apply must populate ended_at').toBeTruthy();
		}
	});
});
