/**
 * Static-flavor regression: hard loads against the embedded-assets binary must
 * render route content (never the SvelteKit default "404 — The requested
 * resource does not exist" error page) and must not request any SvelteKit
 * server-data endpoint (__data.json).
 */
import { test, expect, type Page } from '@playwright/test';

async function login(page: Page) {
	const response = await page.request.post('/api/v1/auth/login', {
		data: { username: 'admin', password: 'adminpassword123' },
	});
	expect(response.ok(), 'login against the e2e backend must succeed').toBeTruthy();
}

function trackServerDataRequests(page: Page): string[] {
	const urls: string[] = [];
	page.on('request', (request) => {
		if (request.url().includes('__data.json')) urls.push(request.url());
	});
	return urls;
}

async function expectHardLoadRenders(page: Page, dataRequests: string[], path: string, heading: string, postMountSignal: string) {
	const postMount = page.waitForResponse((candidate) => candidate.url().includes(postMountSignal));
	const response = await page.goto(path);
	expect(response, 'hard load must return a response').not.toBeNull();
	expect(response!.ok(), 'hard load must return 200 HTML').toBeTruthy();
	await expect(page.getByRole('heading', { name: heading })).toBeVisible();
	await postMount;
	await expect(page.getByText('The requested resource does not exist')).toHaveCount(0);
	expect(dataRequests, `hard load of ${path} issued SvelteKit server-data requests: ${dataRequests.join(', ')}`).toEqual([]);
}

test('hard load of / renders the dashboard without __data.json requests', async ({ page }) => {
	await login(page);
	const dataRequests = trackServerDataRequests(page);
	await expectHardLoadRenders(page, dataRequests, '/', 'Javinizer Control Center', '/api/v1/history/stats');
});

test('hard load of /actresses renders the route without __data.json requests', async ({ page }) => {
	await login(page);
	const dataRequests = trackServerDataRequests(page);
	await expectHardLoadRenders(page, dataRequests, '/actresses', 'Actress Database', '/api/v1/config');
});
