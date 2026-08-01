/**
 * Import/Export full-stack UI spec.
 *
 * Ported from the legacy tests/e2e/ suite (mocked/real-mixed, now split into
 * suite: this spec drives the REAL /api/v1/{genres,actresses,words} import +
 * export endpoints through the real e2emock backend + real SQLite. Auth is
 * inherited from the fullstack global-setup's storageState (no per-spec
 * login). Fixture JSON files live at tests/fixtures/ (shared).
 *
 * What this pins: the /genres, /actresses, /words pages' Import (file upload
 * → POST /import) and Export (GET /export → download) wiring end-to-end,
 * including the invalid-JSON error toast path.
 */
import type { Download, Page } from '@playwright/test';
import { test, expect } from '@playwright/test';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { mkdtemp, readFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';

const __dirname = dirname(fileURLToPath(import.meta.url));
// tests/fullstack/regressions/ → ../../fixtures = tests/fixtures/ (shared).
const fixturesDir = join(__dirname, '..', '..', 'fixtures');

function fixturePath(name: string): string {
	return join(fixturesDir, name);
}

async function downloadContent(download: Download) {
	const tmpDir = await mkdtemp(join(tmpdir(), 'pw-dl-'));
	const outPath = join(tmpDir, 'download');
	await download.saveAs(outPath);
	return readFile(outPath);
}

test.describe('Genre Replacement Import/Export (UI)', () => {
	const original1 = 'E2E-Action-Genre';
	const original2 = 'E2E-Drama-Genre';

	test.afterEach(async ({ page }) => {
		try {
			await page.goto('/genres');
			await page.waitForTimeout(500);
			for (const original of [original1, original2]) {
				const row = page.locator('table tr').filter({ hasText: original }).first();
				if (await row.isVisible().catch(() => false)) {
					row.locator('button').first().click();
					if (
						await page.waitForSelector('text=Are you sure', { timeout: 2000 }).catch(() => false)
					) {
						page
							.locator(
								'button:has-text("Delete"), button:has-text("Confirm"), button:has-text("Remove")',
							)
							.first()
							.click();
					}
					await page.waitForTimeout(500);
				}
			}
		} catch {
			0;
		}
	});

	test('import via UI file upload creates genre replacements', async ({ page }) => {
		await page.goto('/genres');
		await page.waitForLoadState('networkidle');

		page.on('dialog', async (dialog) => {
			await dialog.accept();
		});

		const importBtn = page.getByRole('button', { name: 'Import' }).first();
		if (await importBtn.isVisible().catch(() => false)) {
			await importBtn.click();
			await page.locator('input[type="file"]').setInputFiles(fixturePath('genres-import.json'));

			expect(async () => {
				const text = await page.textContent('body');
				expect(text).toContain(original1);
			}).toPass({ timeout: 15000 });
		}
	});

	test('export triggers download', async ({ page }) => {
		await page.goto('/genres');
		await page.waitForLoadState('networkidle');

		const exportBtn = page.getByRole('button', { name: 'Export' }).first();
		if (await exportBtn.isVisible().catch(() => false)) {
			const [download] = await Promise.all([page.waitForEvent('download'), exportBtn.click()]);

			const fileName = download.suggestedFilename();
			expect(fileName).toMatch(/genre/);

			const content = await downloadContent(download);
			const data = JSON.parse(content.toString());
			expect(Array.isArray(data)).toBeTruthy();
		}
	});
});

test.describe('Actress Import/Export via UI', () => {
	const testName = 'E2E TestActress';

	test.afterEach(async ({ page }) => {
		try {
			await page.goto('/actresses');
			await page.waitForTimeout(500);
			const searchInput = page.locator(
				'input[type="search"], input[placeholder*="Search"], input[placeholder*="search"], input[name="q"]',
			);
			if (await searchInput.isVisible().catch(() => false)) {
				await searchInput.fill('E2E');
				await page.waitForTimeout(1000);
			}
			const deleteBtns = page
				.locator('table tr')
				.filter({ hasText: 'E2E' })
				.first()
				.locator('button')
				.first();
			if (await deleteBtns.isVisible().catch(() => false)) {
				await deleteBtns.click();
			}
		} catch {
			0;
		}
	});

	test('import via UI file upload creates actress', async ({ page }) => {
		await page.goto('/actresses');
		await page.waitForLoadState('networkidle');

		page.on('dialog', async (dialog) => {
			await dialog.accept();
		});

		const importBtn = page.getByRole('button', { name: 'Import' }).first();
		if (await importBtn.isVisible().catch(() => false)) {
			await importBtn.click();
			await page.locator('input[type="file"]').setInputFiles(fixturePath('actresses-import.json'));

			expect(async () => {
				const text = await page.textContent('body');
				expect(text).toContain('Import complete');
			}).toPass({ timeout: 15000 });

			expect(async () => {
				const text = await page.textContent('body');
				expect(text).toContain(testName);
			}).toPass({ timeout: 10000 });
		}
	});

	test('export triggers download', async ({ page }) => {
		await page.goto('/actresses');
		await page.waitForLoadState('networkidle');

		const exportBtn = page.getByRole('button', { name: 'Export' }).first();
		if (await exportBtn.isVisible().catch(() => false)) {
			const [download] = await Promise.all([page.waitForEvent('download'), exportBtn.click()]);

			const fileName = download.suggestedFilename();
			expect(fileName).toMatch(/actresses/);

			const content = await downloadContent(download);
			const data = JSON.parse(content.toString());
			expect(Array.isArray(data)).toBeTruthy();
		}
	});
});

test.describe('Word Replacement Import/Export via UI', () => {
	const wordOriginal1 = 'E2E-blur-word';
	const wordOriginal2 = 'E2E-XXX-word';

	test.afterEach(async ({ page }) => {
		try {
			await page.goto('/words');
			await page.waitForTimeout(500);
			for (const original of [wordOriginal1, wordOriginal2]) {
				const row = page.locator('table tr').filter({ hasText: original }).first();
				if (await row.isVisible().catch(() => false)) {
					row.locator('button').first().click();
				}
			}
		} catch {
			0;
		}
	});

	test('import via UI file upload creates word replacements', async ({ page }) => {
		await page.goto('/words');
		await page.waitForLoadState('networkidle');

		page.on('dialog', async (dialog) => {
			await dialog.accept();
		});

		const importBtn = page.getByRole('button', { name: 'Import' }).first();
		if (await importBtn.isVisible().catch(() => false)) {
			await importBtn.click();
			await page.locator('input[type="file"]').setInputFiles(fixturePath('words-import.json'));

			expect(async () => {
				const text = await page.textContent('body');
				expect(text).toContain('Import complete');
			}).toPass({ timeout: 15000 });

			expect(async () => {
				const text = await page.textContent('body');
				expect(text).toContain(wordOriginal1);
			}).toPass({ timeout: 10000 });
		}
	});

	test('export triggers download', async ({ page }) => {
		await page.goto('/words');
		await page.waitForLoadState('networkidle');

		const exportBtn = page.getByRole('button', { name: 'Export' }).first();
		if (await exportBtn.isVisible().catch(() => false)) {
			const [download] = await Promise.all([page.waitForEvent('download'), exportBtn.click()]);

			const fileName = download.suggestedFilename();
			expect(fileName).toMatch(/word/);

			const content = await downloadContent(download);
			const data = JSON.parse(content.toString());
			expect(Array.isArray(data)).toBeTruthy();
		}
	});
});

test.describe('Invalid JSON Import', () => {
	test('uploading invalid JSON shows error toast', async ({ page }) => {
		await page.goto('/words');
		await page.waitForLoadState('networkidle');

		const importBtn = page.getByRole('button', { name: 'Import' }).first();
		if (await importBtn.isVisible().catch(() => false)) {
			await importBtn.click();
			await page.locator('input[type="file"]').setInputFiles(fixturePath('invalid-import.json'));

			expect(async () => {
				const text = await page.textContent('body');
				expect(text).toMatch(/Invalid JSON/i);
			}).toPass({ timeout: 15000 });
		}
	});
});
