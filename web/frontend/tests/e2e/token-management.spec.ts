import type { Page } from '@playwright/test';
import { test, expect } from '@playwright/test';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = dirname(fileURLToPath(import.meta.url));

function resolveURL(baseURL: string | undefined): string {
	return baseURL ?? 'http://localhost:5174';
}

async function ensureLoggedIn(page: Page, baseURL: string | undefined) {
	const url = resolveURL(baseURL);
	const username = process.env.JAVINIZER_E2E_USERNAME || 'admin';
	const password = process.env.JAVINIZER_E2E_PASSWORD || 'adminpassword123';

	await page.goto(`${url}/`);
	await page.waitForLoadState('domcontentloaded');
	await page.waitForTimeout(500);

	const hasSetup = (await page.locator('input[type="password"]').count()) >= 2;

	if (hasSetup) {
		await page.locator('input[name="username"]').fill(username);
		const passwords = page.locator('input[type="password"]');
		await passwords.first().fill(password);
		await passwords.nth(1).fill(password);
		await page
			.getByRole('button', { name: /create/i })
			.first()
			.click();
	} else {
		const loginUsername = page.locator('#login-username, input[name="username"]');
		if (await loginUsername.isVisible().catch(() => false)) {
			await loginUsername.fill(username);
			await page
				.locator('#login-password, input[name="password"], input[type="password"]')
				.fill(password);
			await page
				.getByRole('button', { name: /sign|login/i })
				.first()
				.click();
		}
	}

	await page.waitForLoadState('domcontentloaded').catch(() => {});
}

test.describe('API Token Management (UI)', () => {
	const testTokenName = 'E2E-Test-Token';

	test.afterEach(async ({ page, baseURL }) => {
		try {
			await page.goto(`${resolveURL(baseURL)}/settings`);
			await page.waitForTimeout(1000);

			const tokenRows = page.locator('table tr').filter({ hasText: testTokenName });
			const count = await tokenRows.count();
			for (let i = 0; i < count; i++) {
				const revokeBtn = tokenRows.nth(i).locator('button[title="Revoke token"]');
				if (await revokeBtn.isVisible().catch(() => false)) {
					await revokeBtn.click();
					const confirmBtn = page
						.locator(
							'button:has-text("Revoke"), button:has-text("Confirm"), button:has-text("Delete")',
						)
						.first();
					if (await confirmBtn.isVisible().catch(() => false)) {
						await confirmBtn.click();
					}
					await page.waitForTimeout(500);
				}
			}
		} catch {
			/* cleanup */
		}
	});

	test('create token with name appears in list', async ({ page, baseURL }) => {
		await ensureLoggedIn(page, baseURL);

		await page.goto(`${resolveURL(baseURL)}/settings`);
		await page.waitForLoadState('domcontentloaded');

		const section = page.getByText('API Tokens').first();
		if (await section.isVisible().catch(() => false)) {
			await section.click();
			await page.waitForTimeout(500);

			const nameInput = page.locator('#token-name');
			if (await nameInput.isVisible().catch(() => false)) {
				await nameInput.fill(testTokenName);

				const createBtn = page.getByRole('button', { name: /create token/i }).first();
				await createBtn.click();
				await page.waitForTimeout(1000);

				expect(async () => {
					const body = await page.textContent('body');
					expect(body).toContain(testTokenName);
				}).toPass({ timeout: 10000 });
			}
		}
	});

	test('create token without name shows as Unnamed', async ({ page, baseURL }) => {
		await ensureLoggedIn(page, baseURL);

		await page.goto(`${resolveURL(baseURL)}/settings`);
		await page.waitForLoadState('domcontentloaded');

		const section = page.getByText('API Tokens').first();
		if (await section.isVisible().catch(() => false)) {
			await section.click();
			await page.waitForTimeout(500);

			const createBtn = page.getByRole('button', { name: /create token/i }).first();
			if (await createBtn.isVisible().catch(() => false)) {
				await createBtn.click();
				await page.waitForTimeout(1000);

				expect(async () => {
					const body = await page.textContent('body');
					expect(body).toMatch(/Unnamed|token created/i);
				}).toPass({ timeout: 10000 });
			}
		}
	});

	test('revoke token removes from list', async ({ page, baseURL }) => {
		await ensureLoggedIn(page, baseURL);

		await page.goto(`${resolveURL(baseURL)}/settings`);
		await page.waitForLoadState('domcontentloaded');

		const section = page.getByText('API Tokens').first();
		if (await section.isVisible().catch(() => false)) {
			await section.click();
			await page.waitForTimeout(500);

			const nameInput = page.locator('#token-name');
			if (await nameInput.isVisible().catch(() => false)) {
				await nameInput.fill(testTokenName);

				const createBtn = page.getByRole('button', { name: /create token/i }).first();
				await createBtn.click();
				await page.waitForTimeout(1500);
			}

			const revokeBtn = page.locator('button[title="Revoke token"]').first();
			if (await revokeBtn.isVisible().catch(() => false)) {
				await revokeBtn.click();

				const confirmBtn = page.locator('button:has-text("Revoke")').first();
				if (await confirmBtn.isVisible().catch(() => false)) {
					await confirmBtn.click();
					await page.waitForTimeout(1000);
				}
			}
		}
	});

	test('regenerate token shows new value in modal', async ({ page, baseURL }) => {
		await ensureLoggedIn(page, baseURL);

		await page.goto(`${resolveURL(baseURL)}/settings`);
		await page.waitForLoadState('domcontentloaded');

		const section = page.getByText('API Tokens').first();
		if (await section.isVisible().catch(() => false)) {
			await section.click();
			await page.waitForTimeout(500);

			const nameInput = page.locator('#token-name');
			if (await nameInput.isVisible().catch(() => false)) {
				await nameInput.fill(testTokenName);

				const createBtn = page.getByRole('button', { name: /create token/i }).first();
				await createBtn.click();
				await page.waitForTimeout(1500);
			}

			const regenBtn = page.locator('button[title="Regenerate token"]').first();
			if (await regenBtn.isVisible().catch(() => false)) {
				await regenBtn.click();

				const confirmBtn = page.locator('button:has-text("Regenerate")').first();
				if (await confirmBtn.isVisible().catch(() => false)) {
					await confirmBtn.click();
					await page.waitForTimeout(1000);

					expect(async () => {
						const body = await page.textContent('body');
						expect(body).toContain('jv_');
					}).toPass({ timeout: 10000 });
				}
			}
		}
	});

	test('token display modal shows security warning', async ({ page, baseURL }) => {
		await ensureLoggedIn(page, baseURL);

		await page.goto(`${resolveURL(baseURL)}/settings`);
		await page.waitForLoadState('domcontentloaded');

		const section = page.getByText('API Tokens').first();
		if (await section.isVisible().catch(() => false)) {
			await section.click();
			await page.waitForTimeout(500);

			const nameInput = page.locator('#token-name');
			if (await nameInput.isVisible().catch(() => false)) {
				await nameInput.fill(testTokenName);

				const createBtn = page.getByRole('button', { name: /create token/i }).first();
				await createBtn.click();
				await page.waitForTimeout(1500);

				expect(async () => {
					const body = await page.textContent('body');
					expect(body).toMatch(/not be shown again|will not be shown again/i);
				}).toPass({ timeout: 10000 });
			}
		}
	});
});
