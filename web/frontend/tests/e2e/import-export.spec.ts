import { test, expect } from '@playwright/test';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { readFileSync } from 'node:fs';

const __dirname = dirname(fileURLToPath(import.meta.url));
const fixturesDir = join(__dirname, '..', 'fixtures');

test.use({ storageState: { cookies: [], origins: [] } });

async function ensureAuthenticated(request: any, page: any) {
	const username = process.env.E2E_AUTH_USERNAME || 'admin';
	const password = process.env.E2E_AUTH_PASSWORD || 'adminpassword123';

	const statusResp = await request.get('/api/v1/auth/status');
	const status = await statusResp.json();

	if (!status.initialized) {
		await request.post('/api/v1/auth/setup', {
			data: { username, password }
		});
	} else if (!status.authenticated) {
		await request.post('/api/v1/auth/login', {
			data: { username, password, remember_me: true }
		});
	}

	if (page) {
		await page.goto('/');
		await page.waitForLoadState('domcontentloaded');
		await page.waitForTimeout(500);

		if ((await page.url()).includes('/actresses') ||
			(await page.url()).includes('/words') ||
			(await page.url()).includes('/genres')) {
			return;
		}

		const loginForm = page.locator('form');
		const setupUsername = page.locator('input[name="username"]');
		const loginUsername = page.locator('#login-username');
		const hasSetup = (await page.locator('input[type="password"]').count()) >= 2;

		if (hasSetup) {
			await setupUsername.fill(username);
			await page.locator('input[type="password"]').first().fill(password);
			await page.locator('input[type="password"]').nth(1).fill(password);
			await Promise.all([
				page.waitForResponse(res => res.url().includes('/auth/setup')),
				page.locator('button').filter({ hasText: /create/i }).click()
			]);
		} else if (await loginUsername.isVisible().catch(() => false)) {
			await loginUsername.fill(username);
			await page.locator('#login-password').fill(password);
			await Promise.all([
				page.waitForResponse(res => res.url().includes('/auth/login')),
				page.locator('button').filter({ hasText: /sign|login/i }).click()
			]);
		}
	}
}

function fxl(path: string): string {
	return join(fixturesDir, path);
}

function readFixture(path: string): unknown {
	return JSON.parse(readFileSync(fxl(path), 'utf-8'));
}

test.describe('Genre Replacement Import/Export (API)', () => {
	const original1 = 'E2E-Action-Genre';
	const original2 = 'E2E-Drama-Genre';

	test.afterEach(async ({ request }) => {
		try { await request.delete(`/api/v1/genres/replacements?original=${encodeURIComponent(original1)}`); } catch { 0; }
		try { await request.delete(`/api/v1/genres/replacements?original=${encodeURIComponent(original2)}`); } catch { 0; }
	});

	test('import-export roundtrip preserves data integrity', async ({ request }) => {
		await ensureAuthenticated(request, null);

		const genresData = readFixture('genres-import.json') as Array<{ original: string; replacement: string }>;

		const importResp = await request.post('/api/v1/genres/replacements/import', {
			data: { replacements: genresData }
		});
		expect(importResp.ok()).toBeTruthy();
		const importBody = await importResp.json();
		expect(importBody.imported).toBe(2);

		const listResp = await request.get('/api/v1/genres/replacements');
		expect(listResp.ok()).toBeTruthy();
		const listView = await listResp.text();
		expect(listView).toContain(original1);

		const exportResp = await request.get('/api/v1/genres/replacements/export');
		expect(exportResp.ok()).toBeTruthy();
		const exported = await exportResp.json();
		expect(Array.isArray(exported)).toBeTruthy();
		const originals = (exported as Array<{ original: string }>).map(r => r.original);
		expect(originals).toContain(original1);
		expect(originals).toContain(original2);
	});
});

test.describe('Actress Import/Export via UI', () => {
	test.beforeEach(async ({ page, request }) => {
		await ensureAuthenticated(request, page);
	});

	test('import via UI file upload creates actress', async ({ page }) => {
		await page.goto('/actresses');
		await page.waitForLoadState('domcontentloaded');
		await page.waitForTimeout(1000);

		page.on('dialog', dialog => dialog.accept());

		const importBtn = page.getByRole('button', { name: 'Import' }).first();
		if (await importBtn.isVisible().catch(() => false)) {
			await importBtn.click();
		}

		await page.setInputFiles('input[type="file"][accept=".json"]', fixturePath('actresses-import.json'));

		await expect(async () => {
			const text = await page.textContent('body');
			expect(text).toContain('Import complete');
		}).toPass({ timeout: 15000 });

		await page.waitForTimeout(1000);

		await expect(async () => {
			const text = await page.textContent('body');
			expect(text).toContain('E2E');
		}).toPass({ timeout: 10000 });

		try {
			const searchInput = page.locator('input[placeholder*="Search"], input[type="search"]').first();
			if (await searchInput.isVisible().catch(() => false)) {
				await searchInput.fill('E2E');
			} else {
				await page.keyboard.press('Control+f');
				await page.locator('input[aria-label="Search"], input[placeholder*="earch"]').fill('E2E');
			}

			await page.waitForTimeout(500);

			const deleteBtns = page.locator('button[title="Delete"], button:has-text("Delete")');
			const count = await deleteBtns.count();
			if (count > 0) {
				await deleteBtns.first().click();
			}
		} catch { 0; }
	});

	test('export triggers download', async ({ page }) => {
		await page.goto('/actresses');
		await page.waitForLoadState('domcontentloaded');

		page.on('dialog', dialog => dialog.accept());

		const importBtn = page.getByRole('button', { name: 'Import' }).first();
		if (await importBtn.isVisible().catch(() => false)) {
			await importBtn.click();
		}

		await page.setInputFiles('input[type="file"][accept=".json"]', fixturePath('actresses-import.json'));

		await expect(async () => {
			const text = await page.textContent('body');
			expect(text).toContain('Import complete');
		}).toPass({ timeout: 15000 });

		await page.waitForTimeout(500);

		const exportBtn = page.getByRole('button', { name: 'Export' });
		if (await exportBtn.isVisible().catch(() => false)) {
			const [download] = await Promise.all([
				page.waitForEvent('download', { timeout: 15000 }),
				exportBtn.click(),
			]);
			const fileName = download.suggestedFilename();
			expect(fileName).toMatch(/actress/i);

			const bufferStream = await download.createReadStream();
			let data = '';
			for await (const chunk of bufferStream) {
				data += chunk.toString();
			}
			expect(data).toContain('E2E');
		}

		try {
			const searchInput = page.locator('input[placeholder*="Search"]').first();
			if (await searchInput.isVisible().catch(() => false)) {
				await searchInput.fill('E2E');
				await page.waitForTimeout(500);
				const deleteBtns = page.locator('button[title="Delete"]').first();
				if (await deleteBtns.isVisible().catch(() => false)) {
					await deleteBtns.click();
				}
			}
		} catch { 0; }
	});
});

test.describe('Word Replacement Import/Export via UI', () => {
	test.beforeEach(async ({ page, request }) => {
		await ensureAuthenticated(request, page);
	});

	test('import via UI file upload creates word replacements', async ({ page, request }) => {
		await page.goto('/words');
		await page.waitForLoadState('domcontentloaded');
		await page.waitForTimeout(1000);

		let dialogCount = 0;
		page.on('dialog', dialog => {
			dialogCount++;
			dialog.accept();
		});

		const importBtn = page.getByRole('button', { name: 'Import' });
		if (await importBtn.isVisible().catch(() => false)) {
			await importBtn.click();
		}

		await page.setInputFiles('input[type="file"][accept=".json"]', fixturePath('words-import.json'));

		await expect(async () => {
			const text = await page.textContent('body');
			expect(text).toContain('Import complete');
		}).toPass({ timeout: 15000 });

		await page.waitForTimeout(500);

		await expect(async () => {
			const text = await page.textContent('body');
			expect(text).toContain('E2E-blur-word');
		}).toPass({ timeout: 10000 });

		const wordOriginals = ['E2E-blur-word', 'E2E-XXX-word'];
		for (const original of wordOriginals) {
			try {
				await request.delete(`/api/v1/words/replacements?original=${encodeURIComponent(original)}`);
			} catch { 0; }
		}
	});

	test('export triggers download', async ({ page, request }) => {
		await page.goto('/words');
		await page.waitForLoadState('domcontentloaded');
		await page.waitForTimeout(1000);

		let dialogCount = 0;
		page.on('dialog', dialog => {
			dialogCount++;
			dialog.accept();
		});

		const importBtn = page.getByRole('button', { name: 'Import' });
		if (await importBtn.isVisible().catch(() => false)) {
			await importBtn.click();
		}

		await page.setInputFiles('input[type="file"][accept=".json"]', fixturePath('words-import.json'));

		await expect(async () => {
			const text = await page.textContent('body');
			expect(text).toContain('Import complete');
		}).toPass({ timeout: 15000 });

		await page.waitForTimeout(500);

		const exportBtn = page.getByRole('button', { name: 'Export' });
		if (await exportBtn.isVisible().catch(() => false)) {
			const [download] = await Promise.all([
				page.waitForEvent('download', { timeout: 15000 }),
				exportBtn.click(),
			]);
			const fileName = download.suggestedFilename();
			expect(fileName).toMatch(/word-replacement/i);
		}

		const wordOriginals = ['E2E-blur-word', 'E2E-XXX-word'];
		for (const original of wordOriginals) {
			try {
				await request.delete(`/api/v1/words/replacements?original=${encodeURIComponent(original)}`);
			} catch { 0; }
		}
	});
});

test.describe('Invalid JSON Import', () => {
	test.beforeEach(async ({ page, request }) => {
		await ensureAuthenticated(request, page);
	});

	test('uploading invalid JSON shows error toast', async ({ page }) => {
		await page.goto('/words');
		await page.waitForLoadState('domcontentloaded');
		await page.waitForTimeout(1000);

		const importBtn = page.getByRole('button', { name: 'Import' });
		if (await importBtn.isVisible().catch(() => false)) {
			await importBtn.click();
		}

		await page.setInputFiles('input[type="file"][accept=".json"]', fixturePath('invalid-import.json'));

		await expect(async () => {
			const text = await page.textContent('body');
			expect(text).toContain('Invalid JSON file');
		}).toPass({ timeout: 10000 });
	});
});

function fixturePath(name: string): string[] {
	return [fxl(name)];
}
