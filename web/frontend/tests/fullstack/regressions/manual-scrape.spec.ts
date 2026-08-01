/**
 * /manual scrape full-stack UI spec.
 *
 * Pins the manual-scrape flow end-to-end: /browse (enable Manual Scrape) →
 * select a file → "Continue to manual review" → /manual renders the file
 * row → override the ID → "Start manual scrape" → ProgressModal →
 * auto-redirect to /review/[jobId] → review page renders the
 * OVERRIDDEN ID's scraped metadata.
 *
 * Real stack: browser → Vite dev server (real SvelteKit /browse + /manual
 * render) → real /api/v1/file/browse + /api/v1/batch/scrape (proxied to
 * cmd/javinizer-e2e) → real e2emock scraper → real :memory: SQLite →
 * real ProgressModal job-polling → real goto('/review/:id').
 *
 * Why this matters: the /manual page is the ONLY entry point for the
 * manual-input override feature (manual_inputs map in the BatchScrapeRequest).
 * A regression in the pending-scrape store handoff (/browse → /manual), the
 * buildManualScrapeRequest plumbing, or the override input's bind:value
 * would silently drop overrides — the scrape would use the filename-derived
 * ID instead of the user's override, and no other spec catches this because
 * no other spec exercises /manual.
 *
 * The override assertion is the load-bearing one: the fixture file is
 * GOOD-902.mp4 but the override sends "GOOD-903" as the manual ID. The
 * e2emock scraper keys its response off the MovieID it receives, so the
 * review page must render "E2E Movie GOOD-903" (not "E2E Movie GOOD-902").
 * That proves the manual_inputs override reached the backend's matcher +
 * the scraper received the overridden ID.
 *
 * Dedicated fixture (GOOD-902.mp4): scrape-only (no organize), so the file
 * isn't moved — but the dedicated fixture keeps the spec self-contained +
 * avoids any shared-state coupling to the canonical GOOD-001.mp4.
 */
import { test, expect, type APIRequestContext, type Page } from '@playwright/test';

import {
	DEFAULT_INPUT_DIR,
	DEFAULT_OUTPUT_DIR,
	loginAgainstRealBackend,
	seedInputFiles,
	waitForJobCompletion,
} from '../helpers';

/** Dedicated fixture — scrape-only, not moved by organize. */
const REVERT_FIXTURE = 'GOOD-902.mp4';
const REVERT_SOURCE = `${DEFAULT_INPUT_DIR}/${REVERT_FIXTURE}`;
/** Manual ID override — the scrape must use THIS ID, not the filename's. */
const OVERRIDDEN_ID = 'GOOD-903';

/** Placeholder on the FileBrowser's editable path input. */
const SOURCE_PATH_PLACEHOLDER = 'Enter path (e.g., /path/to/videos)';
/** Placeholder on the /manual override input (per file row). */
const MANUAL_INPUT_PLACEHOLDER = 'Auto — type ID or URL to override';

test.describe('/manual scrape: manual ID override flows through to the scraped result', () => {
	test.beforeEach(async () => {
		await seedInputFiles([REVERT_FIXTURE]);
	});

	test('/browse → enable Manual Scrape → /manual → override ID → scrape → /review shows the overridden ID metadata', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// ── 1. /browse → enable Manual Scrape ───────────────────────────────
		await page.goto('/browse');
		await page.waitForLoadState('domcontentloaded');
		await page.waitForLoadState('networkidle');

		// Open the expandable Options panel (the "Manual Scrape" checkbox
		// lives inside it). The panel toggles on the "Options" button.
		await page.getByRole('button', { name: /^Options$/i }).click();

		// Toggle the "Manual Scrape" checkbox by clicking its label text.
		// The <label> wraps the checkbox + the "Manual Scrape" span, so
		// clicking the text flips the bound manualScrapeMode state.
		await page.getByRole('checkbox', { name: /Provide IDs or URLs manually/i }).check();

		// The action button's label switches from "Scrape N File(s)" to
		// "Continue to manual review" when manualScrapeMode is on. Asserting
		// it visible pins the toggle took effect before we proceed.
		const continueBtn = page.getByRole('button', { name: /Continue to manual review/i });
		await expect(continueBtn).toBeVisible({ timeout: 5_000 });

		// ── 2. Navigate FileBrowser to the input dir + select the fixture ──
		const sourcePathInput = page.getByPlaceholder(SOURCE_PATH_PLACEHOLDER).first();
		await sourcePathInput.fill(DEFAULT_INPUT_DIR);
		await sourcePathInput.press('Enter');

		const fixtureRow = page.getByText(REVERT_FIXTURE, { exact: true }).first();
		await expect(fixtureRow, 'fixture must render in the FileBrowser listing').toBeVisible({
			timeout: 10_000,
		});
		await fixtureRow.click();
		await expect(page.getByRole('heading', { name: '1 File Selected for Scraping' })).toBeVisible({
			timeout: 5_000,
		});

		// Set a valid destination (allowed dir) so the batch-scrape request
		// doesn't 4xx on destination validation.
		const destPathInput = page.getByPlaceholder('Enter destination path (e.g., /path/to/output)').first();
		await destPathInput.fill(DEFAULT_OUTPUT_DIR);
		await destPathInput.press('Tab');

		// ── 3. "Continue to manual review" → /manual ───────────────────────
		// continueToManual() builds the PendingScrape snapshot, stashes it in
		// the store + sessionStorage, then goto('/manual'). The /manual page
		// hydrates from the store in onMount; without it, it redirects back
		// to /browse — so landing on /manual (not /browse) pins the handoff.
		await continueBtn.click();
		await page.waitForURL('**/manual', { timeout: 10_000 });
		await page.waitForLoadState('domcontentloaded');
		await page.waitForLoadState('networkidle');

		// ── 4. Assert the /manual file row renders ─────────────────────────
		// The row shows the fixture's basename. The "Manual Scrape" heading
		// confirms the page mounted (not redirected back to /browse).
		await expect(page.getByRole('heading', { name: 'Manual Scrape' })).toBeVisible();
		await expect(page.getByText(REVERT_FIXTURE, { exact: true }).first()).toBeVisible({
			timeout: 10_000,
		});

		// The badge on an empty input reads "Auto" (matcher derives ID from
		// the filename). Pinning this before the override proves the input
		// starts empty + the tri-state classifier works. The badge is a
		// [role="status"] span containing an SVG icon + the text, so use
		// toContainText rather than an exact-match filter.
		const badge = page.getByRole('status').first();
		await expect(badge).toContainText('Auto', { timeout: 5_000 });

		// ── 5. Override the ID ─────────────────────────────────────────────
		// The override input's aria-label includes the file path. Fill it
		// with a different ID — the scrape must use THIS ID, not the one
		// derived from the fixture filename.
		const overrideInput = page.getByPlaceholder(MANUAL_INPUT_PLACEHOLDER).first();
		await overrideInput.fill(OVERRIDDEN_ID);

		// The badge flips from "Auto" to "ID" (classifyInput → manual-id).
		// This pins the input's bind:value reached the classifier + the
		// row's reactive state updated. Re-query the badge (same element,
		// but its text reacted to the input change).
		await expect(badge).toContainText('ID', { timeout: 5_000 });

		// ── 6. "Start manual scrape" → ProgressModal → /review/[jobId] ─────
		const startBtn = page.getByRole('button', { name: /Start manual scrape/i });
		await expect(startBtn).toBeEnabled({ timeout: 5_000 });
		await startBtn.click();

		// ProgressModal mounts on startJob (submit → apiClient.batchScrape →
		// startJob(res.job_id)). The modal header confirms the scrape was
		// enqueued vs. an error toast.
		await expect(page.getByText('Batch Scraping Progress')).toBeVisible({ timeout: 10_000 });

		// On `completed` (scrape phase done), ProgressModal starts a 3s
		// countdown then goto('/review/:jobId'). Waiting on the URL is
		// race-free vs. clicking "View Results Now".
		await page.waitForURL(/\/review\/[^/]+/, { timeout: 30_000 });
		await page.waitForLoadState('domcontentloaded');
		await page.waitForLoadState('networkidle');

		// ── 7. Assert the OVERRIDDEN ID's metadata rendered ────────────────
		// e2emock keys its response off the MovieID it receives. The override
		// sent "GOOD-903" (not the filename's "GOOD-902"), so the review page
		// must render "E2E Movie GOOD-903". If it shows "E2E Movie GOOD-902",
		// the manual_inputs override was dropped somewhere in the pipeline
		// (store handoff, buildManualScrapeRequest, or backend matcher).
		await expect(page.locator('body')).toContainText('E2E Movie GOOD-903', { timeout: 15_000 });

		// ── 8. Cross-check: the job's result movie_id is the overridden ID ─
		// Ties the browser render to the backend job state — the job's
		// FileResult.movie_id must be GOOD-903 (the override), NOT GOOD-902
		// (the filename-derived ID). This is the durable backend-side proof
		// the override reached the matcher + scraper.
		const jobId = new URL(page.url()).pathname.split('/review/')[1];
		expect(jobId, 'review URL must carry a jobId').toBeTruthy();

		const job = await waitForJobCompletion(request, jobId);
		const results = Object.values(job.results);
		expect(results.length, 'job must have exactly one result').toBe(1);
		expect(
			results[0].movie_id,
			'movie_id must be the OVERRIDDEN ID (GOOD-903), not the filename-derived ID',
		).toBe(OVERRIDDEN_ID);
		expect(
			results[0].movie?.title ?? '',
			'scraped title must carry the overridden ID marker',
		).toContain('E2E Movie GOOD-903');
	});
});
