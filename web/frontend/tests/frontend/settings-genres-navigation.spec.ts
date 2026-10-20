/**
 * Settings keeps genre switches inline and opens the dedicated genre manager.
 * All API responses are mocked, so this navigation smoke test needs no backend.
 */
import { test, expect } from '@playwright/test';

test('settings opens the genre manager and supports browser history and reload', async ({
	page,
}) => {
	const unexpectedRequests: string[] = [];
	const pageErrors: string[] = [];
	page.on('pageerror', (error) => pageErrors.push(error.message));
	await page.addInitScript(() => localStorage.setItem('javinizer-locale', 'en'));
	await page.routeWebSocket('**/ws/progress', (socket) => socket.close());

	const responses: Record<string, unknown> = {
		'/api/v1/auth/status': { initialized: true, authenticated: true, username: 'admin' },
		'/api/v1/config': {
			metadata: { genre_replacement: { enabled: true, auto_add: false } },
		},
		'/api/v1/scrapers': { scrapers: [] },
		'/api/v1/version': { current: 'v1.6.4', update_available: false, source: 'disabled' },
		'/api/v1/jobs': { jobs: [], total: 0 },
		'/api/v1/tokens': { tokens: [] },
		'/api/v1/r18dev/dump/status': { present: false, running: false, enabled: false, path: '' },
		'/api/v1/genres/replacements': {
			replacements: [
				{
					id: 1,
					original: 'Sample original',
					replacement: 'Sample replacement',
					created_at: '2026-10-20T00:00:00Z',
					updated_at: '2026-10-20T00:00:00Z',
				},
			],
			count: 1,
			limit: 500,
			offset: 0,
			total: 1,
		},
		'/api/v1/genres/ignored': { ignored_genres: ['Sample ignored'], count: 1 },
		'/api/v1/genres/favorites': { favorites: ['Sample favorite'], count: 1 },
	};
	await page.route('**/*', async (route) => {
		const request = route.request();
		const url = new URL(request.url());
		if (url.origin !== new URL(test.info().project.use.baseURL!).origin) {
			unexpectedRequests.push(request.url());
			await route.abort();
		} else if (url.pathname.startsWith('/api/')) {
			if (request.method() === 'GET' && Object.hasOwn(responses, url.pathname)) {
				await route.fulfill({ json: responses[url.pathname] });
			} else {
				unexpectedRequests.push(`${request.method()} ${url.pathname}`);
				await route.fulfill({ status: 404, json: { error: 'Unmocked API request' } });
			}
		} else {
			await route.continue();
		}
	});

	await page.goto('/settings');
	await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible();
	await page.getByRole('button', { name: /^Genre Replacements/ }).click();
	await expect(page.getByRole('checkbox', { name: 'Enable genre replacement' })).toBeChecked();
	await expect(page.getByRole('checkbox', { name: 'Auto-add genres' })).not.toBeChecked();
	await page.getByRole('link', { name: 'Manage Genres' }).click();

	await expect(page).toHaveURL(/\/genres$/);
	await expect(page.getByRole('heading', { name: 'Genres', exact: true })).toBeVisible();
	await expect(page.getByText('Sample original', { exact: true })).toBeVisible();
	await expect(page.getByText('Sample replacement', { exact: true })).toBeVisible();
	await expect(page.getByText('Sample ignored', { exact: true })).toBeVisible();
	await expect(page.getByText('Sample favorite', { exact: true })).toBeVisible();

	await page.goBack();
	await expect(page).toHaveURL(/\/settings$/);
	await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible();
	await page.goForward();
	await expect(page).toHaveURL(/\/genres$/);
	await expect(page.getByText('Sample replacement', { exact: true })).toBeVisible();
	await page.reload();
	await expect(page.getByRole('heading', { name: 'Genres', exact: true })).toBeVisible();
	await expect(page.getByText('Sample replacement', { exact: true })).toBeVisible();
	await page.getByPlaceholder('Search by original or replacement...').fill('not present');
	await expect(page.getByText('Sample original', { exact: true })).toHaveCount(0);
	await page.getByPlaceholder('Search by original or replacement...').clear();
	await expect(page.getByText('Sample original', { exact: true })).toBeVisible();

	expect(unexpectedRequests).toEqual([]);
	expect(pageErrors).toEqual([]);
});
