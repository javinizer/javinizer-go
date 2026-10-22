/** Offline browser contract: filters compose with search/sort and reset pagination. */
import { test, expect } from '@playwright/test';

test('catalog filter/search/sort controls reset pages and retain sort on reload', async ({
	page,
}) => {
	const unexpected: string[] = [];
	const errors: string[] = [];
	const requests: URLSearchParams[] = [];
	page.on('pageerror', (error) => errors.push(error.message));
	await page.addInitScript(() => localStorage.setItem('javinizer-locale', 'en'));
	await page.routeWebSocket('**/ws/progress', (socket) => socket.close());
	const responses: Record<string, unknown> = {
		'/api/v1/auth/status': { initialized: true, authenticated: true, username: 'admin' },
		'/api/v1/config': { metadata: {}, output: {} },
		'/api/v1/scrapers': { scrapers: [] },
		'/api/v1/version': { current: 'v1.6.4', update_available: false, source: 'disabled' },
		'/api/v1/jobs': { jobs: [], total: 0 },
		'/api/v1/actresses/candidates': { candidates: [], total: 0 },
	};
	await page.route('**/*', async (route) => {
		const url = new URL(route.request().url());
		if (url.origin !== new URL(test.info().project.use.baseURL!).origin) {
			unexpected.push(url.href);
			await route.abort();
		} else if (url.pathname === '/api/v1/actresses') {
			requests.push(url.searchParams);
			const offset = Number(url.searchParams.get('offset') ?? 0);
			const actresses = Array.from({ length: 20 }, (_, i) => ({
				id: offset + i + 1,
				first_name: `Catalog ${offset + i + 1}`,
				dmm_id: 1000 + offset + i,
				japanese_name: '名前',
				verified: true,
			}));
			await route.fulfill({ json: { actresses, count: 20, total: 60, limit: 20, offset } });
		} else if (url.pathname.startsWith('/api/')) {
			if (Object.hasOwn(responses, url.pathname))
				await route.fulfill({ json: responses[url.pathname] });
			else {
				unexpected.push(`${route.request().method()} ${url.pathname}`);
				await route.fulfill({ status: 404, json: { error: 'Unmocked API request' } });
			}
		} else await route.continue();
	});
	const last = () => requests.at(-1)!;
	await page.goto('/actresses');
	await expect(page.getByRole('heading', { name: 'Actress Database', exact: true })).toBeVisible();
	await expect.poll(() => requests.length).toBeGreaterThan(0);
	const filter = page.getByRole('combobox', { name: 'Filter actresses' });
	const sort = page.getByRole('combobox', { name: 'Sort actresses by' });
	const next = page.getByRole('button', { name: 'Next', exact: true });
	for (const value of [
		'missing_dmm',
		'has_dmm',
		'missing_thumbnail',
		'missing_japanese_name',
		'japanese_name_only',
		'missing_metadata',
		'',
	]) {
		await next.click();
		await expect.poll(() => last().get('offset')).toBe('20');
		await filter.selectOption(value);
		await expect.poll(() => last().get('filter') ?? '').toBe(value);
		await expect.poll(() => last().get('offset') ?? '0').toBe('0');
		await expect(page.getByText('Page 1 of 3', { exact: true })).toBeVisible();
	}
	await filter.selectOption('has_dmm');
	await next.click();
	await expect.poll(() => last().get('offset')).toBe('20');
	await page.getByRole('textbox', { name: 'Search actresses', exact: true }).fill('  1001  ');
	await page.getByRole('textbox', { name: 'Search actresses', exact: true }).press('Enter');
	await expect.poll(() => last().get('q')).toBe('1001');
	await expect.poll(() => last().get('offset') ?? '0').toBe('0');
	expect(last().get('filter')).toBe('has_dmm');
	await next.click();
	await expect.poll(() => last().get('offset')).toBe('20');
	await sort.selectOption('dmm_id');
	await expect.poll(() => last().get('sort_by')).toBe('dmm_id');
	await expect.poll(() => last().get('offset') ?? '0').toBe('0');
	await next.click();
	await expect.poll(() => last().get('offset')).toBe('20');
	await page.getByTitle('Toggle sort direction').click();
	await expect.poll(() => last().get('sort_order')).toBe('desc');
	await expect.poll(() => last().get('offset') ?? '0').toBe('0');
	await page.getByRole('button', { name: 'Clear', exact: true }).first().click();
	await expect.poll(() => last().get('q')).toBeNull();
	expect(last().get('filter')).toBe('has_dmm');
	await page.reload();
	await expect(sort).toHaveValue('dmm_id');
	await expect.poll(() => last().get('sort_order')).toBe('desc');
	await expect(filter).toHaveValue('');
	expect(unexpected).toEqual([]);
	expect(errors).toEqual([]);
});
