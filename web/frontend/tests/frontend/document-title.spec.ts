import { test, expect, type Page } from '@playwright/test';

async function mockApplication(page: Page, locale = 'en', initiallyAuthenticated = true) {
	let authenticated = initiallyAuthenticated;
	await page.addInitScript((language) => {
		if (!localStorage.getItem('javinizer-locale')) {
			localStorage.setItem('javinizer-locale', language);
			localStorage.setItem('javinizer-locale-choice', language);
		}
	}, locale);
	await page.routeWebSocket('**/ws/**', (socket) => socket.close());
	await page.route('**/api/v1/**', async (route) => {
		const path = new URL(route.request().url()).pathname;
		if (path === '/api/v1/auth/logout') {
			authenticated = false;
			return route.fulfill({ json: {} });
		}
		if (path === '/api/v1/auth/status') {
			return route.fulfill({ json: { initialized: true, authenticated, username: 'admin' } });
		}
		if (path === '/api/v1/config') {
			return route.fulfill({ json: {
				ui: { language: locale },
				output: { operation_mode: 'organize', folder_format: '', file_format: '<ID>', subfolder_format: [] },
				api: { security: { allowed_directories: ['/videos', '/output'] } },
				metadata: { completeness: { enabled: false } }
			} });
		}
		if (path === '/api/v1/cwd') return route.fulfill({ json: { path: '/videos' } });
		if (path === '/api/v1/scrapers') return route.fulfill({ json: { scrapers: [{ name: 'mock', display_title: 'Mock', enabled: true }] } });
		if (path === '/api/v1/browse') return route.fulfill({ json: { current_path: '/videos', parent_path: '/', items: [{ name: 'IPX-123.mp4', path: '/videos/IPX-123.mp4', is_dir: false, size: 10, mod_time: '2026-01-01T00:00:00Z' }] } });
		if (path.startsWith('/api/v1/version')) return route.fulfill({ json: { current: 'test', update_available: false, source: 'disabled' } });
		if (path === '/api/v1/jobs') return route.fulfill({ json: { jobs: [] } });
		return route.fulfill({ json: {} });
	});
	return { setLocale: (language: string) => { locale = language; } };
}

test('restores the fallback through client navigation, history, and logout', async ({ page }) => {
	await mockApplication(page);
	await page.goto('/browse');
	await expect(page).toHaveTitle('Javinizer');
	await page.getByRole('button', { name: /IPX-123\.mp4/ }).click();
	await page.getByRole('button', { name: /^Options$/ }).click();
	await page.getByRole('checkbox', { name: /Provide IDs or URLs manually/ }).check();
	await page.getByRole('button', { name: /Continue to manual review/i }).click();
	await expect(page).toHaveURL(/\/manual$/);
	await expect(page).toHaveTitle('Manual Scrape');

	await page.locator('nav a[href="/browse"]').click();
	await expect(page).toHaveURL(/\/browse$/);
	await expect(page).toHaveTitle('Javinizer');
	for (let iteration = 0; iteration < 2; iteration += 1) {
		await page.goBack();
		await expect(page).toHaveURL(/\/manual$/);
		await expect(page).toHaveTitle('Manual Scrape');
		await page.goForward();
		await expect(page).toHaveURL(/\/browse$/);
		await expect(page).toHaveTitle('Javinizer');
	}

	await page.goBack();
	await expect(page).toHaveTitle('Manual Scrape');
	await page.getByRole('button', { name: 'admin · Logout' }).click();
	await expect(page.getByRole('button', { name: 'Sign In', exact: true })).toBeVisible();
	await expect(page).toHaveURL(/\/manual$/);
	await expect(page).toHaveTitle('Javinizer');
});

test('preserves the localized manual title on direct load and reload', async ({ page }) => {
	const application = await mockApplication(page, 'ja');
	// Manual redirects to Browse without the pending batch it normally inherits.
	await page.addInitScript(() => {
		sessionStorage.setItem('javinizer_pending_scrape', JSON.stringify({
			version: 2,
			files: ['/videos/IPX-123.mp4'],
			applyPlan: null,
			showScraperSelector: false,
			selectedScrapers: [],
			force: false
		}));
	});
	await page.goto('/manual');
	await expect(page).toHaveURL(/\/manual$/);
	await expect(page.getByRole('heading', { name: '手動スクレープ', exact: true })).toBeVisible();
	await expect(page).toHaveTitle('手動スクレープ');
	await page.reload();
	await expect(page).toHaveURL(/\/manual$/);
	await expect(page.getByRole('heading', { name: '手動スクレープ', exact: true })).toBeVisible();
	await expect(page).toHaveTitle('手動スクレープ');
	// Locale selection persists the preference and reloads the application.
	application.setLocale('en');
	await page.evaluate(() => {
		localStorage.setItem('javinizer-locale', 'en');
		localStorage.setItem('javinizer-locale-choice', 'en');
	});
	await page.reload();
	await expect(page).toHaveURL(/\/manual$/);
	await expect(page.getByRole('heading', { name: 'Manual Scrape', exact: true })).toBeVisible();
	await expect(page).toHaveTitle('Manual Scrape');
	await page.locator('nav a[href="/browse"]').click();
	await expect(page).toHaveTitle('Javinizer');
});

test('uses the fallback on direct root, browse, and manual login screens', async ({ page }) => {
	await mockApplication(page, 'en', false);
	for (const path of ['/', '/browse', '/manual']) {
		await page.goto(path);
		await expect(page.getByRole('button', { name: 'Sign In', exact: true })).toBeVisible();
		await expect(page).toHaveTitle('Javinizer');
	}
});
