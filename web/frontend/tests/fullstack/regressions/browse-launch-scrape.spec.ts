/**
 * /browse → launch-scrape → /review/[jobId] full-stack UI spec.
 *
 * This is the PRIMARY user-path spec the fullstack suite was missing: it
 * drives a REAL browser through the real SvelteKit /browse page → real
 * FileBrowser directory listing (real /api/v1/file/browse) → real file
 * selection → real "Scrape" action button → real ProgressModal polling
 * → real auto-redirect to /review/[jobId] → real review-page render of
 * the e2emock-scraped metadata. Every other fullstack spec drives the
 * backend via the `request` fixture (API-only); this one proves the
 * rendered Svelte UI can actually bootstrap a scrape job + land the user
 * on the review page with real scraped data.
 *
 * Real stack: browser → Vite dev server (real SvelteKit /browse render)
 * → real /api/v1/file/browse + /api/v1/file/scan + /api/v1/batch/scrape
 * (proxied to cmd/javinizer-e2e) → real e2emock scraper → real :memory:
 * SQLite → real ProgressModal job-polling → real goto('/review/:id').
 *
 * Why this matters: a regression in any link of the browse→scrape→review
 * chain (FileBrowser path resolution, file-selection state, the action
 * button's startBatchScrape wiring, ProgressModal completion detection,
 * or the review page's post-redirect data fetch) is invisible to the
 * API-only specs. This spec is the only one that renders /browse at all.
 *
 * Determinism: the e2emock scraper returns Title "E2E Movie GOOD-001" +
 * Maker "E2E Test Studio" for the GOOD-001 ID (see
 * internal/scraper/e2emock/e2emock.go successResult). The fixture file
 * GOOD-001.mp4 is seeded into DEFAULT_INPUT_DIR by global-setup. The
 * scrape phase reaches `completed` (organize is a separate explicit
 * step — see organize.spec.ts), so the ProgressModal's terminal-state
 * redirect fires + lands on /review/[jobId].
 */
import { test, expect, type APIRequestContext, type Page } from '@playwright/test';

import {
	DEFAULT_INPUT_DIR,
	DEFAULT_OUTPUT_DIR,
	loginAgainstRealBackend,
	waitForJobCompletion,
} from '../helpers';

/**
 * Placeholder text on the FileBrowser's editable path input (PathInput
 * default placeholder — the browse page does not override it).
 */
const SOURCE_PATH_PLACEHOLDER = 'Enter path (e.g., /path/to/videos)';

/**
 * Placeholder text on the "Output Destination" PathInput (set explicitly
 * in browse/+page.svelte). Used to disambiguate from the FileBrowser's
 * path input — both are PathInput instances but only the destination
 * one carries this placeholder.
 */
const DEST_PATH_PLACEHOLDER = 'Enter destination path (e.g., /path/to/output)';

test.describe('/browse → launch scrape → /review/[jobId]', () => {
	test('selecting a file + clicking Scrape redirects to the review page with scraped metadata', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// ── 1. Open /browse + let the page hydrate ───────────────────────────
		// networkidle waits for the cwd query + config/scrapers queries that
		// feed the FileBrowser's initialPath + the destination default. We
		// override both explicitly below, but waiting avoids a race where the
		// hydration $effect rewrites our inputs after we fill them.
		await page.goto('/browse');
		await page.waitForLoadState('domcontentloaded');
		await page.waitForLoadState('networkidle');

		// ── 2. Navigate the FileBrowser to the seeded input dir ─────────────
		// The FileBrowser's initial path is the backend CWD (the repo root
		// when run via `go run ./cmd/javinizer-e2e`), which is NOT in the
		// e2e backend's AllowedDirectories — so the initial listing is
		// denied/empty. Type the allowed fixture dir + Enter to browse it.
		const sourcePathInput = page.getByPlaceholder(SOURCE_PATH_PLACEHOLDER).first();
		await sourcePathInput.fill(DEFAULT_INPUT_DIR);
		await sourcePathInput.press('Enter');

		// The browse endpoint returns the fixture files; GOOD-001.mp4 is the
		// canonical scrape-success fixture. Wait for its row to render before
		// selecting — guards against a stale "Empty directory" state.
		const goodFileRow = page.getByText('GOOD-001.mp4', { exact: true }).first();
		await expect(goodFileRow, 'GOOD-001.mp4 must render in the FileBrowser listing').toBeVisible({
			timeout: 10_000,
		});

		// ── 3. Select GOOD-001.mp4 ──────────────────────────────────────────
		// Clicking the file-name text toggles the row's selected state (the
		// text lives inside the row's <button>, so the click reaches the
		// toggle handler). The sticky action bar + the "Selected Files" card
		// both react to the selection.
		await goodFileRow.click();

		// The sticky bar shows "<N> file(s) selected"; for exactly one file
		// the text is "1 file selected". The "Selected Files" card heading
		// ("1 File Selected for Scraping") also contains the substring, so
		// match exactly to disambiguate. Asserting this pins that the click
		// actually toggled selection state (vs. e.g. a bubbling bug).
		await expect(page.getByRole('heading', { name: '1 File Selected for Scraping' })).toBeVisible({
			timeout: 5_000,
		});

		// ── 4. Set a valid output destination ───────────────────────────────
		// The scrape phase itself doesn't write to the destination, but the
		// browse page sends `destination` on the batch-scrape request + the
		// backend validates it against AllowedDirectories. The default
		// destination (CWD / repo root) is NOT allowed → would 4xx the
		// scrape. Point it at the allowed output fixture dir.
		const destPathInput = page.getByPlaceholder(DEST_PATH_PLACEHOLDER).first();
		await destPathInput.fill(DEFAULT_OUTPUT_DIR);
		// Blur to commit the onchange handler (persists to localStorage).
		await destPathInput.press('Tab');

		// ── 5. Click "Scrape 1 File" → ProgressModal ────────────────────────
		// The action button's label is "Scrape {N} File{s}" — for one file,
		// "Scrape 1 File". Clicking fires startBatchScrape → POST
		// /api/v1/batch/scrape → startJob(job_id) → ProgressModal mounts +
		// polls the job until terminal.
		const scrapeButton = page.getByRole('button', { name: /Scrape 1 File/i });
		await expect(scrapeButton).toBeEnabled({ timeout: 5_000 });
		await scrapeButton.click();

		// The ProgressModal header confirms the scrape was enqueued (vs. an
		// error toast). Don't over-assert here — the redirect below is the
		// load-bearing signal.
		await expect(page.getByText('Batch Scraping Progress')).toBeVisible({ timeout: 10_000 });

		// ── 6. Wait for the auto-redirect to /review/[jobId] ────────────────
		// ProgressModal polls the job; on `completed` (scrape phase done —
		// organize is a separate step) it starts a 3s countdown then
		// goto('/review/:jobId'). Waiting on the URL is race-free vs.
		// clicking "View Results Now" (which competes with the countdown).
		await page.waitForURL(/\/review\/[^/]+/, { timeout: 30_000 });

		// ── 7. Assert the review page rendered the scraped metadata ─────────
		// The review page does post-mount fetches (/batch/:id, /movies,
		// /scrapers) before rendering the movie card. networkidle gates the
		// text assertion so it doesn't race the fetch.
		await page.waitForLoadState('domcontentloaded');
		await page.waitForLoadState('networkidle');

		// e2emock returns Title "E2E Movie GOOD-001" — rendered by the review
		// page in both grid (ReviewGridCard) + detail (MovieMetadataCard)
		// views. Asserting the title pins the full chain: scrape produced
		// real metadata → job result persisted → review page fetched + rendered it.
		const reviewBody = page.locator('body');
		await expect(reviewBody).toContainText('E2E Movie GOOD-001', { timeout: 15_000 });

		// ── 8. Cross-check: the URL's jobId maps to a real completed job ────
		// Ties the browser navigation to the backend job state — catches a
		// regression where the redirect lands on a stale/wrong jobId.
		const jobId = new URL(page.url()).pathname.split('/review/')[1];
		expect(jobId, 'review URL must carry a jobId').toBeTruthy();

		const job = await waitForJobCompletion(request, jobId);
		const results = Object.values(job.results);
		expect(results.length, 'job must have exactly one result for GOOD-001.mp4').toBe(1);
		expect(results[0].movie_id, 'scraped movie_id must be GOOD-001').toBe('GOOD-001');
		expect(
			results[0].movie?.title ?? '',
			'scraped title must carry the e2emock marker',
		).toContain('E2E Movie GOOD-001');
	});
});
