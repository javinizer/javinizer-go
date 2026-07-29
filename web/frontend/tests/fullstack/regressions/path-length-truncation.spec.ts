/**
 * Path-length truncation full-stack spec — pins the PR #173 clampResult path.
 *
 * TWO truncation mechanisms exist in the organizer; this spec isolates the
 * second (the PR's binary-search clamp), NOT the pre-existing title clamp:
 *
 *  1. applyTitleTruncation (organizer.go buildPlanContext) — pre-truncates
 *     ctx.Title to MaxTitleLength (default 100) BEFORE the template engine
 *     runs. Always fires when the title exceeds 100 bytes.
 *  2. ExecuteWithMaxBytes → clampResult (strategy_organize.go, the PR #173
 *     path) — truncates the RENDERED folder name to fit
 *     folderMaxBytes = MaxPathLength - overheadBytes. Only fires when
 *     folderMaxBytes > 0 AND the rendered folder exceeds it.
 *
 * If the destination is short, folderMaxBytes ≈ 173, so a 100-byte
 * pre-truncated title FITS without clamping — the test would pass even with
 * clampResult bypassed (false positive). To force clampResult to genuinely
 * fire, the destination is padded long enough that folderMaxBytes < 100,
 * so the pre-truncated 100-byte title MUST be clamped further to fit.
 *
 * Decisive assertion: folderName.length < 100. Pre-truncation alone yields
 * exactly 100 bytes; only clampResult truncates below 100. A negative control
 * that bypasses clampResult leaves the folder at 100 → the full path exceeds
 * MaxPathLength → ValidatePathLength errors → organize FAILS → test fails.
 *
 * Deterministic fixture: e2emock returns a 208-byte title + non-empty
 * OriginalTitle for PATH-* IDs. The e2e config's FolderFormat is
 * `<IF:ORIGINALTITLE><TITLE><ELSE><ID></IF>`, so PATH-* IDs render the folder
 * from <TITLE> while GOOD/MULTI IDs (no OriginalTitle) fall through to <ID> —
 * preserving every existing spec's folder-structure assertions. No runtime
 * config hot-reload (which rebuilds the scraper registry and breaks the
 * e2emock injection for subsequent serial specs).
 */
import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { existsSync, readdirSync, rmSync, statSync } from 'node:fs';
import { join } from 'node:path';

import {
	DEFAULT_OUTPUT_DIR,
	DEFAULT_INPUT_DIR,
	loginAgainstRealBackend,
	seedInputFiles,
	submitScrape,
	waitForJobCompletion,
	navigateToReviewPage,
} from '../helpers';

const FIXTURE = 'PATH-906.mp4';
const MOVIE_ID = 'PATH-906';
/** e2emock returns a 208-byte title ("Long " × 40 + "PATH-906"). */
const FULL_TITLE_BYTES = 208;
/** config default MaxTitleLength — applyTitleTruncation caps the title here. */
const MAX_TITLE_LENGTH = 100;
/** config default MaxPathLength — the on-disk path must not exceed this. */
const MAX_PATH_LENGTH = 240;
const DEST_PLACEHOLDER = 'Enter destination path (e.g., /path/to/output)';

test.describe('/review/[jobId] organize: folder name truncated to fit max_path_length', () => {
	let destination: string | undefined;

	test.beforeEach(async () => {
		await seedInputFiles([FIXTURE]);
	});

	test.afterEach(async () => {
		await seedInputFiles([FIXTURE]);
		if (destination) {
			rmSync(destination, { recursive: true, force: true });
			destination = undefined;
		}
	});

	test('long title + long destination forces clampResult to truncate folder below MaxTitleLength', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// ── 1. Scrape the long-title fixture ────────────────────────────────
		// e2emock returns a 208-byte title + non-empty OriginalTitle for PATH-*.
		// buildPlanContext pre-truncates ctx.Title to MaxTitleLength (100) before
		// the template engine runs, so the folder template <TITLE> renders 100
		// bytes — UNLESS clampResult truncates it further (see step 5).
		const job_id = await submitScrape(request, {
			files: [`${DEFAULT_INPUT_DIR}/${FIXTURE}`],
		});
		await waitForJobCompletion(request, job_id);

		// ── 2. Navigate to the review page in the real browser ─────────────
		// The destination is padded long enough that folderMaxBytes < 100. With
		// MaxPathLength=240 and overhead ≈ len(dest)+subfolder+filename+seps,
		// a ~160-byte destination makes folderMaxBytes ≈ 55. The pre-truncated
		// 100-byte title therefore EXCEEDS folderMaxBytes, forcing clampResult
		// (the PR #173 binary-search clamp) to truncate the folder further.
		// A short destination would leave folderMaxBytes ≈ 173 > 100, so the
		// title would fit without clamping — a false positive.
		destination = `${DEFAULT_OUTPUT_DIR}/pt-${'x'.repeat(120)}-${Date.now()}`;
		expect(existsSync(destination), 'precondition: destination must not exist').toBeFalsy();

		await navigateToReviewPage(page, job_id);

		// Force detail view — the only view that surfaces the organize UI.
		await page.getByRole('button', { name: /^Detail$/i }).click();
		await expect(page.locator('body')).toContainText(MOVIE_ID, { timeout: 15_000 });

		// ── 3. Fill destination in the real UI + click Organize ────────────
		const destInput = page.getByPlaceholder(DEST_PLACEHOLDER).first();
		await expect(destInput, 'destination input must render in organize mode').toBeVisible({
			timeout: 10_000,
		});
		await destInput.fill(destination);

		const organizeBtn = page.getByRole('button', { name: /^Organize 1 File$/i }).first();
		await expect(organizeBtn).toBeEnabled({ timeout: 5_000 });
		await organizeBtn.click();

		// ── 4. Wait for the real apply phase to finish ────────────────────
		// If clampResult were broken, ValidatePathLength would reject the
		// over-long path and organize would fail — "Organization Complete!"
		// would never appear. So reaching this point already proves the clamp
		// path executed successfully.
		await expect(page.getByText('Organization Complete!')).toBeVisible({ timeout: 30_000 });

		const organized_job = await waitForJobCompletion(request, job_id, {
			expectStatus: 'organized',
			timeoutMs: 30_000,
		});
		expect(organized_job.status).toBe('organized');

		// ── 5. Assert clampResult truncated the folder below MaxTitleLength ─
		// On-disk structure (SubfolderFormat=["<ID>"], FolderFormat=<TITLE>):
		//   destination/<ID>/<TITLE-clamped>/<ID>.mp4
		expect(existsSync(destination), 'destination directory must exist after organize').toBeTruthy();

		const idSubfolder = join(destination, MOVIE_ID);
		expect(
			existsSync(idSubfolder),
			`<ID> subfolder must exist under destination (got: ${readdirSync(destination).join(', ')})`,
		).toBeTruthy();

		const idEntries = readdirSync(idSubfolder);
		const folderName = idEntries.find((entry) => statSync(join(idSubfolder, entry)).isDirectory());
		expect(
			folderName,
			'a title-derived subfolder must exist under the <ID> subfolder',
		).toBeTruthy();
		expect(folderName, 'folder must be derived from the title template').toMatch(/^Long /);

		// DECISIVE: pre-truncation alone yields exactly 100 bytes. Only
		// clampResult (folderMaxBytes < 100) truncates below it. This is the
		// assertion a bypassed clamp fails to satisfy.
		expect(
			Buffer.byteLength(folderName!),
			`folder name (${Buffer.byteLength(folderName!)} bytes) must be below MaxTitleLength (${MAX_TITLE_LENGTH}) — only clampResult truncates here`,
		).toBeLessThan(MAX_TITLE_LENGTH);
		expect(
			Buffer.byteLength(folderName!),
			'folder name must be shorter than the full 208-byte title',
		).toBeLessThan(FULL_TITLE_BYTES);

		// Core contract: the full on-disk path fits within max_path_length.
		const subEntries = readdirSync(join(idSubfolder, folderName!));
		const videoFile = subEntries.find((e) => e.endsWith('.mp4'));
		expect(videoFile, 'organized video file must exist').toBeTruthy();
		const fullPath = join(idSubfolder, folderName!, videoFile!);
		expect(
			Buffer.byteLength(fullPath),
			`full path (${Buffer.byteLength(fullPath)} bytes) must not exceed max_path_length (${MAX_PATH_LENGTH})`,
		).toBeLessThanOrEqual(MAX_PATH_LENGTH);
	});
});
