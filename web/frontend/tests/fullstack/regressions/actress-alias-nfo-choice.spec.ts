/**
 * Full-stack regression suite for the actress alias-aware dedup + per-movie
 * "Write to NFO as" name-choice feature.
 *
 * Drives the real stack end-to-end: browser → SvelteKit frontend → Vite
 * proxy → Go API server (cmd/javinizer-e2e) → real worker pipeline → real
 * :memory: SQLite → real e2emock scraper at the scraper seam. The e2e
 * backend seeds the actress_aliases table (database.SeedDefaultActressAliases)
 * at startup, and the ALIAS-* e2emock prefix returns an actress whose
 * JapaneseName (朝日芹奈) is a seeded alias mapping to canonical 新セリナ —
 * so the real alias_resolver + the real "Write to NFO as" dropdown are
 * exercised without any dedicated seeding endpoint.
 *
 * Coverage:
 *   - The "Write to NFO as" dropdown appears for an actress with a known
 *     alias group and lists the canonical + every alias.
 *   - Selecting a different name writes it back to the actress's
 *     japanese_name (the NFO <name> source) and persists in the in-page
 *     movie state after save.
 */
import { test, expect, type APIRequestContext, type Page } from '@playwright/test';

import {
	loginAgainstRealBackend,
	submitScrape,
	waitForJobCompletion,
	soleResult,
	navigateToReviewPage,
	DEFAULT_INPUT_DIR,
	seedInputFiles,
	type BatchJobResponse,
} from '../helpers';

// The e2emock ALIAS-* prefix returns an actress with JapaneseName="朝日芹奈",
// which the bootstrap seed maps to canonical 新セリナ alongside aliases
// 青木桃 and 堤セリナ. These must stay in sync with aliasResult() in
// internal/scraper/e2emock/e2emock.go and defaultActressAliases in
// internal/database/actress_alias_repo.go.
const ALIAS_FIXTURE = 'ALIAS-001.mp4';
const MOVIE_ID = 'ALIAS-001';
const SCRAPED_JP_NAME = '朝日芹奈';
const CANONICAL_NAME = '新セリナ';

/**
 * Focus a movie on the review page. The review page mounts in grid view
 * (default_review_view=grid-poster); clicking the grid card switches to
 * detail view, which populates currentMovie and renders the ActressEditor
 * with the movie's actresses. If the page is already in detail view (card
 * already focused), this is a no-op.
 */
async function focusMovie(page: Page, movieId: string): Promise<void> {
	const gridCard = page.locator(`[aria-label="View details for ${movieId}"]`);
	// Only click if the grid card is present; in detail view it's absent and
	// the actress editor is already rendered.
	if ((await gridCard.count()) > 0) {
		await gridCard.first().click();
	}
}

/**
 * Open the actress edit modal by clicking the pencil button on the actress
 * card whose japanese_name matches `jpName`. The pencil is the first button
 * in the card's action row (SquarePen icon); scope to the actress Card
 * containing the name and click that specific button.
 */
async function openActressEdit(page: Page, jpName: string): Promise<void> {
	const nameEl = page.locator(`p[title="${jpName}"]`).first();
	await expect(nameEl, `actress card must render japanese_name=${jpName}`).toBeVisible({
		timeout: 15_000,
	});
	// Climb to the NEAREST card ancestor of the name <p>. A plain
	// `.card.filter({has})` would also match the outer review-page Card (which
	// transitively contains the name) whose first button is "Add Actress";
	// the xpath ancestor axis with [1] selects the closest card ancestor —
	// the inner per-actress Card whose action row holds the pencil + trash.
	const actressCard = nameEl.locator('xpath=ancestor::*[contains(@class,"card")][1]');
	await actressCard.locator('button').first().click();
	await expect(page.getByRole('heading', { name: 'Edit Actress' })).toBeVisible({
		timeout: 10_000,
	});
}

test.describe('Actress alias NFO name choice', () => {
	test('the "Write to NFO as" dropdown lists the canonical + every alias for a known rename', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);
		await seedInputFiles([ALIAS_FIXTURE]);

		const jobId = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/${ALIAS_FIXTURE}`] });
		const job: BatchJobResponse = await waitForJobCompletion(request, jobId);
		expect(job.status).toBe('completed');
		const { result } = soleResult(job);
		expect(result.status).toBe('completed');
		expect(result.movie_id).toBe(MOVIE_ID);

		await navigateToReviewPage(page, jobId);
		await focusMovie(page, MOVIE_ID);
		await openActressEdit(page, SCRAPED_JP_NAME);

		// The dropdown only renders after the debounced alias-group fetch
		// (300ms) resolves. "Write to NFO as" is the label; the <select>
		// (#actress-nfo-name) follows.
		await expect(page.getByText('Write to NFO as', { exact: true })).toBeVisible({
			timeout: 10_000,
		});
		const select = page.locator('#actress-nfo-name');
		await expect(select).toBeVisible();

		// Assert every known name is offered. The canonical is marked
		// "(canonical)". The group is canonical-first then aliases in
		// alias_name order (堤セリナ, 朝日芹奈, 青木桃).
		const optionTexts = await select.locator('option').allTextContents();
		expect(optionTexts, `dropdown options were: ${JSON.stringify(optionTexts)}`).toEqual(
			expect.arrayContaining([
				`${CANONICAL_NAME} (canonical)`,
				'青木桃',
				'堤セリナ',
				SCRAPED_JP_NAME,
			]),
		);
		expect(optionTexts.length).toBe(4);
	});

	test('selecting a different name writes it back to japanese_name and persists after save', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);
		await seedInputFiles([ALIAS_FIXTURE]);

		const jobId = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/${ALIAS_FIXTURE}`] });
		await waitForJobCompletion(request, jobId);
		await navigateToReviewPage(page, jobId);
		await focusMovie(page, MOVIE_ID);
		await openActressEdit(page, SCRAPED_JP_NAME);

		// Wait for the dropdown, then choose the canonical name.
		const select = page.locator('#actress-nfo-name');
		await expect(select).toBeVisible({ timeout: 10_000 });
		await select.selectOption(CANONICAL_NAME);

		// Save the edit. The modal closes on save.
		await page.getByRole('button', { name: 'Save Changes' }).click();
		await expect(page.getByRole('heading', { name: 'Edit Actress' })).toBeHidden({
			timeout: 10_000,
		});

		// The actress card now renders the chosen canonical name as its
		// japanese_name (the NFO <name> source), proving the write-back
		// persisted into the in-page movie state.
		await expect(page.locator(`[title="${CANONICAL_NAME}"]`).first()).toBeVisible({
			timeout: 10_000,
		});
	});
});
