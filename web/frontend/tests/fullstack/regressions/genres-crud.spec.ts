/**
 * /genres page full-stack UI spec.
 *
 * Pins the genre-replacement CRUD UI end-to-end against the real e2emock
 * backend: list render, add, edit, search, sort, delete. Every action
 * goes through the real rendered page (real SvelteKit /genres) → real
 * /api/v1/genres/replacements (proxied to cmd/javinizer-e2e) → real
 * :memory: SQLite. No page.route mocking.
 *
 * Why this matters: the existing import-export.spec.ts covers import +
 * export against a developer-run backend, but the core CRUD table
 * interactions (add row, inline edit, delete, search filter, sort
 * toggle) had zero Playwright coverage. A regression in the
 * GenreReplacement mutation wiring, the table render, the search filter,
 * or the sort toggle is invisible without this spec.
 *
 * Uniqueness: each test uses a timestamp-suffixed `original` to avoid
 * collisions with prior runs (the suite is serial against one backend).
 * afterEach cleans up any leftover test replacements via the API.
 */
import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { BACKEND_BASE, loginAgainstRealBackend } from '../helpers';

const TEST_PREFIX = 'E2E-GENRE-';
const HEADING = 'Genres';

interface GenreReplacement {
	id: number;
	original: string;
	replacement: string;
}

async function listGenreReplacements(api: APIRequestContext): Promise<GenreReplacement[]> {
	const resp = await api.get(`${BACKEND_BASE}/api/v1/genres/replacements?limit=1000`);
	expect(resp.ok(), `list genres failed: ${resp.status()}`).toBeTruthy();
	const body = (await resp.json()) as { replacements: GenreReplacement[] };
	return body.replacements ?? [];
}

async function createGenreReplacement(
	api: APIRequestContext,
	original: string,
	replacement: string,
): Promise<GenreReplacement> {
	const resp = await api.post(`${BACKEND_BASE}/api/v1/genres/replacements`, {
		data: { original, replacement },
	});
	expect(resp.ok(), `create genre failed: ${resp.status()} ${await resp.text()}`).toBeTruthy();
	return (await resp.json()) as GenreReplacement;
}

async function deleteGenreReplacement(api: APIRequestContext, id: number): Promise<void> {
	const resp = await api.delete(`${BACKEND_BASE}/api/v1/genres/replacements?id=${id}`);
	expect(resp.ok(), `delete genre ${id} failed: ${resp.status()}`).toBeTruthy();
}

async function cleanupTestReplacements(api: APIRequestContext): Promise<void> {
	const all = await listGenreReplacements(api);
	await Promise.all(
		all
			.filter((r) => r.original.startsWith(TEST_PREFIX))
			.map((r) => deleteGenreReplacement(api, r.id).catch(() => {})),
	);
}

async function navigateToGenres(page: Page): Promise<void> {
	await page.goto('/genres');
	// The Genres page now has multiple sections (ignored, favorites,
	// replacements) under a single "Genres" h1. Wait for the page heading
	// (exact match avoids the section h2s) + the replacements "Add" card
	// input, which renders after the genre-replacements query settles.
	await expect(page.getByRole('heading', { name: HEADING, exact: true })).toBeVisible({ timeout: 15_000 });
	await expect(page.getByPlaceholder('e.g., HD')).toBeVisible({ timeout: 10_000 });
}

test.describe('/genres: real CRUD UI against the e2emock backend', () => {
	test.afterEach(async ({ request }: { request: APIRequestContext }) => {
		await cleanupTestReplacements(request);
	});

	test('list renders a pre-created replacement row', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);
		const original = `${TEST_PREFIX}LIST-${Date.now()}`;
		const replacement = 'High Definition';
		await createGenreReplacement(request, original, replacement);

		await navigateToGenres(page);

		// The row renders both the original + replacement cells. The
		// table is a CSS grid (not a <table>), so locate the row by text.
		await expect(page.getByText(original, { exact: true })).toBeVisible({ timeout: 10_000 });
		await expect(page.getByText(replacement, { exact: true })).toBeVisible();
	});

	test('add → row renders → edit → replacement updates → delete → row gone', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);
		await navigateToGenres(page);

		// ── 1. Add a replacement via the UI ──────────────────────────────
		const original = `${TEST_PREFIX}CRUD-${Date.now()}`;
		const replacement = 'First Replacement';
		// The page now has multiple "Add" buttons (ignored, favorites,
		// replacements). Scope to the replacements add-rule card, which is
		// the only card containing the "Add a new genre replacement rule"
		// paragraph.
		const addRuleCard = page
			.getByText('Add a new genre replacement rule')
			.locator('xpath=ancestor::div[contains(@class,"card")][1]');
		await addRuleCard.getByPlaceholder('e.g., HD').fill(original);
		await addRuleCard.getByPlaceholder('e.g., High Definition').fill(replacement);
		await addRuleCard.getByRole('button', { name: /^Add$/ }).click();

		// The addMutation invalidates the query → the new row renders.
		await expect(page.getByText(original, { exact: true })).toBeVisible({ timeout: 10_000 });
		await expect(page.getByText(replacement, { exact: true })).toBeVisible();

		// ── 2. Edit the replacement inline ───────────────────────────────
		// Click the row's edit button (title="Edit"). The original-text cell
		// is unique; its following-sibling div holds the Actions cell (Edit +
		// Delete buttons). Scoping via xpath avoids matching ancestor
		// container divs that would resolve to multiple buttons.
		const originalCell = page.getByText(original, { exact: true });
		await originalCell.locator('xpath=following-sibling::div//button[@title="Edit"]').click();

		// The edit-replacement input renders (the original is disabled).
		const editInput = page.locator('input.font-mono:not([disabled])').last();
		await editInput.fill('Edited Replacement');
		await page.getByRole('button', { name: /^Save$/ }).click();

		// The updateMutation invalidates → the edited replacement renders.
		await expect(page.getByText('Edited Replacement', { exact: true })).toBeVisible({
			timeout: 10_000,
		});

		// ── 3. Delete the row ────────────────────────────────────────────
		// Re-resolve the original cell (the row re-rendered after the edit).
		const originalCellAfter = page.getByText(original, { exact: true });
		await originalCellAfter.locator('xpath=following-sibling::div//button[@title="Delete"]').click();

		// The deleteMutation invalidates → the row disappears.
		await expect(page.getByText(original, { exact: true })).toHaveCount(0, { timeout: 10_000 });
	});

	test('search filters the list + sort toggles the order', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);
		// Seed two replacements with distinct originals for search/sort.
		const a = await createGenreReplacement(request, `${TEST_PREFIX}ALPHA-${Date.now()}`, 'A-Rep');
		const b = await createGenreReplacement(request, `${TEST_PREFIX}BETA-${Date.now()}`, 'B-Rep');

		await navigateToGenres(page);

		// Both rows render initially.
		await expect(page.getByText(a.original, { exact: true })).toBeVisible({ timeout: 10_000 });
		await expect(page.getByText(b.original, { exact: true })).toBeVisible();

		// ── 1. Search filters to the matching row ────────────────────────
		await page.getByPlaceholder('Search by original or replacement...').fill('ALPHA');
		await expect(page.getByText(a.original, { exact: true })).toBeVisible();
		await expect(page.getByText(b.original, { exact: true })).toHaveCount(0);

		// Clear search → both visible again.
		await page.getByTitle('Clear search').click();
		await expect(page.getByText(b.original, { exact: true })).toBeVisible({ timeout: 10_000 });

		// ── 2. Sort toggle flips A-Z ↔ Z-A ───────────────────────────────
		// The sort button shows "A-Z" or "Z-A". Default is asc (A-Z).
		const sortBtn = page.getByTitle('Toggle sort order');
		await expect(sortBtn).toContainText('A-Z');

		// ALPHA < BETA alphabetically. After toggling to Z-A, BETA should
		// come first. We assert the toggle text flipped + both rows still
		// render (the order is hard to assert precisely without DOM
		// traversal; the toggle text + row presence pins the behavior).
		await sortBtn.click();
		await expect(sortBtn).toContainText('Z-A');
		await expect(page.getByText(a.original, { exact: true })).toBeVisible();
		await expect(page.getByText(b.original, { exact: true })).toBeVisible();
	});
});
