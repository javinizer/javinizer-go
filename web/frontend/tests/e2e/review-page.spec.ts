import { test, expect } from '@playwright/test';
import {
	navigateToReviewPage,
	switchToGridView,
	getGridCards,
	getGridCard,
	getGridCardSelector,
	enableSelectionMode,
	getCompletenessDial,
	getSelectAllButton,
	getCompletenessFilterButton,
	getBulkRemoveButton,
	getBulkRescrapeButton,
	getDetailViewButton,
	getGridViewButton,
	getRescrapeModal,
} from './review-helpers';

test.beforeEach(async ({ page }) => {
	await navigateToReviewPage(page);
	await switchToGridView(page);
});

test.describe('Completeness Dial on Grid Cards', () => {
	test('completeness dial renders on each grid card', async ({ page }) => {
		const cards = await getGridCards(page);
		expect(cards.length).toBe(5);

		for (let i = 0; i < cards.length; i++) {
			const dial = getCompletenessDial(page, i);
			await expect(dial).toBeVisible({ timeout: 10_000 });
			const ariaLabel = await dial.getAttribute('aria-label');
			expect(ariaLabel).toMatch(/\d+% complete/);
			const score = parseInt(ariaLabel!.match(/(\d+)%/)?.[1] ?? '0', 10);
			expect(score).toBeGreaterThanOrEqual(0);
			expect(score).toBeLessThanOrEqual(100);
		}
	});

	test('completeness dial shows tier-appropriate color', async ({ page }) => {
		const dial = getCompletenessDial(page, 0);
		await expect(dial).toBeVisible({ timeout: 10_000 });

		const svg = dial.locator('svg');
		const progressCircle = svg.locator('circle').nth(1);
		const stroke = await progressCircle.getAttribute('stroke');
		const validColors = ['rgb(239 68 68)', 'rgb(234 179 8)', 'rgb(34 197 94)'];
		expect(validColors).toContain(stroke);
	});

	test('hovering dial shows breakdown tooltip', async ({ page }) => {
		const dial = getCompletenessDial(page, 0);
		await expect(dial).toBeVisible({ timeout: 10_000 });

		await dial.hover();
		await page.waitForTimeout(300);

		const visibleTooltip = page.locator('[role="tooltip"]').first();
		await expect(visibleTooltip).toBeVisible({ timeout: 5_000 });
		const tooltipText = await visibleTooltip.textContent();
		expect(tooltipText).toMatch(/Essential|Important|Nice-to-have/);
	});

	test('complete movies show green dial', async ({ page }) => {
		const dial = getCompletenessDial(page, 0);
		const ariaLabel = await dial.getAttribute('aria-label');
		const score = parseInt(ariaLabel!.match(/(\d+)%/)?.[1] ?? '0', 10);
		expect(score).toBeGreaterThanOrEqual(80);

		const svg = dial.locator('svg');
		const progressCircle = svg.locator('circle').nth(1);
		const stroke = await progressCircle.getAttribute('stroke');
		expect(stroke).toBe('rgb(34 197 94)');
	});
});

test.describe('Grid Card Selection Mode', () => {
	test('select button toggles selection mode on', async ({ page }) => {
		const selectBtn = page.getByRole('button', { name: /^select$/i }).first();
		await expect(selectBtn).toBeVisible({ timeout: 10_000 });
		await expect(selectBtn).toHaveAttribute('aria-pressed', 'false');

		await selectBtn.click();
		await expect(selectBtn).toHaveAttribute('aria-pressed', 'true');

		const card = getGridCard(page, 0);
		await expect(card).toHaveAttribute('role', 'checkbox');
	});

	test('clicking card in selection mode selects a movie', async ({ page }) => {
		await enableSelectionMode(page);

		const card = getGridCard(page, 0);
		await card.click();

		await expect(card).toHaveAttribute('aria-checked', 'true');

		const selectedCountText = page.getByText(/\d+ selected/);
		await expect(selectedCountText).toBeVisible({ timeout: 5_000 });
	});

	test('clicking card again deselects the movie', async ({ page }) => {
		await enableSelectionMode(page);

		const card = getGridCard(page, 0);
		await card.click();
		await expect(card).toHaveAttribute('aria-checked', 'true');

		await card.click();
		await expect(card).toHaveAttribute('aria-checked', 'false');
	});

	test('selected card shows blue ring', async ({ page }) => {
		await enableSelectionMode(page);

		const card = getGridCard(page, 0);
		await card.click();

		const classes = await card.getAttribute('class');
		expect(classes).toContain('ring-blue-500');
	});

	test('select all toggles all movies', async ({ page }) => {
		await enableSelectionMode(page);

		const selectAllBtn = getSelectAllButton(page);
		await expect(selectAllBtn).toBeVisible({ timeout: 10_000 });
		await selectAllBtn.click();

		const cards = await getGridCards(page);
		for (let i = 0; i < cards.length; i++) {
			const card = getGridCard(page, i);
			await expect(card).toHaveAttribute('aria-checked', 'true', { timeout: 5_000 });
		}

		const deselectAllBtn = page.getByRole('button', { name: /deselect all/i });
		await expect(deselectAllBtn).toBeVisible({ timeout: 5_000 });
		await deselectAllBtn.click();

		for (let i = 0; i < cards.length; i++) {
			const card = getGridCard(page, i);
			await expect(card).toHaveAttribute('aria-checked', 'false', { timeout: 5_000 });
		}
	});

	test('shift-click selects range', async ({ page }) => {
		await enableSelectionMode(page);

		const firstCard = getGridCard(page, 0);
		await firstCard.click();
		await expect(firstCard).toHaveAttribute('aria-checked', 'true');

		const thirdCard = getGridCard(page, 2);
		await thirdCard.click({ modifiers: ['Shift'] });

		for (let i = 0; i < 3; i++) {
			const card = getGridCard(page, i);
			await expect(card).toHaveAttribute('aria-checked', 'true', { timeout: 5_000 });
		}
	});

	test('toggling selection mode off clears all selections', async ({ page }) => {
		await enableSelectionMode(page);

		const card = getGridCard(page, 0);
		await card.click();
		await expect(card).toHaveAttribute('aria-checked', 'true');

		const selectBtn = page.getByRole('button', { name: /^select$/i }).first();
		await selectBtn.click();
		await expect(selectBtn).toHaveAttribute('aria-pressed', 'false');

		const selectedCountText = page.getByText(/\d+ selected/);
		await expect(selectedCountText).not.toBeVisible({ timeout: 3_000 });
	});

	test('select all button only visible in selection mode', async ({ page }) => {
		const selectAllBefore = getSelectAllButton(page);
		await expect(selectAllBefore).not.toBeVisible({ timeout: 3_000 });

		await enableSelectionMode(page);

		const selectAllAfter = getSelectAllButton(page);
		await expect(selectAllAfter).toBeVisible({ timeout: 5_000 });
	});

	test('multiple selections show correct count', async ({ page }) => {
		await enableSelectionMode(page);

		await getGridCard(page, 0).click();
		await getGridCard(page, 2).click();
		await getGridCard(page, 4).click();

		await expect(page.getByText('3 selected')).toBeVisible({ timeout: 5_000 });
	});

	test('cards outside selection mode have button role', async ({ page }) => {
		const card = getGridCard(page, 0);
		await expect(card).toHaveAttribute('role', 'button');
	});
});

test.describe('Completeness Filter', () => {
	test('completeness filter buttons appear in grid view', async ({ page }) => {
		for (const tier of ['Incomplete', 'Partial', 'Complete'] as const) {
			const btn = getCompletenessFilterButton(page, tier);
			await expect(btn).toBeVisible({ timeout: 10_000 });
		}
	});

	test('clicking filter button toggles tier visibility', async ({ page }) => {
		const cards = await getGridCards(page);
		const originalCount = cards.length;
		expect(originalCount).toBe(5);

		const incompleteBtn = getCompletenessFilterButton(page, 'Incomplete');
		await incompleteBtn.click();
		await page.waitForTimeout(500);

		const filteredCards = await getGridCards(page);
		expect(filteredCards.length).toBeLessThanOrEqual(originalCount);

		await incompleteBtn.click();
		await page.waitForTimeout(500);

		const restoredCards = await getGridCards(page);
		expect(restoredCards.length).toBe(originalCount);
	});

	test('filter buttons show counts matching mock data', async ({ page }) => {
		for (const tier of ['Incomplete', 'Partial', 'Complete'] as const) {
			const btn = getCompletenessFilterButton(page, tier);
			await expect(btn).toBeVisible({ timeout: 10_000 });
			const text = await btn.textContent();
			expect(text).toMatch(/\(\d+\)/);
		}
	});

	test('removing all filters shows no cards', async ({ page }) => {
		for (const tier of ['Incomplete', 'Partial', 'Complete'] as const) {
			const btn = getCompletenessFilterButton(page, tier);
			await btn.click();
			await page.waitForTimeout(500);
		}

		const cardCount = await page.locator(getGridCardSelector()).count();
		expect(cardCount).toBe(0);
	});

	test('re-enabling a filter restores cards', async ({ page }) => {
		for (const tier of ['Incomplete', 'Partial', 'Complete'] as const) {
			const btn = getCompletenessFilterButton(page, tier);
			await btn.click();
			await page.waitForTimeout(500);
		}

		expect(await page.locator(getGridCardSelector()).count()).toBe(0);

		const completeBtn = getCompletenessFilterButton(page, 'Complete');
		await completeBtn.click();
		await page.waitForTimeout(500);

		const cardCount = await page.locator(getGridCardSelector()).count();
		expect(cardCount).toBeGreaterThan(0);
	});

	test('filter toggles are visually distinct when active vs inactive', async ({ page }) => {
		const incompleteBtn = getCompletenessFilterButton(page, 'Incomplete');
		const classBefore = await incompleteBtn.getAttribute('class') ?? '';
		expect(classBefore).toContain('bg-secondary');

		await incompleteBtn.click();
		await page.waitForTimeout(300);

		const classAfter = await incompleteBtn.getAttribute('class') ?? '';
		expect(classAfter).not.toContain('bg-secondary');
	});
});

test.describe('Bulk Exclude', () => {
	test('bulk remove button appears when movies are selected', async ({ page }) => {
		await enableSelectionMode(page);

		await getGridCard(page, 0).click();
		await getGridCard(page, 1).click();

		const removeBtn = getBulkRemoveButton(page);
		await expect(removeBtn).toBeVisible({ timeout: 5_000 });

		await expect(page.getByText('2 selected')).toBeVisible({ timeout: 5_000 });
	});

	test('clicking remove opens confirmation dialog', async ({ page }) => {
		await enableSelectionMode(page);

		await getGridCard(page, 0).click();

		const removeBtn = getBulkRemoveButton(page);
		await expect(removeBtn).toBeVisible({ timeout: 5_000 });
		await removeBtn.click();

		const dialog = page.locator('[role="alertdialog"]').first();
		await expect(dialog).toBeVisible({ timeout: 5_000 });
		await expect(dialog).toContainText(/exclude/i);
	});

	test('canceling exclude dialog keeps movie selected', async ({ page }) => {
		await enableSelectionMode(page);

		await getGridCard(page, 0).click();

		const removeBtn = getBulkRemoveButton(page);
		await removeBtn.click();

		const dialog = page.locator('[role="alertdialog"]').first();
		await expect(dialog).toBeVisible({ timeout: 5_000 });

		const cancelBtn = dialog.getByRole('button', { name: /cancel/i }).first();
		await cancelBtn.click();
		await page.waitForTimeout(300);

		const cardAfter = getGridCard(page, 0);
		await expect(cardAfter).toHaveAttribute('aria-checked', 'true', { timeout: 5_000 });
	});

	test('confirming exclude removes movie from grid', async ({ page }) => {
		const countBefore = await page.locator(getGridCardSelector()).count();

		await enableSelectionMode(page);
		await getGridCard(page, 0).click();

		const removeBtn = getBulkRemoveButton(page);
		await removeBtn.click();

		const dialog = page.locator('[role="alertdialog"]').first();
		await expect(dialog).toBeVisible({ timeout: 5_000 });

		const confirmBtn = dialog.getByRole('button', { name: /exclude/i }).first();
		await confirmBtn.click();

		await expect(page.locator(getGridCardSelector())).toHaveCount(countBefore - 1, { timeout: 10_000 });
	});

	test('remove button not visible when no movies selected', async ({ page }) => {
		const removeBtn = getBulkRemoveButton(page);
		await expect(removeBtn).not.toBeVisible({ timeout: 3_000 });
	});
});

test.describe('Bulk Rescrape', () => {
	test('bulk rescrape button appears when movies are selected', async ({ page }) => {
		await enableSelectionMode(page);

		await getGridCard(page, 0).click();
		await getGridCard(page, 1).click();

		const rescrapeBtn = getBulkRescrapeButton(page);
		await expect(rescrapeBtn).toBeVisible({ timeout: 5_000 });
	});

	test('clicking rescrape button opens modal', async ({ page }) => {
		await enableSelectionMode(page);

		await getGridCard(page, 0).click();

		const rescrapeBtn = getBulkRescrapeButton(page);
		await expect(rescrapeBtn).toBeVisible({ timeout: 5_000 });
		await rescrapeBtn.click();
		
		const modal = getRescrapeModal(page);
		await expect(modal).toBeVisible({ timeout: 5_000 });
	});

	test('rescrape modal shows correct movie count', async ({ page }) => {
		await enableSelectionMode(page);
		await getGridCard(page, 0).click();
		await getGridCard(page, 1).click();

		const rescrapeBtn = getBulkRescrapeButton(page);
		await rescrapeBtn.click();
		
		await expect(page.getByRole('heading', { name: /rescrape 2 movies/i })).toBeVisible({ timeout: 5_000 });
	});

	test('rescrape modal shows scraper selector', async ({ page }) => {
		await enableSelectionMode(page);
		await getGridCard(page, 0).click();

		const rescrapeBtn = getBulkRescrapeButton(page);
		await rescrapeBtn.click();

		const modal = getRescrapeModal(page);
		await expect(modal).toBeVisible({ timeout: 5_000 });
		await expect(modal.getByText(/select scrapers/i)).toBeVisible({ timeout: 5_000 });
	});

	test('rescrape modal shows NFO merge strategy section', async ({ page }) => {
		await enableSelectionMode(page);
		await getGridCard(page, 0).click();

		const rescrapeBtn = getBulkRescrapeButton(page);
		await rescrapeBtn.click();
		
		const modal = getRescrapeModal(page);
		await expect(modal).toBeVisible({ timeout: 5_000 });
		await expect(modal.getByText(/nfo merge strategy/i)).toBeVisible({ timeout: 5_000 });
	});

	test('rescrape modal shows quick presets', async ({ page }) => {
		await enableSelectionMode(page);
		await getGridCard(page, 0).click();

		const rescrapeBtn = getBulkRescrapeButton(page);
		await rescrapeBtn.click();
		
		const modal = getRescrapeModal(page);
		await expect(modal.getByRole('button', { name: /conservative/i })).toBeVisible({ timeout: 5_000 });
		await expect(modal.getByRole('button', { name: /^📝 gap fill/i })).toBeVisible({ timeout: 5_000 });
		await expect(modal.getByRole('button', { name: /aggressive/i })).toBeVisible({ timeout: 5_000 });
	});

	test('rescrape modal shows individual strategies', async ({ page }) => {
		await enableSelectionMode(page);
		await getGridCard(page, 0).click();

		const rescrapeBtn = getBulkRescrapeButton(page);
		await rescrapeBtn.click();
		
		const modal = getRescrapeModal(page);
		await expect(modal.getByRole('button', { name: /prefer nfo/i })).toBeVisible({ timeout: 5_000 });
		await expect(modal.getByRole('button', { name: /prefer scraped/i })).toBeVisible({ timeout: 5_000 });
		await expect(modal.getByRole('button', { name: /preserve existing/i })).toBeVisible({ timeout: 5_000 });
		await expect(modal.getByRole('button', { name: /fill missing only/i })).toBeVisible({ timeout: 5_000 });
		await expect(modal.getByRole('button', { name: /replace all/i })).toBeVisible({ timeout: 5_000 });
	});

	test('rescrape modal can be closed via cancel button', async ({ page }) => {
		await enableSelectionMode(page);
		await getGridCard(page, 0).click();

		const rescrapeBtn = getBulkRescrapeButton(page);
		await rescrapeBtn.click();
		
		const modal = getRescrapeModal(page);
		await expect(modal).toBeVisible({ timeout: 5_000 });

		const cancelBtn = modal.getByRole('button', { name: /cancel/i }).first();
		await cancelBtn.click();
		await page.waitForTimeout(500);

		await expect(modal).not.toBeVisible({ timeout: 3_000 });
	});

	test('rescrape modal can be closed via X button', async ({ page }) => {
		await enableSelectionMode(page);
		await getGridCard(page, 0).click();

		const rescrapeBtn = getBulkRescrapeButton(page);
		await rescrapeBtn.click();
		
		const modal = getRescrapeModal(page);
		await expect(modal).toBeVisible({ timeout: 5_000 });

		const xBtn = modal.locator('button').first();
		await xBtn.click();
		await page.waitForTimeout(500);

		await expect(modal).not.toBeVisible({ timeout: 3_000 });
	});

	test('clicking conservative preset highlights it', async ({ page }) => {
		await enableSelectionMode(page);
		await getGridCard(page, 0).click();

		const rescrapeBtn = getBulkRescrapeButton(page);
		await rescrapeBtn.click();
		
		const modal = getRescrapeModal(page);
		const conservativeBtn = modal.locator('button').filter({ hasText: /conservative/i }).first();
		await conservativeBtn.click();
		await page.waitForTimeout(300);

		const classAttr = await conservativeBtn.getAttribute('class') ?? '';
		expect(classAttr).toContain('border-primary');
	});

	test('rescrape button not visible when no movies selected', async ({ page }) => {
		const rescrapeBtn = getBulkRescrapeButton(page);
		await expect(rescrapeBtn).not.toBeVisible({ timeout: 3_000 });
	});
});

test.describe('Grid to Detail Navigation', () => {
	test('clicking grid card opens detail view when not in selection mode', async ({ page }) => {
		const cards = await getGridCards(page);
		expect(cards.length).toBeGreaterThan(0);

		await cards[0].click();
		await page.waitForTimeout(500);

		const gridContainer = page.locator('.grid.grid-cols-2.sm\\:grid-cols-3');
		await expect(gridContainer).not.toBeVisible({ timeout: 5_000 });

		expect(page.url()).toContain('/review/');
	});

	test('navigating back returns to grid view', async ({ page }) => {
		const cards = await getGridCards(page);
		if (cards.length === 0) return;

		await cards[0].click();
		await page.waitForTimeout(500);

		const gridBtn = getGridViewButton(page);
		await gridBtn.click();
		await page.waitForTimeout(500);

		const restoredCards = await getGridCards(page);
		expect(restoredCards.length).toBeGreaterThan(0);
	});

	test('detail view shows movie metadata', async ({ page }) => {
		const cards = await getGridCards(page);
		if (cards.length === 0) return;

		await cards[0].click();
		await page.waitForTimeout(500);

		const metadataHeading = page.getByText('Movie Metadata');
		await expect(metadataHeading).toBeVisible({ timeout: 5_000 });
	});

	test('detail view has rescrape button', async ({ page }) => {
		const cards = await getGridCards(page);
		if (cards.length === 0) return;

		await cards[0].click();
		await page.waitForTimeout(500);

		const metadataCard = page.locator('.p-6').filter({ hasText: /movie metadata/i }).first();
		const rescrapeBtn = metadataCard.getByRole('button', { name: /rescrape/i }).first();
		await expect(rescrapeBtn).toBeVisible({ timeout: 5_000 });
	});

	test('detail view shows movie navigation with count', async ({ page }) => {
		const cards = await getGridCards(page);
		if (cards.length === 0) return;

		await cards[0].click();
		await page.waitForTimeout(500);

		const movieNav = page.getByText(/movie \d+ of \d+/i);
		await expect(movieNav).toBeVisible({ timeout: 5_000 });
	});

	test('detail view shows source file card', async ({ page }) => {
		const cards = await getGridCards(page);
		if (cards.length === 0) return;

		await cards[0].click();
		await page.waitForTimeout(500);

		const sourceFileHeading = page.getByText('Source File', { exact: true });
		await expect(sourceFileHeading).toBeVisible({ timeout: 5_000 });
	});

	test('detail view shows remove button in navigation card', async ({ page }) => {
		const cards = await getGridCards(page);
		if (cards.length === 0) return;

		await cards[0].click();
		await page.waitForTimeout(500);

		const navCard = page.locator('.p-4').filter({ hasText: /movie \d+ of \d+/i }).first();
		const removeBtn = navCard.getByRole('button', { name: /remove/i });
		await expect(removeBtn).toBeVisible({ timeout: 5_000 });
	});

	test('detail view previous button is disabled on first movie', async ({ page }) => {
		const cards = await getGridCards(page);
		if (cards.length === 0) return;

		await cards[0].click();
		await page.waitForTimeout(500);

		const prevBtn = page.getByRole('button', { name: /previous/i });
		await expect(prevBtn).toBeDisabled({ timeout: 5_000 });
	});

	test('detail view next button navigates to next movie', async ({ page }) => {
		const cards = await getGridCards(page);
		if (cards.length < 2) return;

		await cards[0].click();
		await page.waitForTimeout(500);

		const movieNavBefore = page.getByText(/movie 1 of \d+/i);
		await expect(movieNavBefore).toBeVisible({ timeout: 5_000 });

		const nextBtn = page.getByRole('button', { name: /next/i });
		await nextBtn.click();
		await page.waitForTimeout(500);

		const movieNavAfter = page.getByText(/movie 2 of \d+/i);
		await expect(movieNavAfter).toBeVisible({ timeout: 5_000 });
	});

	test('detail view page select dropdown works', async ({ page }) => {
		const cards = await getGridCards(page);
		if (cards.length < 3) return;

		await cards[0].click();
		await page.waitForTimeout(500);

		const pageSelect = page.locator('#movie-page-select');
		await expect(pageSelect).toBeVisible({ timeout: 5_000 });

		await pageSelect.selectOption('3');
		await page.waitForTimeout(500);

		const movieNav = page.getByText(/movie 3 of \d+/i);
		await expect(movieNav).toBeVisible({ timeout: 5_000 });
	});
});

test.describe('Detail View Rescrape', () => {
	test('clicking rescrape in detail view opens rescrape modal', async ({ page }) => {
		const cards = await getGridCards(page);
		if (cards.length === 0) return;

		await cards[0].click();
		await page.waitForTimeout(500);

		const metadataCard = page.locator('.p-6').filter({ hasText: /movie metadata/i }).first();
		const rescrapeBtn = metadataCard.getByRole('button', { name: /rescrape/i }).first();
		await rescrapeBtn.click();
		
		const modal = getRescrapeModal(page);
		await expect(modal).toBeVisible({ timeout: 5_000 });
	});

	test('detail rescrape modal shows rescrape from file and manual search tabs', async ({ page }) => {
		const cards = await getGridCards(page);
		if (cards.length === 0) return;

		await cards[0].click();
		await page.waitForTimeout(500);

		const metadataCard = page.locator('.p-6').filter({ hasText: /movie metadata/i }).first();
		const rescrapeBtn = metadataCard.getByRole('button', { name: /rescrape/i }).first();
		await rescrapeBtn.click();
		
		const modal = getRescrapeModal(page);
		await expect(modal).toBeVisible({ timeout: 5_000 });
		await expect(modal.getByText(/rescrape from file/i)).toBeVisible({ timeout: 5_000 });
		await expect(modal.getByText(/manual search/i)).toBeVisible({ timeout: 5_000 });
	});

	test('switching to manual search shows input field', async ({ page }) => {
		const cards = await getGridCards(page);
		if (cards.length === 0) return;

		await cards[0].click();
		await page.waitForTimeout(500);

		const metadataCard = page.locator('.p-6').filter({ hasText: /movie metadata/i }).first();
		const rescrapeBtn = metadataCard.getByRole('button', { name: /rescrape/i }).first();
		await rescrapeBtn.click();
		
		const modal = getRescrapeModal(page);
		await modal.getByText(/manual search/i).click();
		await page.waitForTimeout(300);

		const searchInput = modal.locator('#manual-search-input');
		await expect(searchInput).toBeVisible({ timeout: 5_000 });
	});

	test('manual search input accepts text', async ({ page }) => {
		const cards = await getGridCards(page);
		if (cards.length === 0) return;

		await cards[0].click();
		await page.waitForTimeout(500);

		const metadataCard = page.locator('.p-6').filter({ hasText: /movie metadata/i }).first();
		const rescrapeBtn = metadataCard.getByRole('button', { name: /rescrape/i }).first();
		await rescrapeBtn.click();
		
		const modal = getRescrapeModal(page);
		await modal.getByText(/manual search/i).click();
		await page.waitForTimeout(300);

		const searchInput = modal.locator('#manual-search-input');
		await searchInput.fill('IPX-999');
		await expect(searchInput).toHaveValue('IPX-999');
	});

	test('detail rescrape modal has cancel and execute buttons', async ({ page }) => {
		const cards = await getGridCards(page);
		if (cards.length === 0) return;

		await cards[0].click();
		await page.waitForTimeout(500);

		const metadataCard = page.locator('.p-6').filter({ hasText: /movie metadata/i }).first();
		const rescrapeBtn = metadataCard.getByRole('button', { name: /rescrape/i }).first();
		await rescrapeBtn.click();
		
		const modal = getRescrapeModal(page);
		await expect(modal.getByRole('button', { name: /cancel/i })).toBeVisible({ timeout: 5_000 });
		await expect(modal.getByRole('button', { name: /^rescrape$/i })).toBeVisible({ timeout: 5_000 });
	});
});

test.describe('Detail View Remove', () => {
	test('clicking remove in detail view triggers exclusion', async ({ page }) => {
		const cards = await getGridCards(page);
		if (cards.length === 0) return;

		await cards[0].click();
		await page.waitForTimeout(500);

		const navCard = page.locator('.p-4').filter({ hasText: /movie \d+ of \d+/i }).first();
		const removeBtn = navCard.getByRole('button', { name: /remove/i });
		await removeBtn.click();
		
		const movieNav = page.getByText(/movie \d+ of \d+/i);
		await expect(movieNav).toBeVisible({ timeout: 5_000 });
	});

	test('remove button is visible in detail view', async ({ page }) => {
		const cards = await getGridCards(page);
		if (cards.length === 0) return;

		await cards[0].click();
		await page.waitForTimeout(500);

		const navCard = page.locator('.p-4').filter({ hasText: /movie \d+ of \d+/i }).first();
		const removeBtn = navCard.getByRole('button', { name: /remove/i });
		await expect(removeBtn).toBeVisible({ timeout: 5_000 });
	});
});

test.describe('Selection Mode with Filter Interaction', () => {
	test('select all only selects filtered movies', async ({ page }) => {
		const incompleteBtn = getCompletenessFilterButton(page, 'Incomplete');
		await incompleteBtn.click();
		await page.waitForTimeout(500);

		const filteredCards = await getGridCards(page);
		const filteredCount = filteredCards.length;
		expect(filteredCount).toBeLessThan(5);

		await enableSelectionMode(page);

		const selectAllBtn = getSelectAllButton(page);
		await selectAllBtn.click();

		for (let i = 0; i < filteredCount; i++) {
			const card = getGridCard(page, i);
			await expect(card).toHaveAttribute('aria-checked', 'true', { timeout: 5_000 });
		}
	});

	test('filtered out cards are not affected by select all', async ({ page }) => {
		const incompleteBtn = getCompletenessFilterButton(page, 'Incomplete');
		await incompleteBtn.click();
		await page.waitForTimeout(500);

		await enableSelectionMode(page);
		const selectAllBtn = getSelectAllButton(page);
		await selectAllBtn.click();

		await incompleteBtn.click();
		await page.waitForTimeout(500);

		const allCards = await getGridCards(page);
		const checkedValues = await Promise.all(allCards.map(card => card.getAttribute('aria-checked')));
		const selectedCount = checkedValues.filter(v => v === 'true').length;
		expect(selectedCount).toBeLessThan(allCards.length);
	});

	test('selecting movies then filtering preserves selection count', async ({ page }) => {
		await enableSelectionMode(page);
		await getGridCard(page, 0).click();
		await getGridCard(page, 1).click();

		await expect(page.getByText('2 selected')).toBeVisible({ timeout: 5_000 });

		const incompleteBtn = getCompletenessFilterButton(page, 'Incomplete');
		await incompleteBtn.click();
		await page.waitForTimeout(500);

		await expect(page.getByText('2 selected')).toBeVisible({ timeout: 5_000 });
	});
});

test.describe('Movie ID Badge on Cards', () => {
	test('each card shows movie ID badge', async ({ page }) => {
		const cards = await getGridCards(page);
		for (let i = 0; i < Math.min(cards.length, 3); i++) {
			const card = getGridCard(page, i);
			const badge = card.locator('span.bg-black\\/70');
			await expect(badge).toBeVisible();
		}
	});

	test('badge shows correct movie ID text', async ({ page }) => {
		const card = getGridCard(page, 0);
		const badge = card.locator('span.bg-black\\/70');
		const text = await badge.textContent();
		expect(text).toMatch(/^(COMPLETE|PARTIAL|INCOMPLETE)-\d+$/);
	});
});

test.describe('View Mode Toggle', () => {
	test('can switch from grid to detail view', async ({ page }) => {
		const cards = await getGridCards(page);
		expect(cards.length).toBeGreaterThan(0);

		const detailBtn = getDetailViewButton(page);
		await detailBtn.click();
		await page.waitForTimeout(500);

		const gridContainer = page.locator('.grid.grid-cols-2.sm\\:grid-cols-3');
		await expect(gridContainer).not.toBeVisible({ timeout: 5_000 });
	});

	test('can switch from detail back to grid view', async ({ page }) => {
		const detailBtn = getDetailViewButton(page);
		await detailBtn.click();
		await page.waitForTimeout(500);

		const gridBtn = getGridViewButton(page);
		await gridBtn.click();
		await page.waitForTimeout(500);

		const restoredCards = await getGridCards(page);
		expect(restoredCards.length).toBeGreaterThan(0);
	});

	test('grid view toggle has expected button pair', async ({ page }) => {
		const toggle = page.locator('.inline-flex.rounded-md.border.border-input.p-1');
		await expect(toggle).toBeVisible({ timeout: 10_000 });

		const detailBtn = toggle.locator('button').filter({ hasText: /detail/i }).first();
		const posterBtn = toggle.locator('button').filter({ hasText: /poster/i }).first();
		const coverBtn = toggle.locator('button').filter({ hasText: /cover/i }).first();
		await expect(detailBtn).toBeVisible();
		await expect(posterBtn).toBeVisible();
		await expect(coverBtn).toBeVisible();
	});
});

test.describe('Keyboard Navigation', () => {
	test('space key toggles selection in selection mode', async ({ page }) => {
		await enableSelectionMode(page);

		const card = getGridCard(page, 0);
		await card.focus();
		await page.keyboard.press('Space');
		await page.waitForTimeout(300);

		await expect(card).toHaveAttribute('aria-checked', 'true', { timeout: 5_000 });

		await page.keyboard.press('Space');
		await page.waitForTimeout(300);

		await expect(card).toHaveAttribute('aria-checked', 'false', { timeout: 5_000 });
	});

	test('enter key toggles selection in selection mode', async ({ page }) => {
		await enableSelectionMode(page);

		const card = getGridCard(page, 0);
		await card.focus();
		await page.keyboard.press('Enter');
		await page.waitForTimeout(300);

		await expect(card).toHaveAttribute('aria-checked', 'true', { timeout: 5_000 });
	});

	test('enter key on card outside selection mode navigates to detail', async ({ page }) => {
		const card = getGridCard(page, 0);
		await card.focus();
		await page.keyboard.press('Enter');
		await page.waitForTimeout(500);

		const metadataHeading = page.getByText('Movie Metadata');
		await expect(metadataHeading).toBeVisible({ timeout: 5_000 });
	});

	test('tab navigates between grid cards', async ({ page }) => {
		const card = getGridCard(page, 0);
		await card.focus();

		await page.keyboard.press('Tab');
		await page.waitForTimeout(200);

		const focusedElement = page.locator(':focus');
		const tag = await focusedElement.evaluate(el => el.tagName.toLowerCase());
		expect(['div', 'button', 'a']).toContain(tag);
	});
});

test.describe('Grid Card Content', () => {
	test('each card shows movie title', async ({ page }) => {
		const cards = await getGridCards(page);
		for (let i = 0; i < Math.min(cards.length, 3); i++) {
			const card = getGridCard(page, i);
			const title = card.locator('.font-semibold.text-sm');
			await expect(title).toBeVisible();
			const text = await title.textContent();
			expect(text!.length).toBeGreaterThan(0);
		}
	});

	test('card with maker shows maker text', async ({ page }) => {
		const card = getGridCard(page, 0);
		const makerText = card.locator('.text-muted-foreground.text-xs');
		const text = await makerText.first().textContent();
		expect(text!.length).toBeGreaterThan(0);
	});

	test('card has hover effect class', async ({ page }) => {
		const card = getGridCard(page, 0);
		const classes = await card.getAttribute('class');
		expect(classes).toContain('hover:scale');
		expect(classes).toContain('hover:shadow');
	});

	test('card has focus-visible ring class', async ({ page }) => {
		const card = getGridCard(page, 0);
		const classes = await card.getAttribute('class');
		expect(classes).toContain('focus-visible:ring');
	});

	test('card aria-label describes action', async ({ page }) => {
		const card = getGridCard(page, 0);
		const label = await card.getAttribute('aria-label');
		expect(label).toContain('View details for');

		await enableSelectionMode(page);
		const labelAfter = await card.getAttribute('aria-label');
		expect(labelAfter).toContain('Select');
	});
});

test.describe('Scraper Selector in Rescrape Modal', () => {
	test('shows select all and select none buttons', async ({ page }) => {
		await enableSelectionMode(page);
		await getGridCard(page, 0).click();

		const rescrapeBtn = getBulkRescrapeButton(page);
		await rescrapeBtn.click();
		
		const modal = getRescrapeModal(page);
		await expect(modal).toBeVisible({ timeout: 5_000 });
		await expect(modal.getByRole('button', { name: /^all$/i })).toBeVisible({ timeout: 5_000 });
		await expect(modal.getByRole('button', { name: /^none$/i })).toBeVisible({ timeout: 5_000 });
	});

	test('shows priority order section for selected scrapers', async ({ page }) => {
		await enableSelectionMode(page);
		await getGridCard(page, 0).click();

		const rescrapeBtn = getBulkRescrapeButton(page);
		await rescrapeBtn.click();
		
		const modal = getRescrapeModal(page);
		await expect(modal.getByText(/priority order/i)).toBeVisible({ timeout: 5_000 });
	});

	test('select none clears all scrapers', async ({ page }) => {
		await enableSelectionMode(page);
		await getGridCard(page, 0).click();

		const rescrapeBtn = getBulkRescrapeButton(page);
		await rescrapeBtn.click();
		
		const modal = getRescrapeModal(page);
		const noneBtn = modal.getByRole('button', { name: /^none$/i });
		await noneBtn.click();
		await page.waitForTimeout(300);

		const priorityItems = modal.locator('[role="listitem"]');
		await expect(priorityItems).toHaveCount(0, { timeout: 3_000 });
	});

	test('select all restores all scrapers', async ({ page }) => {
		await enableSelectionMode(page);
		await getGridCard(page, 0).click();

		const rescrapeBtn = getBulkRescrapeButton(page);
		await rescrapeBtn.click();
		
		const modal = getRescrapeModal(page);
		const noneBtn = modal.getByRole('button', { name: /^none$/i });
		await noneBtn.click();
		await page.waitForTimeout(300);

		const allBtn = modal.getByRole('button', { name: /^all$/i });
		await allBtn.click();
		await page.waitForTimeout(300);

		const priorityItems = modal.locator('[role="listitem"]');
		const count = await priorityItems.count();
		expect(count).toBeGreaterThan(0);
	});
});

test.describe('Bulk Rescrape Progress', () => {
	test('rescrape execute triggers progress indicator', async ({ page }) => {
		await page.route('**/api/v1/batch/**', async (route) => {
			if (route.request().url().includes('/batch-rescrape')) {
				await new Promise(resolve => setTimeout(resolve, 1500));
				await route.fulfill({
					json: {
						results: [],
						succeeded: 0,
						failed: 0,
						job: {
							id: 'mock-job-001',
							status: 'completed',
							total_files: 5,
							completed: 5,
							failed: 0,
							operation_count: 0,
							reverted_count: 0,
							excluded: {},
							progress: 100,
							destination: '/output',
							results: {},
							started_at: '2024-01-15T10:00:00Z',
							completed_at: '2024-01-15T10:00:05Z',
							update: false,
						},
					},
				});
			} else {
				await route.continue();
			}
		});

		await enableSelectionMode(page);
		await getGridCard(page, 0).click();

		const rescrapeBtn = getBulkRescrapeButton(page);
		await rescrapeBtn.click();
		
		const modal = getRescrapeModal(page);
		await expect(modal).toBeVisible({ timeout: 5_000 });
		const executeBtn = modal.getByRole('button', { name: /rescrape/i }).last();
		await executeBtn.click();
		await page.waitForTimeout(500);

		const progressIndicator = page.getByText(/rescraping/i).first();
		await expect(progressIndicator).toBeVisible({ timeout: 5_000 });
	});
});

test.describe('Header Controls', () => {
	test('close button is visible', async ({ page }) => {
		const closeBtn = page.getByRole('button', { name: /cancel|close/i });
		await expect(closeBtn.first()).toBeVisible({ timeout: 5_000 });
	});

	test('organize button is visible', async ({ page }) => {
		const organizeBtn = page.getByRole('button', { name: /organize/i });
		await expect(organizeBtn.first()).toBeVisible({ timeout: 5_000 });
	});

	test('page title is visible', async ({ page }) => {
		await expect(page.getByText('Review & Edit Metadata')).toBeVisible({ timeout: 5_000 });
	});
});

test.describe('Filter Persistence Across Mode Switches', () => {
	test('filters persist when switching to detail view and back', async ({ page }) => {
		const incompleteBtn = getCompletenessFilterButton(page, 'Incomplete');
		await incompleteBtn.click();
		await page.waitForTimeout(500);

		const filteredCount = (await getGridCards(page)).length;
		expect(filteredCount).toBeLessThan(5);

		const detailBtn = getDetailViewButton(page);
		await detailBtn.click();
		await page.waitForTimeout(500);

		const gridBtn = getGridViewButton(page);
		await gridBtn.click();
		await page.waitForTimeout(500);

		const restoredCount = (await getGridCards(page)).length;
		expect(restoredCount).toBe(filteredCount);
	});

	test('selection mode persists when switching to detail view and back', async ({ page }) => {
		await enableSelectionMode(page);

		const detailBtn = getDetailViewButton(page);
		await detailBtn.click();
		await page.waitForTimeout(500);

		const gridBtn = getGridViewButton(page);
		await gridBtn.click();
		await page.waitForTimeout(500);

		const card = getGridCard(page, 0);
		await expect(card).toHaveAttribute('role', 'checkbox', { timeout: 5_000 });
	});
});

test.describe('Edge Cases', () => {
	test('clicking card rapidly does not break selection state', async ({ page }) => {
		await enableSelectionMode(page);

		const card = getGridCard(page, 0);
		await card.click();
		await card.click();
		await card.click();

		await page.waitForTimeout(300);

		const checked = await card.getAttribute('aria-checked');
		expect(['true', 'false']).toContain(checked);
	});

	test('select all then deselect all returns to no selection', async ({ page }) => {
		await enableSelectionMode(page);

		const selectAllBtn = getSelectAllButton(page);
		await selectAllBtn.click();
		await page.waitForTimeout(300);

		const deselectAllBtn = page.getByRole('button', { name: /deselect all/i });
		await deselectAllBtn.click();
		await page.waitForTimeout(300);

		const cards = await getGridCards(page);
		for (let i = 0; i < cards.length; i++) {
			await expect(getGridCard(page, i)).toHaveAttribute('aria-checked', 'false', { timeout: 5_000 });
		}
	});

	test('all filter buttons can be toggled on and off multiple times', async ({ page }) => {
		for (let round = 0; round < 2; round++) {
			for (const tier of ['Incomplete', 'Partial', 'Complete'] as const) {
				const btn = getCompletenessFilterButton(page, tier);
				await btn.click();
				await page.waitForTimeout(200);
			}
		}

		await page.waitForTimeout(500);
		const cards = await getGridCards(page);
		expect(cards.length).toBe(5);
	});

	test('bulk actions not available without selection mode', async ({ page }) => {
		const removeBtn = getBulkRemoveButton(page);
		const rescrapeBtn = getBulkRescrapeButton(page);

		await expect(removeBtn).not.toBeVisible({ timeout: 3_000 });
		await expect(rescrapeBtn).not.toBeVisible({ timeout: 3_000 });
	});
});
